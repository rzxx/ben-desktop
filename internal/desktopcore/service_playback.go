package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
	playbackcore "ben/desktop/internal/playback"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	jobKindEnsureRecordingEncoding = "ensure-recording-encoding"
	jobKindEnsureAlbumEncodings    = "ensure-album-encodings"
	jobKindEnsurePlaylistEncodings = "ensure-playlist-encodings"
	jobKindPreparePlayback         = "prepare-playback"
	jobKindPinRecordingOffline     = "pin-recording-offline"
	jobKindPinAlbumOffline         = "pin-album-offline"
	jobKindPinPlaylistOffline      = "pin-playlist-offline"
	jobKindRefreshPinnedAlbum      = "refresh-pinned-album"
	jobKindRefreshPinnedPlaylist   = "refresh-pinned-playlist"

	pinnedScopeWorkerCount  = 3
	pinnedScopeDebounceWait = time.Second
)

type PlaybackService struct {
	app *App

	mu           sync.Mutex
	preparations map[string]apitypes.PlaybackPreparationStatus

	reconcileMu     sync.Mutex
	reconcileTimers map[string]*time.Timer
}

func newPlaybackService(app *App) *PlaybackService {
	service := &PlaybackService{
		app:             app,
		preparations:    make(map[string]apitypes.PlaybackPreparationStatus),
		reconcileTimers: make(map[string]*time.Timer),
	}
	if app != nil {
		app.SubscribeCatalogChanges(service.handlePinnedScopeCatalogChange)
	}
	return service
}

func (s *PlaybackService) EnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (bool, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return false, err
	}
	return s.ensureRecordingEncodingForLocalContext(ctx, local, recordingID, preferredProfile)
}

func (s *PlaybackService) StartEnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return JobSnapshot{}, fmt.Errorf("recording id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	jobID := playbackEnsureRecordingEncodingJobID(local.LibraryID, recordingID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindEnsureRecordingEncoding,
		local.LibraryID,
		"queued recording encoding",
		"recording encoding canceled because the library is no longer active",
		func(runCtx context.Context) {
			_, _ = s.ensureRecordingEncodingJob(runCtx, local, recordingID, profile)
		},
	)
}

func (s *PlaybackService) ensureRecordingEncodingForLocalContext(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (bool, error) {
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return false, err
	}
	return s.app.transcode.EnsureRecordingEncoding(ctx, local, resolvedRecordingID, profile, local.DeviceID)
}

func (s *PlaybackService) EnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (apitypes.EnsureEncodingBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.EnsureEncodingBatchResult{}, err
	}
	return s.ensureAlbumEncodingsForLocalContext(ctx, local, albumID, preferredProfile, nil)
}

func (s *PlaybackService) StartEnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return JobSnapshot{}, fmt.Errorf("album id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	jobID := playbackEnsureScopeEncodingsJobID(local.LibraryID, "album", albumID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindEnsureAlbumEncodings,
		local.LibraryID,
		"queued album encoding batch",
		"album encoding batch canceled because the library is no longer active",
		func(runCtx context.Context) {
			job := s.app.jobs.Track(jobID, jobKindEnsureAlbumEncodings, local.LibraryID)
			_, _ = s.ensureAlbumEncodingsForLocalContext(runCtx, local, albumID, profile, job)
		},
	)
}

func (s *PlaybackService) ensureAlbumEncodingsForLocalContext(ctx context.Context, local apitypes.LocalContext, albumID, preferredProfile string, job *JobTracker) (apitypes.EnsureEncodingBatchResult, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return apitypes.EnsureEncodingBatchResult{}, fmt.Errorf("album id is required")
	}
	if job != nil {
		job.Queued(0, "queued album encoding batch")
		job.Running(0.1, "collecting album recordings")
	}
	recordingIDs, err := s.recordingIDsForAlbum(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "album encoding batch canceled because the library is no longer active", nil)
			} else {
				job.Fail(1, "album encoding batch failed", err)
			}
		}
		return apitypes.EnsureEncodingBatchResult{}, err
	}
	return s.ensureScopeEncodings(ctx, local, recordingIDs, preferredProfile, job, "album encoding batch", "album recordings")
}

func (s *PlaybackService) EnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (apitypes.EnsureEncodingBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.EnsureEncodingBatchResult{}, err
	}
	return s.ensurePlaylistEncodingsForLocalContext(ctx, local, playlistID, preferredProfile, nil)
}

func (s *PlaybackService) StartEnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return JobSnapshot{}, fmt.Errorf("playlist id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	jobID := playbackEnsureScopeEncodingsJobID(local.LibraryID, "playlist", playlistID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindEnsurePlaylistEncodings,
		local.LibraryID,
		"queued playlist encoding batch",
		"playlist encoding batch canceled because the library is no longer active",
		func(runCtx context.Context) {
			job := s.app.jobs.Track(jobID, jobKindEnsurePlaylistEncodings, local.LibraryID)
			_, _ = s.ensurePlaylistEncodingsForLocalContext(runCtx, local, playlistID, profile, job)
		},
	)
}

func (s *PlaybackService) ensurePlaylistEncodingsForLocalContext(ctx context.Context, local apitypes.LocalContext, playlistID, preferredProfile string, job *JobTracker) (apitypes.EnsureEncodingBatchResult, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return apitypes.EnsureEncodingBatchResult{}, fmt.Errorf("playlist id is required")
	}
	if job != nil {
		job.Queued(0, "queued playlist encoding batch")
		job.Running(0.1, "collecting playlist recordings")
	}
	recordingIDs, err := s.recordingIDsForPlaylist(ctx, local.LibraryID, playlistID)
	if err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "playlist encoding batch canceled because the library is no longer active", nil)
			} else {
				job.Fail(1, "playlist encoding batch failed", err)
			}
		}
		return apitypes.EnsureEncodingBatchResult{}, err
	}
	return s.ensureScopeEncodings(ctx, local, recordingIDs, preferredProfile, job, "playlist encoding batch", "playlist recordings")
}

func (s *PlaybackService) ensureScopeEncodings(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string, job *JobTracker, scopeLabel string, emptyMessage string) (apitypes.EnsureEncodingBatchResult, error) {
	out := apitypes.EnsureEncodingBatchResult{}
	seen := make(map[string]struct{}, len(recordingIDs))
	uniqueIDs := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		recordingID = strings.TrimSpace(recordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := seen[recordingID]; ok {
			continue
		}
		seen[recordingID] = struct{}{}
		uniqueIDs = append(uniqueIDs, recordingID)
	}
	out.Recordings = len(uniqueIDs)
	if job != nil && len(uniqueIDs) == 0 {
		job.Complete(1, "no "+strings.TrimSpace(emptyMessage)+" require encoding")
	}
	for index, recordingID := range uniqueIDs {
		if job != nil {
			total := len(uniqueIDs)
			progress := 0.15
			if total > 0 {
				progress = 0.15 + (0.75 * float64(index) / float64(total))
			}
			job.Running(progress, fmt.Sprintf("encoding %d of %d recordings", index+1, total))
		}
		created, err := s.ensureRecordingEncodingForLocalContext(ctx, local, recordingID, preferredProfile)
		if err != nil {
			if job != nil {
				if errors.Is(err, context.Canceled) {
					job.Fail(1, scopeLabel+" canceled because the library is no longer active", nil)
				} else {
					job.Fail(1, scopeLabel+" failed", err)
				}
			}
			return apitypes.EnsureEncodingBatchResult{}, err
		}
		if created {
			out.Created++
		} else {
			out.Skipped++
		}
	}
	if job != nil {
		job.Complete(1, ensureEncodingBatchMessage(out, scopeLabel))
	}
	return out, nil
}

func (s *PlaybackService) ensureRecordingEncodingJob(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (bool, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	jobID := playbackEnsureRecordingEncodingJobID(local.LibraryID, recordingID, profile)
	job := s.app.jobs.Track(jobID, jobKindEnsureRecordingEncoding, local.LibraryID)
	if job != nil {
		job.Queued(0, "queued recording encoding")
		job.Running(0.2, "resolving recording variant")
	}
	created, err := s.ensureRecordingEncodingForLocalContext(ctx, local, recordingID, profile)
	if err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "recording encoding canceled because the library is no longer active", nil)
			} else {
				job.Fail(1, "recording encoding failed", err)
			}
		}
		return false, err
	}
	if job != nil {
		if created {
			job.Complete(1, "recording encoding created")
		} else {
			job.Complete(1, "recording encoding already cached")
		}
	}
	return created, nil
}

func (s *PlaybackService) EnsurePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	if _, err := s.app.transcode.EnsureRecordingEncoding(ctx, local, resolvedRecordingID, profile, local.DeviceID); err != nil && !errors.Is(err, ErrProviderOnlyTranscode) {
		return apitypes.PlaybackRecordingResult{}, err
	}

	blobID, encodingID, ok, err := s.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	if ok {
		path, err := s.pathForBlob(blobID)
		if err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		var asset OptimizedAssetModel
		if err := s.app.storage.WithContext(ctx).
			Where("library_id = ? AND optimized_asset_id = ?", local.LibraryID, encodingID).
			Take(&asset).Error; err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		return apitypes.PlaybackRecordingResult{
			EncodingID: encodingID,
			BlobID:     blobID,
			Profile:    strings.TrimSpace(asset.Profile),
			Bitrate:    asset.Bitrate,
			Bytes:      int(info.Size()),
			FromLocal:  true,
			SourceKind: apitypes.PlaybackSourceCachedOpt,
		}, nil
	}

	if localPath, ok, err := s.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID); err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	} else if ok {
		info, err := os.Stat(localPath)
		if err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		return apitypes.PlaybackRecordingResult{
			Profile:    profile,
			Bytes:      int(info.Size()),
			FromLocal:  true,
			SourceKind: apitypes.PlaybackSourceLocalFile,
			LocalPath:  localPath,
		}, nil
	}

	availability, err := s.GetRecordingAvailability(ctx, resolvedRecordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	switch availability.State {
	case apitypes.AvailabilityPlayableRemoteOpt:
		if result, fetched, err := s.ensureRemotePlaybackRecording(ctx, local, resolvedRecordingID, profile); err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		} else if fetched {
			return result, nil
		}
		return apitypes.PlaybackRecordingResult{
			Profile:    profile,
			SourceKind: apitypes.PlaybackSourceRemoteOpt,
			Reason:     apitypes.PlaybackUnavailableNetworkOff,
		}, fmt.Errorf("recording %s requires remote optimized fetch", resolvedRecordingID)
	case apitypes.AvailabilityWaitingProviderTranscode:
		if result, fetched, err := s.ensureRemotePlaybackRecording(ctx, local, resolvedRecordingID, profile); err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		} else if fetched {
			return result, nil
		}
		return apitypes.PlaybackRecordingResult{
			Profile:    profile,
			SourceKind: apitypes.PlaybackSourceRemoteOpt,
			Reason:     apitypes.PlaybackUnavailableNetworkOff,
		}, fmt.Errorf("recording %s requires provider transcode", resolvedRecordingID)
	default:
		return apitypes.PlaybackRecordingResult{
			Profile: profile,
			Reason:  availability.Reason,
		}, fmt.Errorf("recording %s is not available for playback", resolvedRecordingID)
	}
}

func (s *PlaybackService) EnsurePlaybackAlbum(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return apitypes.PlaybackBatchResult{}, fmt.Errorf("album id is required")
	}
	recordingIDs, err := s.recordingIDsForAlbum(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	return s.ensurePlaybackScope(ctx, recordingIDs, preferredProfile)
}

func (s *PlaybackService) EnsurePlaybackPlaylist(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return apitypes.PlaybackBatchResult{}, fmt.Errorf("playlist id is required")
	}
	recordingIDs, err := s.recordingIDsForPlaylist(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	return s.ensurePlaybackScope(ctx, recordingIDs, preferredProfile)
}

func (s *PlaybackService) ensurePlaybackScope(ctx context.Context, recordingIDs []string, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	out := apitypes.PlaybackBatchResult{}
	seen := make(map[string]struct{}, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		recordingID = strings.TrimSpace(recordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := seen[recordingID]; ok {
			continue
		}
		seen[recordingID] = struct{}{}
		result, err := s.EnsurePlaybackRecording(ctx, recordingID, preferredProfile)
		if err != nil {
			return apitypes.PlaybackBatchResult{}, err
		}
		out.Tracks++
		out.TotalBytes += int64(result.Bytes)
		if result.FromLocal {
			out.LocalHits++
		} else {
			out.RemoteFetches++
		}
	}
	return out, nil
}

func (s *PlaybackService) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}
	return s.inspectPlaybackRecording(ctx, local, recordingID, preferredProfile)
}

func (s *PlaybackService) inspectPlaybackRecording(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}

	status := apitypes.PlaybackPreparationStatus{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: profile,
		Phase:            apitypes.PlaybackPreparationUnavailable,
		UpdatedAt:        time.Now().UTC(),
	}

	if localPath, ok, err := s.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID); err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	} else if ok {
		uri, err := fileURIFromPath(localPath)
		if err != nil {
			return apitypes.PlaybackPreparationStatus{}, err
		}
		status.Phase = apitypes.PlaybackPreparationReady
		status.SourceKind = apitypes.PlaybackSourceLocalFile
		status.PlayableURI = uri
		return status, nil
	}

	if blobID, encodingID, ok, err := s.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile); err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	} else if ok {
		uri, err := s.fileURIForBlob(blobID)
		if err == nil {
			status.Phase = apitypes.PlaybackPreparationReady
			status.SourceKind = apitypes.PlaybackSourceCachedOpt
			status.PlayableURI = uri
			status.BlobID = blobID
			status.EncodingID = encodingID
			return status, nil
		}
	}

	items, err := s.ListRecordingAvailability(ctx, resolvedRecordingID, profile)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}
	hasRemoteProvider := false
	hasRemoteCached := false
	remoteOnline := false
	for _, item := range items {
		if item.DeviceID == local.DeviceID {
			continue
		}
		if item.CachedOptimized {
			hasRemoteCached = true
		}
		if item.SourcePresent && canProvideLocalMedia(item.Role) {
			hasRemoteProvider = true
		}
		if item.LastSeenAt != nil && item.LastSeenAt.UTC().After(time.Now().UTC().Add(-availabilityOnlineWindow)) {
			remoteOnline = true
		}
	}
	switch {
	case hasRemoteCached || hasRemoteProvider:
		if !remoteOnline {
			status.Reason = apitypes.PlaybackUnavailableProviderOffline
		} else {
			status.Reason = apitypes.PlaybackUnavailableNetworkOff
		}
	default:
		status.Reason = apitypes.PlaybackUnavailableNoPath
	}
	return status, nil
}

