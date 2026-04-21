package desktopcore

import (
	"context"
	"fmt"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	playbackcore "ben/desktop/internal/playback"
)

type inspectorCatalogAdapter struct {
	inspector *Inspector
	local     apitypes.LocalContext
}

func (a *inspectorCatalogAdapter) Close() error { return nil }

func (a *inspectorCatalogAdapter) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return a.inspector.listRecordingsForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListRecordingsCursor(ctx context.Context, req apitypes.RecordingCursorRequest) (apitypes.CursorPage[apitypes.RecordingListItem], error) {
	return a.inspector.listRecordingsCursorForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return a.inspector.getRecordingForLocal(ctx, a.local, recordingID)
}

func (a *inspectorCatalogAdapter) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return apitypes.AlbumListItem{}, fmt.Errorf("album id is required")
	}

	rows, err := a.inspector.loadAlbumVariants(ctx, a.local.LibraryID, []string{albumID})
	if err != nil {
		return apitypes.AlbumListItem{}, err
	}
	if len(rows) == 0 {
		rows, err = a.inspector.loadAlbumVariantsByCluster(ctx, a.local.LibraryID, albumID)
		if err != nil {
			return apitypes.AlbumListItem{}, err
		}
	}
	if len(rows) == 0 {
		return apitypes.AlbumListItem{}, fmt.Errorf("album %s not found", albumID)
	}

	album := rows[0]
	return apitypes.AlbumListItem{
		LibraryAlbumID:          strings.TrimSpace(album.AlbumClusterID),
		PreferredVariantAlbumID: strings.TrimSpace(album.AlbumVariantID),
		AlbumID:                 strings.TrimSpace(album.AlbumVariantID),
		AlbumClusterID:          strings.TrimSpace(album.AlbumClusterID),
		Title:                   strings.TrimSpace(album.Title),
	}, nil
}

func (a *inspectorCatalogAdapter) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return a.inspector.listAlbumTracksForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return apitypes.PlaylistListItem{}, fmt.Errorf("playlist id is required")
	}
	if a.inspector.app.offline != nil && a.inspector.app.offline.matchesPlaylistID(a.local, playlistID) {
		return a.inspector.app.offline.summaryForLocal(ctx, a.local)
	}

	var playlist Playlist
	if err := a.inspector.app.storage.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", a.local.LibraryID, playlistID).
		Take(&playlist).Error; err != nil {
		return apitypes.PlaylistListItem{}, err
	}

	return apitypes.PlaylistListItem{
		PlaylistID: strings.TrimSpace(playlist.PlaylistID),
		Name:       strings.TrimSpace(playlist.Name),
		Kind:       apitypes.PlaylistKind(strings.TrimSpace(playlist.Kind)),
		CreatedBy:  strings.TrimSpace(playlist.CreatedBy),
		UpdatedAt:  playlist.UpdatedAt,
	}, nil
}

