package desktopcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apitypes "ben/core/api/types"
	"gorm.io/gorm/clause"
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

func (s *PlaybackService) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}
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
	status, err := s.InspectPlaybackRecording(ctx, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackPreparationStatus{}, err
	}
	if purpose == "" {
		purpose = apitypes.PlaybackPreparationPlayNow
	}
	status.Purpose = purpose
	s.mu.Lock()
	s.preparations[s.preparationKey(recordingID, status.PreferredProfile)] = status
	s.mu.Unlock()
	return status, nil
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
	path, err := s.pathForBlob(artwork.BlobID)
	if err != nil {
		return result, nil
	}
	if _, err := os.Stat(path); err != nil {
		return result, nil
	}
	result.LocalPath = path
	result.Available = true
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
		RecordingID: resolvedRecordingID,
		AlbumID:     albumID,
	}
	if albumID == "" {
		return result, nil
	}
	var rows []ArtworkVariant
	if err := s.app.db.WithContext(ctx).
		Where("library_id = ? AND scope_type = 'album' AND scope_id = ? AND variant = ?", local.LibraryID, albumID, strings.TrimSpace(variant)).
		Find(&rows).Error; err != nil {
		return apitypes.RecordingArtworkResult{}, err
	}
	if len(rows) == 0 {
		return result, nil
	}
	ref := choosePreferredArtwork(rows)
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
		WHERE oa.library_id = m.library_id AND oa.created_by_device_id = m.device_id AND oa.track_variant_id = ? AND (? = '' OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS optimized_present,
	CASE WHEN EXISTS (
		SELECT 1 FROM device_asset_caches dac
		JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
		WHERE dac.library_id = m.library_id AND dac.device_id = m.device_id AND dac.is_cached = 1 AND oa.track_variant_id = ? AND (? = '' OR oa.profile = ?)
	) THEN 1 ELSE 0 END AS cached_optimized
FROM memberships m
LEFT JOIN devices d ON d.device_id = m.device_id
LEFT JOIN peer_sync_states pss ON pss.library_id = m.library_id AND pss.device_id = m.device_id
WHERE m.library_id = ?
ORDER BY CASE WHEN m.device_id = ? THEN 0 ELSE 1 END, m.device_id ASC`
	var rows []row
	if err := s.app.db.WithContext(ctx).Raw(query,
		resolvedRecordingID,
		resolvedRecordingID, profile, profile,
		resolvedRecordingID, profile, profile,
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
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	out := apitypes.RecordingPlaybackAvailability{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: profile,
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
	providerFound := false
	providerOnline := false
	for _, item := range items {
		if item.DeviceID != local.DeviceID && item.CachedOptimized {
			hasRemoteCached = true
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
	case hasRemoteCached:
		out.State = apitypes.AvailabilityUnavailableProvider
		out.Reason = apitypes.PlaybackUnavailableNetworkOff
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

func (s *PlaybackService) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	resolvedRecordingID, profile, err := s.resolvePlaybackVariant(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	result, err := s.prepareRecordingOfflineResult(ctx, local, resolvedRecordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	if err := s.upsertOfflinePin(ctx, local.LibraryID, local.DeviceID, "recording", resolvedRecordingID, profile); err != nil {
		return apitypes.PlaybackRecordingResult{}, err
	}
	return result, nil
}

func (s *PlaybackService) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	resolvedRecordingID, _, resolveErr := s.resolvePlaybackVariant(ctx, local, recordingID, "")
	if resolveErr == nil && strings.TrimSpace(resolvedRecordingID) != "" {
		recordingID = resolvedRecordingID
	}
	return s.deleteOfflinePin(ctx, local.LibraryID, local.DeviceID, "recording", recordingID)
}

func (s *PlaybackService) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return apitypes.PlaybackBatchResult{}, fmt.Errorf("album id is required")
	}
	recordingIDs, err := s.recordingIDsForAlbum(ctx, local.LibraryID, albumID)
	if err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	if len(recordingIDs) == 0 {
		return apitypes.PlaybackBatchResult{}, fmt.Errorf("no recordings found for album %s", albumID)
	}
	return s.pinOfflineScope(ctx, local, "album", albumID, recordingIDs, preferredProfile)
}

func (s *PlaybackService) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	return s.deleteOfflinePin(ctx, local.LibraryID, local.DeviceID, "album", albumID)
}

func (s *PlaybackService) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
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
	if len(recordingIDs) == 0 {
		return apitypes.PlaybackBatchResult{}, fmt.Errorf("no recordings found for playlist %s", playlistID)
	}
	return s.pinOfflineScope(ctx, local, "playlist", playlistID, recordingIDs, preferredProfile)
}

func (s *PlaybackService) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	return s.deleteOfflinePin(ctx, local.LibraryID, local.DeviceID, "playlist", playlistID)
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
	if len(recordingIDs) == 0 {
		profile := s.resolvePlaybackProfile(preferredProfile)
		if err := s.upsertOfflinePin(ctx, local.LibraryID, local.DeviceID, "playlist", playlistID, profile); err != nil {
			return apitypes.PlaybackBatchResult{}, err
		}
		return apitypes.PlaybackBatchResult{}, nil
	}
	return s.pinOfflineScope(ctx, local, "playlist", playlistID, recordingIDs, preferredProfile)
}

func (s *PlaybackService) UnpinLikedOffline(ctx context.Context) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	return s.deleteOfflinePin(ctx, local.LibraryID, local.DeviceID, "playlist", likedPlaylistIDForLibrary(local.LibraryID))
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
	summaries := make([]apitypes.TrackAvailabilitySummary, 0, len(tracks.Items))
	for _, track := range tracks.Items {
		summary, _, _, err := s.recordingAvailabilitySummary(ctx, local, track.RecordingID, preferredProfile)
		if err != nil {
			return apitypes.AlbumAvailabilityOverview{}, err
		}
		summaries = append(summaries, summary)
		out.Tracks = append(out.Tracks, apitypes.AlbumTrackAvailabilityOverview{Track: track})
	}
	out.Availability = aggregateAvailabilitySummaries(summaries)
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

func (s *PlaybackService) preparationKey(recordingID, profile string) string {
	return strings.TrimSpace(recordingID) + "|" + strings.TrimSpace(profile)
}

func (s *PlaybackService) bestLocalRecordingPath(ctx context.Context, libraryID, deviceID, recordingID string) (string, bool, error) {
	type localPathRow struct{ LocalPath string }
	query := `
SELECT sf.local_path
FROM source_files sf
JOIN track_variants req ON req.library_id = sf.library_id
JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
WHERE sf.library_id = ? AND sf.device_id = ? AND sf.is_present = 1 AND req.track_variant_id = ? AND cand.track_cluster_id = req.track_cluster_id
ORDER BY CASE WHEN sf.track_variant_id = ? THEN 0 ELSE 1 END ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC
LIMIT 1`
	var result localPathRow
	if err := s.app.db.WithContext(ctx).Raw(query, libraryID, deviceID, recordingID, recordingID).Scan(&result).Error; err != nil {
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
WHERE e.library_id = ? AND cand.track_cluster_id = req.track_cluster_id AND COALESCE(de.is_cached, 0) = 1 AND (? = '' OR e.profile = ?)
ORDER BY CASE WHEN sf.track_variant_id = ? THEN 0 ELSE 1 END ASC, e.bitrate DESC, e.optimized_asset_id ASC
LIMIT 1`
	var result encodingRow
	if err := s.app.db.WithContext(ctx).Raw(query, recordingID, libraryID, deviceID, libraryID, profile, profile, recordingID).Scan(&result).Error; err != nil {
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

func (s *PlaybackService) pathForBlob(blobID string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(blobID), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) != "b3" {
		return "", fmt.Errorf("invalid blob id")
	}
	hashHex := strings.ToLower(strings.TrimSpace(parts[1]))
	if len(hashHex) != 64 {
		return "", fmt.Errorf("invalid blob id")
	}
	return filepath.Join(s.app.cfg.BlobRoot, "b3", hashHex[:2], hashHex[2:4], hashHex), nil
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

func (s *PlaybackService) pinOfflineScope(ctx context.Context, local apitypes.LocalContext, scope, scopeID string, recordingIDs []string, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	profile := s.resolvePlaybackProfile(preferredProfile)
	seenRecordings := make(map[string]struct{}, len(recordingIDs))
	out := apitypes.PlaybackBatchResult{}
	for _, recordingID := range recordingIDs {
		recordingID = strings.TrimSpace(recordingID)
		if recordingID == "" {
			continue
		}
		if _, ok := seenRecordings[recordingID]; ok {
			continue
		}
		seenRecordings[recordingID] = struct{}{}
		resolvedRecordingID, _, err := s.resolvePlaybackVariant(ctx, local, recordingID, profile)
		if err != nil {
			return apitypes.PlaybackBatchResult{}, err
		}
		result, err := s.prepareRecordingOfflineResult(ctx, local, resolvedRecordingID, profile)
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
	if err := s.upsertOfflinePin(ctx, local.LibraryID, local.DeviceID, scope, scopeID, profile); err != nil {
		return apitypes.PlaybackBatchResult{}, err
	}
	return out, nil
}

func (s *PlaybackService) prepareRecordingOfflineResult(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (apitypes.PlaybackRecordingResult, error) {
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
		if err := s.app.db.WithContext(ctx).
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

	return apitypes.PlaybackRecordingResult{}, fmt.Errorf("recording %s has no local or cached asset available for offline pinning", recordingID)
}

func (s *PlaybackService) upsertOfflinePin(ctx context.Context, libraryID, deviceID, scope, scopeID, profile string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scope == "" || scopeID == "" {
		return fmt.Errorf("offline pin scope and scope id are required")
	}
	now := time.Now().UTC()
	return s.app.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
			{Name: "scope"},
			{Name: "scope_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"profile", "updated_at"}),
	}).Create(&OfflinePin{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		Scope:     scope,
		ScopeID:   scopeID,
		Profile:   strings.TrimSpace(profile),
		CreatedAt: now,
		UpdatedAt: now,
	}).Error
}

func (s *PlaybackService) deleteOfflinePin(ctx context.Context, libraryID, deviceID, scope, scopeID string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return fmt.Errorf("%s id is required", scope)
	}
	return s.app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", libraryID, deviceID, scope, scopeID).
		Delete(&OfflinePin{}).Error
}

func (s *PlaybackService) recordingIDsForAlbum(ctx context.Context, libraryID, albumID string) ([]string, error) {
	type row struct{ RecordingID string }
	var rows []row
	if err := s.app.db.WithContext(ctx).
		Table("album_tracks").
		Select("track_variant_id AS recording_id").
		Where("library_id = ? AND album_variant_id = ?", libraryID, strings.TrimSpace(albumID)).
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
	if err := s.app.db.WithContext(ctx).Raw(query, libraryID, strings.TrimSpace(playlistID)).Scan(&rows).Error; err != nil {
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