func (s *PlaybackService) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}
	return s.preparePlaybackRecordingForLocalContext(ctx, local, recordingID, preferredProfile, purpose)
}

func (s *PlaybackService) PreparePlaybackTarget(ctx context.Context, target playbackcore.PlaybackTargetRef, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return s.PreparePlaybackRecording(ctx, playbackTargetInputID(target), preferredProfile, purpose)
}

func (s *PlaybackService) StartPreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	profile := s.resolvePlaybackProfile(preferredProfile)
	purpose = normalizePlaybackPreparationPurpose(purpose)
	jobID := playbackPreparationJobID(local.LibraryID, recordingID, profile, purpose)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindPreparePlayback,
		local.LibraryID,
		"queued playback preparation",
		"playback preparation canceled because the library is no longer active",
		func(runCtx context.Context) {
			_, _ = s.finishPlaybackPreparationForLocalContext(runCtx, local, recordingID, profile, purpose)
		},
	)
}

func (s *PlaybackService) preparePlaybackRecordingForLocalContext(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	purpose = normalizePlaybackPreparationPurpose(purpose)

	status, shouldPrepareAsync, err := s.inspectPlaybackPreparationStatus(ctx, local, recordingID, profile, purpose)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}
	if shouldPrepareAsync {
		jobID := playbackPreparationJobID(local.LibraryID, recordingID, profile, purpose)
		if _, err := s.app.startActiveLibraryJob(
			ctx,
			jobID,
			jobKindPreparePlayback,
			local.LibraryID,
			"queued playback preparation",
			"playback preparation canceled because the library is no longer active",
			func(runCtx context.Context) {
				_, _ = s.finishPlaybackPreparationForLocalContext(runCtx, local, recordingID, profile, purpose)
			},
		); err != nil {
			failed := status
			failed.Phase = apitypes.PlaybackPreparationFailed
			failed.UpdatedAt = time.Now().UTC()
			s.storePreparation(failed)
			return apitypes.PlaybackPreparationStatus{}, err
		}
		return status, nil
	}
	return s.finishPlaybackPreparationForLocalContext(ctx, local, recordingID, profile, purpose)
}

func (s *PlaybackService) inspectPlaybackPreparationStatus(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, bool, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	purpose = normalizePlaybackPreparationPurpose(purpose)
	status, err := s.inspectPlaybackRecording(ctx, local, recordingID, profile)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, false, err
	}
	status.Purpose = purpose
	if status.Phase == apitypes.PlaybackPreparationReady {
		return status, false, nil
	}
	switch status.Reason {
	case apitypes.PlaybackUnavailableNetworkOff, apitypes.PlaybackUnavailableProviderOffline:
		availability, err := s.GetRecordingAvailability(ctx, recordingID, profile)
		if err != nil {
			return apitypes.PlaybackPreparationStatus{}, false, err
		}
		switch availability.State {
		case apitypes.AvailabilityPlayableRemoteOpt:
			status.Phase = apitypes.PlaybackPreparationPreparingFetch
			status.SourceKind = apitypes.PlaybackSourceRemoteOpt
			status.UpdatedAt = time.Now().UTC()
			s.storePreparation(status)
			return status, true, nil
		case apitypes.AvailabilityWaitingProviderTranscode:
			status.Phase = apitypes.PlaybackPreparationPreparingTranscode
			status.SourceKind = apitypes.PlaybackSourceRemoteOpt
			status.UpdatedAt = time.Now().UTC()
			s.storePreparation(status)
			return status, true, nil
		}
	}
	return status, false, nil
}

func (s *PlaybackService) finishPlaybackPreparationForLocalContext(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	purpose = normalizePlaybackPreparationPurpose(purpose)
	job := s.app.jobs.Track(playbackPreparationJobID(local.LibraryID, recordingID, profile, purpose), jobKindPreparePlayback, local.LibraryID)
	job.Queued(0, "queued playback preparation")
	job.Running(0.35, "inspecting playback availability")

	status, err := s.inspectPlaybackRecording(ctx, local, recordingID, profile)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(1, "playback preparation canceled because the library is no longer active", nil)
			return apitypes.PlaybackPreparationStatus{}, err
		}
		job.Fail(1, "playback preparation failed", err)
		return apitypes.PlaybackPreparationStatus{}, err
	}
	status.Purpose = purpose
	if status.Phase != apitypes.PlaybackPreparationReady {
		switch status.Reason {
		case apitypes.PlaybackUnavailableNetworkOff, apitypes.PlaybackUnavailableProviderOffline:
			availability, availabilityErr := s.GetRecordingAvailability(ctx, recordingID, profile)
			if availabilityErr != nil {
				if errors.Is(availabilityErr, context.Canceled) {
					job.Fail(1, "playback preparation canceled because the library is no longer active", nil)
					return apitypes.PlaybackPreparationStatus{}, availabilityErr
				}
				job.Fail(1, "playback preparation failed", availabilityErr)
				return apitypes.PlaybackPreparationStatus{}, availabilityErr
			}
			switch availability.State {
			case apitypes.AvailabilityPlayableRemoteOpt:
				status.Phase = apitypes.PlaybackPreparationPreparingFetch
				status.SourceKind = apitypes.PlaybackSourceRemoteOpt
				s.storePreparation(status)
				job.Running(0.65, "fetching remote optimized asset")
				if _, fetched, fetchErr := s.ensureRemotePlaybackRecording(ctx, local, recordingID, profile); fetchErr != nil {
					status.Phase = apitypes.PlaybackPreparationFailed
					status.UpdatedAt = time.Now().UTC()
					s.storePreparation(status)
					if errors.Is(fetchErr, context.Canceled) {
						job.Fail(1, "playback preparation canceled because the library is no longer active", nil)
						return apitypes.PlaybackPreparationStatus{}, fetchErr
					}
					job.Fail(1, "playback preparation failed", fetchErr)
					return apitypes.PlaybackPreparationStatus{}, fetchErr
				} else {
					status, err = s.inspectPlaybackRecording(ctx, local, recordingID, profile)
					if err != nil {
						if errors.Is(err, context.Canceled) {
							job.Fail(1, "playback preparation canceled because the library is no longer active", nil)
							return apitypes.PlaybackPreparationStatus{}, err
						}
						job.Fail(1, "playback preparation failed", err)
						return apitypes.PlaybackPreparationStatus{}, err
					}
					_ = fetched
				}
			case apitypes.AvailabilityWaitingProviderTranscode:
				status.Phase = apitypes.PlaybackPreparationPreparingTranscode
				status.SourceKind = apitypes.PlaybackSourceRemoteOpt
				s.storePreparation(status)
				job.Running(0.65, "requesting provider transcode")
				if _, fetched, fetchErr := s.ensureRemotePlaybackRecording(ctx, local, recordingID, profile); fetchErr != nil {
					status.Phase = apitypes.PlaybackPreparationFailed
					status.UpdatedAt = time.Now().UTC()
					s.storePreparation(status)
					if errors.Is(fetchErr, context.Canceled) {
						job.Fail(1, "playback preparation canceled because the library is no longer active", nil)
						return apitypes.PlaybackPreparationStatus{}, fetchErr
					}
					job.Fail(1, "playback preparation failed", fetchErr)
					return apitypes.PlaybackPreparationStatus{}, fetchErr
				} else {
					status, err = s.inspectPlaybackRecording(ctx, local, recordingID, profile)
					if err != nil {
						if errors.Is(err, context.Canceled) {
							job.Fail(1, "playback preparation canceled because the library is no longer active", nil)
							return apitypes.PlaybackPreparationStatus{}, err
						}
						job.Fail(1, "playback preparation failed", err)
						return apitypes.PlaybackPreparationStatus{}, err
					}
					_ = fetched
				}
			}
		}
	}
	s.mu.Lock()
	s.preparations[s.preparationKey(recordingID, status.PreferredProfile)] = status
	s.mu.Unlock()
	if status.Phase == apitypes.PlaybackPreparationReady {
		job.Complete(1, playbackPreparationReadyMessage(status))
	} else {
		job.Fail(1, playbackPreparationUnavailableMessage(status), nil)
	}
	return status, nil
}

func normalizePlaybackPreparationPurpose(purpose apitypes.PlaybackPreparationPurpose) apitypes.PlaybackPreparationPurpose {
	if purpose == "" {
		return apitypes.PlaybackPreparationPlayNow
	}
	return purpose
}

func (s *PlaybackService) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	key := s.preparationKey(recordingID, profile)
	s.mu.Lock()
	status, ok := s.preparations[key]
	s.mu.Unlock()
	if ok {
		return status, nil
	}
	return s.InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackService) GetPlaybackTargetPreparation(ctx context.Context, target playbackcore.PlaybackTargetRef, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.GetPlaybackPreparation(ctx, playbackTargetInputID(target), preferredProfile)
}

func (s *PlaybackService) storePreparation(status apitypes.PlaybackPreparationStatus) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preparations[s.preparationKey(status.RecordingID, status.PreferredProfile)] = status
}

func (s *PlaybackService) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	status, err := s.PreparePlaybackRecording(ctx, recordingID, preferredProfile, apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		return apitypes.PlaybackResolveResult{}, err
	}
	result := apitypes.PlaybackResolveResult{
		RecordingID: strings.TrimSpace(recordingID),
		Profile:     status.PreferredProfile,
		SourceKind:  status.SourceKind,
		Reason:      status.Reason,
		PlayableURI: strings.TrimSpace(status.PlayableURI),
		EncodingID:  strings.TrimSpace(status.EncodingID),
		BlobID:      strings.TrimSpace(status.BlobID),
	}
	switch status.Phase {
	case apitypes.PlaybackPreparationReady:
		switch status.SourceKind {
		case apitypes.PlaybackSourceLocalFile:
			result.State = apitypes.AvailabilityPlayableLocalFile
		case apitypes.PlaybackSourceCachedOpt:
			result.State = apitypes.AvailabilityPlayableCachedOpt
		default:
			result.State = apitypes.AvailabilityPlayableRemoteOpt
		}
	default:
		if status.Reason == apitypes.PlaybackUnavailableNoPath {
			result.State = apitypes.AvailabilityUnavailableNoPath
		} else {
			result.State = apitypes.AvailabilityUnavailableProvider
		}
	}
	return result, nil
}

func (s *PlaybackService) ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	artwork.BlobID = strings.TrimSpace(artwork.BlobID)
	artwork.MIME = strings.TrimSpace(artwork.MIME)
	artwork.FileExt = normalizeArtworkFileExt(artwork.FileExt, artwork.MIME)
	artwork.Variant = strings.TrimSpace(artwork.Variant)
	result := apitypes.ArtworkResolveResult{Artwork: artwork}
	if artwork.BlobID == "" {
		return result, nil
	}
	if artwork.FileExt == "" {
		return result, nil
	}
	path, ok, err := s.app.blobs.ArtworkFilePath(artwork.BlobID, artwork.FileExt)
	if err != nil {
		return result, nil
	}
	if !ok {
		return result, nil
	}
	result.LocalPath = path
	result.Available = true
	return result, nil
}

func (s *PlaybackService) ResolveAlbumArtwork(ctx context.Context, albumID, variant string) (apitypes.RecordingArtworkResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	albumID = strings.TrimSpace(albumID)
	result := apitypes.RecordingArtworkResult{AlbumID: albumID}
	if albumID == "" {
		return result, nil
	}
	if libraryAlbumID, ok, err := s.app.catalog.albumClusterIDForVariant(ctx, local.LibraryID, albumID); err != nil {
		return apitypes.RecordingArtworkResult{}, err
	} else if ok {
		result.LibraryAlbumID = libraryAlbumID
	} else {
		result.LibraryAlbumID = albumID
	}
	variantAlbumID, ok, err := s.app.catalog.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	if ok {
		result.VariantAlbumID = variantAlbumID
	}
	ref, err := s.app.catalog.loadArtworkRef(ctx, local.LibraryID, "album", albumID, variant)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	if strings.TrimSpace(ref.BlobID) == "" && strings.TrimSpace(variantAlbumID) != "" && variantAlbumID != albumID {
		ref, err = s.app.catalog.loadArtworkRef(ctx, local.LibraryID, "album", variantAlbumID, variant)
		if err != nil {
			return apitypes.RecordingArtworkResult{}, err
		}
	}
	if strings.TrimSpace(ref.BlobID) == "" {
		return result, nil
	}
	resolved, err := s.ResolveArtworkRef(ctx, ref)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	result.Artwork = resolved.Artwork
	result.LocalPath = resolved.LocalPath
	result.Available = resolved.Available
	return result, nil
}

func (s *PlaybackService) ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	resolvedRecordingID, _, err := s.resolvePlaybackVariant(ctx, local, recordingID, "")
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	libraryRecordingID, _, err := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, resolvedRecordingID)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	variants, err := s.app.catalog.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, s.app.cfg.TranscodeProfile)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	albumID := ""
	for _, item := range variants {
		if item.TrackVariantID == resolvedRecordingID && strings.TrimSpace(item.AlbumVariantID) != "" {
			albumID = strings.TrimSpace(item.AlbumVariantID)
			break
		}
	}
	if albumID == "" && len(variants) > 0 {
		albumID = strings.TrimSpace(variants[0].AlbumVariantID)
	}
	result := apitypes.RecordingArtworkResult{
		RecordingID:        resolvedRecordingID,
		LibraryRecordingID: libraryRecordingID,
		VariantRecordingID: resolvedRecordingID,
		AlbumID:            albumID,
		VariantAlbumID:     albumID,
	}
	if albumID == "" {
		return result, nil
	}
	if libraryAlbumID, ok, err := s.app.catalog.albumClusterIDForVariant(ctx, local.LibraryID, albumID); err != nil {
		return apitypes.RecordingArtworkResult{}, err
	} else if ok {
		result.LibraryAlbumID = libraryAlbumID
	}
	ref, err := s.app.catalog.loadArtworkRef(ctx, local.LibraryID, "album", albumID, variant)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	if strings.TrimSpace(ref.BlobID) == "" {
		return result, nil
	}
	resolved, err := s.ResolveArtworkRef(ctx, ref)
	if err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	result.Artwork = resolved.Artwork
	result.LocalPath = resolved.LocalPath
	result.Available = resolved.Available
	return result, nil
}