func (a *inspectorCatalogAdapter) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return a.inspector.listPlaylistTracksForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListPlaylistTracksCursor(ctx context.Context, req apitypes.PlaylistTrackCursorRequest) (apitypes.CursorPage[apitypes.PlaylistTrackItem], error) {
	return a.inspector.listPlaylistTracksCursorForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return a.inspector.listLikedRecordingsForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListLikedRecordingsCursor(ctx context.Context, req apitypes.LikedRecordingCursorRequest) (apitypes.CursorPage[apitypes.LikedRecordingItem], error) {
	return a.inspector.listLikedRecordingsCursorForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListOfflineRecordings(ctx context.Context, req apitypes.OfflineRecordingListRequest) (apitypes.Page[apitypes.OfflineRecordingItem], error) {
	if a.inspector.app.offline == nil {
		return apitypes.Page[apitypes.OfflineRecordingItem]{}, fmt.Errorf("offline service is unavailable")
	}
	seeds, pageInfo, err := a.inspector.app.offline.listSeedsPage(ctx, a.local, req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.OfflineRecordingItem]{}, err
	}
	items, err := a.inspector.app.catalog.buildOfflineRecordingItems(ctx, a.local.LibraryID, a.local.DeviceID, seeds)
	if err != nil {
		return apitypes.Page[apitypes.OfflineRecordingItem]{}, err
	}
	return apitypes.Page[apitypes.OfflineRecordingItem]{Items: items, Page: pageInfo}, nil
}

func (a *inspectorCatalogAdapter) ListOfflineRecordingsCursor(ctx context.Context, req apitypes.OfflineRecordingCursorRequest) (apitypes.CursorPage[apitypes.OfflineRecordingItem], error) {
	if a.inspector.app.offline == nil {
		return apitypes.CursorPage[apitypes.OfflineRecordingItem]{}, fmt.Errorf("offline service is unavailable")
	}
	seeds, pageInfo, err := a.inspector.app.offline.listSeedsCursor(ctx, a.local, req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.OfflineRecordingItem]{}, err
	}
	items, err := a.inspector.app.catalog.buildOfflineRecordingItems(ctx, a.local.LibraryID, a.local.DeviceID, seeds)
	if err != nil {
		return apitypes.CursorPage[apitypes.OfflineRecordingItem]{}, err
	}
	return apitypes.CursorPage[apitypes.OfflineRecordingItem]{
		Items: items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (a *inspectorCatalogAdapter) SubscribeCatalogChanges(func(apitypes.CatalogChangeEvent)) func() {
	return func() {}
}

func (a *inspectorCatalogAdapter) InspectPlaybackRecording(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) PreparePlaybackRecording(context.Context, string, string, apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) GetPlaybackPreparation(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) ResolvePlaybackRecording(context.Context, string, string) (apitypes.PlaybackResolveResult, error) {
	return apitypes.PlaybackResolveResult{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) PreparePlaybackTarget(context.Context, playbackcore.PlaybackTargetRef, string, apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) GetPlaybackTargetPreparation(context.Context, playbackcore.PlaybackTargetRef, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) GetPlaybackTargetAvailability(context.Context, playbackcore.PlaybackTargetRef, string) (apitypes.RecordingPlaybackAvailability, error) {
	return apitypes.RecordingPlaybackAvailability{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) ListPlaybackTargetAvailability(context.Context, playbackcore.TargetAvailabilityRequest) ([]playbackcore.TargetAvailability, error) {
	return nil, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) ResolveArtworkRef(context.Context, apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	return apitypes.ArtworkResolveResult{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) ResolveAlbumArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) ResolveRecordingArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) GetRecordingAvailability(context.Context, string, string) (apitypes.RecordingPlaybackAvailability, error) {
	return apitypes.RecordingPlaybackAvailability{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) ListRecordingPlaybackAvailability(context.Context, apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error) {
	return nil, fmt.Errorf("not implemented")
}

func (i *Inspector) TracePlaybackContext(ctx context.Context, req TracePlaybackContextRequest) (PlaybackContextTrace, error) {
	trace := PlaybackContextTrace{
		SchemaVersion:  inspectSchemaVersion,
		Request:        inspectorRequest(req),
		Identity:       map[string]any{},
		RawRows:        map[string]any{},
		Decisions:      []InspectDecision{},
		ComputedOutput: map[string]any{},
		Anomalies:      []InspectAnomaly{},
	}

	resolution, local, err := i.resolveLocalContext(ctx, req.ResolveInspectContextRequest)
	trace.Context = resolution
	if err != nil {
		return trace, err
	}

	adapter := &inspectorCatalogAdapter{inspector: i, local: local}
	loader := playbackcore.NewCatalogLoader(adapter)

	var (
		contextInput playbackcore.PlaybackContextInput
		sourceReq    playbackcore.PlaybackSourceRequest
		rawRows      map[string]any
		albumFamily  string
	)
	switch strings.ToLower(strings.TrimSpace(req.Kind)) {
	case "album":
		sourceReq = loader.BuildAlbumSource(req.ID)
		if err == nil {
			albumFamily, err = i.albumFamilyForRequest(ctx, local, req.ID)
		}
		rawRows, _ = i.albumContextRawRows(ctx, local, req.ID)
	case "playlist":
		sourceReq = loader.BuildPlaylistSource(req.ID)
		rawRows, _ = i.playlistContextRawRows(ctx, local, req.ID)
	case "liked":
		sourceReq = loader.BuildLikedSource()
		rawRows, _ = i.likedContextRawRows(ctx, local)
	case "recording":
		sourceReq = loader.BuildRecordingSource(req.ID)
		rawRows = map[string]any{}
	default:
		err = fmt.Errorf("unsupported context kind %q", req.Kind)
	}
	if rawRows != nil {
		trace.RawRows = rawRows
	}
	if err != nil {
		return trace, err
	}
	contextInput, err = loader.MaterializeSource(ctx, sourceReq)
	if err != nil {
		return trace, err
	}

	trace.Identity = map[string]any{
		"context_kind": strings.ToLower(strings.TrimSpace(req.Kind)),
		"context_id":   strings.TrimSpace(contextInput.ID),
	}
	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "materialize_context",
		Inputs: map[string]any{
			"kind": req.Kind,
			"id":   req.ID,
		},
		Result: map[string]any{
			"start_index": contextInput.StartIndex,
			"item_count":  len(contextInput.Items),
		},
		Reason: "materialize playback context through internal/playback.CatalogLoader using the inspector's explicit read-only context",
	})

	entryOutputs := make([]map[string]any, 0, len(contextInput.Items))
	for index, item := range contextInput.Items {
		logicalResolvedVariantID := ""
		if logicalID := strings.TrimSpace(item.Target.LogicalRecordingID); logicalID != "" {
			logicalResolvedVariantID, _, err = i.app.playback.resolvePlaybackVariant(ctx, local, logicalID, resolution.Selected.PreferredProfile)
			if err != nil {
				return trace, err
			}
		}
		exactResolvedVariantID := ""
		if exactID := strings.TrimSpace(item.Target.ExactVariantRecordingID); exactID != "" {
			exactResolvedVariantID, _, err = i.app.playback.resolvePlaybackVariant(ctx, local, exactID, resolution.Selected.PreferredProfile)
			if err != nil {
				return trace, err
			}
		}
		resolvedVariantID, resolvedProfile, err := i.app.playback.resolvePlaybackVariant(ctx, local, playbackTargetInputID(item.Target), resolution.Selected.PreferredProfile)
		if err != nil {
			return trace, err
		}
		availability, err := i.getRecordingAvailabilityForLocal(ctx, local, playbackTargetInputID(item.Target), resolvedProfile, resolution.Selected.NetworkRunning)
		if err != nil {
			return trace, err
		}
		output := map[string]any{
			"index": index,
			"item": map[string]any{
				"library_recording_id": item.LibraryRecordingID,
				"variant_recording_id": item.VariantRecordingID,
				"recording_id":         item.RecordingID,
				"title":                item.Title,
				"source_kind":          item.SourceKind,
				"source_id":            item.SourceID,
				"source_item_id":       item.SourceItemID,
				"album_id":             item.AlbumID,
				"variant_album_id":     item.VariantAlbumID,
			},
			"target": map[string]any{
				"logical_recording_id":       item.Target.LogicalRecordingID,
				"exact_variant_recording_id": item.Target.ExactVariantRecordingID,
				"resolution_policy":          item.Target.ResolutionPolicy,
			},
			"logical_resolution_variant_id": logicalResolvedVariantID,
			"exact_resolution_variant_id":   exactResolvedVariantID,
			"resolved_variant_id":           resolvedVariantID,
			"availability":                  availabilityMap(availability),
		}
		entryOutputs = append(entryOutputs, output)

		if albumFamily != "" && logicalResolvedVariantID != "" {
			families, famErr := i.albumClustersForExactVariant(ctx, local.LibraryID, logicalResolvedVariantID)
			if famErr != nil {
				return trace, famErr
			}
			if len(families) > 0 && !stringSliceContains(families, albumFamily) {
				trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
					anomalyAlbumContextEntryResolvesToForeignAlbumVariant,
					"error",
					"logical album context entry resolves outside the owning album family",
					map[string]any{
						"context_album_cluster_id":    albumFamily,
						"logical_recording_id":        item.Target.LogicalRecordingID,
						"logical_resolved_variant_id": logicalResolvedVariantID,
						"exact_variant_recording_id":  item.Target.ExactVariantRecordingID,
						"resolved_album_cluster_ids":  families,
						"context_index":               index,
					},
				))
			}
		}
	}

	trace.ComputedOutput = map[string]any{
		"kind":                       contextInput.Kind,
		"id":                         contextInput.ID,
		"start_index":                contextInput.StartIndex,
		"materialized_session_items": entryOutputs,
	}
	trace.Anomalies = sortAnomalies(trace.Anomalies)
	return trace, nil
}

