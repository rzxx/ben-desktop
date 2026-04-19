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
)

const (
	jobKindEnsureRecordingEncoding = "ensure-recording-encoding"
	jobKindEnsureAlbumEncodings    = "ensure-album-encodings"
	jobKindEnsurePlaylistEncodings = "ensure-playlist-encodings"
	jobKindPreparePlayback         = "prepare-playback"
	jobKindPinRecording            = "pin-recording"
	jobKindPinAlbum                = "pin-album"
	jobKindPinPlaylist             = "pin-playlist"
	jobKindRefreshPinnedRecording  = "refresh-pinned-recording"
	jobKindRefreshPinnedAlbum      = "refresh-pinned-album"
	jobKindRefreshPinnedPlaylist   = "refresh-pinned-playlist"

	pinnedScopeWorkerCount  = 3
	pinnedScopeDebounceWait = time.Second
)

type PlaybackService struct {
	app *App

	mu           sync.Mutex
	preparations map[string]apitypes.PlaybackPreparationStatus
}

func newPlaybackService(app *App) *PlaybackService {
	return &PlaybackService{
		app:          app,
		preparations: make(map[string]apitypes.PlaybackPreparationStatus),
	}
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
	recordingIDs, err := s.libraryRecordingIDsForPlaylist(ctx, local.LibraryID, playlistID)
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
	resolvedRecordingID, profile, exactRequested, err := s.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	if _, err := s.app.transcode.EnsureRecordingEncoding(ctx, local, resolvedRecordingID, profile, local.DeviceID); err != nil && !errors.Is(err, ErrProviderOnlyTranscode) {
		return apitypes.PlaybackRecordingResult{}, err
	}
	remoteRecordingID := resolvedRecordingID
	if !exactRequested {
		remoteRecordingID = strings.TrimSpace(recordingID)
	}

	blobID, encodingID, ok, err := s.bestCachedEncodingWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile, exactRequested)
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

	if localPath, ok, err := s.bestLocalRecordingPathWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, exactRequested); err != nil {
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

	availability, err := s.GetRecordingAvailability(ctx, recordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	switch availability.State {
	case apitypes.AvailabilityPlayableRemoteOpt:
		if result, fetched, err := s.ensureRemotePlaybackRecording(ctx, local, remoteRecordingID, profile); err != nil {
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
		if result, fetched, err := s.ensureRemotePlaybackRecording(ctx, local, remoteRecordingID, profile); err != nil {
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
	recordingIDs, err := s.libraryRecordingIDsForPlaylist(ctx, local.LibraryID, playlistID)
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
	resolvedRecordingID, profile, exactRequested, err := s.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}

	status := apitypes.PlaybackPreparationStatus{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: profile,
		Phase:            apitypes.PlaybackPreparationUnavailable,
		UpdatedAt:        time.Now().UTC(),
	}

	if localPath, ok, err := s.bestLocalRecordingPathWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, exactRequested); err != nil {
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

	if blobID, encodingID, ok, err := s.bestCachedEncodingWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile, exactRequested); err != nil {
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

	items, err := s.ListRecordingAvailability(ctx, recordingID, profile)
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
	recordingID = strings.TrimSpace(recordingID)
	resolvedRecordingID, profile, exactVariant, err := s.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
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
		SELECT 1
		FROM source_files sf
		JOIN track_variants req ON req.library_id = sf.library_id AND req.track_variant_id = ?
		JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
		WHERE sf.library_id = m.library_id AND sf.device_id = m.device_id AND sf.is_present = 1 AND cand.track_cluster_id = req.track_cluster_id
	) THEN 1 ELSE 0 END AS source_present,
	CASE WHEN EXISTS (
		SELECT 1
		FROM optimized_assets oa
		JOIN source_files sf ON sf.library_id = oa.library_id AND sf.source_file_id = oa.source_file_id
		JOIN track_variants req ON req.library_id = oa.library_id AND req.track_variant_id = ?
		JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
		WHERE oa.library_id = m.library_id AND oa.created_by_device_id = m.device_id AND cand.track_cluster_id = req.track_cluster_id AND (? = '' OR oa.profile = ? OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS optimized_present,
	CASE WHEN EXISTS (
		SELECT 1
		FROM device_asset_caches dac
		JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
		JOIN source_files sf ON sf.library_id = oa.library_id AND sf.source_file_id = oa.source_file_id
		JOIN track_variants req ON req.library_id = oa.library_id AND req.track_variant_id = ?
		JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
		WHERE dac.library_id = m.library_id AND dac.device_id = m.device_id AND dac.is_cached = 1 AND cand.track_cluster_id = req.track_cluster_id AND (? = '' OR oa.profile = ? OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS cached_optimized
FROM memberships m
LEFT JOIN devices d ON d.device_id = m.device_id
LEFT JOIN peer_sync_states pss ON pss.library_id = m.library_id AND pss.device_id = m.device_id
WHERE m.library_id = ?
ORDER BY CASE WHEN m.device_id = ? THEN 0 ELSE 1 END, m.device_id ASC`
	args := []any{
		resolvedRecordingID,
		resolvedRecordingID, profile, profile, aliasProfile,
		resolvedRecordingID, profile, profile, aliasProfile,
		local.LibraryID, local.DeviceID,
	}
	if exactVariant {
		query = `
SELECT
	m.device_id,
	m.role,
	COALESCE(d.peer_id, '') AS peer_id,
	d.last_seen_at,
	pss.last_success_at,
	CASE WHEN EXISTS (
		SELECT 1
		FROM source_files sf
		WHERE sf.library_id = m.library_id AND sf.device_id = m.device_id AND sf.is_present = 1 AND sf.track_variant_id = ?
	) THEN 1 ELSE 0 END AS source_present,
	CASE WHEN EXISTS (
		SELECT 1
		FROM optimized_assets oa
		JOIN source_files sf ON sf.library_id = oa.library_id AND sf.source_file_id = oa.source_file_id
		WHERE oa.library_id = m.library_id AND oa.created_by_device_id = m.device_id AND sf.track_variant_id = ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS optimized_present,
	CASE WHEN EXISTS (
		SELECT 1
		FROM device_asset_caches dac
		JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
		JOIN source_files sf ON sf.library_id = oa.library_id AND sf.source_file_id = oa.source_file_id
		WHERE dac.library_id = m.library_id AND dac.device_id = m.device_id AND dac.is_cached = 1 AND sf.track_variant_id = ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS cached_optimized
FROM memberships m
LEFT JOIN devices d ON d.device_id = m.device_id
LEFT JOIN peer_sync_states pss ON pss.library_id = m.library_id AND pss.device_id = m.device_id
WHERE m.library_id = ?
ORDER BY CASE WHEN m.device_id = ? THEN 0 ELSE 1 END, m.device_id ASC`
		args = []any{
			resolvedRecordingID,
			resolvedRecordingID, profile, profile, aliasProfile,
			resolvedRecordingID, profile, profile, aliasProfile,
			local.LibraryID, local.DeviceID,
		}
	}
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
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
	resolvedRecordingID, profile, exactRequested, err := s.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	out := apitypes.RecordingPlaybackAvailability{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: profile,
	}
	if pinned, pinErr := s.recordingScopePinned(ctx, local.LibraryID, local.DeviceID, strings.TrimSpace(recordingID), profile); pinErr != nil {
		return apitypes.RecordingPlaybackAvailability{}, pinErr
	} else {
		out.Pinned = pinned
	}
	if localPath, ok, err := s.bestLocalRecordingPathWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, exactRequested); err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	} else if ok {
		out.State = apitypes.AvailabilityPlayableLocalFile
		out.SourceKind = apitypes.PlaybackSourceLocalFile
		out.LocalPath = localPath
		return out, nil
	}
	if _, _, ok, err := s.bestCachedEncodingWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile, exactRequested); err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	} else if ok {
		out.State = apitypes.AvailabilityPlayableCachedOpt
		out.SourceKind = apitypes.PlaybackSourceCachedOpt
		return out, nil
	}
	items, err := s.ListRecordingAvailability(ctx, recordingID, profile)
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
	partition := partitionResolvedRecordingRequests(recordingIDs, resolution)
	exactLocalPaths, err := s.batchBestLocalRecordingPaths(ctx, local.LibraryID, local.DeviceID, partition.exactResolvedIDs, true)
	if err != nil {
		return nil, err
	}
	logicalLocalPaths, err := s.batchBestLocalRecordingPaths(ctx, local.LibraryID, local.DeviceID, partition.logicalResolvedIDs, false)
	if err != nil {
		return nil, err
	}
	exactCachedRecordings, err := s.batchBestCachedRecordingIDs(ctx, local.LibraryID, local.DeviceID, partition.exactResolvedIDs, resolution.profile, true)
	if err != nil {
		return nil, err
	}
	logicalCachedRecordings, err := s.batchBestCachedRecordingIDs(ctx, local.LibraryID, local.DeviceID, partition.logicalResolvedIDs, resolution.profile, false)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-availabilityOnlineWindow)
	networkRunning := s.app.NetworkStatus().Running
	exactFacts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		partition.exactResolvedIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		cutoff,
		true,
	)
	if err != nil {
		return nil, err
	}
	logicalFacts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		partition.logicalResolvedIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		cutoff,
		false,
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
		resolvedRecordingID := strings.TrimSpace(resolution.resolvedByRecording[recordingID])
		exactRequested := resolution.exactRequestedByID[recordingID]
		item := apitypes.RecordingPlaybackAvailability{
			RecordingID:      recordingID,
			PreferredProfile: resolution.profile,
			Pinned:           trackPins[recordingID],
		}

		localPath := logicalLocalPaths[resolvedRecordingID]
		cachedRecording := logicalCachedRecordings[resolvedRecordingID]
		fact := logicalFacts[resolvedRecordingID]
		if exactRequested {
			localPath = exactLocalPaths[resolvedRecordingID]
			cachedRecording = exactCachedRecordings[resolvedRecordingID]
			fact = exactFacts[resolvedRecordingID]
		}
		if localPath != "" {
			item.State = apitypes.AvailabilityPlayableLocalFile
			item.SourceKind = apitypes.PlaybackSourceLocalFile
			item.LocalPath = localPath
			out = append(out, item)
			continue
		}
		if cachedRecording {
			item.State = apitypes.AvailabilityPlayableCachedOpt
			item.SourceKind = apitypes.PlaybackSourceCachedOpt
			out = append(out, item)
			continue
		}

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
	resolvedRecordingID, profile, _, err := s.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
	return resolvedRecordingID, profile, err
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
	profile             string
	exactRequestedByID  map[string]bool
	resolvedByRecording map[string]string
}

func (s *PlaybackService) resolvePlaybackVariantsBatch(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) (recordingBatchResolution, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	profile := s.resolvePlaybackProfile(preferredProfile)
	if len(recordingIDs) == 0 {
		return recordingBatchResolution{
			profile:             profile,
			exactRequestedByID:  map[string]bool{},
			resolvedByRecording: map[string]string{},
		}, nil
	}

	type row struct {
		VariantID string
		ClusterID string
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).
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

	exactRequestedByID := make(map[string]bool, len(recordingIDs))
	resolvedByRecording := make(map[string]string, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		if _, ok := exactVariantIDs[recordingID]; ok {
			exactRequestedByID[recordingID] = true
		}
		resolvedID := recordingID
		if _, ok := exactVariantIDs[recordingID]; ok {
			resolvedID = recordingID
		} else if clusterID := clusterByRecording[recordingID]; clusterID != "" {
			if preferredID := chooseRecordingVariantID(variantsByCluster[clusterID], preferredByCluster[clusterID]); preferredID != "" {
				resolvedID = preferredID
			}
		}
		resolvedByRecording[recordingID] = resolvedID
	}

	return recordingBatchResolution{
		profile:             profile,
		exactRequestedByID:  exactRequestedByID,
		resolvedByRecording: resolvedByRecording,
	}, nil
}

type partitionedResolvedRecordingRequests struct {
	exactResolvedIDs   []string
	logicalResolvedIDs []string
}

func partitionResolvedRecordingRequests(recordingIDs []string, resolution recordingBatchResolution) partitionedResolvedRecordingRequests {
	out := partitionedResolvedRecordingRequests{}
	exactSeen := make(map[string]struct{}, len(recordingIDs))
	logicalSeen := make(map[string]struct{}, len(recordingIDs))
	for _, recordingID := range compactNonEmptyStrings(recordingIDs) {
		resolvedRecordingID := strings.TrimSpace(resolution.resolvedByRecording[recordingID])
		if resolvedRecordingID == "" {
			continue
		}
		if resolution.exactRequestedByID[recordingID] {
			if _, ok := exactSeen[resolvedRecordingID]; ok {
				continue
			}
			exactSeen[resolvedRecordingID] = struct{}{}
			out.exactResolvedIDs = append(out.exactResolvedIDs, resolvedRecordingID)
			continue
		}
		if _, ok := logicalSeen[resolvedRecordingID]; ok {
			continue
		}
		logicalSeen[resolvedRecordingID] = struct{}{}
		out.logicalResolvedIDs = append(out.logicalResolvedIDs, resolvedRecordingID)
	}
	return out
}

func (s *PlaybackService) resolvePlaybackRequest(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (string, string, bool, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "", "", false, fmt.Errorf("recording id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	if exact, ok, err := s.trackVariantExists(ctx, local.LibraryID, recordingID); err != nil {
		return "", "", false, err
	} else if ok {
		return exact, profile, true, nil
	}
	variants, err := s.app.catalog.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, recordingID, profile)
	if err != nil {
		return "", "", false, err
	}
	explicitPreferredID, _, err := s.app.catalog.preferredRecordingVariantID(ctx, local.LibraryID, local.DeviceID, recordingID)
	if err != nil {
		return "", "", false, err
	}
	if preferredID := chooseRecordingVariantID(variants, explicitPreferredID); preferredID != "" {
		return preferredID, profile, false, nil
	}
	return recordingID, profile, false, nil
}

func (s *PlaybackService) trackVariantExists(ctx context.Context, libraryID, recordingID string) (string, bool, error) {
	var row TrackVariantModel
	if err := s.app.storage.ReadWithContext(ctx).
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
	if err := s.app.storage.ReadWithContext(ctx).
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
	exactVariant, err := s.isExactTrackVariantID(ctx, libraryID, recordingID)
	if err != nil {
		return "", false, err
	}
	return s.bestLocalRecordingPathWithExactness(ctx, libraryID, deviceID, recordingID, exactVariant)
}

func (s *PlaybackService) bestLocalRecordingPathWithExactness(ctx context.Context, libraryID, deviceID, recordingID string, exactVariant bool) (string, bool, error) {
	type localPathRow struct{ LocalPath string }
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
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, args...).Scan(&result).Error; err != nil {
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
	exactVariant, err := s.isExactTrackVariantID(ctx, libraryID, recordingID)
	if err != nil {
		return "", "", false, err
	}
	return s.bestCachedEncodingWithExactness(ctx, libraryID, deviceID, recordingID, profile, exactVariant)
}

func (s *PlaybackService) bestCachedEncodingWithExactness(ctx context.Context, libraryID, deviceID, recordingID, profile string, exactVariant bool) (string, string, bool, error) {
	aliasProfile := normalizedPlaybackProfileAlias(profile)
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
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, args...).Scan(&result).Error; err != nil {
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

func (s *PlaybackService) batchBestLocalRecordingPaths(ctx context.Context, libraryID, deviceID string, recordingIDs []string, exactVariant bool) (map[string]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]string{}, nil
	}

	type row struct {
		RecordingID string
		LocalPath   string
	}
	query := `
SELECT
	sf.track_variant_id AS recording_id,
	sf.local_path
FROM source_files sf
WHERE sf.library_id = ? AND sf.track_variant_id IN ? AND sf.device_id = ? AND sf.is_present = 1
ORDER BY sf.track_variant_id ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC`
	if !exactVariant {
		query = `
SELECT
	req.track_variant_id AS recording_id,
	sf.local_path
FROM track_variants req
JOIN track_variants cand ON cand.library_id = req.library_id AND cand.track_cluster_id = req.track_cluster_id
JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = cand.track_variant_id
WHERE req.library_id = ? AND req.track_variant_id IN ? AND sf.device_id = ? AND sf.is_present = 1
ORDER BY req.track_variant_id ASC, CASE WHEN sf.track_variant_id = req.track_variant_id THEN 0 ELSE 1 END ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC`
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, libraryID, recordingIDs, deviceID).Scan(&rows).Error; err != nil {
		return nil, err
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

func (s *PlaybackService) batchBestCachedRecordingIDs(ctx context.Context, libraryID, deviceID string, recordingIDs []string, profile string, exactVariant bool) (map[string]bool, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return map[string]bool{}, nil
	}

	aliasProfile := normalizedPlaybackProfileAlias(profile)

	type row struct {
		RecordingID string
		BlobID      string
	}
	query := `
SELECT
	sf.track_variant_id AS recording_id,
	e.blob_id
FROM optimized_assets e
JOIN source_files sf ON sf.library_id = e.library_id AND sf.source_file_id = e.source_file_id
LEFT JOIN device_asset_caches de ON de.library_id = e.library_id AND de.optimized_asset_id = e.optimized_asset_id AND de.device_id = ?
WHERE e.library_id = ? AND sf.track_variant_id IN ? AND COALESCE(de.is_cached, 0) = 1 AND (? = '' OR e.profile = ? OR e.profile = ?)
ORDER BY sf.track_variant_id ASC, e.bitrate DESC, e.optimized_asset_id ASC`
	if !exactVariant {
		query = `
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
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, deviceID, libraryID, recordingIDs, profile, profile, aliasProfile).Scan(&rows).Error; err != nil {
		return nil, err
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

type resolvedRecordingPinTarget struct {
	scopeID             string
	scopeRecordingID    string
	resolvedRecordingID string
	clusterID           string
	profile             string
	exactRequested      bool
}

type pinScopeMaterializationOutcome struct {
	result       apitypes.PlaybackBatchResult
	total        int
	pendingCount int
}

func (s *PlaybackService) resolveRecordingPinTarget(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) (resolvedRecordingPinTarget, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return resolvedRecordingPinTarget{}, fmt.Errorf("recording id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	clusterID, ok, err := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID)
	if err != nil {
		return resolvedRecordingPinTarget{}, err
	}
	if !ok {
		return resolvedRecordingPinTarget{}, fmt.Errorf("recording %s not found", recordingID)
	}
	clusterID = strings.TrimSpace(clusterID)
	if exactRecordingID, ok, err := s.trackVariantExists(ctx, local.LibraryID, recordingID); err != nil {
		return resolvedRecordingPinTarget{}, err
	} else if ok {
		exactRecordingID = strings.TrimSpace(exactRecordingID)
		return resolvedRecordingPinTarget{
			scopeID:             exactRecordingID,
			scopeRecordingID:    exactRecordingID,
			resolvedRecordingID: exactRecordingID,
			clusterID:           clusterID,
			profile:             profile,
			exactRequested:      true,
		}, nil
	}

	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, clusterID, profile)
	if err != nil {
		return resolvedRecordingPinTarget{}, err
	}
	return resolvedRecordingPinTarget{
		scopeID:             clusterID,
		scopeRecordingID:    clusterID,
		resolvedRecordingID: resolvedRecordingID,
		clusterID:           clusterID,
		profile:             profile,
		exactRequested:      false,
	}, nil
}

func (s *PlaybackService) resolvePinScope(ctx context.Context, local apitypes.LocalContext, scope, scopeID, preferredProfile string) (string, []string, string, error) {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	profile := s.resolvePlaybackProfile(preferredProfile)
	switch scope {
	case "album":
		resolvedAlbumID, recordingIDs, err := s.resolveAlbumPinScope(ctx, local, scopeID)
		if err != nil {
			return "", nil, "", err
		}
		return resolvedAlbumID, recordingIDs, profile, nil
	case "playlist":
		if scopeID == "" {
			return "", nil, "", fmt.Errorf("playlist id is required")
		}
		recordingIDs, err := s.libraryRecordingIDsForPlaylist(ctx, local.LibraryID, scopeID)
		if err != nil {
			return "", nil, "", err
		}
		return scopeID, recordingIDs, profile, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported pin scope %q", scope)
	}
}

func (s *PlaybackService) resolveAlbumPinScope(ctx context.Context, local apitypes.LocalContext, albumID string) (string, []string, error) {
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

func (s *PlaybackService) validateAlbumPinStart(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) error {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil
	}

	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, preferredProfile)
	if err != nil {
		return err
	}
	partition := partitionResolvedRecordingRequests(recordingIDs, resolution)
	exactFacts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		partition.exactResolvedIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		time.Now().UTC().Add(-availabilityOnlineWindow),
		true,
	)
	if err != nil {
		return err
	}
	logicalFacts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		partition.logicalResolvedIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		time.Now().UTC().Add(-availabilityOnlineWindow),
		false,
	)
	if err != nil {
		return err
	}
	for _, recordingID := range recordingIDs {
		resolvedRecordingID := strings.TrimSpace(resolution.resolvedByRecording[recordingID])
		fact := logicalFacts[resolvedRecordingID]
		if resolution.exactRequestedByID[recordingID] {
			fact = exactFacts[resolvedRecordingID]
		}
		if !fact.hasLocalSource {
			return nil
		}
	}
	return fmt.Errorf("local albums do not need offline pinning")
}

func (s *PlaybackService) emitPinAvailabilityInvalidation(local apitypes.LocalContext, scope, scopeID string, recordingIDs []string) {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	s.app.emitPinChange(apitypes.PinChangeEvent{InvalidateAll: true})
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

func (s *PlaybackService) runRecordingPinJob(ctx context.Context, local apitypes.LocalContext, target resolvedRecordingPinTarget) {
	jobID := pinJobID(local.LibraryID, "recording", target.scopeID, target.profile)
	job := s.app.jobs.Track(jobID, jobKindPinRecording, local.LibraryID)
	if job == nil {
		return
	}
	job.Queued(0, "queued track pin")
	job.Running(0.1, "Checking 1 track")
	result, pending, err := s.materializeRecordingPinBestEffort(ctx, local, target)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(0, "track pin canceled", nil)
			return
		}
		job.Fail(0, "track pin failed", err)
		return
	}
	if err := s.app.pin.reconcileScope(ctx, local, "recording", target.scopeID, target.profile); err != nil {
		job.Fail(0, "track pin failed", err)
		return
	}
	job.Complete(1, recordingPinCompletionMessage(result, pending))
	if pending {
		s.app.pin.schedulePinScopeRefreshRetry(local.LibraryID, "recording", target.scopeID, target.profile)
	}
	s.emitPinAvailabilityInvalidation(local, "recording", target.scopeID, compactNonEmptyStrings([]string{target.scopeRecordingID, target.clusterID}))
}

func (s *PlaybackService) runPinScopeJob(ctx context.Context, local apitypes.LocalContext, scope, scopeID, profile string, recordingIDs []string, kind, label string) {
	jobID := pinJobID(local.LibraryID, scope, scopeID, profile)
	job := s.app.jobs.Track(jobID, kind, local.LibraryID)
	if job == nil {
		return
	}
	job.Queued(0, "queued "+label)
	outcome, err := s.materializePinScopeWithJob(ctx, local, scope, scopeID, recordingIDs, profile, job)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(0, label+" canceled", nil)
			return
		}
		job.Fail(pinJobProgress(outcome.result.Tracks, maxInt(outcome.total, 1)), label+" failed", err)
		return
	}
	job.Complete(1, pinScopeCompletionMessage(outcome.result, outcome.pendingCount, outcome.total))
	if outcome.pendingCount > 0 {
		s.app.pin.schedulePinScopeRefreshRetry(local.LibraryID, scope, scopeID, profile)
	}
	s.emitPinAvailabilityInvalidation(local, scope, scopeID, recordingIDs)
}

func (s *PlaybackService) materializePinScopeWithJob(ctx context.Context, local apitypes.LocalContext, scope, scopeID string, recordingIDs []string, profile string, job *JobTracker) (pinScopeMaterializationOutcome, error) {
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
	if job != nil {
		job.Running(0.05, fmt.Sprintf("Checked 0/%d tracks", len(uniqueRecordings)))
	}
	if len(uniqueRecordings) == 0 {
		outcome := pinScopeMaterializationOutcome{}
		return outcome, s.app.pin.reconcileScope(ctx, local, scope, scopeID, profile)
	}
	fetchableRecordings, pendingRecordings, err := s.recordingsReadyForPinFetch(ctx, local, uniqueRecordings, profile)
	if err != nil {
		return pinScopeMaterializationOutcome{}, err
	}

	type resultItem struct {
		recordingID string
		result      apitypes.PlaybackRecordingResult
		err         error
	}

	workerCount := 1
	if job != nil {
		workerCount = pinnedScopeWorkerCount
	}
	if len(fetchableRecordings) < workerCount {
		workerCount = len(fetchableRecordings)
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	outcome := pinScopeMaterializationOutcome{
		total:        len(uniqueRecordings),
		pendingCount: len(pendingRecordings),
	}
	if len(fetchableRecordings) == 0 {
		return outcome, s.app.pin.reconcileScope(ctx, local, scope, scopeID, profile)
	}

	workCh := make(chan string)
	resultCh := make(chan resultItem, len(fetchableRecordings))
	var workers sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for recordingID := range workCh {
				resolvedRecordingID, _, exactRequested, err := s.resolvePlaybackRequest(workCtx, local, recordingID, profile)
				if err != nil {
					resultCh <- resultItem{recordingID: recordingID, err: err}
					continue
				}
				result, err := s.prepareRecordingPinResult(workCtx, local, recordingID, resolvedRecordingID, profile, exactRequested)
				if err != nil {
					resultCh <- resultItem{recordingID: recordingID, err: err}
					continue
				}
				resultCh <- resultItem{recordingID: recordingID, result: result}
			}
		}()
	}

	go func() {
		stopped := false
		for _, recordingID := range fetchableRecordings {
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

	processed := 0
	for item := range resultCh {
		processed++
		if item.err != nil {
			if errors.Is(item.err, context.Canceled) && workCtx.Err() != nil {
				return outcome, item.err
			}
			outcome.pendingCount++
			s.logPinnedFetchDeferred(scope, scopeID, item.recordingID, item.err)
		} else {
			outcome.result.Tracks++
			outcome.result.TotalBytes += int64(item.result.Bytes)
			if item.result.FromLocal {
				outcome.result.LocalHits++
			} else {
				outcome.result.RemoteFetches++
			}
		}
		if job != nil {
			job.Running(pinJobProgress(processed, len(fetchableRecordings)), fmt.Sprintf("Checked %d/%d tracks", processed, len(fetchableRecordings)))
		}
	}
	return outcome, s.app.pin.reconcileScope(ctx, local, scope, scopeID, profile)
}

func (s *PlaybackService) materializeRecordingPinBestEffort(ctx context.Context, local apitypes.LocalContext, target resolvedRecordingPinTarget) (apitypes.PlaybackRecordingResult, bool, error) {
	recordings, pending, err := s.recordingsReadyForPinFetch(ctx, local, []string{target.scopeRecordingID}, target.profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}
	if len(recordings) == 0 {
		return apitypes.PlaybackRecordingResult{}, len(pending) > 0, nil
	}
	result, err := s.prepareRecordingPinResult(ctx, local, target.scopeRecordingID, target.resolvedRecordingID, target.profile, target.exactRequested)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return apitypes.PlaybackRecordingResult{}, false, err
		}
		s.logPinnedFetchDeferred("recording", target.scopeID, target.scopeRecordingID, err)
		return apitypes.PlaybackRecordingResult{}, true, nil
	}
	return result, false, nil
}

func (s *PlaybackService) recordingsReadyForPinFetch(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) ([]string, []string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil, nil
	}

	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, preferredProfile)
	if err != nil {
		return nil, nil, err
	}
	partition := partitionResolvedRecordingRequests(recordingIDs, resolution)
	exactFacts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		partition.exactResolvedIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		time.Now().UTC().Add(-availabilityOnlineWindow),
		true,
	)
	if err != nil {
		return nil, nil, err
	}
	logicalFacts, err := s.batchRecordingPlaybackFacts(
		ctx,
		local.LibraryID,
		local.DeviceID,
		partition.logicalResolvedIDs,
		resolution.profile,
		normalizedPlaybackProfileAlias(resolution.profile),
		time.Now().UTC().Add(-availabilityOnlineWindow),
		false,
	)
	if err != nil {
		return nil, nil, err
	}
	networkRunning := s.app.NetworkStatus().Running
	fetchable := make([]string, 0, len(recordingIDs))
	pending := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		resolvedRecordingID := strings.TrimSpace(resolution.resolvedByRecording[recordingID])
		fact := logicalFacts[resolvedRecordingID]
		if resolution.exactRequestedByID[recordingID] {
			fact = exactFacts[resolvedRecordingID]
		}
		canFetch := fact.hasLocalSource || fact.hasLocalCached
		if !canFetch && networkRunning && (fact.hasRemoteCachedOnline || fact.hasRemoteSourceOnline) {
			canFetch = true
		}
		if canFetch {
			fetchable = append(fetchable, recordingID)
		} else {
			pending = append(pending, recordingID)
		}
	}
	return fetchable, pending, nil
}