func (s *PlaybackService) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return nil, err
	}
	aliasProfile := normalizedPlaybackProfileAlias(profile)
	type row struct {
		DeviceID         string
		Role             string
		PeerID           string
		LastSeenAt       *time.Time
		LastSyncSuccess  *time.Time
		SourcePresent    int
		OptimizedPresent int
		CachedOptimized  int
	}
	query := `
SELECT
	m.device_id,
	m.role,
	COALESCE(d.peer_id, '') AS peer_id,
	d.last_seen_at,
	pss.last_success_at,
	CASE WHEN EXISTS (
		SELECT 1 FROM source_files sf
		WHERE sf.library_id = m.library_id AND sf.device_id = m.device_id AND sf.is_present = 1 AND sf.track_variant_id = ?
	) THEN 1 ELSE 0 END AS source_present,
	CASE WHEN EXISTS (
		SELECT 1 FROM optimized_assets oa
		WHERE oa.library_id = m.library_id AND oa.created_by_device_id = m.device_id AND oa.track_variant_id = ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS optimized_present,
	CASE WHEN EXISTS (
		SELECT 1 FROM device_asset_caches dac
		JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
		WHERE dac.library_id = m.library_id AND dac.device_id = m.device_id AND dac.is_cached = 1 AND oa.track_variant_id = ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS cached_optimized
FROM memberships m
LEFT JOIN devices d ON d.device_id = m.device_id
LEFT JOIN peer_sync_states pss ON pss.library_id = m.library_id AND pss.device_id = m.device_id
WHERE m.library_id = ?
ORDER BY CASE WHEN m.device_id = ? THEN 0 ELSE 1 END, m.device_id ASC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query,
		resolvedRecordingID,
		resolvedRecordingID, profile, profile, aliasProfile,
		resolvedRecordingID, profile, profile, aliasProfile,
		local.LibraryID, local.DeviceID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]apitypes.RecordingAvailabilityItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, apitypes.RecordingAvailabilityItem{
			DeviceID:          strings.TrimSpace(row.DeviceID),
			Role:              strings.TrimSpace(row.Role),
			PeerID:            strings.TrimSpace(row.PeerID),
			LastSeenAt:        row.LastSeenAt,
			LastSyncSuccessAt: row.LastSyncSuccess,
			SourcePresent:     row.SourcePresent > 0,
			OptimizedPresent:  row.OptimizedPresent > 0,
			CachedOptimized:   row.CachedOptimized > 0,
		})
	}
	return out, nil
}

func (s *PlaybackService) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	libraryRecordingID := strings.TrimSpace(recordingID)
	if resolvedLibraryRecordingID, ok, resolveErr := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID); resolveErr == nil && ok && strings.TrimSpace(resolvedLibraryRecordingID) != "" {
		libraryRecordingID = strings.TrimSpace(resolvedLibraryRecordingID)
	}
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	out := apitypes.RecordingPlaybackAvailability{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: profile,
	}
	if pinned, pinErr := s.recordingScopePinned(ctx, local.LibraryID, local.DeviceID, libraryRecordingID, profile); pinErr != nil {
		return apitypes.RecordingPlaybackAvailability{}, pinErr
	} else {
		out.Pinned = pinned
	}
	if localPath, ok, err := s.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID); err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	} else if ok {
		out.State = apitypes.AvailabilityPlayableLocalFile
		out.SourceKind = apitypes.PlaybackSourceLocalFile
		out.LocalPath = localPath
		return out, nil
	}
	if _, _, ok, err := s.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile); err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	} else if ok {
		out.State = apitypes.AvailabilityPlayableCachedOpt
		out.SourceKind = apitypes.PlaybackSourceCachedOpt
		return out, nil
	}
	items, err := s.ListRecordingAvailability(ctx, resolvedRecordingID, profile)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	hasRemoteCached := false
	remoteCachedOnline := false
	providerFound := false
	providerOnline := false
	networkRunning := s.app.NetworkStatus().Running
	for _, item := range items {
		if item.DeviceID != local.DeviceID && item.CachedOptimized {
			hasRemoteCached = true
			if item.LastSeenAt != nil && item.LastSeenAt.UTC().After(time.Now().UTC().Add(-availabilityOnlineWindow)) {
				remoteCachedOnline = true
			}
		}
		if item.DeviceID == local.DeviceID {
			continue
		}
		if item.SourcePresent && canProvideLocalMedia(item.Role) {
			providerFound = true
			if item.LastSeenAt != nil && item.LastSeenAt.UTC().After(time.Now().UTC().Add(-availabilityOnlineWindow)) {
				providerOnline = true
			}
		}
	}
	switch {
	case hasRemoteCached && remoteCachedOnline && networkRunning:
		out.State = apitypes.AvailabilityPlayableRemoteOpt
		out.SourceKind = apitypes.PlaybackSourceRemoteOpt
	case hasRemoteCached && !remoteCachedOnline:
		out.State = apitypes.AvailabilityUnavailableProvider
		out.Reason = apitypes.PlaybackUnavailableProviderOffline
	case hasRemoteCached:
		out.State = apitypes.AvailabilityUnavailableProvider
		out.Reason = apitypes.PlaybackUnavailableNetworkOff
	case providerFound && providerOnline && networkRunning:
		out.State = apitypes.AvailabilityWaitingProviderTranscode
		out.SourceKind = apitypes.PlaybackSourceRemoteOpt
	case !providerFound:
		out.State = apitypes.AvailabilityUnavailableNoPath
		out.Reason = apitypes.PlaybackUnavailableNoPath
	case !providerOnline:
		out.State = apitypes.AvailabilityUnavailableProvider
		out.Reason = apitypes.PlaybackUnavailableProviderOffline
	default:
		out.State = apitypes.AvailabilityUnavailableProvider
		out.Reason = apitypes.PlaybackUnavailableNetworkOff
	}
	return out, nil
}

func (s *PlaybackService) GetPlaybackTargetAvailability(ctx context.Context, target playbackcore.PlaybackTargetRef, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return s.GetRecordingAvailability(ctx, playbackTargetInputID(target), preferredProfile)
}

func (s *PlaybackService) ListRecordingPlaybackAvailability(ctx context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	recordingIDs := compactNonEmptyStrings(req.RecordingIDs)
	if len(recordingIDs) == 0 {
		return []apitypes.RecordingPlaybackAvailability{}, nil
	}
	return s.batchRecordingPlaybackAvailability(ctx, local, recordingIDs, req.PreferredProfile)
}

func (s *PlaybackService) StartPinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	libraryRecordingID, resolvedRecordingID, profile, err := s.resolveRecordingPinTarget(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return JobSnapshot{}, err
	}
	if err := s.upsertOfflinePin(ctx, local, "recording", libraryRecordingID, profile); err != nil {
		return JobSnapshot{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "recording", libraryRecordingID, []string{libraryRecordingID})
	jobID := playbackPinOfflineJobID(local.LibraryID, "recording", libraryRecordingID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindPinRecordingOffline,
		local.LibraryID,
		"queued track pin",
		"track pin canceled because the library is no longer active",
		func(runCtx context.Context) {
			s.runRecordingPinOfflineJob(runCtx, local, libraryRecordingID, resolvedRecordingID, profile)
		},
	)
}

func (s *PlaybackService) StartPinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	scopeID, recordingIDs, profile, err := s.resolveOfflinePinScope(ctx, local, "album", albumID, preferredProfile)
	if err != nil {
		return JobSnapshot{}, err
	}
	if err := s.ensureAlbumPinEligible(ctx, local, recordingIDs, profile); err != nil {
		return JobSnapshot{}, err
	}
	if err := s.upsertOfflinePin(ctx, local, "album", scopeID, profile); err != nil {
		return JobSnapshot{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "album", scopeID, recordingIDs)
	jobID := playbackPinOfflineJobID(local.LibraryID, "album", scopeID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindPinAlbumOffline,
		local.LibraryID,
		"queued album pin",
		"album pin canceled because the library is no longer active",
		func(runCtx context.Context) {
			s.runScopePinOfflineJob(runCtx, local, "album", scopeID, profile, recordingIDs, jobKindPinAlbumOffline, "album pin")
		},
	)
}

func (s *PlaybackService) StartPinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	scopeID, recordingIDs, profile, err := s.resolveOfflinePinScope(ctx, local, "playlist", playlistID, preferredProfile)
	if err != nil {
		return JobSnapshot{}, err
	}
	if err := s.upsertOfflinePin(ctx, local, "playlist", scopeID, profile); err != nil {
		return JobSnapshot{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "playlist", scopeID, recordingIDs)
	jobID := playbackPinOfflineJobID(local.LibraryID, "playlist", scopeID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindPinPlaylistOffline,
		local.LibraryID,
		"queued playlist pin",
		"playlist pin canceled because the library is no longer active",
		func(runCtx context.Context) {
			s.runScopePinOfflineJob(runCtx, local, "playlist", scopeID, profile, recordingIDs, jobKindPinPlaylistOffline, "playlist pin")
		},
	)
}

func (s *PlaybackService) ListPlaybackTargetAvailability(ctx context.Context, req playbackcore.TargetAvailabilityRequest) ([]playbackcore.TargetAvailability, error) {
	recordingIDs := make([]string, 0, len(req.Targets))
	seen := make(map[string]struct{}, len(req.Targets))
	for _, target := range req.Targets {
		recordingID := playbackTargetInputID(target)
		if recordingID == "" {
			continue
		}
		if _, ok := seen[recordingID]; ok {
			continue
		}
		seen[recordingID] = struct{}{}
		recordingIDs = append(recordingIDs, recordingID)
	}
	items, err := s.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:     recordingIDs,
		PreferredProfile: req.PreferredProfile,
	})
	if err != nil {
		return nil, err
	}
	statusByRecordingID := make(map[string]apitypes.RecordingPlaybackAvailability, len(items))
	for _, item := range items {
		statusByRecordingID[strings.TrimSpace(item.RecordingID)] = item
	}
	out := make([]playbackcore.TargetAvailability, 0, len(req.Targets))
	for _, target := range req.Targets {
		recordingID := playbackTargetInputID(target)
		out = append(out, playbackcore.TargetAvailability{
			Target: target,
			Status: statusByRecordingID[recordingID],
		})
	}
	return out, nil
}

func (s *PlaybackService) ListAlbumAvailabilitySummaries(ctx context.Context, req apitypes.AlbumAvailabilitySummaryListRequest) ([]apitypes.AlbumAvailabilitySummaryItem, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	albumIDs := compactNonEmptyStrings(req.AlbumIDs)
	if len(albumIDs) == 0 {
		return []apitypes.AlbumAvailabilitySummaryItem{}, nil
	}
	summaries, err := s.albumAvailabilitySummaries(ctx, local, albumIDs, req.PreferredProfile)
	if err != nil {
		return nil, err
	}
	out := make([]apitypes.AlbumAvailabilitySummaryItem, 0, len(albumIDs))
	for _, albumID := range albumIDs {
		out = append(out, apitypes.AlbumAvailabilitySummaryItem{
			AlbumID:          albumID,
			PreferredProfile: req.PreferredProfile,
			Availability:     summaries[albumID],
		})
	}
	return out, nil
}

func (s *PlaybackService) batchRecordingPlaybackAvailability(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) ([]apitypes.RecordingPlaybackAvailability, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return []apitypes.RecordingPlaybackAvailability{}, nil
	}

	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, preferredProfile)
	if err != nil {
		return nil, err
	}
	localPaths, err := s.batchBestLocalRecordingPaths(ctx, local.LibraryID, local.DeviceID, resolution.resolvedRecordingIDs)
	if err != nil {
		return nil, err
	}
	cachedRecordings, err := s.batchBestCachedRecordingIDs(ctx, local.LibraryID, local.DeviceID, resolution.resolvedRecordingIDs, resolution.profile)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-availabilityOnlineWindow)
	networkRunning := s.app.NetworkStatus().Running
	facts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		resolution.resolvedRecordingIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		cutoff,
	)
	if err != nil {
		return nil, err
	}

	trackPins, err := s.batchTrackPins(ctx, local.LibraryID, local.DeviceID, recordingIDs, resolution.profile, normalizedPlaybackProfileAlias(resolution.profile))
	if err != nil {
		return nil, err
	}

	out := make([]apitypes.RecordingPlaybackAvailability, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		resolvedRecordingID := resolution.resolvedByRecording[recordingID]
		item := apitypes.RecordingPlaybackAvailability{
			RecordingID:      recordingID,
			PreferredProfile: resolution.profile,
			Pinned:           trackPins[recordingID],
		}

		if localPath := localPaths[resolvedRecordingID]; localPath != "" {
			item.State = apitypes.AvailabilityPlayableLocalFile
			item.SourceKind = apitypes.PlaybackSourceLocalFile
			item.LocalPath = localPath
			out = append(out, item)
			continue
		}
		if cachedRecordings[resolvedRecordingID] {
			item.State = apitypes.AvailabilityPlayableCachedOpt
			item.SourceKind = apitypes.PlaybackSourceCachedOpt
			out = append(out, item)
			continue
		}

		fact := facts[resolvedRecordingID]
		switch {
		case fact.hasRemoteCached && fact.hasRemoteCachedOnline && networkRunning:
			item.State = apitypes.AvailabilityPlayableRemoteOpt
			item.SourceKind = apitypes.PlaybackSourceRemoteOpt
		case fact.hasRemoteCached && !fact.hasRemoteCachedOnline:
			item.State = apitypes.AvailabilityUnavailableProvider
			item.Reason = apitypes.PlaybackUnavailableProviderOffline
		case fact.hasRemoteCached:
			item.State = apitypes.AvailabilityUnavailableProvider
			item.Reason = apitypes.PlaybackUnavailableNetworkOff
		case fact.hasRemoteSource && fact.hasRemoteSourceOnline && networkRunning:
			item.State = apitypes.AvailabilityWaitingProviderTranscode
			item.SourceKind = apitypes.PlaybackSourceRemoteOpt
		case !fact.hasRemoteSource:
			item.State = apitypes.AvailabilityUnavailableNoPath
			item.Reason = apitypes.PlaybackUnavailableNoPath
		case !fact.hasRemoteSourceOnline:
			item.State = apitypes.AvailabilityUnavailableProvider
			item.Reason = apitypes.PlaybackUnavailableProviderOffline
		default:
			item.State = apitypes.AvailabilityUnavailableProvider
			item.Reason = apitypes.PlaybackUnavailableNetworkOff
		}
		out = append(out, item)
	}

	return out, nil
}

func (s *PlaybackService) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	libraryRecordingID, resolvedRecordingID, profile, err := s.resolveRecordingPinTarget(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	result, err := s.prepareRecordingOfflineResult(ctx, local, resolvedRecordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	if err := s.upsertOfflinePin(ctx, local, "recording", libraryRecordingID, profile); err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "recording", libraryRecordingID, []string{libraryRecordingID})
	return result, nil
}

func (s *PlaybackService) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if resolvedRecordingID, ok, resolveErr := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID); resolveErr == nil && ok && strings.TrimSpace(resolvedRecordingID) != "" {
		recordingID = resolvedRecordingID
	}
	if err := s.deleteOfflinePin(ctx, local, "recording", recordingID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:         apitypes.CatalogChangeInvalidateAvailability,
		Entity:       apitypes.CatalogChangeEntityTracks,
		RecordingIDs: []string{recordingID},
	})
	return nil
}

func (s *PlaybackService) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	scopeID, recordingIDs, profile, err := s.resolveOfflinePinScope(ctx, local, "album", albumID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	if err := s.ensureAlbumPinEligible(ctx, local, recordingIDs, profile); err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	result, err := s.pinOfflineScope(ctx, local, "album", scopeID, recordingIDs, profile)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "album", scopeID, recordingIDs)
	return result, nil
}

func (s *PlaybackService) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if resolvedAlbumID, ok, resolveErr := s.app.catalog.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, albumID); resolveErr == nil && ok && strings.TrimSpace(resolvedAlbumID) != "" {
		albumID = resolvedAlbumID
	}
	if err := s.deleteOfflinePin(ctx, local, "album", albumID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateAvailability,
		Entity:   apitypes.CatalogChangeEntityAlbum,
		EntityID: albumID,
		AlbumIDs: []string{albumID},
	})
	return nil
}

func (s *PlaybackService) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	scopeID, recordingIDs, profile, err := s.resolveOfflinePinScope(ctx, local, "playlist", playlistID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	result, err := s.pinOfflineScope(ctx, local, "playlist", scopeID, recordingIDs, profile)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "playlist", scopeID, recordingIDs)
	return result, nil
}

func (s *PlaybackService) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if err := s.deleteOfflinePin(ctx, local, "playlist", playlistID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateAvailability,
		Entity:   apitypes.CatalogChangeEntityPlaylistTracks,
		EntityID: playlistID,
		QueryKey: "playlistTracks:" + playlistID,
	})
	return nil
}

func (s *PlaybackService) PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	playlistID := likedPlaylistIDForLibrary(local.LibraryID)
	recordingIDs, err := s.recordingIDsForPlaylist(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	result, err := s.pinOfflineScope(ctx, local, "playlist", playlistID, recordingIDs, preferredProfile)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	s.emitOfflinePinAvailabilityInvalidation(local, "playlist", playlistID, recordingIDs)
	return result, nil
}

func (s *PlaybackService) UnpinLikedOffline(ctx context.Context) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	playlistID := likedPlaylistIDForLibrary(local.LibraryID)
	if err := s.deleteOfflinePin(ctx, local, "playlist", playlistID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateAvailability,
		Entity:   apitypes.CatalogChangeEntityLiked,
		EntityID: playlistID,
		QueryKey: "liked",
	})
	return nil
}

func (s *PlaybackService) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.RecordingAvailabilityOverview{}, err
	}
	summary, playback, devices, err := s.recordingAvailabilitySummary(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.RecordingAvailabilityOverview{}, err
	}
	variants, err := s.app.catalog.ListRecordingVariants(ctx, apitypes.RecordingVariantListRequest{
		RecordingID: strings.TrimSpace(recordingID),
		PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		return apitypes.RecordingAvailabilityOverview{}, err
	}
	out := apitypes.RecordingAvailabilityOverview{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: s.resolvePlaybackProfile(preferredProfile),
		Playback:         playback,
		Availability:     summary,
		Devices:          devices,
	}
	for _, variant := range variants.Items {
		variantDevices, err := s.ListRecordingAvailability(ctx, variant.RecordingID, preferredProfile)
		if err != nil {
			return apitypes.RecordingAvailabilityOverview{}, err
		}
		out.Variants = append(out.Variants, apitypes.RecordingVariantAvailabilityOverview{
			Variant: variant,
			Devices: variantDevices,
		})
	}
	return out, nil
}

func (s *PlaybackService) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.AlbumAvailabilityOverview{}, err
	}
	tracks, err := s.app.catalog.ListAlbumTracks(ctx, apitypes.AlbumTrackListRequest{
		AlbumID:     strings.TrimSpace(albumID),
		PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		return apitypes.AlbumAvailabilityOverview{}, err
	}
	variants, err := s.app.catalog.ListAlbumVariants(ctx, apitypes.AlbumVariantListRequest{
		AlbumID:     strings.TrimSpace(albumID),
		PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		return apitypes.AlbumAvailabilityOverview{}, err
	}
	out := apitypes.AlbumAvailabilityOverview{
		AlbumID:          strings.TrimSpace(albumID),
		PreferredProfile: s.resolvePlaybackProfile(preferredProfile),
	}
	summaries, err := s.albumAvailabilitySummaries(ctx, local, []string{albumID}, preferredProfile)
	if err != nil {
		return apitypes.AlbumAvailabilityOverview{}, err
	}
	out.Availability = summaries[strings.TrimSpace(albumID)]
	for _, track := range tracks.Items {
		out.Tracks = append(out.Tracks, apitypes.AlbumTrackAvailabilityOverview{Track: track})
	}
	for _, variant := range variants.Items {
		out.Variants = append(out.Variants, apitypes.AlbumVariantAvailabilityOverview{Variant: variant})
	}
	return out, nil
}

func (s *PlaybackService) resolvePlaybackVariant(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (string, string, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "", "", fmt.Errorf("recording id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	if exact, ok, err := s.trackVariantExists(ctx, local.LibraryID, recordingID); err != nil {
		return "", "", err
	} else if ok {
		return exact, profile, nil
	}
	variants, err := s.app.catalog.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, recordingID, profile)
	if err != nil {
		return "", "", err
	}
	explicitPreferredID, _, err := s.app.catalog.preferredRecordingVariantID(ctx, local.LibraryID, local.DeviceID, recordingID)
	if err != nil {
		return "", "", err
	}
	if preferredID := chooseRecordingVariantID(variants, explicitPreferredID); preferredID != "" {
		return preferredID, profile, nil
	}
	return recordingID, profile, nil
}

func (s *PlaybackService) resolvePlaybackProfile(preferredProfile string) string {
	preferredProfile = strings.TrimSpace(preferredProfile)
	if preferredProfile != "" {
		return preferredProfile
	}
	return strings.TrimSpace(s.app.cfg.TranscodeProfile)
}

func playbackTargetInputID(target playbackcore.PlaybackTargetRef) string {
	switch target.ResolutionPolicy {
	case playbackcore.PlaybackTargetResolutionExact:
		return firstNonEmptyString(
			target.ExactVariantRecordingID,
			target.LogicalRecordingID,
		)
	default:
		return firstNonEmptyString(
			target.LogicalRecordingID,
			target.ExactVariantRecordingID,
		)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

type recordingBatchResolution struct {
	profile              string
	resolvedByRecording  map[string]string
	resolvedRecordingIDs []string
}

func (s *PlaybackService) resolvePlaybackVariantsBatch(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) (recordingBatchResolution, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	profile := s.resolvePlaybackProfile(preferredProfile)
	if len(recordingIDs) == 0 {
		return recordingBatchResolution{
			profile:              profile,
			resolvedByRecording:  map[string]string{},
			resolvedRecordingIDs: []string{},
		}, nil
	}

	type row struct {
		VariantID string
		ClusterID string
	}
	var rows []row
	if err := s.app.storage.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Select("track_variant_id AS variant_id, track_cluster_id AS cluster_id").
		Where("library_id = ? AND (track_variant_id IN ? OR track_cluster_id IN ?)", local.LibraryID, recordingIDs, recordingIDs).
		Scan(&rows).Error; err != nil {
		return recordingBatchResolution{}, err
	}

	clusterByRecording := make(map[string]string, len(rows))
	exactVariantIDs := make(map[string]struct{}, len(rows))
	clusterIDs := make([]string, 0, len(rows))
	clusterSeen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		recordingID := strings.TrimSpace(row.VariantID)
		clusterID := strings.TrimSpace(row.ClusterID)
		if recordingID == "" || clusterID == "" {
			continue
		}
		clusterByRecording[recordingID] = clusterID
		exactVariantIDs[recordingID] = struct{}{}
		if _, ok := clusterByRecording[clusterID]; !ok {
			clusterByRecording[clusterID] = clusterID
		}
		if _, ok := clusterSeen[clusterID]; ok {
			continue
		}
		clusterSeen[clusterID] = struct{}{}
		clusterIDs = append(clusterIDs, clusterID)
	}

	variantsByCluster, err := s.app.catalog.listRecordingVariantRowsForClusters(ctx, local.LibraryID, local.DeviceID, clusterIDs, profile)
	if err != nil {
		return recordingBatchResolution{}, err
	}
	preferredByCluster, err := s.app.catalog.preferredRecordingVariantIDsForClusters(ctx, local.LibraryID, local.DeviceID, clusterIDs)
	if err != nil {
		return recordingBatchResolution{}, err
	}

	resolvedByRecording := make(map[string]string, len(recordingIDs))
	resolvedRecordingIDs := make([]string, 0, len(recordingIDs))
	resolvedSeen := make(map[string]struct{}, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		resolvedID := recordingID
		if _, ok := exactVariantIDs[recordingID]; ok {
			resolvedID = recordingID
		} else if clusterID := clusterByRecording[recordingID]; clusterID != "" {
			if preferredID := chooseRecordingVariantID(variantsByCluster[clusterID], preferredByCluster[clusterID]); preferredID != "" {
				resolvedID = preferredID
			}
		}
		resolvedByRecording[recordingID] = resolvedID
		if _, ok := resolvedSeen[resolvedID]; ok {
			continue
		}
		resolvedSeen[resolvedID] = struct{}{}
		resolvedRecordingIDs = append(resolvedRecordingIDs, resolvedID)
	}

	return recordingBatchResolution{
		profile:              profile,
		resolvedByRecording:  resolvedByRecording,
		resolvedRecordingIDs: resolvedRecordingIDs,
	}, nil
}

func (s *PlaybackService) trackVariantExists(ctx context.Context, libraryID, recordingID string) (string, bool, error) {
	var row TrackVariantModel
	if err := s.app.storage.WithContext(ctx).
		Select("track_variant_id").
		Where("library_id = ? AND track_variant_id = ?", libraryID, strings.TrimSpace(recordingID)).
		Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(row.TrackVariantID), true, nil
}

func (s *PlaybackService) preparationKey(recordingID, profile string) string {
	return strings.TrimSpace(recordingID) + "|" + strings.TrimSpace(profile)
}

func playbackPreparationJobID(libraryID, recordingID, profile string, purpose apitypes.PlaybackPreparationPurpose) string {
	return strings.TrimSpace(libraryID) + "|prepare-playback|" + strings.TrimSpace(recordingID) + "|" + strings.TrimSpace(profile) + "|" + strings.TrimSpace(string(purpose))
}

func playbackEnsureRecordingEncodingJobID(libraryID, recordingID, profile string) string {
	return strings.TrimSpace(libraryID) + "|ensure-recording-encoding|" + strings.TrimSpace(recordingID) + "|" + strings.TrimSpace(profile)
}

func playbackEnsureScopeEncodingsJobID(libraryID, scope, scopeID, profile string) string {
	return strings.TrimSpace(libraryID) + "|ensure-" + strings.TrimSpace(scope) + "-encodings|" + strings.TrimSpace(scopeID) + "|" + strings.TrimSpace(profile)
}

func ensureEncodingBatchMessage(result apitypes.EnsureEncodingBatchResult, scopeLabel string) string {
	scopeLabel = strings.TrimSpace(scopeLabel)
	switch {
	case result.Recordings == 0:
		return "no recordings required encoding"
	case result.Created == result.Recordings:
		return scopeLabel + " completed"
	case result.Created == 0:
		return scopeLabel + " already satisfied"
	default:
		return fmt.Sprintf("%s encoded %d of %d recordings", scopeLabel, result.Created, result.Recordings)
	}
}

func playbackPreparationReadyMessage(status apitypes.PlaybackPreparationStatus) string {
	switch status.SourceKind {
	case apitypes.PlaybackSourceLocalFile:
		return "playback ready from local file"
	case apitypes.PlaybackSourceCachedOpt:
		return "playback ready from cached optimized asset"
	default:
		return "playback ready"
	}
}

func playbackPreparationUnavailableMessage(status apitypes.PlaybackPreparationStatus) string {
	switch status.Reason {
	case apitypes.PlaybackUnavailableProviderOffline:
		return "playback unavailable: provider offline"
	case apitypes.PlaybackUnavailableNetworkOff:
		return "playback unavailable: network fetch required"
	case apitypes.PlaybackUnavailableNoPath:
		return "playback unavailable: no playable source"
	default:
		return "playback unavailable"
	}
}

func (s *PlaybackService) exactTrackVariantIDsSet(ctx context.Context, libraryID string, recordingIDs []string) (map[string]struct{}, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]struct{}{}, nil
	}

	type row struct{ RecordingID string }
	var rows []row
	if err := s.app.storage.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Select("track_variant_id AS recording_id").
		Where("library_id = ? AND track_variant_id IN ?", libraryID, recordingIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		recordingID := strings.TrimSpace(row.RecordingID)
		if recordingID == "" {
			continue
		}
		out[recordingID] = struct{}{}
	}
	return out, nil
}

func (s *PlaybackService) isExactTrackVariantID(ctx context.Context, libraryID, recordingID string) (bool, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return false, nil
	}
	exactIDs, err := s.exactTrackVariantIDsSet(ctx, libraryID, []string{recordingID})
	if err != nil {
		return false, err
	}
	_, ok := exactIDs[recordingID]
	return ok, nil
}

func (s *PlaybackService) bestLocalRecordingPath(ctx context.Context, libraryID, deviceID, recordingID string) (string, bool, error) {
	type localPathRow struct{ LocalPath string }
	exactVariant, err := s.isExactTrackVariantID(ctx, libraryID, recordingID)
	if err != nil {
		return "", false, err
	}
	query := `
