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
	jobKindRescanAll  = "scan-library"
	jobKindRescanRoot = "scan-root"
)

var supportedAudioExt = map[string]struct{}{
	".aac":  {},
	".flac": {},
	".m4a":  {},
	".mp3":  {},
	".ogg":  {},
	".opus": {},
	".wav":  {},
}

func (s *IngestService) RescanNow(ctx context.Context) (apitypes.ScanStats, error) {
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
	return s.runTrackedScan(ctx, local.LibraryID, local.DeviceID, roots, jobKindRescanAll)
}

func (s *IngestService) StartRescanNow(ctx context.Context) (JobSnapshot, error) {
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
	return s.startTrackedScan(ctx, local.LibraryID, local.DeviceID, roots, jobKindRescanAll, "queued library scan")
}

func (s *IngestService) RescanRoot(ctx context.Context, root string) (apitypes.ScanStats, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.ScanStats{}, err
	}
	if !canProvideLocalMedia(local.Role) {
		return apitypes.ScanStats{}, fmt.Errorf("local ingest requires owner, admin, or member role")
	}
	roots, err := normalizeScanRoots([]string{root})
	if err != nil {
		return apitypes.ScanStats{}, err
	}
	if len(roots) == 0 {
		return apitypes.ScanStats{}, nil
	}
	return s.runTrackedScan(ctx, local.LibraryID, local.DeviceID, roots, jobKindRescanRoot)
}

func (s *IngestService) StartRescanRoot(ctx context.Context, root string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}
	if !canProvideLocalMedia(local.Role) {
		return JobSnapshot{}, fmt.Errorf("local ingest requires owner, admin, or member role")
	}
	roots, err := normalizeScanRoots([]string{root})
	if err != nil {
		return JobSnapshot{}, err
	}
	if len(roots) == 0 {
		return JobSnapshot{}, nil
	}
	return s.startTrackedScan(ctx, local.LibraryID, local.DeviceID, roots, jobKindRescanRoot, "queued root scan")
}

func (s *IngestService) runTrackedScan(ctx context.Context, libraryID, deviceID string, roots []string, jobKind string) (apitypes.ScanStats, error) {
	normalized := normalizedWatcherRoots(roots)
	flight, leader := s.app.beginScanFlight(libraryID, normalized)
	if !leader {
		if err := waitForScanFlight(ctx, flight); err != nil {
			return apitypes.ScanStats{}, err
		}
		return flight.stats, flight.err
	}

	jobID := scanJobID(libraryID, deviceID, normalized, jobKind)
	job := s.app.jobs.Track(jobID, jobKind, libraryID)
	if job != nil {
		job.Queued(0, "queued library scan")
	}

	var (
		stats  apitypes.ScanStats
		runErr error
	)
	defer func() {
		s.app.finishScanFlight(flight, stats, runErr)
		if job == nil {
			return
		}
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				job.Fail(1, "scan canceled because the library is no longer active", nil)
				return
			}
			job.Fail(1, "scan failed", runErr)
			return
		}
		job.Complete(1, scanCompletionMessage(stats))
	}()

	if job != nil {
		job.Running(0.05, "enumerating scan roots")
	}
	stats, runErr = s.runScanCycle(ctx, libraryID, deviceID, normalized, job)
	return stats, runErr
}

func (s *IngestService) startTrackedScan(ctx context.Context, libraryID, deviceID string, roots []string, jobKind, queuedMessage string) (JobSnapshot, error) {
	normalized := normalizedWatcherRoots(roots)
	jobID := scanJobID(libraryID, deviceID, normalized, jobKind)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKind,
		libraryID,
		queuedMessage,
		"scan canceled because the library is no longer active",
		func(runCtx context.Context) {
			_, _ = s.runTrackedScan(runCtx, libraryID, deviceID, normalized, jobKind)
		},
	)
}

func (a *App) beginScanFlight(libraryID string, roots []string) (*scanFlight, bool) {
	a.activityMu.Lock()
	defer a.activityMu.Unlock()

	if a.scanFlight != nil && strings.TrimSpace(a.scanFlight.libraryID) == strings.TrimSpace(libraryID) {
		for _, root := range roots {
			a.scanFlight.roots[scanRootKey(root)] = root
		}
		return a.scanFlight, false
	}

	flight := &scanFlight{
		libraryID: strings.TrimSpace(libraryID),
		roots:     make(map[string]string, len(roots)),
		done:      make(chan struct{}),
	}
	for _, root := range roots {
		flight.roots[scanRootKey(root)] = root
	}
	a.scanFlight = flight
	return flight, true
}

