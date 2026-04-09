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

func (a *inspectorCatalogAdapter) ListRecordings(context.Context, apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return apitypes.Page[apitypes.RecordingListItem]{}, fmt.Errorf("not implemented")
}

func (a *inspectorCatalogAdapter) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return a.inspector.getRecordingForLocal(ctx, a.local, recordingID)
}

func (a *inspectorCatalogAdapter) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return a.inspector.listAlbumTracksForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return a.inspector.listPlaylistTracksForLocal(ctx, a.local, req)
}

func (a *inspectorCatalogAdapter) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return a.inspector.listLikedRecordingsForLocal(ctx, a.local, req)
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
		rawRows      map[string]any
		albumFamily  string
	)
	switch strings.ToLower(strings.TrimSpace(req.Kind)) {
	case "album":
		contextInput, err = loader.LoadAlbumContext(ctx, req.ID)
		if err == nil {
			albumFamily, err = i.albumFamilyForRequest(ctx, local, req.ID)
		}
		rawRows, _ = i.albumContextRawRows(ctx, local, req.ID)
	case "playlist":
		contextInput, err = loader.LoadPlaylistContext(ctx, req.ID)
		rawRows, _ = i.playlistContextRawRows(ctx, local, req.ID)
	case "liked":
		contextInput, err = loader.LoadLikedContext(ctx)
		rawRows, _ = i.likedContextRawRows(ctx, local)
	case "recording":
		contextInput, err = loader.LoadRecordingContext(ctx, req.ID)
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

func (i *Inspector) listPlaylistTracksForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	type seedRow struct {
		ItemID             string
		LibraryRecordingID string
		AddedAt            time.Time
	}
	query := `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
GROUP BY pi.item_id, pi.track_variant_id, pi.position_key, pi.added_at, p.kind
ORDER BY CASE WHEN p.kind = 'liked' THEN 0 ELSE 1 END ASC,
	CASE WHEN p.kind = 'liked' THEN pi.added_at END DESC,
	CASE WHEN p.kind <> 'liked' THEN pi.position_key END ASC,
	pi.item_id ASC`
	var seeds []seedRow
	if err := i.app.storage.WithContext(ctx).Raw(query, local.LibraryID, strings.TrimSpace(req.PlaylistID)).Scan(&seeds).Error; err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	pagedSeeds, pageInfo := pageItems(seeds, req.PageRequest)
	clusterIDs := make([]string, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		clusterIDs = append(clusterIDs, strings.TrimSpace(seed.LibraryRecordingID))
	}
	rowsByCluster, err := i.app.catalog.listRecordingVariantRowsForClusters(ctx, local.LibraryID, local.DeviceID, clusterIDs, i.resolvePreferredProfile(""))
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	preferredByCluster, err := i.app.catalog.preferredRecordingVariantIDsForClusters(ctx, local.LibraryID, local.DeviceID, clusterIDs)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	out := make([]apitypes.PlaylistTrackItem, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		variants := rowsByCluster[clusterID]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[clusterID])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		out = append(out, apitypes.PlaylistTrackItem{
			ItemID:             strings.TrimSpace(seed.ItemID),
			LibraryRecordingID: clusterID,
			RecordingID:        strings.TrimSpace(chosen.TrackVariantID),
			Title:              strings.TrimSpace(chosen.Title),
			DurationMS:         chosen.DurationMS,
			Artists:            append([]string(nil), chosen.Artists...),
			AddedAt:            seed.AddedAt,
		})
	}
	pageInfo.Returned = len(out)
	return apitypes.Page[apitypes.PlaylistTrackItem]{Items: out, Page: pageInfo}, nil
}

func (i *Inspector) listLikedRecordingsForLocal(ctx context.Context, local apitypes.LocalContext, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	type seedRow struct {
		LibraryRecordingID string
		AddedAt            time.Time
	}
	query := `
SELECT
	pi.track_variant_id AS library_recording_id,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
GROUP BY pi.item_id, pi.track_variant_id, pi.added_at
ORDER BY pi.added_at DESC, pi.item_id DESC`
	var seeds []seedRow
	if err := i.app.storage.WithContext(ctx).Raw(query, local.LibraryID, likedPlaylistIDForLibrary(local.LibraryID)).Scan(&seeds).Error; err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	pagedSeeds, pageInfo := pageItems(seeds, req.PageRequest)
	clusterIDs := make([]string, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		clusterIDs = append(clusterIDs, strings.TrimSpace(seed.LibraryRecordingID))
	}
	rowsByCluster, err := i.app.catalog.listRecordingVariantRowsForClusters(ctx, local.LibraryID, local.DeviceID, clusterIDs, i.resolvePreferredProfile(""))
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	preferredByCluster, err := i.app.catalog.preferredRecordingVariantIDsForClusters(ctx, local.LibraryID, local.DeviceID, clusterIDs)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	out := make([]apitypes.LikedRecordingItem, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		variants := rowsByCluster[clusterID]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[clusterID])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		out = append(out, apitypes.LikedRecordingItem{
			LibraryRecordingID: clusterID,
			RecordingID:        strings.TrimSpace(chosen.TrackVariantID),
			Title:              strings.TrimSpace(chosen.Title),
			DurationMS:         chosen.DurationMS,
			Artists:            append([]string(nil), chosen.Artists...),
			AddedAt:            seed.AddedAt,
		})
	}
	pageInfo.Returned = len(out)
	return apitypes.Page[apitypes.LikedRecordingItem]{Items: out, Page: pageInfo}, nil
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
