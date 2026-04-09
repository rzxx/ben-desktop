package desktopcore

import (
	"context"
	"fmt"
	"sort"
	"strings"

	apitypes "ben/desktop/api/types"
)

func (i *Inspector) TraceRecording(ctx context.Context, req TraceRecordingRequest) (RecordingTrace, error) {
	trace := RecordingTrace{
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

	requestedID := strings.TrimSpace(req.ID)
	requestedKind, clusterID, err := i.recordingIDKind(ctx, local.LibraryID, requestedID)
	if err != nil {
		return trace, err
	}
	trace.Identity = map[string]any{
		"requested_id":   requestedID,
		"requested_kind": requestedKind,
		"cluster_id":     clusterID,
	}
	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "identify_recording",
		Inputs: map[string]any{
			"requested_id": requestedID,
		},
		Result: map[string]any{
			"requested_kind": requestedKind,
			"cluster_id":     clusterID,
		},
		Reason: "resolve the requested recording into its logical track cluster before tracing candidate variants",
	})

	variants, err := i.app.catalog.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, requestedID, resolution.Selected.PreferredProfile)
	if err != nil {
		return trace, err
	}
	explicitPreferredID, _, err := i.app.catalog.preferredRecordingVariantID(ctx, local.LibraryID, local.DeviceID, requestedID)
	if err != nil {
		return trace, err
	}
	heuristicPreferredID := chooseRecordingVariantID(variants, "")
	resolvedVariantID, resolvedProfile, err := i.app.playback.resolvePlaybackVariant(ctx, local, requestedID, resolution.Selected.PreferredProfile)
	if err != nil {
		return trace, err
	}
	resolvedAvailability, err := i.getRecordingAvailabilityForLocal(ctx, local, requestedID, resolvedProfile, resolution.Selected.NetworkRunning)
	if err != nil {
		return trace, err
	}
	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "compare_recording_variants",
		Inputs: map[string]any{
			"explicit_preferred_variant_id": explicitPreferredID,
			"variants":                      recordingComparatorInputsList(variants, explicitPreferredID),
		},
		Result: map[string]any{
			"heuristic_preferred_variant_id": heuristicPreferredID,
			"resolved_playback_variant_id":   resolvedVariantID,
		},
		Reason: "reuse compareRecordingVariants and resolvePlaybackVariant to mirror current playback winner selection",
	})

	variantIDs := make([]string, 0, len(variants))
	sourceFileIDs := make([]string, 0, len(variants))
	clusterIDs := make([]string, 0, 1)
	if clusterID != "" {
		clusterIDs = append(clusterIDs, clusterID)
	}
	for _, row := range variants {
		variantIDs = append(variantIDs, row.TrackVariantID)
		sourceFileIDs = append(sourceFileIDs, row.SourceFileID)
	}

	trackModels, err := i.loadTrackVariants(ctx, local.LibraryID, variantIDs)
	if err != nil {
		return trace, err
	}
	albumTracks, err := i.loadAlbumTracksByTrackIDs(ctx, local.LibraryID, variantIDs)
	if err != nil {
		return trace, err
	}
	albumVariantIDs := make([]string, 0, len(albumTracks))
	for _, row := range albumTracks {
		albumVariantIDs = append(albumVariantIDs, row.AlbumVariantID)
	}
	albumModels, err := i.loadAlbumVariants(ctx, local.LibraryID, albumVariantIDs)
	if err != nil {
		return trace, err
	}
	prefs, err := i.loadPreferences(ctx, local.LibraryID, local.DeviceID, "track", clusterIDs)
	if err != nil {
		return trace, err
	}
	sourceFiles, err := i.loadSourceFilesByTrackIDs(ctx, local.LibraryID, variantIDs)
	if err != nil {
		return trace, err
	}
	assets, err := i.loadOptimizedAssetsBySourceIDs(ctx, local.LibraryID, sourceFileIDs)
	if err != nil {
		return trace, err
	}
	assetIDs := make([]string, 0, len(assets))
	blobIDs := make([]string, 0, len(assets))
	for _, row := range assets {
		assetIDs = append(assetIDs, row.OptimizedAssetID)
		blobIDs = append(blobIDs, row.BlobID)
	}
	deviceCaches, err := i.loadDeviceAssetCaches(ctx, local.LibraryID, assetIDs)
	if err != nil {
		return trace, err
	}
	pinMembers, err := i.loadPinMembersByRecordings(ctx, local.LibraryID, append(append([]string{}, variantIDs...), clusterID))
	if err != nil {
		return trace, err
	}
	pinBlobRefs, err := i.loadPinBlobRefsByBlobIDs(ctx, blobIDs)
	if err != nil {
		return trace, err
	}
	memberships, err := i.loadMemberships(ctx, local.LibraryID)
	if err != nil {
		return trace, err
	}
	memberDeviceIDs := make([]string, 0, len(memberships))
	for _, row := range memberships {
		memberDeviceIDs = append(memberDeviceIDs, row.DeviceID)
	}
	devices, err := i.loadDevices(ctx, memberDeviceIDs)
	if err != nil {
		return trace, err
	}

	membershipsByVariant, albumClustersByVariant, clusterAlbumClusters, err := i.recordingMemberships(ctx, local.LibraryID, variantIDs)
	if err != nil {
		return trace, err
	}
	if len(clusterAlbumClusters) > 1 {
		trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
			anomalyRecordingClusterSpansMultipleAlbumClusters,
			"warning",
			"recording cluster belongs to more than one album cluster",
			map[string]any{
				"track_cluster_id": clusterID,
				"album_cluster_ids": clusterAlbumClusters,
			},
		))
	}
	for variantID, memberships := range membershipsByVariant {
		if len(memberships) > 1 {
			trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
				anomalyRecordingVariantLinkedToMultipleAlbumVariants,
				"warning",
				"recording variant is linked to multiple album variants",
				map[string]any{
					"track_variant_id": variantID,
					"album_variant_ids": memberships,
				},
			))
		}
	}
	if explicitPreferredID != "" && heuristicPreferredID != "" && explicitPreferredID != heuristicPreferredID {
		trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
			anomalyExplicitVariantDiffersFromPreferredResolution,
			"warning",
			"explicit preferred recording variant overrides the heuristic winner",
			map[string]any{
				"explicit_preferred_variant_id":  explicitPreferredID,
				"heuristic_preferred_variant_id": heuristicPreferredID,
			},
		))
	}
	clusterPreferredID := chooseRecordingVariantID(variants, explicitPreferredID)
	if requestedKind == "variant" && requestedID != "" && clusterPreferredID != "" && clusterPreferredID != requestedID {
		trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
			anomalyRequestedExactShadowedByClusterResolution,
			"warning",
			"an exact variant request would resolve to another variant when treated as a logical recording",
			map[string]any{
				"requested_variant_id": requestedID,
				"logical_winner_id":    clusterPreferredID,
			},
		))
	}
	for _, row := range variants {
		memberships := membershipsByVariant[strings.TrimSpace(row.TrackVariantID)]
		if len(memberships) > 1 && row.AlbumVariantID != "" && row.AlbumVariantID == memberships[0] {
			trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
				anomalyCacheAssociationDependsOnMinAlbumVariantID,
				"warning",
				"recording variant album attribution depends on the minimum album_variant_id ordering",
				map[string]any{
					"track_variant_id":       row.TrackVariantID,
					"selected_album_variant": row.AlbumVariantID,
					"album_variant_ids":      memberships,
				},
			))
		}
	}

	trace.RawRows = map[string]any{
		"track_variants":             dumpTrackVariants(trackModels),
		"album_tracks":               dumpAlbumTracks(albumTracks),
		"album_variants":             dumpAlbumVariants(albumModels),
		"device_variant_preferences": dumpPreferences(prefs),
		"source_files":               dumpSourceFiles(sourceFiles),
		"optimized_assets":           dumpOptimizedAssets(assets),
		"device_asset_caches":        dumpDeviceAssetCaches(deviceCaches),
		"pin_members":                dumpPinMembers(pinMembers),
		"pin_blob_refs":              dumpPinBlobRefs(pinBlobRefs),
		"memberships":                dumpMemberships(memberships),
		"devices":                    dumpDevices(devices),
	}
	trace.ComputedOutput = map[string]any{
		"explicit_preferred_variant_id":  explicitPreferredID,
		"heuristic_preferred_variant_id": heuristicPreferredID,
		"resolved_playback_variant_id":   resolvedVariantID,
		"resolved_playback_profile":      resolvedProfile,
		"resolved_playback_availability": availabilityMap(resolvedAvailability),
		"candidate_variants":             recordingVariantOutputs(variants, membershipsByVariant, albumClustersByVariant),
		"cache_blob_ids":                 sortStrings(blobIDs),
	}
	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "resolve_recording_availability",
		Inputs: map[string]any{
			"requested_id":     requestedID,
			"network_running":  resolution.Selected.NetworkRunning,
			"preferred_profile": resolvedProfile,
		},
		Result: map[string]any{
			"availability": availabilityMap(resolvedAvailability),
		},
		Reason: "evaluate local sources, cached assets, remote providers, and pin state for the resolved playback variant",
	})
	trace.Anomalies = sortAnomalies(trace.Anomalies)
	return trace, nil
}