func (i *Inspector) getRecordingAvailabilityForLocal(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string, networkRunning bool) (apitypes.RecordingPlaybackAvailability, error) {
	recordingID = strings.TrimSpace(recordingID)
	resolvedRecordingID, profile, exactRequested, err := i.app.playback.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	out := apitypes.RecordingPlaybackAvailability{
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: profile,
	}
	if pinned, pinErr := i.app.playback.recordingScopePinned(ctx, local.LibraryID, local.DeviceID, strings.TrimSpace(recordingID), profile); pinErr != nil {
		return apitypes.RecordingPlaybackAvailability{}, pinErr
	} else {
		out.Pinned = pinned
	}
	if localPath, ok, err := i.app.playback.bestLocalRecordingPathWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, exactRequested); err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	} else if ok {
		out.State = apitypes.AvailabilityPlayableLocalFile
		out.SourceKind = apitypes.PlaybackSourceLocalFile
		out.LocalPath = localPath
		return out, nil
	}
	if _, _, ok, err := i.app.playback.bestCachedEncodingWithExactness(ctx, local.LibraryID, local.DeviceID, resolvedRecordingID, profile, exactRequested); err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	} else if ok {
		out.State = apitypes.AvailabilityPlayableCachedOpt
		out.SourceKind = apitypes.PlaybackSourceCachedOpt
		return out, nil
	}
	items, err := i.listRecordingAvailabilityForLocal(ctx, local, recordingID, profile)
	if err != nil {
		return apitypes.RecordingPlaybackAvailability{}, err
	}
	hasRemoteCached := false
	remoteCachedOnline := false
	providerFound := false
	providerOnline := false
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