SELECT sf.local_path
FROM source_files sf
JOIN track_variants req ON req.library_id = sf.library_id
JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
WHERE sf.library_id = ? AND sf.device_id = ? AND sf.is_present = 1 AND req.track_variant_id = ? AND cand.track_cluster_id = req.track_cluster_id
ORDER BY CASE WHEN sf.track_variant_id = ? THEN 0 ELSE 1 END ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC
LIMIT 1`
	args := []any{libraryID, deviceID, recordingID, recordingID}
	if exactVariant {
		query = `
SELECT sf.local_path
FROM source_files sf
WHERE sf.library_id = ? AND sf.device_id = ? AND sf.is_present = 1 AND sf.track_variant_id = ?
ORDER BY sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC
LIMIT 1`
		args = []any{libraryID, deviceID, recordingID}
	}
	var result localPathRow
	if err := s.app.storage.WithContext(ctx).Raw(query, args...).Scan(&result).Error; err != nil {
		return "", false, err
	}
	if strings.TrimSpace(result.LocalPath) == "" {
		return "", false, nil
	}
	if _, err := os.Stat(result.LocalPath); err != nil {
		return "", false, nil
	}
	return result.LocalPath, true, nil
}

func (s *PlaybackService) bestCachedEncoding(ctx context.Context, libraryID, deviceID, recordingID, profile string) (string, string, bool, error) {
	aliasProfile := normalizedPlaybackProfileAlias(profile)
	exactVariant, err := s.isExactTrackVariantID(ctx, libraryID, recordingID)
	if err != nil {
		return "", "", false, err
	}
	type encodingRow struct {
		BlobID           string
		OptimizedAssetID string
	}
	query := `