func (i *Inspector) TraceAlbum(ctx context.Context, req TraceAlbumRequest) (AlbumTrace, error) {
	trace := AlbumTrace{
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

	requestedID := strings.TrimSpace(req.ID)
	requestedKind, clusterID, err := i.albumIDKind(ctx, local.LibraryID, requestedID)
	if err != nil {
		return trace, err
	}
	trace.Identity = map[string]any{
		"requested_id":   requestedID,
		"requested_kind": requestedKind,
		"cluster_id":     clusterID,
	}

	variants, err := i.app.catalog.listAlbumVariantsRows(ctx, local.LibraryID, local.DeviceID, requestedID)
	if err != nil {
		return trace, err
	}
	explicitPreferredID, _, err := i.app.catalog.preferredAlbumVariantID(ctx, local.LibraryID, local.DeviceID, requestedID)
	if err != nil {
		return trace, err
	}
	heuristicPreferredID := chooseAlbumVariantID(albumVariantItemsFromRows(variants), "")
	selectedVariantID := explicitPreferredID
	if selectedVariantID == "" {
		selectedVariantID = heuristicPreferredID
	}
	if selectedVariantID == "" && len(variants) > 0 {
		selectedVariantID = variants[0].AlbumVariantID
	}
	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "compare_album_variants",
		Inputs: map[string]any{
			"explicit_preferred_variant_id": explicitPreferredID,
			"variants":                      albumComparatorInputsList(variants, explicitPreferredID),
		},
		Result: map[string]any{
			"heuristic_preferred_variant_id": heuristicPreferredID,
			"selected_album_variant_id":      selectedVariantID,
		},
		Reason: "reuse compareAlbumVariants and album preference resolution to mirror current album winner selection",
	})
	if explicitPreferredID != "" && heuristicPreferredID != "" && explicitPreferredID != heuristicPreferredID {
		trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
			anomalyExplicitVariantDiffersFromPreferredResolution,
			"warning",
			"explicit preferred album variant overrides the heuristic winner",
			map[string]any{
				"explicit_preferred_variant_id":  explicitPreferredID,
				"heuristic_preferred_variant_id": heuristicPreferredID,
			},
		))
	}

	albumModels, err := i.loadAlbumVariantsByCluster(ctx, local.LibraryID, clusterID)
	if err != nil {
		return trace, err
	}
	prefs, err := i.loadPreferences(ctx, local.LibraryID, local.DeviceID, "album", []string{clusterID})
	if err != nil {
		return trace, err
	}
	selectedTracks, err := i.loadAlbumTracksByAlbumIDs(ctx, local.LibraryID, []string{selectedVariantID})
	if err != nil {
		return trace, err
	}
	trackVariantIDs := make([]string, 0, len(selectedTracks))
	for _, row := range selectedTracks {
		trackVariantIDs = append(trackVariantIDs, row.TrackVariantID)
	}
	trackModels, err := i.loadTrackVariants(ctx, local.LibraryID, trackVariantIDs)
	if err != nil {
		return trace, err
	}
	sourceFiles, err := i.loadSourceFilesByTrackIDs(ctx, local.LibraryID, trackVariantIDs)
	if err != nil {
		return trace, err
	}
	clusterMemberships, clusterAlbumClusters, err := i.trackClusterMemberships(ctx, local.LibraryID, trackVariantIDs)
	if err != nil {
		return trace, err
	}
	for clusterID, memberships := range clusterAlbumClusters {
		if len(memberships) > 1 {
			trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
				anomalyRecordingClusterSpansMultipleAlbumClusters,
				"warning",
				"album track cluster spans multiple album clusters",
				map[string]any{
					"track_cluster_id": clusterID,
					"album_cluster_ids": memberships,
				},
			))
		}
	}

	trace.RawRows = map[string]any{
		"album_variants":             dumpAlbumVariants(albumModels),
		"device_variant_preferences": dumpPreferences(prefs),
		"album_tracks":               dumpAlbumTracks(selectedTracks),
		"track_variants":             dumpTrackVariants(trackModels),
		"source_files":               dumpSourceFiles(sourceFiles),
	}
	trace.ComputedOutput = map[string]any{
		"explicit_preferred_variant_id":  explicitPreferredID,
		"heuristic_preferred_variant_id": heuristicPreferredID,
		"selected_album_variant_id":      selectedVariantID,
		"album_variants":                 albumVariantOutputs(variants),
		"selected_album_tracks":          albumTrackOutputs(selectedTracks, trackModels, clusterMemberships),
	}
	trace.Anomalies = sortAnomalies(trace.Anomalies)
	return trace, nil
}