func (a *App) finishScanFlight(flight *scanFlight, stats apitypes.ScanStats, err error) {
	a.activityMu.Lock()
	defer a.activityMu.Unlock()

	if a.scanFlight == flight {
		a.scanFlight = nil
	}
	flight.stats = stats
	flight.err = err
	close(flight.done)
}

func waitForScanFlight(ctx context.Context, flight *scanFlight) error {
	if flight == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-flight.done:
		return nil
	}
}

func (s *IngestService) runScanCycle(ctx context.Context, libraryID, deviceID string, roots []string, job *JobTracker) (apitypes.ScanStats, error) {
	workers := 1
	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:      "enumerating",
		RootsTotal: len(roots),
		Workers:    workers,
	})

	pathsByRoot := make(map[string][]string, len(roots))
	totalTracks := 0
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return apitypes.ScanStats{}, err
		}
		s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
			status.Phase = "enumerating"
			status.CurrentRoot = root
			status.CurrentPath = ""
		})
		if job != nil {
			job.Running(0.05, "enumerating "+root)
		}
		paths, err := enumerateAudioPaths(ctx, root)
		if err != nil {
			s.app.setScanActivity(apitypes.ScanActivityStatus{
				Phase:       "failed",
				RootsTotal:  len(roots),
				Workers:     workers,
				CurrentRoot: root,
				Errors:      1,
			})
			return apitypes.ScanStats{}, err
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

	var (
		combined   apitypes.ScanStats
		tracksDone int
		rootsDone  int
	)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return combined, err
		}
		paths := pathsByRoot[root]
		sort.Strings(paths)
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return combined, err
			}
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

			imported, skipped, err := s.ingestPath(ctx, libraryID, deviceID, path)
			combined.Scanned++
			if imported {
				combined.Imported++
			}
			if skipped {
				combined.SkippedUnchanged++
			}
			if err != nil {
				combined.Errors++
			}
			tracksDone++
			s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
				status.TracksDone = tracksDone
				status.CurrentRoot = root
				status.CurrentPath = path
				status.WorkersActive = 0
				status.Errors = combined.Errors
			})
			if err != nil {
				s.app.setScanActivity(apitypes.ScanActivityStatus{
					Phase:         "failed",
					RootsTotal:    len(roots),
					RootsDone:     rootsDone,
					TracksTotal:   totalTracks,
					TracksDone:    tracksDone,
					CurrentRoot:   root,
					CurrentPath:   path,
					Workers:       workers,
					WorkersActive: 0,
					Errors:        combined.Errors,
				})
				return combined, err
			}
		}

		if _, err := s.reconcileRootPresence(ctx, libraryID, deviceID, root, paths); err != nil {
			s.app.setScanActivity(apitypes.ScanActivityStatus{
				Phase:       "failed",
				RootsTotal:  len(roots),
				RootsDone:   rootsDone,
				TracksTotal: totalTracks,
				TracksDone:  tracksDone,
				CurrentRoot: root,
				Workers:     workers,
				Errors:      combined.Errors + 1,
			})
			return combined, err
		}

		rootsDone++
		s.app.updateScanActivity(func(status *apitypes.ScanActivityStatus) {
			status.RootsDone = rootsDone
			status.CurrentRoot = root
			status.CurrentPath = ""
			status.WorkersActive = 0
		})
	}

	s.app.setScanActivity(apitypes.ScanActivityStatus{
		Phase:       "completed",
		RootsTotal:  len(roots),
		RootsDone:   len(roots),
		TracksTotal: totalTracks,
		TracksDone:  tracksDone,
		Workers:     workers,
		Errors:      combined.Errors,
	})
	return combined, nil
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

func (s *IngestService) ingestPath(ctx context.Context, libraryID, deviceID, path string) (bool, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, false, err
	}

	state, err := s.lookupFileState(ctx, libraryID, deviceID, path)
	if err != nil {
		return false, false, err
	}
	mtimeNS := info.ModTime().UnixNano()
	if state.HasState && state.IsPresent && state.MTimeNS == mtimeNS && state.SizeBytes == info.Size() {
		return false, true, nil
	}

	tags, err := s.app.tagReader.Read(path)
	if err != nil {
		return false, false, err
	}
	hashHex, err := sha256File(path)
	if err != nil {
		return false, false, err
	}
	if err := s.upsertIngest(ctx, ingestRecord{
		LibraryID:    libraryID,
		DeviceID:     deviceID,
		Path:         path,
		MTimeNS:      mtimeNS,
		SizeBytes:    info.Size(),
		HashAlgo:     "sha256",
		HashHex:      hashHex,
		SourceFileID: "sha256:" + hashHex,
		Tags:         tags,
	}); err != nil {
		return false, false, err
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
	LibraryID    string
	DeviceID     string
	Path         string
	MTimeNS      int64
	SizeBytes    int64
	HashAlgo     string
	HashHex      string
	SourceFileID string
	Tags         Tags
}

