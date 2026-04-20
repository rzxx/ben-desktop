package desktopcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	jobKindRepairLibrary = "repair-library"
)

type scanExecutionMode string

const (
	scanModeStartup scanExecutionMode = "startup"
	scanModeDelta   scanExecutionMode = "delta"
	scanModeRepair  scanExecutionMode = "repair"
)

type deltaScanScope struct {
	audioPaths    []string
	presenceRoots []string
	artworkRoots  []string
}

func recordScanError(stats *apitypes.ScanStats, err error) {
	if stats == nil || err == nil {
		return
	}
	stats.Errors++
	if strings.TrimSpace(stats.FirstError) == "" {
		stats.FirstError = strings.TrimSpace(err.Error())
	}
}

var supportedAudioExt = map[string]struct{}{
	".aac":  {},
	".flac": {},
	".m4a":  {},
	".mp3":  {},
	".ogg":  {},
	".opus": {},
	".wav":  {},
}

func (s *IngestService) RepairLibrary(ctx context.Context) (apitypes.ScanStats, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.ScanStats{}, err
	}
	if !canProvideLocalMedia(local.Role) {
		return apitypes.ScanStats{}, fmt.Errorf("local ingest requires owner, admin, or member role")
	}
	roots, err := s.app.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return apitypes.ScanStats{}, err
	}
	return s.runTrackedRepair(ctx, local.LibraryID, local.DeviceID, roots, "repair canceled")
}

func (s *IngestService) StartRepairLibrary(ctx context.Context) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}
	if !canProvideLocalMedia(local.Role) {
		return JobSnapshot{}, fmt.Errorf("local ingest requires owner, admin, or member role")
	}
	roots, err := s.app.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return JobSnapshot{}, err
	}
	return s.startTrackedRepair(ctx, local.LibraryID, local.DeviceID, roots, "queued library repair")
}

func (s *IngestService) repairRoots(ctx context.Context, roots []string) (apitypes.ScanStats, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.ScanStats{}, err
	}
	if !canProvideLocalMedia(local.Role) {
		return apitypes.ScanStats{}, fmt.Errorf("local ingest requires owner, admin, or member role")
	}
	normalized, err := normalizeScanRoots(roots)
	if err != nil {
		return apitypes.ScanStats{}, err
	}
	if len(normalized) == 0 {
		return apitypes.ScanStats{}, nil
	}
	return s.runTrackedRepair(ctx, local.LibraryID, local.DeviceID, normalized, "repair canceled")
}

func (s *IngestService) runTrackedRepair(ctx context.Context, libraryID, deviceID string, roots []string, callerCanceledMessage string) (apitypes.ScanStats, error) {
	normalized := normalizedWatcherRoots(roots)
	coord, err := s.app.activeScanCoordinator(ctx, libraryID, deviceID)
	if err != nil {
		return apitypes.ScanStats{}, err
	}

	jobID := scanJobID(libraryID, deviceID, normalized, jobKindRepairLibrary)
	job := s.app.jobs.Track(jobID, jobKindRepairLibrary, libraryID)
	if job != nil {
		job.Queued(0, "queued library repair")
	}
	return coord.submitRepair(ctx, normalized, job, callerCanceledMessage, "repair canceled because the library is no longer active")
}

func (s *IngestService) startTrackedRepair(ctx context.Context, libraryID, deviceID string, roots []string, queuedMessage string) (JobSnapshot, error) {
	normalized := normalizedWatcherRoots(roots)
	jobID := scanJobID(libraryID, deviceID, normalized, jobKindRepairLibrary)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindRepairLibrary,
		libraryID,
		queuedMessage,
		"repair canceled because the library is no longer active",
		func(runCtx context.Context) {
			if _, err := s.runTrackedRepair(runCtx, libraryID, deviceID, normalized, "repair canceled because the library is no longer active"); err != nil {
				s.app.failActiveLibraryJobIfActive(jobID, jobKindRepairLibrary, libraryID, "repair canceled because the library is no longer active", err)
			}
		},
	)
}