func (s *PlaybackService) logPinnedFetchDeferred(scope, scopeID, recordingID string, err error) {
	if s == nil || s.app == nil || s.app.cfg.Logger == nil || err == nil {
		return
	}
	s.app.cfg.Logger.Printf(
		"desktopcore: deferred pinned fetch for %s %s track %s: %v",
		strings.TrimSpace(scope),
		strings.TrimSpace(scopeID),
		strings.TrimSpace(recordingID),
		err,
	)
}

func recordingPinCompletionMessage(result apitypes.PlaybackRecordingResult, pending bool) string {
	if pending {
		if result.Bytes > 0 {
			return "Track pinned; fetch will retry for remaining assets"
		}
		return "Track pinned; waiting for source availability"
	}
	return "Track pinned for offline playback"
}

func pinScopeCompletionMessage(result apitypes.PlaybackBatchResult, pendingCount, total int) string {
	switch {
	case total <= 0:
		return "Pinned scope active"
	case pendingCount > 0 && result.Tracks > 0:
		return fmt.Sprintf("Pinned scope active: cached %d/%d tracks, %d waiting for availability", result.Tracks, total, pendingCount)
	case pendingCount > 0:
		return fmt.Sprintf("Pinned scope active: %d tracks waiting for availability", pendingCount)
	case result.Tracks == 1:
		return "Pinned 1 track offline"
	default:
		return fmt.Sprintf("Pinned %d tracks offline", result.Tracks)
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (s *PlaybackService) prepareRecordingPinResult(ctx context.Context, local apitypes.LocalContext, requestRecordingID, resolvedRecordingID, profile string, exactRequested bool) (apitypes.PlaybackRecordingResult, error) {
	requestRecordingID = strings.TrimSpace(requestRecordingID)
	resolvedRecordingID = strings.TrimSpace(resolvedRecordingID)
	blobID, encodingID, ok, err := s.bestCachedEncodingWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile, exactRequested)
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

	if localPath, ok, err := s.bestLocalRecordingPathWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, exactRequested); err != nil {
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

	remoteRecordingID := resolvedRecordingID
	if !exactRequested {
		remoteRecordingID = requestRecordingID
	}
	if result, fetched, err := s.ensureRemotePlaybackRecording(ctx, local, remoteRecordingID, profile); err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	} else if fetched {
		return result, nil
	}

	return apitypes.PlaybackRecordingResult{}, fmt.Errorf("recording %s has no local or cached asset available for offline pinning", requestRecordingID)
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

func (s *PlaybackService) libraryRecordingIDsForPlaylist(ctx context.Context, libraryID, playlistID string) ([]string, error) {
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
	partition := partitionResolvedRecordingRequests(recordingIDs, resolution)
	exactFacts, err := s.batchRecordingPlaybackFacts(ctx, local.LibraryID, local.DeviceID, partition.exactResolvedIDs, profile, aliasProfile, cutoff, true)
	if err != nil {
		return nil, err
	}
	logicalFacts, err := s.batchRecordingPlaybackFacts(ctx, local.LibraryID, local.DeviceID, partition.logicalResolvedIDs, profile, aliasProfile, cutoff, false)
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
			resolvedRecordingID := strings.TrimSpace(resolution.resolvedByRecording[recordingID])
			playbackFacts := logicalFacts[resolvedRecordingID]
			if resolution.exactRequestedByID[recordingID] {
				playbackFacts = exactFacts[resolvedRecordingID]
			}
			fact := playbackFacts.summary(s.app.NetworkStatus().Running)
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

func (s *PlaybackService) batchRecordingPlaybackFacts(ctx context.Context, libraryID, localDeviceID string, recordingIDs []string, profile, aliasProfile string, cutoff time.Time, exactVariant bool) (map[string]recordingPlaybackFacts, error) {
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
	req.track_variant_id AS recording_id,
	MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN 1 ELSE 0 END) AS has_local_source,
	MAX(CASE WHEN sf.device_id <> ? AND sf.is_present = 1 AND COALESCE(m.role, '') IN ('owner', 'admin', 'member') THEN 1 ELSE 0 END) AS has_remote_source,
	MAX(CASE WHEN sf.device_id <> ? AND sf.is_present = 1 AND COALESCE(m.role, '') IN ('owner', 'admin', 'member') AND d.last_seen_at >= ? THEN 1 ELSE 0 END) AS has_remote_source_online
FROM track_variants req
LEFT JOIN track_variants cand ON cand.library_id = req.library_id AND cand.track_cluster_id = req.track_cluster_id
LEFT JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = cand.track_variant_id
LEFT JOIN memberships m ON m.library_id = sf.library_id AND m.device_id = sf.device_id
LEFT JOIN devices d ON d.device_id = sf.device_id
WHERE req.library_id = ? AND req.track_variant_id IN ?
GROUP BY req.track_variant_id`
	if exactVariant {
		sourceQuery = `
SELECT
	req.track_variant_id AS recording_id,
	MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN 1 ELSE 0 END) AS has_local_source,
	MAX(CASE WHEN sf.device_id <> ? AND sf.is_present = 1 AND COALESCE(m.role, '') IN ('owner', 'admin', 'member') THEN 1 ELSE 0 END) AS has_remote_source,
	MAX(CASE WHEN sf.device_id <> ? AND sf.is_present = 1 AND COALESCE(m.role, '') IN ('owner', 'admin', 'member') AND d.last_seen_at >= ? THEN 1 ELSE 0 END) AS has_remote_source_online
FROM track_variants req
LEFT JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = req.track_variant_id
LEFT JOIN memberships m ON m.library_id = sf.library_id AND m.device_id = sf.device_id
LEFT JOIN devices d ON d.device_id = sf.device_id
WHERE req.library_id = ? AND req.track_variant_id IN ?
GROUP BY req.track_variant_id`
	}
	var sourceRows []sourceRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(sourceQuery, localDeviceID, localDeviceID, localDeviceID, cutoff, libraryID, recordingIDs).Scan(&sourceRows).Error; err != nil {
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
	req.track_variant_id AS recording_id,
	MAX(CASE WHEN dac.device_id = ? AND dac.is_cached = 1 THEN 1 ELSE 0 END) AS has_local_cached,
	MAX(CASE WHEN dac.device_id <> ? AND dac.is_cached = 1 THEN 1 ELSE 0 END) AS has_remote_cached,
	MAX(CASE WHEN dac.device_id <> ? AND dac.is_cached = 1 AND d.last_seen_at >= ? THEN 1 ELSE 0 END) AS has_remote_cached_online
FROM track_variants req
LEFT JOIN track_variants cand ON cand.library_id = req.library_id AND cand.track_cluster_id = req.track_cluster_id
LEFT JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = cand.track_variant_id
LEFT JOIN optimized_assets oa ON oa.library_id = sf.library_id AND oa.source_file_id = sf.source_file_id
LEFT JOIN device_asset_caches dac ON dac.library_id = oa.library_id AND dac.optimized_asset_id = oa.optimized_asset_id
LEFT JOIN devices d ON d.device_id = dac.device_id
WHERE req.library_id = ? AND req.track_variant_id IN ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
GROUP BY req.track_variant_id`
	if exactVariant {
		cacheQuery = `
SELECT
	req.track_variant_id AS recording_id,
	MAX(CASE WHEN dac.device_id = ? AND dac.is_cached = 1 THEN 1 ELSE 0 END) AS has_local_cached,
	MAX(CASE WHEN dac.device_id <> ? AND dac.is_cached = 1 THEN 1 ELSE 0 END) AS has_remote_cached,
	MAX(CASE WHEN dac.device_id <> ? AND dac.is_cached = 1 AND d.last_seen_at >= ? THEN 1 ELSE 0 END) AS has_remote_cached_online
FROM track_variants req
LEFT JOIN source_files sf ON sf.library_id = req.library_id AND sf.track_variant_id = req.track_variant_id
LEFT JOIN optimized_assets oa ON oa.library_id = sf.library_id AND oa.source_file_id = sf.source_file_id
LEFT JOIN device_asset_caches dac ON dac.library_id = oa.library_id AND dac.optimized_asset_id = oa.optimized_asset_id
LEFT JOIN devices d ON d.device_id = dac.device_id
WHERE req.library_id = ? AND req.track_variant_id IN ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
GROUP BY req.track_variant_id`
	}
	var cacheRows []cacheRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(cacheQuery, localDeviceID, localDeviceID, localDeviceID, cutoff, libraryID, recordingIDs, profile, profile, aliasProfile).Scan(&cacheRows).Error; err != nil {
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

func (s *PlaybackService) batchAlbumPins(ctx context.Context, libraryID, localDeviceID string, albumIDs []string, profile, aliasProfile string) (map[string]bool, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return map[string]bool{}, nil
	}

	type row struct{ ScopeID string }
	var rows []row
	query := `
SELECT scope_id
FROM pin_roots
WHERE library_id = ? AND device_id = ? AND scope = 'album' AND scope_id IN ? AND (profile = ? OR profile = ?)`
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, libraryID, localDeviceID, albumIDs, profile, aliasProfile).Scan(&rows).Error; err != nil {
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
	if err := s.app.storage.ReadWithContext(ctx).
		Model(&TrackVariantModel{}).
		Select("track_variant_id, track_cluster_id").
		Where("library_id = ? AND (track_variant_id IN ? OR track_cluster_id IN ?)", libraryID, recordingIDs, recordingIDs).
		Scan(&variants).Error; err != nil {
		return nil, err
	}
	clusterIDs := make([]string, 0, len(variants))
	clusterSeen := make(map[string]struct{}, len(variants))
	clusterByInput := make(map[string]string, len(recordingIDs))
	exactInputs := make(map[string]struct{}, len(recordingIDs))
	for _, row := range variants {
		variantID := strings.TrimSpace(row.TrackVariantID)
		clusterID := strings.TrimSpace(row.TrackClusterID)
		if clusterID == "" {
			continue
		}
		if variantID != "" {
			clusterByInput[variantID] = clusterID
			exactInputs[variantID] = struct{}{}
		}
		clusterByInput[clusterID] = clusterID
		if _, ok := clusterSeen[clusterID]; ok {
			continue
		}
		clusterSeen[clusterID] = struct{}{}
		clusterIDs = append(clusterIDs, clusterID)
	}

	for _, recordingID := range recordingIDs {
		recordingID = strings.TrimSpace(recordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := clusterByInput[recordingID]; !ok {
			clusterByInput[recordingID] = recordingID
		}
		clusterID := strings.TrimSpace(clusterByInput[recordingID])
		if clusterID == "" {
			continue
		}
		if _, ok := clusterSeen[clusterID]; ok {
			continue
		}
		clusterSeen[clusterID] = struct{}{}
		clusterIDs = append(clusterIDs, clusterID)
	}

	exactRecordingIDs := make([]string, 0, len(exactInputs))
	for recordingID := range exactInputs {
		exactRecordingIDs = append(exactRecordingIDs, recordingID)
	}
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	exactRecordingIDs = compactNonEmptyStrings(exactRecordingIDs)
	if len(clusterIDs) == 0 && len(exactRecordingIDs) == 0 {
		return map[string]bool{}, nil
	}
	local := apitypes.LocalContext{
		LibraryID: libraryID,
		DeviceID:  localDeviceID,
	}
	resolution, err := s.resolvePlaybackVariantsBatch(ctx, local, clusterIDs, profile)
	if err != nil && len(clusterIDs) > 0 {
		return nil, err
	}
	if len(clusterIDs) == 0 {
		clusterIDs = []string{""}
	}
	if len(exactRecordingIDs) == 0 {
		exactRecordingIDs = []string{""}
	}

	type row struct {
		LibraryRecordingID string
		VariantRecordingID string
		ResolutionPolicy   string
	}
	var rows []row
	query := `
SELECT library_recording_id, variant_recording_id, resolution_policy
FROM pin_members
WHERE library_id = ? AND device_id = ? AND scope = 'recording' AND (profile = ? OR profile = ?) AND (library_recording_id IN ? OR variant_recording_id IN ?)`
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, libraryID, localDeviceID, profile, aliasProfile, clusterIDs, exactRecordingIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}

	pinnedByCluster := make(map[string]struct{}, len(rows))
	pinnedByVariant := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		libraryRecordingID := strings.TrimSpace(row.LibraryRecordingID)
		variantRecordingID := strings.TrimSpace(row.VariantRecordingID)
		if variantRecordingID != "" {
			pinnedByVariant[variantRecordingID] = struct{}{}
		}
		if libraryRecordingID == "" {
			continue
		}
		if strings.TrimSpace(row.ResolutionPolicy) == "logical_preferred" {
			pinnedByCluster[libraryRecordingID] = struct{}{}
			continue
		}
		preferredID := strings.TrimSpace(resolution.resolvedByRecording[libraryRecordingID])
		if preferredID != "" && preferredID == variantRecordingID {
			pinnedByCluster[libraryRecordingID] = struct{}{}
		}
	}
	out := make(map[string]bool, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		recordingID = strings.TrimSpace(recordingID)
		clusterID := strings.TrimSpace(clusterByInput[recordingID])
		if clusterID == "" {
			clusterID = recordingID
		}
		_, exactInput := exactInputs[recordingID]
		if exactInput {
			_, out[recordingID] = pinnedByVariant[recordingID]
			continue
		}
		_, out[recordingID] = pinnedByCluster[clusterID]
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