func (s *IngestService) upsertIngest(ctx context.Context, in ingestRecord) error {
	now := time.Now().UTC()
	local := apitypes.LocalContext{LibraryID: in.LibraryID, DeviceID: in.DeviceID}
	_, albumKey, _ := normalizedRecordKeys(in.Tags)
	albumVariantID := stableNameID("album", albumKey)
	if err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
			DeviceID:     in.DeviceID,
			SourceFileID: in.SourceFileID,
			LibraryID:    in.LibraryID,
			LocalPath:    filepath.Clean(in.Path),
			MTimeNS:      in.MTimeNS,
			SizeBytes:    in.SizeBytes,
			HashAlgo:     in.HashAlgo,
			HashHex:      in.HashHex,
			Tags:         in.Tags,
			IsPresent:    true,
		})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if s.app.artwork != nil {
		if err := s.app.artwork.ReconcileAlbumArtwork(ctx, local, albumVariantID); err != nil && !errors.Is(err, context.Canceled) {
			s.app.cfg.Logger.Errorf("artwork reconcile after ingest failed for album %s: %v", albumVariantID, err)
		}
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

	var rows []SourceFileModel
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", libraryID, deviceID, true).
		Find(&rows).Error; err != nil {
		return 0, err
	}

	missingIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if !pathWithinRoot(row.LocalPath, root) {
			continue
		}
		if _, ok := seen[localPathKey(row.LocalPath)]; ok {
			continue
		}
		missingIDs = append(missingIDs, row.SourceFileID)
	}
	if len(missingIDs) == 0 {
		return 0, nil
	}

	local := apitypes.LocalContext{LibraryID: libraryID, DeviceID: deviceID}
	now := time.Now().UTC()
	var rowsAffected int64
	missingTrackVariantIDs := make([]string, 0, len(missingIDs))
	missingRowCount := 0
	err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
			if !pathWithinRoot(row.LocalPath, root) {
				continue
			}
			if _, ok := seen[localPathKey(row.LocalPath)]; ok {
				continue
			}
			missingTrackVariantIDs = append(missingTrackVariantIDs, row.TrackVariantID)
			missingRowCount++
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
	if rowsAffected == 0 || missingRowCount == 0 || s.app.artwork == nil {
		return rowsAffected, nil
	}
	albumIDs, err := s.app.artwork.albumIDsForTrackVariants(ctx, libraryID, missingTrackVariantIDs)
	if err != nil {
		return rowsAffected, err
	}
	for _, albumID := range albumIDs {
		if reconcileErr := s.app.artwork.ReconcileAlbumArtwork(ctx, local, albumID); reconcileErr != nil && !errors.Is(reconcileErr, context.Canceled) {
			s.app.cfg.Logger.Errorf("artwork reconcile after root presence update failed for album %s: %v", albumID, reconcileErr)
		}
	}
	return rowsAffected, nil
}

func normalizedRecordKeys(tags Tags) (recordingKey, albumKey, groupKey string) {
	primaryArtist := firstArtist(tags.Artists)
	durationBucket := tags.DurationMS / 2000
	recordingKey = normalizeCatalogKey(strings.Join([]string{primaryArtist, tags.Title, fmt.Sprintf("%d", durationBucket)}, "|"))
	albumKey = normalizeCatalogKey(strings.Join([]string{firstNonEmpty(tags.AlbumArtist, primaryArtist), tags.Album, fmt.Sprintf("%d", tags.Year)}, "|"))
	groupKey = normalizeCatalogKey(strings.Join([]string{firstNonEmpty(tags.AlbumArtist, primaryArtist), tags.Album}, "|"))
	return recordingKey, albumKey, groupKey
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
	return fmt.Sprintf("scan complete: %d scanned, %d imported, %d unchanged, %d errors", stats.Scanned, stats.Imported, stats.SkippedUnchanged, stats.Errors)
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