func (i *Inspector) getRecordingForLocal(ctx context.Context, local apitypes.LocalContext, recordingID string) (apitypes.RecordingListItem, error) {
	variants, err := i.app.catalog.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, recordingID, i.resolvePreferredProfile(""))
	if err != nil {
		return apitypes.RecordingListItem{}, err
	}
	if len(variants) == 0 {
		return apitypes.RecordingListItem{}, fmt.Errorf("recording %s not found", recordingID)
	}
	explicitPreferredID, _, err := i.app.catalog.preferredRecordingVariantID(ctx, local.LibraryID, local.DeviceID, recordingID)
	if err != nil {
		return apitypes.RecordingListItem{}, err
	}
	preferredID := chooseRecordingVariantID(variants, explicitPreferredID)
	chosen := variants[0]
	for _, variant := range variants {
		if variant.TrackVariantID == preferredID {
			chosen = variant
			break
		}
	}
	return apitypes.RecordingListItem{
		LibraryRecordingID:          strings.TrimSpace(chosen.TrackClusterID),
		PreferredVariantRecordingID: strings.TrimSpace(chosen.TrackVariantID),
		TrackClusterID:              strings.TrimSpace(chosen.TrackClusterID),
		RecordingID:                 strings.TrimSpace(chosen.TrackClusterID),
		Title:                       strings.TrimSpace(chosen.Title),
		DurationMS:                  chosen.DurationMS,
		Artists:                     append([]string(nil), chosen.Artists...),
		VariantCount:                int64(len(variants)),
		HasVariants:                 len(variants) > 1,
	}, nil
}