SELECT
	e.blob_id,
	e.optimized_asset_id AS optimized_asset_id
FROM optimized_assets e
JOIN source_files sf ON sf.library_id = e.library_id AND sf.source_file_id = e.source_file_id
JOIN track_variants req ON req.library_id = e.library_id AND req.track_variant_id = ?
JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
LEFT JOIN device_asset_caches de ON de.library_id = ? AND de.optimized_asset_id = e.optimized_asset_id AND de.device_id = ?
WHERE e.library_id = ? AND cand.track_cluster_id = req.track_cluster_id AND COALESCE(de.is_cached, 0) = 1 AND (? = '' OR e.profile = ? OR e.profile = ?)
ORDER BY CASE WHEN sf.track_variant_id = ? THEN 0 ELSE 1 END ASC, e.bitrate DESC, e.optimized_asset_id ASC
LIMIT 1`
	args := []any{recordingID, libraryID, deviceID, libraryID, profile, profile, aliasProfile, recordingID}
	if exactVariant {
		query = `
SELECT
	e.blob_id,
	e.optimized_asset_id AS optimized_asset_id
FROM optimized_assets e
JOIN source_files sf ON sf.library_id = e.library_id AND sf.source_file_id = e.source_file_id
LEFT JOIN device_asset_caches de ON de.library_id = e.library_id AND de.optimized_asset_id = e.optimized_asset_id AND de.device_id = ?
WHERE e.library_id = ? AND sf.track_variant_id = ? AND COALESCE(de.is_cached, 0) = 1 AND (? = '' OR e.profile = ? OR e.profile = ?)
ORDER BY e.bitrate DESC, e.optimized_asset_id ASC
LIMIT 1`
		args = []any{deviceID, libraryID, recordingID, profile, profile, aliasProfile}
	}
	var result encodingRow
	if err := s.app.storage.WithContext(ctx).Raw(query, args...).Scan(&result).Error; err != nil {
		return "", "", false, err
	}
	if strings.TrimSpace(result.BlobID) == "" {
		return "", "", false, nil
	}
	if _, err := s.pathForBlob(result.BlobID); err != nil {
		return "", "", false, nil
	}
	return strings.TrimSpace(result.BlobID), strings.TrimSpace(result.OptimizedAssetID), true, nil
}

func (s *PlaybackService) batchBestLocalRecordingPaths(ctx context.Context, libraryID, deviceID string, recordingIDs []string) (map[string]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]string{}, nil
	}
	exactVariantIDs, err := s.exactTrackVariantIDsSet(ctx, libraryID, recordingIDs)
	if err != nil {
		return nil, err
	}
	exactIDs := make([]string, 0, len(recordingIDs))
	clusterIDs := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		if _, ok := exactVariantIDs[recordingID]; ok {
			exactIDs = append(exactIDs, recordingID)
			continue
		}
		clusterIDs = append(clusterIDs, recordingID)
	}

	type row struct {
		RecordingID string
		LocalPath   string
	}
	rows := make([]row, 0, len(recordingIDs))
	if len(exactIDs) > 0 {
		exactQuery := `
SELECT
	sf.track_variant_id AS recording_id,
	sf.local_path
FROM source_files sf
WHERE sf.library_id = ? AND sf.track_variant_id IN ? AND sf.device_id = ? AND sf.is_present = 1
ORDER BY sf.track_variant_id ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC`
		var exactRows []row
		if err := s.app.storage.WithContext(ctx).Raw(exactQuery, libraryID, exactIDs, deviceID).Scan(&exactRows).Error; err != nil {
			return nil, err
		}
		rows = append(rows, exactRows...)
	}
	if len(clusterIDs) > 0 {
		query := `
SELECT
	req.track_variant_id AS recording_id,
	sf.local_path
FROM track_variants req
JOIN track_variants cand ON cand.library_id = req.library_id AND cand.track_cluster_id = req.track_cluster_id
JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = cand.track_variant_id
WHERE req.library_id = ? AND req.track_variant_id IN ? AND sf.device_id = ? AND sf.is_present = 1
ORDER BY req.track_variant_id ASC, CASE WHEN sf.track_variant_id = req.track_variant_id THEN 0 ELSE 1 END ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC`
		var clusterRows []row
		if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, clusterIDs, deviceID).Scan(&clusterRows).Error; err != nil {
			return nil, err
		}
		rows = append(rows, clusterRows...)
	}

	out := make(map[string]string, len(recordingIDs))
	for _, row := range rows {
		recordingID := strings.TrimSpace(row.RecordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := out[recordingID]; ok {
			continue
		}
		localPath := strings.TrimSpace(row.LocalPath)
		if localPath == "" {
			continue
		}
		if _, err := os.Stat(localPath); err != nil {
			continue
		}
		out[recordingID] = localPath
	}
	return out, nil
}

func (s *PlaybackService) batchBestCachedRecordingIDs(ctx context.Context, libraryID, deviceID string, recordingIDs []string, profile string) (map[string]bool, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]bool{}, nil
	}

	aliasProfile := normalizedPlaybackProfileAlias(profile)
	exactVariantIDs, err := s.exactTrackVariantIDsSet(ctx, libraryID, recordingIDs)
	if err != nil {
		return nil, err
	}
	exactIDs := make([]string, 0, len(recordingIDs))
	clusterIDs := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		if _, ok := exactVariantIDs[recordingID]; ok {
			exactIDs = append(exactIDs, recordingID)
			continue
		}
		clusterIDs = append(clusterIDs, recordingID)
	}

	type row struct {
		RecordingID string
		BlobID      string
	}
	rows := make([]row, 0, len(recordingIDs))
	if len(exactIDs) > 0 {
		exactQuery := `
SELECT
	sf.track_variant_id AS recording_id,
	e.blob_id
FROM optimized_assets e
JOIN source_files sf ON sf.library_id = e.library_id AND sf.source_file_id = e.source_file_id
LEFT JOIN device_asset_caches de ON de.library_id = e.library_id AND de.optimized_asset_id = e.optimized_asset_id AND de.device_id = ?
WHERE e.library_id = ? AND sf.track_variant_id IN ? AND COALESCE(de.is_cached, 0) = 1 AND (? = '' OR e.profile = ? OR e.profile = ?)
ORDER BY sf.track_variant_id ASC, e.bitrate DESC, e.optimized_asset_id ASC`
		var exactRows []row
		if err := s.app.storage.WithContext(ctx).Raw(exactQuery, deviceID, libraryID, exactIDs, profile, profile, aliasProfile).Scan(&exactRows).Error; err != nil {
			return nil, err
		}
		rows = append(rows, exactRows...)
	}
	if len(clusterIDs) > 0 {
		query := `
SELECT
	req.track_variant_id AS recording_id,
	e.blob_id