func (s *IngestService) runFullScanPass(ctx context.Context, mode scanExecutionMode, libraryID, deviceID string, roots []string, job *JobTracker) (stats apitypes.ScanStats, err error) {
	workers := 1
	currentRoot := ""
	currentPath := ""
	rootsDone := 0
	totalTracks := 0
	tracksDone := 0
	beforeImpact := newScanImpactSet()
	defer func() {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		s.app.setScanActivity(apitypes.ScanActivityStatus{
			Phase:       "failed",
			RootsTotal:  len(roots),
			RootsDone:   rootsDone,
			TracksTotal: totalTracks,
			TracksDone:  tracksDone,
			CurrentRoot: currentRoot,
			CurrentPath: currentPath,
			Workers:     workers,
			Errors:      maxInt(stats.Errors, 1),
		})
	}()
	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:      "enumerating",
		RootsTotal: len(roots),
		Workers:    workers,
	})
	if mode != scanModeRepair {
		beforeImpact, err = s.captureScanImpact(ctx, libraryID, deviceID, roots, nil)
		if err != nil {
			return stats, err
		}
	}

	pathsByRoot := make(map[string][]string, len(roots))
	for _, root := range roots {
		if err = ctx.Err(); err != nil {
			return stats, err
		}
		currentRoot = root
		currentPath = ""
		s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
			status.Phase = "enumerating"
			status.CurrentRoot = root
			status.CurrentPath = ""
		})
		if job != nil {
			job.Running(0.05, "enumerating "+root)
		}
		var paths []string
		paths, err = enumerateAudioPaths(ctx, root)
		if err != nil {
			return stats, err
		}
		pathsByRoot[root] = paths
		totalTracks += len(paths)
	}

	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:       "ingesting",
		RootsTotal:  len(roots),
		TracksTotal: totalTracks,
		Workers:     workers,
	})

	catalogMutated := false
	for _, root := range roots {
		if err = ctx.Err(); err != nil {
			return stats, err
		}
		currentRoot = root
		paths := pathsByRoot[root]
		sort.Strings(paths)
		for _, path := range paths {
			if err = ctx.Err(); err != nil {
				return stats, err
			}
			currentPath = path
			s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
				status.Phase = "ingesting"
				status.RootsDone = rootsDone
				status.CurrentRoot = root
				status.CurrentPath = path
				status.WorkersActive = 1
				status.TracksDone = tracksDone
				status.TracksTotal = totalTracks
				status.Workers = workers
			})
			if job != nil {
				progress := scanProgress(rootsDone, len(roots), tracksDone, totalTracks)
				job.Running(progress, "ingesting "+filepath.Base(path))
			}

			imported, skipped, err := s.ingestPath(ctx, libraryID, deviceID, root, path)
			stats.Scanned++
			if imported {
				stats.Imported++
				catalogMutated = true
			}
			if skipped {
				stats.SkippedUnchanged++
			}
			if err != nil {
				recordScanError(&stats, err)
			}
			tracksDone++
			s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
				status.TracksDone = tracksDone
				status.CurrentRoot = root
				status.CurrentPath = path
				status.WorkersActive = 0
				status.Errors = stats.Errors
			})
			if err != nil {
				if mode == scanModeRepair {
					continue
				}
				return stats, err
			}
		}

		rowsAffected, err := s.reconcileRootPresence(ctx, libraryID, deviceID, root, paths)
		if err != nil {
			return stats, err
		}
		if rowsAffected > 0 {
			catalogMutated = true
		}

		rootsDone++
		s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
			status.RootsDone = rootsDone
			status.CurrentRoot = root
			status.CurrentPath = ""
			status.WorkersActive = 0
		})
	}

	local := apitypes.LocalContext{LibraryID: libraryID, DeviceID: deviceID}
	if mode == scanModeRepair {
		if err = s.app.rebuildCatalogMaterializationFull(ctx, libraryID, &local); err != nil {
			return stats, err
		}
		if err = s.app.clearScanRepairRequired(ctx, libraryID, deviceID); err != nil {
			return stats, err
		}
	} else if catalogMutated {
		afterImpact, impactErr := s.captureScanImpact(ctx, libraryID, deviceID, roots, nil)
		if impactErr != nil {
			s.recordAutoRepairRequired(ctx, libraryID, deviceID, scanRepairReasonScopedImpactCorrupt, impactErr)
		} else if err = s.applyAutomaticScopedCatalogUpdate(ctx, libraryID, deviceID, &local, beforeImpact.merge(afterImpact)); err != nil {
			return stats, err
		}
	} else if err = s.reconcileScanArtworkScope(ctx, libraryID, deviceID, roots, nil); err != nil {
		return stats, err
	}

	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:       "completed",
		RootsTotal:  len(roots),
		RootsDone:   len(roots),
		TracksTotal: totalTracks,
		TracksDone:  tracksDone,
		Workers:     workers,
		Errors:      stats.Errors,
	})
	return stats, nil
}