func (i *Inspector) listRecordingAvailabilityForLocal(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	recordingID = strings.TrimSpace(recordingID)
	resolvedRecordingID, profile, exactVariant, err := i.app.playback.resolvePlaybackRequest(ctx, local, recordingID, preferredProfile)
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
	if err := i.app.storage.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
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

func (i *Inspector) listAlbumTracksForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	type row struct {
		RecordingID    string
		TrackClusterID string
		Title          string
		DurationMS     int64
		DiscNo         int
		TrackNo        int
		ArtistsCSV     string
	}
	albumID := strings.TrimSpace(req.AlbumID)
	if explicitAlbumID, ok, err := i.app.catalog.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, albumID); err != nil {
		return apitypes.Page[apitypes.AlbumTrackItem]{}, err
	} else if ok && explicitAlbumID != "" {
		albumID = explicitAlbumID
	}
	query := `
SELECT
	at.track_variant_id AS recording_id,
	r.track_cluster_id AS track_cluster_id,
	r.title,
	r.duration_ms,
	at.disc_no,
	at.track_no,
	COALESCE(GROUP_CONCAT(ar.name, '` + artistSeparator + `'), '') AS artists_csv
FROM album_tracks at
JOIN track_variants r ON r.library_id = at.library_id AND r.track_variant_id = at.track_variant_id
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
WHERE at.library_id = ? AND at.album_variant_id = ?
GROUP BY at.track_variant_id, r.track_cluster_id, r.title, r.duration_ms, at.disc_no, at.track_no
ORDER BY at.disc_no ASC, at.track_no ASC, at.track_variant_id ASC`
	var rows []row
	if err := i.app.storage.WithContext(ctx).Raw(query, local.LibraryID, albumID).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.AlbumTrackItem]{}, err
	}
	paged, pageInfo := pageItems(rows, req.PageRequest)
	out := make([]apitypes.AlbumTrackItem, 0, len(paged))
	for _, row := range paged {
		out = append(out, apitypes.AlbumTrackItem{
			LibraryRecordingID: strings.TrimSpace(row.TrackClusterID),
			VariantRecordingID: strings.TrimSpace(row.RecordingID),
			RecordingID:        strings.TrimSpace(row.RecordingID),
			Title:              strings.TrimSpace(row.Title),
			DurationMS:         row.DurationMS,
			DiscNo:             row.DiscNo,
			TrackNo:            row.TrackNo,
			Artists:            splitArtists(row.ArtistsCSV),
		})
	}
	return apitypes.Page[apitypes.AlbumTrackItem]{Items: out, Page: pageInfo}, nil
}