FROM track_variants req
JOIN track_variants cand ON cand.library_id = req.library_id AND cand.track_cluster_id = req.track_cluster_id
JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = cand.track_variant_id
JOIN optimized_assets e ON e.library_id = sf.library_id AND e.source_file_id = sf.source_file_id
LEFT JOIN device_asset_caches de ON de.library_id = e.library_id AND de.optimized_asset_id = e.optimized_asset_id AND de.device_id = ?
WHERE req.library_id = ? AND req.track_variant_id IN ? AND COALESCE(de.is_cached, 0) = 1 AND (? = '' OR e.profile = ? OR e.profile = ?)
ORDER BY req.track_variant_id ASC, CASE WHEN sf.track_variant_id = req.track_variant_id THEN 0 ELSE 1 END ASC, e.bitrate DESC, e.optimized_asset_id ASC`
		var clusterRows []row
		if err := s.app.storage.WithContext(ctx).Raw(query, deviceID, libraryID, clusterIDs, profile, profile, aliasProfile).Scan(&clusterRows).Error; err != nil {
			return nil, err
		}
		rows = append(rows, clusterRows...)
	}

	out := make(map[string]bool, len(recordingIDs))
	for _, row := range rows {
		recordingID := strings.TrimSpace(row.RecordingID)
		if recordingID == "" {
			continue
		}
		if out[recordingID] {
			continue
		}
		blobID := strings.TrimSpace(row.BlobID)
		if blobID == "" {
			continue
		}
		path, err := s.pathForBlob(blobID)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out[recordingID] = true
	}
	return out, nil
}

func (s *PlaybackService) pathForBlob(blobID string) (string, error) {
	return s.app.blobs.Path(blobID)
}

func (s *PlaybackService) fileURIForBlob(blobID string) (string, error) {
	path, err := s.pathForBlob(blobID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return fileURIFromPath(path)
}

func (s *PlaybackService) resolveRecordingPinTarget(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (string, string, string, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "", "", "", fmt.Errorf("recording id is required")
	}
	libraryRecordingID, ok, err := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID)
	if err != nil {
		return "", "", "", err
	}
	if !ok {
		return "", "", "", fmt.Errorf("recording %s not found", recordingID)
	}
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return "", "", "", err
	}
	return strings.TrimSpace(libraryRecordingID), resolvedRecordingID, profile, nil
}

func (s *PlaybackService) resolveOfflinePinScope(ctx context.Context, local apitypes.LocalContext, scope, scopeID, preferredProfile string) (string, []string, string, error) {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	profile := s.resolvePlaybackProfile(preferredProfile)
	switch scope {
	case "album":
		resolvedAlbumID, recordingIDs, err := s.resolveAlbumOfflineScope(ctx, local, scopeID)
		if err != nil {
			return "", nil, "", err
		}
		return resolvedAlbumID, recordingIDs, profile, nil
	case "playlist":
		if scopeID == "" {
			return "", nil, "", fmt.Errorf("playlist id is required")
		}
		recordingIDs, err := s.recordingIDsForPlaylist(ctx, local.LibraryID, scopeID)
		if err != nil {
			return "", nil, "", err
		}
		return scopeID, recordingIDs, profile, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported offline pin scope %q", scope)
	}
}

func (s *PlaybackService) resolveAlbumOfflineScope(ctx context.Context, local apitypes.LocalContext, albumID string) (string, []string, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return "", nil, fmt.Errorf("album id is required")
	}

	resolvedAlbumID, ok, err := s.app.catalog.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return "", nil, err
	}
	if !ok || strings.TrimSpace(resolvedAlbumID) == "" {
		return "", nil, fmt.Errorf("album %s not found", albumID)
	}

	recordingIDs, err := s.recordingIDsForAlbum(ctx, local.LibraryID, local.DeviceID, strings.TrimSpace(resolvedAlbumID))
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(resolvedAlbumID), recordingIDs, nil
}

func (s *PlaybackService) ensureAlbumPinEligible(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) error {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil
	}

	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, preferredProfile)
	if err != nil {
		return err
	}
	facts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		resolution.resolvedRecordingIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		time.Now().UTC().Add(-availabilityOnlineWindow),
	)
	if err != nil {
		return err
	}
	for _, recordingID := range resolution.resolvedRecordingIDs {
		if !facts[strings.TrimSpace(recordingID)].hasLocalSource {
			return nil
		}
	}
	return fmt.Errorf("local albums do not need offline pinning")
}

func (s *PlaybackService) emitOfflinePinAvailabilityInvalidation(local apitypes.LocalContext, scope, scopeID string, recordingIDs []string) {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	switch scope {
	case "recording":
		s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
			Kind:         apitypes.CatalogChangeInvalidateAvailability,
			Entity:       apitypes.CatalogChangeEntityTracks,
			RecordingIDs: recordingIDs,
		})
	case "album":
		s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
			Kind:         apitypes.CatalogChangeInvalidateAvailability,
			Entity:       apitypes.CatalogChangeEntityAlbum,
			EntityID:     scopeID,
			AlbumIDs:     []string{scopeID},
			RecordingIDs: recordingIDs,
		})
	case "playlist":
		if scopeID == likedPlaylistIDForLibrary(local.LibraryID) {
			s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
				Kind:         apitypes.CatalogChangeInvalidateAvailability,
				Entity:       apitypes.CatalogChangeEntityLiked,
				EntityID:     scopeID,
				QueryKey:     "liked",
				RecordingIDs: recordingIDs,
			})
			return
		}
		s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
			Kind:         apitypes.CatalogChangeInvalidateAvailability,
			Entity:       apitypes.CatalogChangeEntityPlaylistTracks,
			EntityID:     scopeID,
			QueryKey:     "playlistTracks:" + scopeID,
			RecordingIDs: recordingIDs,
		})
	}
}

func (s *PlaybackService) runRecordingPinOfflineJob(ctx context.Context, local apitypes.LocalContext, libraryRecordingID, resolvedRecordingID, profile string) {
	jobID := playbackPinOfflineJobID(local.LibraryID, "recording", libraryRecordingID, profile)
	job := s.app.jobs.Track(jobID, jobKindPinRecordingOffline, local.LibraryID)
	if job == nil {
		return
	}
	job.Queued(0, "queued track pin")
	job.Running(0.1, "Cached 0/1 tracks")
	if _, err := s.prepareRecordingOfflineResult(ctx, local, resolvedRecordingID, profile); err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(0, "track pin canceled", nil)
			return
		}
		job.Fail(0, "track pin failed", err)
		return
	}
	job.Complete(1, "Track pinned for offline playback")
	s.emitOfflinePinAvailabilityInvalidation(local, "recording", libraryRecordingID, []string{libraryRecordingID})
}

func (s *PlaybackService) runScopePinOfflineJob(ctx context.Context, local apitypes.LocalContext, scope, scopeID, profile string, recordingIDs []string, kind, label string) {
	jobID := playbackPinOfflineJobID(local.LibraryID, scope, scopeID, profile)
	job := s.app.jobs.Track(jobID, kind, local.LibraryID)
	if job == nil {
		return
	}
	job.Queued(0, "queued "+label)
	result, err := s.pinOfflineScopeWithJob(ctx, local, scope, scopeID, recordingIDs, profile, job)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(0, label+" canceled", nil)
			return
		}
		job.Fail(pinJobProgress(result.Tracks, len(compactNonEmptyStrings(recordingIDs))), label+" failed", err)
		return
	}
	if result.Tracks == 1 {
		job.Complete(1, "Pinned 1 track offline")
	} else {
		job.Complete(1, fmt.Sprintf("Pinned %d tracks offline", result.Tracks))
	}
	s.emitOfflinePinAvailabilityInvalidation(local, scope, scopeID, recordingIDs)
}

func (s *PlaybackService) pinOfflineScope(ctx context.Context, local apitypes.LocalContext, scope, scopeID string, recordingIDs []string, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	return s.pinOfflineScopeWithJob(ctx, local, scope, scopeID, recordingIDs, profile, nil)
}

func (s *PlaybackService) pinOfflineScopeWithJob(ctx context.Context, local apitypes.LocalContext, scope, scopeID string, recordingIDs []string, profile string, job *JobTracker) (apitypes.PlaybackBatchResult, error) {
	seenRecordings := make(map[string]struct{}, len(recordingIDs))
	uniqueRecordings := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		recordingID = strings.TrimSpace(recordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := seenRecordings[recordingID]; ok {
			continue
		}
		seenRecordings[recordingID] = struct{}{}
		uniqueRecordings = append(uniqueRecordings, recordingID)
	}
	if err := s.upsertOfflinePin(ctx, local, scope, scopeID, profile); err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	if job != nil {
		job.Running(0.05, fmt.Sprintf("Cached 0/%d tracks", len(uniqueRecordings)))
	}
	if len(uniqueRecordings) == 0 {
		return apitypes.PlaybackBatchResult{}, nil
	}

	type resultItem struct {
		result apitypes.PlaybackRecordingResult
		err    error
	}

	workerCount := 1
	if job != nil {
		workerCount = pinnedScopeWorkerCount
	}
	if len(uniqueRecordings) < workerCount {
		workerCount = len(uniqueRecordings)
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	workCh := make(chan string)
	resultCh := make(chan resultItem, len(uniqueRecordings))
	var workers sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for recordingID := range workCh {
				resolvedRecordingID, _, err := s.resolvePlaybackVariant(workCtx, local, recordingID, profile)
				if err != nil {
					resultCh <- resultItem{err: err}
					cancel()
					continue
				}
				result, err := s.prepareRecordingOfflineResult(workCtx, local, resolvedRecordingID, profile)
				if err != nil {
					resultCh <- resultItem{err: err}
					cancel()
					continue
				}
				resultCh <- resultItem{result: result}
			}
		}()
	}

	go func() {
		stopped := false
		for _, recordingID := range uniqueRecordings {
			if stopped {
				break
			}
			select {
			case <-workCtx.Done():
				stopped = true
			case workCh <- recordingID:
			}
		}
		close(workCh)
		workers.Wait()
		close(resultCh)
	}()

	out := apitypes.PlaybackBatchResult{}
	completed := 0
	var firstErr error
	for item := range resultCh {
		if item.err != nil && firstErr == nil {
			firstErr = item.err
		}
		if item.err != nil {
			continue
		}
		completed++
		out.Tracks++
		out.TotalBytes += int64(item.result.Bytes)
		if item.result.FromLocal {
			out.LocalHits++
		} else {
			out.RemoteFetches++
		}
		if job != nil {
			job.Running(pinJobProgress(completed, len(uniqueRecordings)), fmt.Sprintf("Cached %d/%d tracks", completed, len(uniqueRecordings)))
		}
	}
	if firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

func (s *PlaybackService) prepareRecordingOfflineResult(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (apitypes.PlaybackRecordingResult, error) {
	if _, err := s.app.transcode.EnsureRecordingEncoding(ctx, local, recordingID, profile, local.DeviceID); err != nil && !errors.Is(err, ErrProviderOnlyTranscode) {
		return apitypes.PlaybackRecordingResult{}, err
	}

	blobID, encodingID, ok, err := s.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, recordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	if ok {
		path, err := s.pathForBlob(blobID)
		if err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		var asset OptimizedAssetModel
		if err := s.app.storage.WithContext(ctx).
			Where("library_id = ? AND optimized_asset_id = ?", local.LibraryID, encodingID).
			Take(&asset).Error; err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		return apitypes.PlaybackRecordingResult{
			EncodingID: encodingID,
			BlobID:     blobID,
			Profile:    strings.TrimSpace(asset.Profile),
			Bitrate:    asset.Bitrate,
			Bytes:      int(info.Size()),
			FromLocal:  true,
			SourceKind: apitypes.PlaybackSourceCachedOpt,
		}, nil
	}

	if localPath, ok, err := s.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, recordingID); err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	} else if ok {
		info, err := os.Stat(localPath)
		if err != nil {
			return apitypes.PlaybackRecordingResult{}, err
		}
		return apitypes.PlaybackRecordingResult{
			Profile:    profile,
			Bytes:      int(info.Size()),
			FromLocal:  true,
			SourceKind: apitypes.PlaybackSourceLocalFile,
			LocalPath:  localPath,
		}, nil
	}

	if result, fetched, err := s.ensureRemotePlaybackRecording(ctx, local, recordingID, profile); err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	} else if fetched {
		return result, nil
	}

	return apitypes.PlaybackRecordingResult{}, fmt.Errorf("recording %s has no local or cached asset available for offline pinning", recordingID)
}

func (s *PlaybackService) upsertOfflinePin(ctx context.Context, local apitypes.LocalContext, scope, scopeID, profile string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scope == "" || scopeID == "" {
		return fmt.Errorf("offline pin scope and scope id are required")
	}
	now := time.Now().UTC()
	return s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing OfflinePin
		err := tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
			Take(&existing).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		profile = strings.TrimSpace(profile)
		if err == nil && strings.TrimSpace(existing.Profile) == profile {
			return nil
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "device_id"},
				{Name: "scope"},
				{Name: "scope_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"profile", "updated_at"}),
		}).Create(&OfflinePin{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
			Scope:     scope,
			ScopeID:   scopeID,
			Profile:   profile,
			CreatedAt: now,
			UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		_, err = s.app.appendLocalOplogTx(tx, local, entityTypeOfflinePin, offlinePinEntityID(local.DeviceID, scope, scopeID), "upsert", offlinePinOplogPayload{
			DeviceID:    local.DeviceID,
			Scope:       scope,
			ScopeID:     scopeID,
			Profile:     profile,
			UpdatedAtNS: now.UnixNano(),
		})
		return err
	})
}

func (s *PlaybackService) deleteOfflinePin(ctx context.Context, local apitypes.LocalContext, scope, scopeID string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return fmt.Errorf("%s id is required", scope)
	}
	return s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
			Delete(&OfflinePin{})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		_, err := s.app.appendLocalOplogTx(tx, local, entityTypeOfflinePin, offlinePinEntityID(local.DeviceID, scope, scopeID), "delete", offlinePinOplogPayload{
			DeviceID: local.DeviceID,
			Scope:    scope,
			ScopeID:  scopeID,
		})
		return err
	})
}

func (s *PlaybackService) recordingIDsForAlbum(ctx context.Context, libraryID, deviceID, albumID string) ([]string, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return nil, nil
	}
	var explicitAlbum AlbumVariantModel
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).Take(&explicitAlbum).Error; err == nil {
		type row struct{ RecordingID string }
		var rows []row
		if err := s.app.storage.WithContext(ctx).
			Table("album_tracks").
			Select("track_variant_id AS recording_id").
			Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).
			Order("disc_no ASC, track_no ASC, track_variant_id ASC").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]string, 0, len(rows))
		seen := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			recordingID := strings.TrimSpace(row.RecordingID)
			if recordingID == "" {
				continue
			}
			if _, ok := seen[recordingID]; ok {
				continue
			}
			seen[recordingID] = struct{}{}
			out = append(out, recordingID)
		}
		return out, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	type row struct{ RecordingID string }
	explicitAlbumID, ok, err := s.app.catalog.explicitAlbumVariantID(ctx, libraryID, deviceID, albumID)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(explicitAlbumID) == "" {
		return nil, nil
	}
	var rows []row
	query := `
SELECT
	tv.track_cluster_id AS recording_id
FROM album_tracks at
JOIN track_variants tv ON tv.library_id = at.library_id AND tv.track_variant_id = at.track_variant_id
WHERE at.library_id = ? AND at.album_variant_id = ?
GROUP BY tv.track_cluster_id, at.disc_no, at.track_no
ORDER BY at.disc_no ASC, at.track_no ASC, tv.track_cluster_id ASC`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, explicitAlbumID).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		recordingID := strings.TrimSpace(row.RecordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := seen[recordingID]; ok {
			continue
		}
		seen[recordingID] = struct{}{}
		out = append(out, recordingID)
	}
	return out, nil
}

func (s *PlaybackService) recordingIDsForPlaylist(ctx context.Context, libraryID, playlistID string) ([]string, error) {
	type row struct{ RecordingID string }
	var rows []row
	query := `
SELECT pi.track_variant_id AS recording_id
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
ORDER BY pi.position_key ASC, pi.item_id ASC`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, strings.TrimSpace(playlistID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		recordingID := strings.TrimSpace(row.RecordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := seen[recordingID]; ok {
			continue
		}
		seen[recordingID] = struct{}{}
		out = append(out, recordingID)
	}
	return out, nil
}

func (s *PlaybackService) albumAvailabilitySummaries(ctx context.Context, local apitypes.LocalContext, albumIDs []string, preferredProfile string) (map[string]apitypes.AggregateAvailabilitySummary, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return map[string]apitypes.AggregateAvailabilitySummary{}, nil
	}

	grouped := make(map[string][]string, len(albumIDs))
	pinScopeByRequested := make(map[string]string, len(albumIDs))
	pinScopeIDs := make([]string, 0, len(albumIDs))
	pinScopeSeen := make(map[string]struct{}, len(albumIDs))
	recordingIDs := make([]string, 0, len(albumIDs)*8)
	recordingSeen := make(map[string]struct{}, len(albumIDs)*8)
	for _, albumID := range albumIDs {
		trimmedAlbumID := strings.TrimSpace(albumID)
		pinScopeID, ok, err := s.app.catalog.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, trimmedAlbumID)
		if err != nil {
			return nil, err
		}
		if !ok || strings.TrimSpace(pinScopeID) == "" {
			pinScopeID = trimmedAlbumID
		}
		pinScopeByRequested[trimmedAlbumID] = strings.TrimSpace(pinScopeID)
		if _, ok := pinScopeSeen[strings.TrimSpace(pinScopeID)]; !ok {
			pinScopeSeen[strings.TrimSpace(pinScopeID)] = struct{}{}
			pinScopeIDs = append(pinScopeIDs, strings.TrimSpace(pinScopeID))
		}

		recordings, err := s.recordingIDsForAlbum(ctx, local.LibraryID, local.DeviceID, trimmedAlbumID)
		if err != nil {
			return nil, err
		}
		grouped[trimmedAlbumID] = recordings
		for _, recordingID := range recordings {
			recordingID = strings.TrimSpace(recordingID)
			if recordingID == "" {
				continue
			}
			if _, ok := recordingSeen[recordingID]; ok {
				continue
			}
			recordingSeen[recordingID] = struct{}{}
			recordingIDs = append(recordingIDs, recordingID)
		}
	}

	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, preferredProfile)
	if err != nil {
		return nil, err
	}
	profile := resolution.profile
	aliasProfile := normalizedPlaybackProfileAlias(profile)
	cutoff := time.Now().UTC().Add(-availabilityOnlineWindow)
	facts, err := s.batchRecordingAvailabilityFacts(ctx, local.LibraryID, local.DeviceID, resolution.resolvedRecordingIDs, profile, aliasProfile, s.app.NetworkStatus().Running, cutoff)
	if err != nil {
		return nil, err
	}
	albumPins, err := s.batchAlbumPins(ctx, local.LibraryID, local.DeviceID, pinScopeIDs, profile, aliasProfile)
	if err != nil {
		return nil, err
	}
	trackPins, err := s.batchTrackPins(ctx, local.LibraryID, local.DeviceID, recordingIDs, profile, aliasProfile)
	if err != nil {
		return nil, err
	}

	out := make(map[string]apitypes.AggregateAvailabilitySummary, len(albumIDs))
	for _, albumID := range albumIDs {
		trimmedAlbumID := strings.TrimSpace(albumID)
		recordings := grouped[trimmedAlbumID]
		summary := apitypes.AggregateAvailabilitySummary{
			TrackCount: int64(len(recordings)),
		}
		albumPinned := albumPins[pinScopeByRequested[trimmedAlbumID]]
		summary.ScopePinned = albumPinned
		for _, recordingID := range recordings {
			fact := facts[resolution.resolvedByRecording[recordingID]]
			if fact.isLocal {
				summary.IsLocal = true
				summary.LocalTrackCount++
			}
			if fact.hasLocalSource {
				summary.LocalSourceTrackCount++
			}
			if fact.hasRemotePath {
				summary.HasRemote = true
				summary.RemoteTrackCount++
			}
			if fact.hasLocalCached {
				summary.CachedTrackCount++
			}
			if fact.availableNow {
				summary.AvailableTrackCount++
				summary.AvailableNowTrackCount++
			} else if fact.offline {
				summary.OfflineTrackCount++
			} else {
				summary.UnavailableTrackCount++
			}
			if trackPins[recordingID] {
				summary.PinnedTrackCount++
			}
		}
		if albumPinned {
			summary.PinnedTrackCount = summary.TrackCount
		}
		summary.State = deriveAggregateAvailabilityState(summary)
		out[trimmedAlbumID] = summary
	}
	return out, nil
}

type recordingAvailabilityFacts struct {
	availableNow   bool
	offline        bool
	isLocal        bool
	hasLocalCached bool
	hasLocalSource bool
	hasRemotePath  bool
}

type recordingPlaybackFacts struct {
	hasLocalSource        bool
	hasRemoteSource       bool
	hasRemoteSourceOnline bool
	hasLocalCached        bool
	hasRemoteCached       bool
	hasRemoteCachedOnline bool
}

func (f recordingPlaybackFacts) summary(networkRunning bool) recordingAvailabilityFacts {
	availableNow := f.hasLocalSource || f.hasLocalCached
	if networkRunning && (f.hasRemoteCachedOnline || f.hasRemoteSourceOnline) {
		availableNow = true
	}
	hasRemotePath := f.hasRemoteCached || f.hasRemoteSource
	return recordingAvailabilityFacts{
		availableNow:   availableNow,
		offline:        !availableNow && hasRemotePath,
		isLocal:        f.hasLocalSource || f.hasLocalCached,
		hasLocalCached: f.hasLocalCached,
		hasLocalSource: f.hasLocalSource,
		hasRemotePath:  hasRemotePath,
	}
}

func (s *PlaybackService) batchRecordingPlaybackFacts(ctx context.Context, libraryID, localDeviceID string, recordingIDs []string, profile, aliasProfile string, cutoff time.Time) (map[string]recordingPlaybackFacts, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]recordingPlaybackFacts{}, nil
	}

	type sourceRow struct {
		RecordingID           string
		HasLocalSource        int
		HasRemoteSource       int
		HasRemoteSourceOnline int
	}
	sourceQuery := `