func (s *IngestService) runDeltaScanPass(ctx context.Context, libraryID, deviceID string, scope deltaScanScope, job *JobTracker) (stats apitypes.ScanStats, err error) {
	scope.audioPaths = normalizeScanPaths(scope.audioPaths)
	scope.presenceRoots = normalizedWatcherRoots(scope.presenceRoots)
	scope.artworkRoots = normalizedWatcherRoots(scope.artworkRoots)
	workers := 1
	tracksTotal := len(scope.audioPaths)
	currentRoot := ""
	currentPath := ""
	rootsDone := 0
	defer func() {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		s.app.setScanActivity(apitypes.ScanActivityStatus{
			Phase:       "failed",
			RootsTotal:  len(scope.presenceRoots),
			RootsDone:   rootsDone,
			TracksTotal: tracksTotal,
			TracksDone:  stats.Scanned,
			CurrentRoot: currentRoot,
			CurrentPath: currentPath,
			Workers:     workers,
			Errors:      maxInt(stats.Errors, 1),
		})
	}()
	beforeImpact, err := s.captureScanImpact(ctx, libraryID, deviceID, scope.presenceRoots, scope.audioPaths)
	if err != nil {
		return stats, err
	}

	deviceRoots, err := s.app.scanRootsForDevice(ctx, libraryID, deviceID)
	if err != nil {
		return stats, err
	}
	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:       "ingesting",
		RootsTotal:  len(scope.presenceRoots),
		TracksTotal: tracksTotal,
		Workers:     workers,
	})

	catalogMutated := false
	for index, path := range scope.audioPaths {
		if err = ctx.Err(); err != nil {
			return stats, err
		}
		if !isAudioPath(path) {
			continue
		}
		currentPath = path
		currentRoot = ""
		s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
			status.Phase = "ingesting"
			status.CurrentPath = path
			status.TracksDone = index
			status.TracksTotal = tracksTotal
			status.Workers = workers
			status.WorkersActive = 1
			status.RootsTotal = len(scope.presenceRoots)
		})
		if job != nil {
			job.Running(scanProgress(0, maxInt(len(scope.presenceRoots), 1), index, maxInt(tracksTotal, 1)), "reconciling "+filepath.Base(path))
		}
		info, statErr := os.Stat(path)
		switch {
		case statErr == nil && info.IsDir():
			continue
		case statErr == nil:
			root := bestScanRootForPath(path, deviceRoots)
			currentRoot = root
			imported, skipped, ingestErr := s.ingestPath(ctx, libraryID, deviceID, root, path)
			stats.Scanned++
			if imported {
				stats.Imported++
				catalogMutated = true
			}
			if skipped {
				stats.SkippedUnchanged++
			}
			if ingestErr != nil {
				recordScanError(&stats, ingestErr)
				return stats, ingestErr
			}
		case errors.Is(statErr, os.ErrNotExist):
			stats.Scanned++
			rowsAffected, reconcileErr := s.reconcileMissingPaths(ctx, libraryID, deviceID, []string{path})
			if reconcileErr != nil {
				recordScanError(&stats, reconcileErr)
				return stats, reconcileErr
			}
			if rowsAffected > 0 {
				catalogMutated = true
			}
			if rowsAffected == 0 {
				stats.SkippedUnchanged++
			}
		default:
			recordScanError(&stats, statErr)
			return stats, statErr
		}
	}

	for _, root := range scope.presenceRoots {
		if err = ctx.Err(); err != nil {
			return stats, err
		}
		currentRoot = root
		currentPath = ""
		s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
			status.Phase = "enumerating"
			status.CurrentRoot = root
			status.CurrentPath = ""
			status.WorkersActive = 0
		})
		paths, err := enumerateAudioPaths(ctx, root)
		if err != nil {
			return stats, err
		}
		rowsAffected, err := s.reconcileRootPresence(ctx, libraryID, deviceID, root, paths)
		if err != nil {
			return stats, err
		}
		if rowsAffected > 0 {
			catalogMutated = true
		}
		rootsDone++
	}

	if catalogMutated {
		local := apitypes.LocalContext{LibraryID: libraryID, DeviceID: deviceID}
		afterImpact, impactErr := s.captureScanImpact(ctx, libraryID, deviceID, scope.presenceRoots, scope.audioPaths)
		if impactErr != nil {
			s.recordAutoRepairRequired(ctx, libraryID, deviceID, scanRepairReasonScopedImpactCorrupt, impactErr)
		} else {
			impact := beforeImpact.merge(afterImpact)
			if err = s.applyAutomaticScopedCatalogUpdate(ctx, libraryID, deviceID, &local, impact); err != nil {
				return stats, err
			}
		}
	}
	artworkRoots := mergeNormalizedRoots(scope.presenceRoots, scope.artworkRoots)
	if !catalogMutated {
		if err = s.reconcileScanArtworkScope(ctx, libraryID, deviceID, artworkRoots, scope.audioPaths); err != nil {
			return stats, err
		}
	} else if len(scope.artworkRoots) > 0 {
		if err = s.reconcileScanArtworkScope(ctx, libraryID, deviceID, scope.artworkRoots, nil); err != nil {
			return stats, err
		}
	}

	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:       "completed",
		RootsTotal:  len(scope.presenceRoots),
		RootsDone:   len(scope.presenceRoots),
		TracksTotal: tracksTotal,
		TracksDone:  tracksTotal,
		Workers:     workers,
		Errors:      stats.Errors,
	})
	return stats, nil
}