func (i *Inspector) listRecordingsForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	seeds, pageInfo, err := i.app.catalog.listTrackSourceSeedsPage(ctx, local.LibraryID, req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}
	return i.app.catalog.listCollapsedRecordings(ctx, local.LibraryID, local.DeviceID, seeds, pageInfo)
}

func (i *Inspector) listRecordingsCursorForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.RecordingCursorRequest) (apitypes.CursorPage[apitypes.RecordingListItem], error) {
	seeds, pageInfo, err := i.app.catalog.listTrackSourceSeedsCursor(ctx, local.LibraryID, req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.RecordingListItem]{}, err
	}
	page, err := i.app.catalog.listCollapsedRecordings(ctx, local.LibraryID, local.DeviceID, seeds, apitypes.PageInfo{
		Limit:    pageInfo.Limit,
		Returned: len(seeds),
		HasMore:  pageInfo.HasMore,
	})
	if err != nil {
		return apitypes.CursorPage[apitypes.RecordingListItem]{}, err
	}
	return apitypes.CursorPage[apitypes.RecordingListItem]{
		Items: page.Items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(page.Items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (i *Inspector) listPlaylistTracksForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	seeds, pageInfo, err := i.app.catalog.listPlaylistSourceSeedsPage(ctx, local.LibraryID, strings.TrimSpace(req.PlaylistID), req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	items, err := i.app.catalog.buildPlaylistTrackItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	return apitypes.Page[apitypes.PlaylistTrackItem]{Items: items, Page: pageInfo}, nil
}

func (i *Inspector) listPlaylistTracksCursorForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.PlaylistTrackCursorRequest) (apitypes.CursorPage[apitypes.PlaylistTrackItem], error) {
	seeds, pageInfo, err := i.app.catalog.listPlaylistSourceSeedsCursor(ctx, local.LibraryID, strings.TrimSpace(req.PlaylistID), req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.PlaylistTrackItem]{}, err
	}
	items, err := i.app.catalog.buildPlaylistTrackItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.CursorPage[apitypes.PlaylistTrackItem]{}, err
	}
	return apitypes.CursorPage[apitypes.PlaylistTrackItem]{
		Items: items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (i *Inspector) listLikedRecordingsForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	seeds, pageInfo, err := i.app.catalog.listLikedSourceSeedsPage(ctx, local.LibraryID, req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	items, err := i.app.catalog.buildLikedRecordingItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	return apitypes.Page[apitypes.LikedRecordingItem]{Items: items, Page: pageInfo}, nil
}

func (i *Inspector) listLikedRecordingsCursorForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.LikedRecordingCursorRequest) (apitypes.CursorPage[apitypes.LikedRecordingItem], error) {
	seeds, pageInfo, err := i.app.catalog.listLikedSourceSeedsCursor(ctx, local.LibraryID, req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.LikedRecordingItem]{}, err
	}
	items, err := i.app.catalog.buildLikedRecordingItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.CursorPage[apitypes.LikedRecordingItem]{}, err
	}
	return apitypes.CursorPage[apitypes.LikedRecordingItem]{
		Items: items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (i *Inspector) albumFamilyForRequest(ctx context.Context, local apitypes.LocalContext, albumID string) (string, error) {
	clusterID, ok, err := i.app.catalog.albumClusterIDForVariant(ctx, local.LibraryID, albumID)
	if err != nil {
		return "", err
	}
	if ok {
		return clusterID, nil
	}
	return "", nil
}

func (i *Inspector) albumClustersForRecordingRequest(ctx context.Context, libraryID, recordingID string) ([]string, error) {
	type row struct {
		AlbumClusterID string
	}
	var rows []row
	query := `
SELECT DISTINCT av.album_cluster_id
FROM track_variants req
JOIN track_variants cand ON cand.library_id = req.library_id AND cand.track_cluster_id = req.track_cluster_id
JOIN album_tracks at ON at.library_id = cand.library_id AND at.track_variant_id = cand.track_variant_id
JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
WHERE req.library_id = ? AND req.track_variant_id = ?
ORDER BY av.album_cluster_id ASC`
	if err := i.app.storage.WithContext(ctx).Raw(query, libraryID, strings.TrimSpace(recordingID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.AlbumClusterID))
	}
	return compactNonEmptyStrings(out), nil
}