SELECT
	sf.track_variant_id AS recording_id,
	MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN 1 ELSE 0 END) AS has_local_source,
	MAX(CASE WHEN sf.device_id <> ? AND sf.is_present = 1 AND COALESCE(m.role, '') IN ('owner', 'admin', 'member') THEN 1 ELSE 0 END) AS has_remote_source,
	MAX(CASE WHEN sf.device_id <> ? AND sf.is_present = 1 AND COALESCE(m.role, '') IN ('owner', 'admin', 'member') AND d.last_seen_at >= ? THEN 1 ELSE 0 END) AS has_remote_source_online
FROM source_files sf
LEFT JOIN memberships m ON m.library_id = sf.library_id AND m.device_id = sf.device_id
LEFT JOIN devices d ON d.device_id = sf.device_id
WHERE sf.library_id = ? AND sf.track_variant_id IN ?
GROUP BY sf.track_variant_id`
	var sourceRows []sourceRow
	if err := s.app.storage.WithContext(ctx).Raw(sourceQuery, localDeviceID, localDeviceID, localDeviceID, cutoff, libraryID, recordingIDs).Scan(&sourceRows).Error; err != nil {
		return nil, err
	}

	type cacheRow struct {
		RecordingID           string
		HasLocalCached        int
		HasRemoteCached       int
		HasRemoteCachedOnline int
	}
	cacheQuery := `
SELECT
	oa.track_variant_id AS recording_id,
	MAX(CASE WHEN dac.device_id = ? AND dac.is_cached = 1 THEN 1 ELSE 0 END) AS has_local_cached,
	MAX(CASE WHEN dac.device_id <> ? AND dac.is_cached = 1 THEN 1 ELSE 0 END) AS has_remote_cached,
	MAX(CASE WHEN dac.device_id <> ? AND dac.is_cached = 1 AND d.last_seen_at >= ? THEN 1 ELSE 0 END) AS has_remote_cached_online
FROM optimized_assets oa
JOIN device_asset_caches dac ON dac.library_id = oa.library_id AND dac.optimized_asset_id = oa.optimized_asset_id
LEFT JOIN devices d ON d.device_id = dac.device_id
WHERE oa.library_id = ? AND oa.track_variant_id IN ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
GROUP BY oa.track_variant_id`
	var cacheRows []cacheRow
	if err := s.app.storage.WithContext(ctx).Raw(cacheQuery, localDeviceID, localDeviceID, localDeviceID, cutoff, libraryID, recordingIDs, profile, profile, aliasProfile).Scan(&cacheRows).Error; err != nil {
		return nil, err
	}

	combined := make(map[string]recordingPlaybackFacts, len(recordingIDs))
	for _, row := range sourceRows {
		recordingID := strings.TrimSpace(row.RecordingID)
		next := combined[recordingID]
		next.hasLocalSource = row.HasLocalSource > 0
		next.hasRemoteSource = row.HasRemoteSource > 0
		next.hasRemoteSourceOnline = row.HasRemoteSourceOnline > 0
		combined[recordingID] = next
	}
	for _, row := range cacheRows {
		recordingID := strings.TrimSpace(row.RecordingID)
		next := combined[recordingID]
		next.hasLocalCached = row.HasLocalCached > 0
		next.hasRemoteCached = row.HasRemoteCached > 0
		next.hasRemoteCachedOnline = row.HasRemoteCachedOnline > 0
		combined[recordingID] = next
	}

	return combined, nil
}

func (s *PlaybackService) batchRecordingAvailabilityFacts(ctx context.Context, libraryID, localDeviceID string, recordingIDs []string, profile, aliasProfile string, networkRunning bool, cutoff time.Time) (map[string]recordingAvailabilityFacts, error) {
	combined, err := s.batchRecordingPlaybackFacts(ctx, libraryID, localDeviceID, recordingIDs, profile, aliasProfile, cutoff)
	if err != nil {
		return nil, err
	}

	out := make(map[string]recordingAvailabilityFacts, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		out[recordingID] = combined[recordingID].summary(networkRunning)
	}
	return out, nil
}

func (s *PlaybackService) batchAlbumPins(ctx context.Context, libraryID, localDeviceID string, albumIDs []string, profile, aliasProfile string) (map[string]bool, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return map[string]bool{}, nil
	}

	type row struct{ ScopeID string }
	var rows []row
	query := `
SELECT scope_id
FROM offline_pins
WHERE library_id = ? AND device_id = ? AND scope = 'album' AND scope_id IN ? AND (profile = ? OR profile = ?)`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, localDeviceID, albumIDs, profile, aliasProfile).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.ScopeID)] = true
	}
	return out, nil
}

func (s *PlaybackService) batchTrackPins(ctx context.Context, libraryID, localDeviceID string, recordingIDs []string, profile, aliasProfile string) (map[string]bool, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]bool{}, nil
	}

	type variantRow struct {
		TrackVariantID string
		TrackClusterID string
	}
	var variants []variantRow
	if err := s.app.storage.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Select("track_variant_id, track_cluster_id").
		Where("library_id = ? AND (track_variant_id IN ? OR track_cluster_id IN ?)", libraryID, recordingIDs, recordingIDs).
		Scan(&variants).Error; err != nil {
		return nil, err
	}
	clusterIDs := make([]string, 0, len(variants))
	clusterSeen := make(map[string]struct{}, len(variants))
	clusterByInput := make(map[string]string, len(recordingIDs))
	for _, row := range variants {
		variantID := strings.TrimSpace(row.TrackVariantID)
		clusterID := strings.TrimSpace(row.TrackClusterID)
		if clusterID == "" {
			continue
		}
		if variantID != "" {
			clusterByInput[variantID] = clusterID
		}
		clusterByInput[clusterID] = clusterID
		if _, ok := clusterSeen[clusterID]; ok {
			continue
		}
		clusterSeen[clusterID] = struct{}{}
		clusterIDs = append(clusterIDs, clusterID)
	}
	if len(clusterIDs) == 0 {
		return map[string]bool{}, nil
	}
	type row struct{ ScopeID string }
	var rows []row
	query := `
SELECT scope_id
FROM offline_pins
WHERE library_id = ? AND device_id = ? AND scope = 'recording' AND scope_id IN ? AND (profile = ? OR profile = ?)`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, localDeviceID, clusterIDs, profile, aliasProfile).Scan(&rows).Error; err != nil {
		return nil, err
	}

	pinnedClusters := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		pinnedClusters[strings.TrimSpace(row.ScopeID)] = struct{}{}
	}
	out := make(map[string]bool, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		clusterID := strings.TrimSpace(clusterByInput[recordingID])
		if clusterID == "" {
			clusterID = strings.TrimSpace(recordingID)
		}
		if _, ok := pinnedClusters[clusterID]; ok {
			out[strings.TrimSpace(recordingID)] = true
		}
	}
	return out, nil
}

func (s *PlaybackService) recordingScopePinned(ctx context.Context, libraryID, localDeviceID, recordingID, profile string) (bool, error) {
	pins, err := s.batchTrackPins(ctx, libraryID, localDeviceID, []string{recordingID}, profile, normalizedPlaybackProfileAlias(profile))
	if err != nil {
		return false, err
	}
	return pins[strings.TrimSpace(recordingID)], nil
}

func (s *PlaybackService) batchPlaylistPins(ctx context.Context, libraryID, localDeviceID string, playlistIDs []string, profile, aliasProfile string) (map[string]bool, error) {
	playlistIDs = compactNonEmptyStrings(playlistIDs)
	if len(playlistIDs) == 0 {
		return map[string]bool{}, nil
	}

	type row struct{ ScopeID string }
	var rows []row
	query := `
