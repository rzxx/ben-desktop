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