func (s *IngestService) applyAutomaticScopedCatalogUpdate(ctx context.Context, libraryID, deviceID string, local *apitypes.LocalContext, impact scanImpactSet) error {
	if !impact.hasTargets() {
		s.recordAutoRepairRequired(ctx, libraryID, deviceID, scanRepairReasonScopedImpactEmpty, errors.New("automatic scan could not derive scoped catalog impact"))
		return nil
	}
	if err := s.app.rebuildCatalogMaterializationScoped(ctx, libraryID, local, impact); err != nil {
		s.recordAutoRepairRequired(ctx, libraryID, deviceID, scanRepairReasonScopedImpactCorrupt, err)
		return nil
	}
	return nil
}

func (s *IngestService) recordAutoRepairRequired(ctx context.Context, libraryID, deviceID, reason string, err error) {
	if s == nil || s.app == nil {
		return
	}
	detail := ""
	if err != nil {
		detail = strings.TrimSpace(err.Error())
		s.app.logf("desktopcore: automatic scan marked repair required for %s/%s (%s): %v", libraryID, deviceID, reason, err)
	} else {
		s.app.logf("desktopcore: automatic scan marked repair required for %s/%s (%s)", libraryID, deviceID, reason)
	}
	if markErr := s.app.markScanRepairRequired(ctx, libraryID, deviceID, reason, detail); markErr != nil {
		s.app.logf("desktopcore: persist scan repair required state failed for %s/%s: %v", libraryID, deviceID, markErr)
	}
}

func (s *IngestService) removeScanRootsFromCatalog(ctx context.Context, libraryID, deviceID string, roots []string) error {
	roots = normalizedWatcherRoots(roots)
	if len(roots) == 0 {
		return nil
	}

	beforeImpact, err := s.captureScanImpact(ctx, libraryID, deviceID, roots, nil)
	if err != nil {
		return err
	}
	rows, err := s.loadPresentSourceFilesForScanScope(ctx, libraryID, deviceID, roots, nil)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	rowsAffected, err := s.markSourceFilesMissing(ctx, libraryID, deviceID, rows)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return nil
	}

	local := apitypes.LocalContext{LibraryID: libraryID, DeviceID: deviceID}
	afterImpact, impactErr := s.captureScanImpact(ctx, libraryID, deviceID, roots, nil)
	if impactErr != nil {
		s.recordAutoRepairRequired(ctx, libraryID, deviceID, scanRepairReasonScopedImpactCorrupt, impactErr)
		return nil
	}
	return s.applyAutomaticScopedCatalogUpdate(ctx, libraryID, deviceID, &local, beforeImpact.merge(afterImpact))
}

func mergeNormalizedRoots(groups ...[]string) []string {
	merged := make(map[string]string)
	for _, group := range groups {
		for _, root := range group {
			root = filepath.Clean(strings.TrimSpace(root))
			if root == "" {
				continue
			}
			merged[scanRootKey(root)] = root
		}
	}
	return sortedWatcherRoots(merged)
}

type scanImpactSet struct {
	trackVariantSet map[string]struct{}
	recordingSet    map[string]struct{}
	albumVariantSet map[string]struct{}
	albumSet        map[string]struct{}
	artistSet       map[string]struct{}
}

func newScanImpactSet() scanImpactSet {
	return scanImpactSet{
		trackVariantSet: make(map[string]struct{}),
		recordingSet:    make(map[string]struct{}),
		albumVariantSet: make(map[string]struct{}),
		albumSet:        make(map[string]struct{}),
		artistSet:       make(map[string]struct{}),
	}
}

func (i scanImpactSet) hasTargets() bool {
	return len(i.trackVariantSet) > 0 || len(i.albumVariantSet) > 0
}

func (s *IngestService) captureScanImpact(ctx context.Context, libraryID, deviceID string, roots, paths []string) (scanImpactSet, error) {
	roots = normalizedWatcherRoots(roots)
	paths = normalizeScanPaths(paths)
	result := newScanImpactSet()
	if len(roots) == 0 && len(paths) == 0 {
		return result, nil
	}

	rows, err := s.loadPresentSourceFilesForScanScope(ctx, libraryID, deviceID, roots, paths)
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		trackVariantID := strings.TrimSpace(row.TrackVariantID)
		if trackVariantID == "" {
			continue
		}
		result.trackVariantSet[trackVariantID] = struct{}{}
	}
	return s.expandScanImpact(ctx, libraryID, result)
}