SELECT scope_id
FROM offline_pins
WHERE library_id = ? AND device_id = ? AND scope = 'playlist' AND scope_id IN ? AND (profile = ? OR profile = ?)`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, localDeviceID, playlistIDs, profile, aliasProfile).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.ScopeID)] = true
	}
	return out, nil
}

func deriveAggregateAvailabilityState(summary apitypes.AggregateAvailabilitySummary) apitypes.AggregateAvailabilityState {
	if summary.TrackCount == 0 {
		return apitypes.AggregateAvailabilityStateUnavailable
	}
	switch {
	case summary.LocalSourceTrackCount == summary.TrackCount:
		return apitypes.AggregateAvailabilityStateLocal
	case summary.ScopePinned || summary.PinnedTrackCount == summary.TrackCount:
		return apitypes.AggregateAvailabilityStatePinned
	case summary.CachedTrackCount == summary.TrackCount:
		return apitypes.AggregateAvailabilityStateCached
	case summary.AvailableNowTrackCount == summary.TrackCount:
		return apitypes.AggregateAvailabilityStateAvailable
	case summary.AvailableNowTrackCount > 0:
		return apitypes.AggregateAvailabilityStatePartial
	case summary.OfflineTrackCount > 0:
		return apitypes.AggregateAvailabilityStateOffline
	default:
		return apitypes.AggregateAvailabilityStateUnavailable
	}
}

func (s *PlaybackService) recordingAvailabilitySummary(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (apitypes.TrackAvailabilitySummary, apitypes.RecordingPlaybackAvailability, []apitypes.RecordingAvailabilityItem, error) {
	playback, err := s.GetRecordingAvailability(ctx, recordingID, preferredProfile)
	if err != nil {
		return apitypes.TrackAvailabilitySummary{}, apitypes.RecordingPlaybackAvailability{}, nil, err
	}
	devices, err := s.ListRecordingAvailability(ctx, recordingID, preferredProfile)
	if err != nil {
		return apitypes.TrackAvailabilitySummary{}, apitypes.RecordingPlaybackAvailability{}, nil, err
	}
	return buildTrackAvailabilitySummary(local.DeviceID, playback, devices), playback, devices, nil
}

func buildTrackAvailabilitySummary(localDeviceID string, playback apitypes.RecordingPlaybackAvailability, devices []apitypes.RecordingAvailabilityItem) apitypes.TrackAvailabilitySummary {
	out := apitypes.TrackAvailabilitySummary{
		State:      playback.State,
		SourceKind: playback.SourceKind,
		Reason:     playback.Reason,
	}
	for _, item := range devices {
		isLocalDevice := item.DeviceID == localDeviceID
		hasPath := item.SourcePresent || item.CachedOptimized || item.OptimizedPresent
		if hasPath {
			out.AvailableDeviceCount++
		}
		if isLocalDevice {
			if item.SourcePresent {
				out.HasLocalSource = true
			}
			if item.CachedOptimized {
				out.HasLocalCachedOptimized = true
			}
			if hasPath {
				out.LocalDeviceCount++
			}
			continue
		}
		if item.SourcePresent {
			out.HasRemoteSource = true
		}
		if item.CachedOptimized {
			out.HasRemoteCachedOptimized = true
		}
		if hasPath {
			out.RemoteDeviceCount++
		}
	}
	out.IsLocal = out.HasLocalSource || out.HasLocalCachedOptimized ||
		playback.SourceKind == apitypes.PlaybackSourceLocalFile ||
		playback.SourceKind == apitypes.PlaybackSourceCachedOpt
	return out
}

func aggregateAvailabilitySummaries(items []apitypes.TrackAvailabilitySummary) apitypes.AggregateAvailabilitySummary {
	out := apitypes.AggregateAvailabilitySummary{}
	for _, item := range items {
		if item.IsLocal {
			out.IsLocal = true
			out.LocalTrackCount++
		}
		if item.HasRemoteSource || item.HasRemoteCachedOptimized || item.RemoteDeviceCount > 0 {
			out.HasRemote = true
			out.RemoteTrackCount++
		}
		if item.HasLocalCachedOptimized {
			out.CachedTrackCount++
		}
		switch item.State {
		case apitypes.AvailabilityPlayableLocalFile,
			apitypes.AvailabilityPlayableCachedOpt,
			apitypes.AvailabilityPlayableRemoteOpt,
			apitypes.AvailabilityWaitingTranscode:
			out.AvailableTrackCount++
		default:
			out.UnavailableTrackCount++
		}
	}
	return out
}

func (s *PlaybackService) handlePinnedScopeCatalogChange(event apitypes.CatalogChangeEvent) {
	if s == nil || s.app == nil || event.Kind != apitypes.CatalogChangeInvalidateBase {
		return
	}
	if !pinnedScopeCatalogChangeRelevant(event) {
		return
	}

	local, err := s.app.EnsureLocalContext(context.Background())
	if err != nil || strings.TrimSpace(local.LibraryID) == "" {
		return
	}

	ctx := context.Background()
	if albumIDs, playlistIDs, targeted, err := s.affectedPinnedScopesForEvent(ctx, local, event); err == nil {
		if targeted {
			for _, pin := range albumIDs {
				s.schedulePinnedScopeRefresh(local.LibraryID, pin.Scope, pin.ScopeID, pin.Profile)
			}
			for _, pin := range playlistIDs {
				s.schedulePinnedScopeRefresh(local.LibraryID, pin.Scope, pin.ScopeID, pin.Profile)
			}
			return
		}
	}

	if !event.InvalidateAll {
		return
	}
	pins, err := s.listPinnedCollectionScopes(ctx, local.LibraryID, local.DeviceID, nil)
	if err != nil {
		return
	}
	for _, pin := range pins {
		s.schedulePinnedScopeRefresh(local.LibraryID, pin.Scope, pin.ScopeID, pin.Profile)
	}
}

func pinnedScopeCatalogChangeRelevant(event apitypes.CatalogChangeEvent) bool {
	if len(compactNonEmptyStrings(event.AlbumIDs)) > 0 || len(compactNonEmptyStrings(event.RecordingIDs)) > 0 {
		return true
	}
	switch event.Entity {
	case apitypes.CatalogChangeEntityAlbum,
		apitypes.CatalogChangeEntityAlbums,
		apitypes.CatalogChangeEntityTracks,
		apitypes.CatalogChangeEntityAlbumTracks,
		apitypes.CatalogChangeEntityPlaylistTracks,
		apitypes.CatalogChangeEntityLiked:
		return true
	}
	return event.InvalidateAll && event.Entity == ""
}

func (s *PlaybackService) affectedPinnedScopesForEvent(ctx context.Context, local apitypes.LocalContext, event apitypes.CatalogChangeEvent) ([]OfflinePin, []OfflinePin, bool, error) {
	albumIDs := compactNonEmptyStrings(event.AlbumIDs)
	recordingIDs := compactNonEmptyStrings(event.RecordingIDs)
	playlistIDs := []string{}

	switch event.Entity {
	case apitypes.CatalogChangeEntityAlbum:
		if entityID := strings.TrimSpace(event.EntityID); entityID != "" {
			albumIDs = append(albumIDs, entityID)
		}
	case apitypes.CatalogChangeEntityPlaylistTracks, apitypes.CatalogChangeEntityLiked:
		if entityID := strings.TrimSpace(event.EntityID); entityID != "" {
			playlistIDs = append(playlistIDs, entityID)
		}
	}

	if len(recordingIDs) > 0 {
		recordingAlbumIDs, err := s.albumIDsForRecordings(ctx, local.LibraryID, recordingIDs)
		if err != nil {
			return nil, nil, false, err
		}
		recordingPlaylistIDs, err := s.playlistIDsForRecordings(ctx, local.LibraryID, recordingIDs)
		if err != nil {
			return nil, nil, false, err
		}
		albumIDs = append(albumIDs, recordingAlbumIDs...)
		playlistIDs = append(playlistIDs, recordingPlaylistIDs...)
	}

	albumClusterIDs, err := s.albumClusterIDs(ctx, local.LibraryID, albumIDs)
	if err != nil {
		return nil, nil, false, err
	}
	playlistIDs = compactNonEmptyStrings(playlistIDs)
	if len(albumClusterIDs) == 0 && len(playlistIDs) == 0 {
		return nil, nil, false, nil
	}

	albumPins, err := s.listPinnedAlbumScopesForClusters(ctx, local.LibraryID, local.DeviceID, albumClusterIDs)
	if err != nil {
		return nil, nil, false, err
	}
	playlistPins, err := s.listPinnedCollectionScopes(ctx, local.LibraryID, local.DeviceID, map[string][]string{"playlist": playlistIDs})
	if err != nil {
		return nil, nil, false, err
	}
	return albumPins, playlistPins, true, nil
}

func (s *PlaybackService) albumClusterIDs(ctx context.Context, libraryID string, albumIDs []string) ([]string, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(albumIDs))
	seen := make(map[string]struct{}, len(albumIDs))
	for _, albumID := range albumIDs {
		clusterID := strings.TrimSpace(albumID)
		if resolvedClusterID, ok, err := s.app.catalog.albumClusterIDForVariant(ctx, libraryID, albumID); err != nil {
			return nil, err
		} else if ok && strings.TrimSpace(resolvedClusterID) != "" {
			clusterID = strings.TrimSpace(resolvedClusterID)
		}
		if clusterID == "" {
			continue
		}
		if _, ok := seen[clusterID]; ok {
			continue
		}
		seen[clusterID] = struct{}{}
		out = append(out, clusterID)
	}
	return out, nil
}

func (s *PlaybackService) albumIDsForRecordings(ctx context.Context, libraryID string, recordingIDs []string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}

	type row struct{ AlbumID string }
	var rows []row
	query := `
SELECT DISTINCT av.album_cluster_id AS album_id
FROM album_tracks at
JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
JOIN track_variants tv ON tv.library_id = at.library_id AND tv.track_variant_id = at.track_variant_id
WHERE at.library_id = ? AND (tv.track_variant_id IN ? OR tv.track_cluster_id IN ?)`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, recordingIDs, recordingIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.AlbumID))
	}
	return compactNonEmptyStrings(out), nil
}

func (s *PlaybackService) playlistIDsForRecordings(ctx context.Context, libraryID string, recordingIDs []string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}

	type variantRow struct {
		TrackVariantID string
		TrackClusterID string
	}
	var variants []variantRow
	if err := s.app.storage.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Select("track_variant_id, track_cluster_id").
		Where("library_id = ? AND (track_variant_id IN ? OR track_cluster_id IN ?)", libraryID, recordingIDs, recordingIDs).
		Scan(&variants).Error; err != nil {
		return nil, err
	}
	clusterIDs := make([]string, 0, len(variants))
	seen := make(map[string]struct{}, len(variants))
	for _, row := range variants {
		clusterID := strings.TrimSpace(row.TrackClusterID)
		if clusterID == "" {
			continue
		}
		if _, ok := seen[clusterID]; ok {
			continue
		}
		seen[clusterID] = struct{}{}
		clusterIDs = append(clusterIDs, clusterID)
	}
	if len(clusterIDs) == 0 {
		return nil, nil
	}

	type row struct{ PlaylistID string }
	var rows []row
	query := `
SELECT DISTINCT playlist_id
FROM playlist_items
WHERE library_id = ? AND deleted_at IS NULL AND track_variant_id IN ?`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.PlaylistID))
	}
	return compactNonEmptyStrings(out), nil
}

func (s *PlaybackService) listPinnedCollectionScopes(ctx context.Context, libraryID, deviceID string, byScope map[string][]string) ([]OfflinePin, error) {
	var pins []OfflinePin
	query := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope IN ?", libraryID, deviceID, []string{"album", "playlist"})
	if byScope != nil {
		albumIDs := compactNonEmptyStrings(byScope["album"])
		playlistIDs := compactNonEmptyStrings(byScope["playlist"])
		switch {
		case len(albumIDs) == 0 && len(playlistIDs) == 0:
			return []OfflinePin{}, nil
		case len(albumIDs) == 0:
			query = query.Where("scope = ? AND scope_id IN ?", "playlist", playlistIDs)
		case len(playlistIDs) == 0:
			query = query.Where("scope = ? AND scope_id IN ?", "album", albumIDs)
		default:
			query = query.Where("(scope = ? AND scope_id IN ?) OR (scope = ? AND scope_id IN ?)", "album", albumIDs, "playlist", playlistIDs)
		}
	}
	if err := query.Find(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

func (s *PlaybackService) listPinnedAlbumScopesForClusters(ctx context.Context, libraryID, deviceID string, clusterIDs []string) ([]OfflinePin, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return []OfflinePin{}, nil
	}

	var pins []OfflinePin
	query := `
SELECT op.library_id, op.device_id, op.scope, op.scope_id, op.profile, op.created_at, op.updated_at
FROM offline_pins op
JOIN album_variants av ON av.library_id = op.library_id AND av.album_variant_id = op.scope_id
WHERE op.library_id = ? AND op.device_id = ? AND op.scope = 'album' AND av.album_cluster_id IN ?`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, deviceID, clusterIDs).Scan(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

func (s *PlaybackService) schedulePinnedScopeRefresh(libraryID, scope, scopeID, profile string) {
	libraryID = strings.TrimSpace(libraryID)
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	profile = strings.TrimSpace(profile)
	if libraryID == "" || scope == "" || scopeID == "" || profile == "" {
		return
	}

	jobID := playbackRefreshPinnedScopeJobID(libraryID, scope, scopeID, profile)
	s.reconcileMu.Lock()
	if timer, ok := s.reconcileTimers[jobID]; ok {
		timer.Reset(pinnedScopeDebounceWait)
		s.reconcileMu.Unlock()
		return
	}
	s.reconcileTimers[jobID] = time.AfterFunc(pinnedScopeDebounceWait, func() {
		s.reconcileMu.Lock()
		delete(s.reconcileTimers, jobID)
		s.reconcileMu.Unlock()

		_, _ = s.app.startActiveLibraryJob(
			context.Background(),
			jobID,
			refreshPinnedScopeJobKind(scope),
			libraryID,
			"queued pinned scope refresh",
			"pinned scope refresh canceled because the library is no longer active",
			func(runCtx context.Context) {
				s.runPinnedScopeRefreshJob(runCtx, libraryID, scope, scopeID, profile)
			},
		)
	})
	s.reconcileMu.Unlock()
}

func (s *PlaybackService) runPinnedScopeRefreshJob(ctx context.Context, libraryID, scope, scopeID, profile string) {
	kind := refreshPinnedScopeJobKind(scope)
	job := s.app.jobs.Track(playbackRefreshPinnedScopeJobID(libraryID, scope, scopeID, profile), kind, libraryID)
	if job == nil {
		return
	}
	job.Queued(0, "queued pinned scope refresh")

	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}
	if strings.TrimSpace(local.LibraryID) != strings.TrimSpace(libraryID) {
		job.Fail(0, "pinned scope refresh canceled because the library is no longer active", nil)
		return
	}

	var pin OfflinePin
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
		Take(&pin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			job.Complete(1, "Pinned scope no longer active")
			return
		}
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}

	resolvedScopeID, recordingIDs, _, err := s.resolveOfflinePinScope(ctx, local, scope, scopeID, profile)
	if err != nil {
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}
	pendingIDs, err := s.filterRecordingsNeedingOfflineFetch(ctx, local, recordingIDs, profile)
	if err != nil {
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}
	if len(pendingIDs) == 0 {
		job.Complete(1, "Pinned scope already up to date")
		return
	}

	result, err := s.pinOfflineScopeWithJob(ctx, local, scope, resolvedScopeID, pendingIDs, profile, job)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(pinJobProgress(result.Tracks, len(pendingIDs)), "pinned scope refresh canceled", nil)
			return
		}
		job.Fail(pinJobProgress(result.Tracks, len(pendingIDs)), "pinned scope refresh failed", err)
		return
	}
	job.Complete(1, "Pinned scope refreshed")
	s.emitOfflinePinAvailabilityInvalidation(local, scope, resolvedScopeID, recordingIDs)
}

func (s *PlaybackService) filterRecordingsNeedingOfflineFetch(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}
	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, preferredProfile)
	if err != nil {
		return nil, err
	}
	cachedRecordings, err := s.batchBestCachedRecordingIDs(ctx, local.LibraryID, local.DeviceID, resolution.resolvedRecordingIDs, resolution.profile)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		resolvedRecordingID := resolution.resolvedByRecording[recordingID]
		if cachedRecordings[resolvedRecordingID] {
			continue
		}
		out = append(out, recordingID)
	}
	return out, nil
}

func playbackPinOfflineJobID(libraryID, scope, scopeID, profile string) string {
	return "playback:pin-offline:" + strings.TrimSpace(libraryID) + ":" + strings.TrimSpace(scope) + ":" + strings.TrimSpace(scopeID) + ":" + strings.TrimSpace(profile)
}

func playbackRefreshPinnedScopeJobID(libraryID, scope, scopeID, profile string) string {
	return "playback:refresh-pinned-scope:" + strings.TrimSpace(libraryID) + ":" + strings.TrimSpace(scope) + ":" + strings.TrimSpace(scopeID) + ":" + strings.TrimSpace(profile)
}

func refreshPinnedScopeJobKind(scope string) string {
	switch strings.TrimSpace(scope) {
	case "album":
		return jobKindRefreshPinnedAlbum
	default:
		return jobKindRefreshPinnedPlaylist
	}
}

func pinJobProgress(done, total int) float64 {
	if total <= 0 {
		return 1
	}
	return clampJobProgress(0.1 + (0.85 * (float64(done) / float64(total))))
}

func normalizedPlaybackProfileAlias(profile string) string {
	switch strings.TrimSpace(profile) {
	case "desktop":
		return audioProfileVBRHigh
	default:
		return strings.TrimSpace(profile)
	}
}