func recordingComparatorInputsList(rows []recordingVariantRow, explicitPreferredID string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, recordingComparatorInputs(row, explicitPreferredID))
	}
	return out
}

func albumComparatorInputsList(rows []albumVariantRow, explicitPreferredID string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, albumComparatorInputs(row, explicitPreferredID))
	}
	return out
}

func albumVariantItemsFromRows(rows []albumVariantRow) []apitypes.AlbumVariantItem {
	out := make([]apitypes.AlbumVariantItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, apitypes.AlbumVariantItem{
			AlbumID:         strings.TrimSpace(row.AlbumVariantID),
			AlbumClusterID:  strings.TrimSpace(row.AlbumClusterID),
			Title:           strings.TrimSpace(row.Title),
			Artists:         append([]string(nil), row.Artists...),
			Year:            row.Year,
			Edition:         strings.TrimSpace(row.Edition),
			TrackCount:      row.TrackCount,
			BestQualityRank: row.BestQualityRank,
			LocalTrackCount: row.LocalTrackCount,
		})
	}
	return out
}

func recordingVariantOutputs(rows []recordingVariantRow, memberships map[string][]string, albumClusters map[string][]string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		variantID := strings.TrimSpace(row.TrackVariantID)
		out = append(out, map[string]any{
			"track_variant_id":     variantID,
			"track_cluster_id":     strings.TrimSpace(row.TrackClusterID),
			"title":                strings.TrimSpace(row.Title),
			"duration_ms":          row.DurationMS,
			"artists":              append([]string(nil), row.Artists...),
			"source_file_id":       strings.TrimSpace(row.SourceFileID),
			"album_variant_id":     strings.TrimSpace(row.AlbumVariantID),
			"album_variant_ids":    memberships[variantID],
			"album_cluster_ids":    albumClusters[variantID],
			"is_present_local":     row.IsPresentLocal,
			"is_cached_local":      row.IsCachedLocal,
			"quality_rank":         row.QualityRank,
			"bitrate":              row.Bitrate,
			"container":            strings.TrimSpace(row.Container),
			"codec":                strings.TrimSpace(row.Codec),
		})
	}
	return out
}