func (i scanImpactSet) merge(other scanImpactSet) scanImpactSet {
	for key := range other.trackVariantSet {
		i.trackVariantSet[key] = struct{}{}
	}
	for key := range other.recordingSet {
		i.recordingSet[key] = struct{}{}
	}
	for key := range other.albumVariantSet {
		i.albumVariantSet[key] = struct{}{}
	}
	for key := range other.albumSet {
		i.albumSet[key] = struct{}{}
	}
	for key := range other.artistSet {
		i.artistSet[key] = struct{}{}
	}
	return i
}

func (i scanImpactSet) trackVariantIDs() []string {
	return sortedSetKeys(i.trackVariantSet)
}

func (i scanImpactSet) recordingIDs() []string {
	return sortedSetKeys(i.recordingSet)
}

func (i scanImpactSet) albumIDs() []string {
	return sortedSetKeys(i.albumSet)
}

func (i scanImpactSet) albumVariantIDs() []string {
	return sortedSetKeys(i.albumVariantSet)
}

func (i scanImpactSet) artistIDs() []string {
	return sortedSetKeys(i.artistSet)
}

func (s *IngestService) expandScanImpact(ctx context.Context, libraryID string, result scanImpactSet) (scanImpactSet, error) {
	libraryID = strings.TrimSpace(libraryID)
	trackVariantIDs := result.trackVariantIDs()
	if libraryID == "" || len(trackVariantIDs) == 0 {
		return result, nil
	}

	var tracks []TrackVariantModel
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND track_variant_id IN ?", libraryID, trackVariantIDs).
		Find(&tracks).Error; err != nil {
		return result, err
	}
	for _, track := range tracks {
		recordingID := firstNonEmpty(strings.TrimSpace(track.TrackClusterID), strings.TrimSpace(track.TrackVariantID))
		if recordingID != "" {
			result.recordingSet[recordingID] = struct{}{}
		}
	}

	var albumTracks []AlbumTrack
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND track_variant_id IN ?", libraryID, trackVariantIDs).
		Find(&albumTracks).Error; err != nil {
		return result, err
	}
	for _, albumTrack := range albumTracks {
		if albumVariantID := strings.TrimSpace(albumTrack.AlbumVariantID); albumVariantID != "" {
			result.albumVariantSet[albumVariantID] = struct{}{}
		}
	}

	albumVariantIDs := result.albumVariantIDs()
	if len(albumVariantIDs) > 0 {
		var albums []AlbumVariantModel
		if err := s.app.storage.WithContext(ctx).
			Where("library_id = ? AND album_variant_id IN ?", libraryID, albumVariantIDs).
			Find(&albums).Error; err != nil {
			return result, err
		}
		for _, album := range albums {
			albumID := firstNonEmpty(strings.TrimSpace(album.AlbumClusterID), strings.TrimSpace(album.AlbumVariantID))
			if albumID != "" {
				result.albumSet[albumID] = struct{}{}
			}
		}
	}

	if len(trackVariantIDs) > 0 {
		var trackCredits []Credit
		if err := s.app.storage.WithContext(ctx).
			Where("library_id = ? AND entity_type = ? AND entity_id IN ?", libraryID, "track", trackVariantIDs).
			Find(&trackCredits).Error; err != nil {
			return result, err
		}
		for _, credit := range trackCredits {
			if artistID := strings.TrimSpace(credit.ArtistID); artistID != "" {
				result.artistSet[artistID] = struct{}{}
			}
		}
	}

	if len(albumVariantIDs) > 0 {
		var albumCredits []Credit
		if err := s.app.storage.WithContext(ctx).
			Where("library_id = ? AND entity_type = ? AND entity_id IN ?", libraryID, "album", albumVariantIDs).
			Find(&albumCredits).Error; err != nil {
			return result, err
		}
		for _, credit := range albumCredits {
			if artistID := strings.TrimSpace(credit.ArtistID); artistID != "" {
				result.artistSet[artistID] = struct{}{}
			}
		}
	}
	return result, nil
}

func (s *IngestService) reconcileScanArtworkScope(ctx context.Context, libraryID, deviceID string, roots, paths []string) error {
	if s == nil || s.app == nil || s.app.artwork == nil {
		return nil
	}
	local := apitypes.LocalContext{LibraryID: libraryID, DeviceID: deviceID}
	impact, err := s.captureScanImpact(ctx, libraryID, deviceID, roots, paths)
	if err != nil {
		return err
	}
	return s.app.reconcileLocalAlbumArtworkBestEffort(ctx, local, impact.albumVariantIDs())
}

func sortedSetKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func bestScanRootForPath(path string, roots []string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	best := ""
	for _, root := range roots {
		if !pathWithinRoot(path, root) {
			continue
		}
		if len(root) > len(best) {
			best = root
		}
	}
	if best != "" {
		return best
	}
	return filepath.Dir(path)
}

func sameScanRootSet(left, right []string) bool {
	left = normalizedWatcherRoots(left)
	right = normalizedWatcherRoots(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if scanRootKey(left[index]) != scanRootKey(right[index]) {
			return false
		}
	}
	return true
}

func enumerateAudioPaths(ctx context.Context, root string) ([]string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan root is not a directory: %q", root)
	}

	paths := make([]string, 0, 64)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !isAudioPath(path) {
			return nil
		}
		paths = append(paths, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func isAudioPath(path string) bool {
	_, ok := supportedAudioExt[strings.ToLower(filepath.Ext(path))]
	return ok
}

func (s *IngestService) ingestPath(ctx context.Context, libraryID, deviceID, root, path string) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, false, fmt.Errorf("%s: %w", filepath.Clean(path), err)
	}

	state, err := s.lookupFileState(ctx, libraryID, deviceID, path)
	if err != nil {
		return false, false, fmt.Errorf("%s: %w", filepath.Clean(path), err)
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	mtimeNS := info.ModTime().UnixNano()
	if state.HasState && state.IsPresent && state.MTimeNS == mtimeNS && state.SizeBytes == info.Size() {
		return false, true, nil
	}

	tags, err := s.app.tagReader.Read(path)
	if err != nil {
		return false, false, fmt.Errorf("%s: %w", filepath.Clean(path), err)
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	hashHex, err := sha256File(path)
	if err != nil {
		return false, false, fmt.Errorf("%s: %w", filepath.Clean(path), err)
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	if err := s.upsertIngest(ctx, ingestRecord{
		LibraryID:       libraryID,
		DeviceID:        deviceID,
		Path:            path,
		MTimeNS:         mtimeNS,
		SizeBytes:       info.Size(),
		HashAlgo:        "sha256",
		HashHex:         hashHex,
		SourceFileID:    sourceFileIDForDevicePath(deviceID, path),
		EditionScopeKey: editionScopeKeyForPath(root, path, tags),
		Tags:            tags,
	}); err != nil {
		return false, false, fmt.Errorf("%s: %w", filepath.Clean(path), err)
	}
	return true, false, nil
}

type fileState struct {
	HasState  bool
	MTimeNS   int64
	SizeBytes int64
	IsPresent bool
}

func (s *IngestService) lookupFileState(ctx context.Context, libraryID, deviceID, path string) (fileState, error) {
	var row SourceFileModel
	err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND path_key = ?", libraryID, deviceID, localPathKey(path)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return fileState{}, nil
		}
		return fileState{}, err
	}
	return fileState{
		HasState:  true,
		MTimeNS:   row.MTimeNS,
		SizeBytes: row.SizeBytes,
		IsPresent: row.IsPresent,
	}, nil
}

type ingestRecord struct {
	LibraryID       string
	DeviceID        string
	Path            string
	MTimeNS         int64
	SizeBytes       int64
	HashAlgo        string
	HashHex         string
	SourceFileID    string
	EditionScopeKey string
	Tags            Tags
}