func (i *Inspector) albumClustersForExactVariant(ctx context.Context, libraryID, variantID string) ([]string, error) {
	type row struct {
		AlbumClusterID string
	}
	var rows []row
	query := `
SELECT DISTINCT av.album_cluster_id
FROM album_tracks at
JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
WHERE at.library_id = ? AND at.track_variant_id = ?
ORDER BY av.album_cluster_id ASC`
	if err := i.app.storage.WithContext(ctx).Raw(query, libraryID, strings.TrimSpace(variantID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.AlbumClusterID))
	}
	return compactNonEmptyStrings(out), nil
}

func (i *Inspector) albumContextRawRows(ctx context.Context, local apitypes.LocalContext, albumID string) (map[string]any, error) {
	explicitAlbumID, ok, err := i.app.catalog.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return nil, err
	}
	if !ok {
		explicitAlbumID = strings.TrimSpace(albumID)
	}
	albumRows, err := i.loadAlbumVariants(ctx, local.LibraryID, []string{explicitAlbumID})
	if err != nil {
		return nil, err
	}
	trackRows, err := i.loadAlbumTracksByAlbumIDs(ctx, local.LibraryID, []string{explicitAlbumID})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"album_variants": dumpAlbumVariants(albumRows),
		"album_tracks":   dumpAlbumTracks(trackRows),
	}, nil
}

func (i *Inspector) playlistContextRawRows(ctx context.Context, local apitypes.LocalContext, playlistID string) (map[string]any, error) {
	var playlists []Playlist
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ?", local.LibraryID, strings.TrimSpace(playlistID)).
		Find(&playlists).Error; err != nil {
		return nil, err
	}
	var items []PlaylistItem
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", local.LibraryID, strings.TrimSpace(playlistID)).
		Order("position_key ASC, item_id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return map[string]any{
		"playlists":      dumpPlaylists(playlists),
		"playlist_items": dumpPlaylistItems(items),
	}, nil
}

func (i *Inspector) likedContextRawRows(ctx context.Context, local apitypes.LocalContext) (map[string]any, error) {
	return i.playlistContextRawRows(ctx, local, likedPlaylistIDForLibrary(local.LibraryID))
}