func albumVariantOutputs(rows []albumVariantRow) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"album_variant_id":   strings.TrimSpace(row.AlbumVariantID),
			"album_cluster_id":   strings.TrimSpace(row.AlbumClusterID),
			"title":              strings.TrimSpace(row.Title),
			"artists":            append([]string(nil), row.Artists...),
			"year":               row.Year,
			"edition":            strings.TrimSpace(row.Edition),
			"track_count":        row.TrackCount,
			"best_quality_rank":  row.BestQualityRank,
			"local_track_count":  row.LocalTrackCount,
		})
	}
	return out
}

func albumTrackOutputs(rows []AlbumTrack, trackRows []TrackVariantModel, memberships map[string][]map[string]any) []map[string]any {
	trackByVariant := make(map[string]TrackVariantModel, len(trackRows))
	for _, row := range trackRows {
		trackByVariant[strings.TrimSpace(row.TrackVariantID)] = row
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		variantID := strings.TrimSpace(row.TrackVariantID)
		track := trackByVariant[variantID]
		out = append(out, map[string]any{
			"album_variant_id":       strings.TrimSpace(row.AlbumVariantID),
			"track_variant_id":       variantID,
			"track_cluster_id":       strings.TrimSpace(track.TrackClusterID),
			"disc_no":                row.DiscNo,
			"track_no":               row.TrackNo,
			"title":                  strings.TrimSpace(track.Title),
			"cross_album_memberships": memberships[variantID],
		})
	}
	return out
}

func availabilityMap(item apitypes.RecordingPlaybackAvailability) map[string]any {
	return map[string]any{
		"recording_id":      strings.TrimSpace(item.RecordingID),
		"preferred_profile": strings.TrimSpace(item.PreferredProfile),
		"pinned":            item.Pinned,
		"state":             item.State,
		"source_kind":       item.SourceKind,
		"local_path":        strings.TrimSpace(item.LocalPath),
		"reason":            item.Reason,
	}
}