func (s *IngestService) upsertIngest(ctx context.Context, in ingestRecord) error {
	now := time.Now().UTC()
	local := apitypes.LocalContext{LibraryID: in.LibraryID, DeviceID: in.DeviceID}
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		pathKey := localPathKey(in.Path)
		sourceFingerprint := in.HashAlgo + ":" + in.HashHex
		conflicts, err := conflictingPathSourceFilesTx(tx, in.LibraryID, in.DeviceID, pathKey, sourceFingerprint)
		if err != nil {
			return err
		}
		for _, conflict := range conflicts {
			if _, err := s.app.appendLocalOplogTx(tx, local, entityTypeSourceFile, sourceFileEntityID(conflict.DeviceID, conflict.SourceFileID), "delete", map[string]any{
				"deviceId":     conflict.DeviceID,
				"sourceFileId": conflict.SourceFileID,
			}); err != nil {
				return err
			}
		}
		if err := upsertIngestTx(tx, in, now, true); err != nil {
			return err
		}
		_, err = s.app.appendLocalOplogTx(tx, local, entityTypeSourceFile, sourceFileEntityID(in.DeviceID, in.SourceFileID), "upsert", sourceFileOplogPayload{
			DeviceID:        in.DeviceID,
			SourceFileID:    in.SourceFileID,
			LibraryID:       in.LibraryID,
			LocalPath:       filepath.Clean(in.Path),
			EditionScopeKey: in.EditionScopeKey,
			MTimeNS:         in.MTimeNS,
			SizeBytes:       in.SizeBytes,
			HashAlgo:        in.HashAlgo,
			HashHex:         in.HashHex,
			Tags:            in.Tags,
			IsPresent:       true,
		})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func upsertArtistsAndCredits(tx *gorm.DB, libraryID, trackVariantID, albumVariantID string, artists []string, albumArtist string) error {
	seenArtists := make(map[string]struct{}, len(artists)+1)

	for idx, artistName := range compactNonEmptyStrings(artists) {
		artistKey := normalizeCatalogKey(artistName)
		if artistKey == "" {
			continue
		}
		artistID := stableNameID("artist", artistKey)
		if _, ok := seenArtists[artistID]; ok {
			continue
		}
		seenArtists[artistID] = struct{}{}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "library_id"}, {Name: "artist_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "name_sort"}),
		}).Create(&Artist{
			LibraryID: libraryID,
			ArtistID:  artistID,
			Name:      artistName,
			NameSort:  strings.ToLower(strings.TrimSpace(artistName)),
		}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Credit{
			LibraryID:  libraryID,
			EntityType: "track",
			EntityID:   trackVariantID,
			ArtistID:   artistID,
			Role:       "primary",
			Ord:        idx + 1,
		}).Error; err != nil {
			return err
		}
	}

	albumArtist = strings.TrimSpace(albumArtist)
	if albumArtist == "" {
		return nil
	}
	artistKey := normalizeCatalogKey(albumArtist)
	if artistKey == "" {
		return nil
	}
	artistID := stableNameID("artist", artistKey)
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "artist_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "name_sort"}),
	}).Create(&Artist{
		LibraryID: libraryID,
		ArtistID:  artistID,
		Name:      albumArtist,
		NameSort:  strings.ToLower(albumArtist),
	}).Error; err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Credit{
		LibraryID:  libraryID,
		EntityType: "album",
		EntityID:   albumVariantID,
		ArtistID:   artistID,
		Role:       "primary",
		Ord:        1,
	}).Error
}

func (s *IngestService) reconcileRootPresence(ctx context.Context, libraryID, deviceID, root string, seenPaths []string) (int64, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return 0, nil
	}
	seen := make(map[string]struct{}, len(seenPaths))
	for _, path := range seenPaths {
		seen[localPathKey(path)] = struct{}{}
	}

	rows, err := s.loadPresentSourceFilesForScanScope(ctx, libraryID, deviceID, []string{root}, nil)
	if err != nil {
		return 0, err
	}

	missingRows := make([]SourceFileModel, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[localPathKey(row.LocalPath)]; ok {
			continue
		}
		missingRows = append(missingRows, row)
	}
	if len(missingRows) == 0 {
		return 0, nil
	}
	return s.markSourceFilesMissing(ctx, libraryID, deviceID, missingRows)
}

func (s *IngestService) reconcileMissingPaths(ctx context.Context, libraryID, deviceID string, paths []string) (int64, error) {
	rows, err := s.loadPresentSourceFilesForScanScope(ctx, libraryID, deviceID, nil, paths)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return s.markSourceFilesMissing(ctx, libraryID, deviceID, rows)
}

func (s *IngestService) markSourceFilesMissing(ctx context.Context, libraryID, deviceID string, rows []SourceFileModel) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	local := apitypes.LocalContext{LibraryID: libraryID, DeviceID: deviceID}
	now := time.Now().UTC()
	var rowsAffected int64
	missingIDs := make([]string, 0, len(rows))
	seenIDs := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		sourceFileID := strings.TrimSpace(row.SourceFileID)
		if sourceFileID == "" {
			continue
		}
		if _, ok := seenIDs[sourceFileID]; ok {
			continue
		}
		seenIDs[sourceFileID] = struct{}{}
		missingIDs = append(missingIDs, sourceFileID)
	}
	if len(missingIDs) == 0 {
		return 0, nil
	}

	err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.
			Model(&SourceFileModel{}).
			Where("library_id = ? AND device_id = ? AND source_file_id IN ? AND is_present = ?", libraryID, deviceID, missingIDs, true).
			Updates(map[string]any{
				"is_present":   false,
				"last_seen_at": now,
				"updated_at":   now,
			})
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		for _, row := range rows {
			payload, err := sourceFileOplogPayloadFromRow(row)
			if err != nil {
				return err
			}
			payload.IsPresent = false
			if _, err := s.app.appendLocalOplogTx(tx, local, entityTypeSourceFile, sourceFileEntityID(row.DeviceID, row.SourceFileID), "upsert", payload); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return rowsAffected, err
	}
	return rowsAffected, nil
}