func (i *Inspector) recordingMemberships(ctx context.Context, libraryID string, variantIDs []string) (map[string][]string, map[string][]string, []string, error) {
	type row struct {
		TrackVariantID string
		AlbumVariantID string
		AlbumClusterID string
	}
	var rows []row
	query := `
SELECT at.track_variant_id, at.album_variant_id, av.album_cluster_id
FROM album_tracks at
JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
WHERE at.library_id = ? AND at.track_variant_id IN ?
ORDER BY at.track_variant_id ASC, at.album_variant_id ASC`
	if err := i.app.storage.WithContext(ctx).Raw(query, libraryID, compactNonEmptyStrings(variantIDs)).Scan(&rows).Error; err != nil {
		return nil, nil, nil, err
	}
	albumVariantsByTrack := make(map[string][]string)
	albumClustersByTrack := make(map[string][]string)
	clusterSeen := map[string]struct{}{}
	clusterOut := make([]string, 0)
	for _, row := range rows {
		trackID := strings.TrimSpace(row.TrackVariantID)
		albumID := strings.TrimSpace(row.AlbumVariantID)
		clusterID := strings.TrimSpace(row.AlbumClusterID)
		if trackID == "" {
			continue
		}
		if albumID != "" && !stringSliceContains(albumVariantsByTrack[trackID], albumID) {
			albumVariantsByTrack[trackID] = append(albumVariantsByTrack[trackID], albumID)
		}
		if clusterID != "" && !stringSliceContains(albumClustersByTrack[trackID], clusterID) {
			albumClustersByTrack[trackID] = append(albumClustersByTrack[trackID], clusterID)
		}
		if clusterID != "" {
			if _, ok := clusterSeen[clusterID]; !ok {
				clusterSeen[clusterID] = struct{}{}
				clusterOut = append(clusterOut, clusterID)
			}
		}
	}
	for key := range albumVariantsByTrack {
		sort.Strings(albumVariantsByTrack[key])
	}
	for key := range albumClustersByTrack {
		sort.Strings(albumClustersByTrack[key])
	}
	sort.Strings(clusterOut)
	return albumVariantsByTrack, albumClustersByTrack, clusterOut, nil
}

func (i *Inspector) trackClusterMemberships(ctx context.Context, libraryID string, trackVariantIDs []string) (map[string][]map[string]any, map[string][]string, error) {
	type row struct {
		TrackVariantID string
		TrackClusterID string
		AlbumVariantID string
		AlbumClusterID string
	}
	var rows []row
	query := `
SELECT tv.track_variant_id, tv.track_cluster_id, at.album_variant_id, av.album_cluster_id
FROM track_variants tv
LEFT JOIN album_tracks at ON at.library_id = tv.library_id AND at.track_variant_id = tv.track_variant_id
LEFT JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
WHERE tv.library_id = ? AND tv.track_variant_id IN ?
ORDER BY tv.track_variant_id ASC, at.album_variant_id ASC`
	if err := i.app.storage.WithContext(ctx).Raw(query, libraryID, compactNonEmptyStrings(trackVariantIDs)).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	out := make(map[string][]map[string]any)
	clusterAlbums := make(map[string][]string)
	for _, row := range rows {
		variantID := strings.TrimSpace(row.TrackVariantID)
		clusterID := strings.TrimSpace(row.TrackClusterID)
		albumVariantID := strings.TrimSpace(row.AlbumVariantID)
		albumClusterID := strings.TrimSpace(row.AlbumClusterID)
		if variantID == "" {
			continue
		}
		if albumVariantID != "" {
			out[variantID] = append(out[variantID], map[string]any{
				"album_variant_id": albumVariantID,
				"album_cluster_id": albumClusterID,
				"track_cluster_id": clusterID,
			})
		}
		if clusterID != "" && albumClusterID != "" && !stringSliceContains(clusterAlbums[clusterID], albumClusterID) {
			clusterAlbums[clusterID] = append(clusterAlbums[clusterID], albumClusterID)
		}
	}
	for key := range clusterAlbums {
		sort.Strings(clusterAlbums[key])
	}
	return out, clusterAlbums, nil
}