func (s *IngestService) loadPresentSourceFilesForScanScope(ctx context.Context, libraryID, deviceID string, roots, paths []string) ([]SourceFileModel, error) {
	roots = normalizedWatcherRoots(roots)
	paths = normalizeScanPaths(paths)
	if len(roots) == 0 && len(paths) == 0 {
		return nil, nil
	}

	scopeQuery, scopeArgs := sourceFilesForScanScopeQuery(roots, paths)
	query := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", libraryID, deviceID, true)
	if scopeQuery != "" {
		query = query.Where(scopeQuery, scopeArgs...)
	}

	var rows []SourceFileModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func sourceFilesForScanScopeQuery(roots, paths []string) (string, []any) {
	parts := make([]string, 0, len(roots)+1)
	args := make([]any, 0, len(roots)*2+1)
	if len(paths) > 0 {
		pathKeys := make([]string, 0, len(paths))
		for _, path := range paths {
			if key := localPathKey(path); key != "" {
				pathKeys = append(pathKeys, key)
			}
		}
		if len(pathKeys) > 0 {
			parts = append(parts, "path_key IN ?")
			args = append(args, pathKeys)
		}
	}
	for _, root := range roots {
		rootKey := localPathKey(root)
		if rootKey == "" {
			continue
		}
		parts = append(parts, "(path_key = ? OR path_key LIKE ?)")
		args = append(args, rootKey, rootKey+string(filepath.Separator)+"%")
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func normalizedRecordKeys(tags Tags) (recordingKey, albumKey, groupKey string) {
	primaryArtist := firstArtist(tags.Artists)
	recordingKey = normalizeCatalogKey(strings.Join([]string{primaryArtist, tags.Title}, "|"))
	albumKey = normalizeCatalogKey(strings.Join([]string{firstNonEmpty(tags.AlbumArtist, primaryArtist), tags.Album, fmt.Sprintf("%d", tags.Year)}, "|"))
	groupKey = normalizeCatalogKey(strings.Join([]string{firstNonEmpty(tags.AlbumArtist, primaryArtist), tags.Album}, "|"))
	return recordingKey, albumKey, groupKey
}

func normalizedTrackClusterKey(recordingKey, groupKey string) string {
	recordingKey = strings.TrimSpace(recordingKey)
	groupKey = strings.TrimSpace(groupKey)
	if groupKey == "" {
		return recordingKey
	}
	return normalizeCatalogKey(strings.Join([]string{recordingKey, groupKey}, "|"))
}

func normalizeCatalogKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Join(strings.Fields(value), " ")
}

func firstArtist(artists []string) string {
	if len(artists) == 0 {
		return ""
	}
	return strings.TrimSpace(artists[0])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func maxTrackNumber(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func localPathKey(path string) string {
	return scanRootKey(filepath.Clean(strings.TrimSpace(path)))
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	root = filepath.Clean(strings.TrimSpace(root))
	if path == "" || root == "" {
		return false
	}
	if localPathKey(path) == localPathKey(root) {
		return true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if stdruntime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix))
	}
	return strings.HasPrefix(path, prefix)
}

func normalizedWatcherRoots(roots []string) []string {
	clean, _ := normalizeScanRoots(roots)
	sort.Strings(clean)
	return clean
}

func scanJobID(libraryID, deviceID string, roots []string, kind string) string {
	sum := sha256.Sum256([]byte(kind + ":" + libraryID + ":" + deviceID + ":" + strings.Join(roots, "|")))
	return kind + ":" + hex.EncodeToString(sum[:8])
}

func scanProgress(rootsDone, rootsTotal, tracksDone, tracksTotal int) float64 {
	if rootsTotal == 0 {
		return 0.5
	}
	rootWeight := float64(rootsDone) / float64(rootsTotal)
	if tracksTotal == 0 {
		return 0.2 + rootWeight*0.7
	}
	trackWeight := float64(tracksDone) / float64(tracksTotal)
	return 0.2 + (rootWeight*0.2 + trackWeight*0.6)
}

func scanCompletionMessage(stats apitypes.ScanStats) string {
	message := fmt.Sprintf("scan complete: %d scanned, %d imported, %d unchanged, %d errors", stats.Scanned, stats.Imported, stats.SkippedUnchanged, stats.Errors)
	if stats.Errors > 0 && strings.TrimSpace(stats.FirstError) != "" {
		message += "; first error: " + strings.TrimSpace(stats.FirstError)
	}
	return message
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
