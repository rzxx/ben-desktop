package desktopcore

import (
	"context"
	"strings"
)

func (i *Inspector) TraceRecordingCache(ctx context.Context, req TraceRecordingCacheRequest) (RecordingCacheTrace, error) {
	trace := RecordingCacheTrace{
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

	variants, err := i.app.catalog.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, requestedID, resolution.Selected.PreferredProfile)
	if err != nil {
		return trace, err
	}
	variantIDs := make([]string, 0, len(variants))
	sourceFileIDs := make([]string, 0, len(variants))
	for _, row := range variants {
		variantIDs = append(variantIDs, row.TrackVariantID)
		sourceFileIDs = append(sourceFileIDs, row.SourceFileID)
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
	albumTracks, err := i.loadAlbumTracksByTrackIDs(ctx, local.LibraryID, variantIDs)
	if err != nil {
		return trace, err
	}
	albumVariantIDs := make([]string, 0, len(albumTracks))
	for _, row := range albumTracks {
		albumVariantIDs = append(albumVariantIDs, row.AlbumVariantID)
	}
	albumVariants, err := i.loadAlbumVariants(ctx, local.LibraryID, albumVariantIDs)
	if err != nil {
		return trace, err
	}
	pinBlobRefs, err := i.loadPinBlobRefsByBlobIDs(ctx, blobIDs)
	if err != nil {
		return trace, err
	}

	blobAssociations, err := i.blobAlbumAssociations(ctx, local.LibraryID, blobIDs)
	if err != nil {
		return trace, err
	}
	for blobID, clusters := range blobAssociations {
		if len(clusters) > 1 {
			trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
				anomalyCacheBlobAssociatedWithMultipleAlbumClusters,
				"warning",
				"cached blob is associated with multiple album clusters",
				map[string]any{
					"blob_id":           blobID,
					"album_cluster_ids": clusters,
				},
			))
		}
	}
	for _, row := range variants {
		memberships, _, _, err := i.recordingMemberships(ctx, local.LibraryID, []string{row.TrackVariantID})
		if err != nil {
			return trace, err
		}
		linked := memberships[strings.TrimSpace(row.TrackVariantID)]
		if len(linked) > 1 && row.AlbumVariantID != "" && row.AlbumVariantID == linked[0] {
			trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
				anomalyCacheAssociationDependsOnMinAlbumVariantID,
				"warning",
				"recording cache attribution depends on the minimum album_variant_id ordering",
				map[string]any{
					"track_variant_id":       row.TrackVariantID,
					"selected_album_variant": row.AlbumVariantID,
					"album_variant_ids":      linked,
				},
			))
		}
	}

	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "collect_recording_cache_rows",
		Inputs: map[string]any{
			"requested_id": requestedID,
		},
		Result: map[string]any{
			"variant_count": len(variants),
			"blob_ids":      sortStrings(blobIDs),
		},
		Reason: "follow recording variants through source files, optimized assets, cache rows, and pin references",
	})
	trace.RawRows = map[string]any{
		"source_files":        dumpSourceFiles(sourceFiles),
		"optimized_assets":    dumpOptimizedAssets(assets),
		"device_asset_caches": dumpDeviceAssetCaches(deviceCaches),
		"album_tracks":        dumpAlbumTracks(albumTracks),
		"album_variants":      dumpAlbumVariants(albumVariants),
		"pin_blob_refs":       dumpPinBlobRefs(pinBlobRefs),
	}
	trace.ComputedOutput = map[string]any{
		"blob_ids":            sortStrings(blobIDs),
		"blob_album_clusters": blobAssociations,
		"blob_files":          i.blobFiles(blobIDs),
	}
	trace.Anomalies = sortAnomalies(trace.Anomalies)
	return trace, nil
}

func (i *Inspector) TraceBlob(ctx context.Context, req TraceBlobRequest) (BlobTrace, error) {
	trace := BlobTrace{
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

	blobID := strings.TrimSpace(req.BlobID)
	trace.Identity = map[string]any{"blob_id": blobID}

	assets, err := i.loadOptimizedAssetsByBlobID(ctx, local.LibraryID, blobID)
	if err != nil {
		return trace, err
	}
	sourceIDs := make([]string, 0, len(assets))
	assetIDs := make([]string, 0, len(assets))
	for _, row := range assets {
		sourceIDs = append(sourceIDs, row.SourceFileID)
		assetIDs = append(assetIDs, row.OptimizedAssetID)
	}
	sourceFiles, err := i.loadSourceFilesByIDs(ctx, local.LibraryID, sourceIDs)
	if err != nil {
		return trace, err
	}
	deviceCaches, err := i.loadDeviceAssetCaches(ctx, local.LibraryID, assetIDs)
	if err != nil {
		return trace, err
	}
	trackIDs := make([]string, 0, len(sourceFiles))
	for _, row := range sourceFiles {
		trackIDs = append(trackIDs, row.TrackVariantID)
	}
	albumTracks, err := i.loadAlbumTracksByTrackIDs(ctx, local.LibraryID, trackIDs)
	if err != nil {
		return trace, err
	}
	albumVariantIDs := make([]string, 0, len(albumTracks))
	for _, row := range albumTracks {
		albumVariantIDs = append(albumVariantIDs, row.AlbumVariantID)
	}
	albumVariants, err := i.loadAlbumVariants(ctx, local.LibraryID, albumVariantIDs)
	if err != nil {
		return trace, err
	}
	pinBlobRefs, err := i.loadPinBlobRefsByBlobIDs(ctx, []string{blobID})
	if err != nil {
		return trace, err
	}
	var artworkRows []ArtworkVariant
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND blob_id = ?", local.LibraryID, blobID).
		Order("scope_type ASC, scope_id ASC, variant ASC").
		Find(&artworkRows).Error; err != nil {
		return trace, err
	}

	blobAssociations, err := i.blobAlbumAssociations(ctx, local.LibraryID, []string{blobID})
	if err != nil {
		return trace, err
	}
	if len(blobAssociations[blobID]) > 1 {
		trace.Anomalies = append(trace.Anomalies, inspectAnomaly(
			anomalyCacheBlobAssociatedWithMultipleAlbumClusters,
			"warning",
			"blob is associated with multiple album clusters",
			map[string]any{
				"blob_id":           blobID,
				"album_cluster_ids": blobAssociations[blobID],
			},
		))
	}
	trace.Decisions = append(trace.Decisions, InspectDecision{
		Step: "trace_blob",
		Inputs: map[string]any{
			"blob_id": blobID,
		},
		Result: map[string]any{
			"optimized_asset_count": len(assets),
			"artwork_variant_count": len(artworkRows),
		},
		Reason: "collect all recording-cache and artwork references that point at the blob id",
	})
	trace.RawRows = map[string]any{
		"optimized_assets":    dumpOptimizedAssets(assets),
		"source_files":        dumpSourceFiles(sourceFiles),
		"device_asset_caches": dumpDeviceAssetCaches(deviceCaches),
		"album_tracks":        dumpAlbumTracks(albumTracks),
		"album_variants":      dumpAlbumVariants(albumVariants),
		"pin_blob_refs":       dumpPinBlobRefs(pinBlobRefs),
		"artwork_variants":    dumpArtworkVariants(artworkRows),
	}
	trace.ComputedOutput = map[string]any{
		"album_cluster_ids": blobAssociations[blobID],
		"blob_file":         i.blobFiles([]string{blobID})[blobID],
	}
	trace.Anomalies = sortAnomalies(trace.Anomalies)
	return trace, nil
}

func (i *Inspector) loadSourceFilesByIDs(ctx context.Context, libraryID string, sourceIDs []string) ([]SourceFileModel, error) {
	sourceIDs = compactNonEmptyStrings(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	var rows []SourceFileModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND source_file_id IN ?", libraryID, sourceIDs).
		Order("device_id ASC, source_file_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) blobAlbumAssociations(ctx context.Context, libraryID string, blobIDs []string) (map[string][]string, error) {
	blobIDs = compactNonEmptyStrings(blobIDs)
	out := make(map[string][]string, len(blobIDs))
	if len(blobIDs) == 0 {
		return out, nil
	}
	type row struct {
		BlobID         string
		AlbumClusterID string
	}
	query := `
SELECT DISTINCT oa.blob_id, COALESCE(av.album_cluster_id, at.album_variant_id) AS album_cluster_id
FROM optimized_assets oa
JOIN source_files sf ON sf.library_id = oa.library_id AND sf.source_file_id = oa.source_file_id
JOIN album_tracks at ON at.library_id = sf.library_id AND at.track_variant_id = sf.track_variant_id
LEFT JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
WHERE oa.library_id = ? AND oa.blob_id IN ?
ORDER BY oa.blob_id ASC, av.album_cluster_id ASC`
	var rows []row
	if err := i.app.storage.WithContext(ctx).Raw(query, libraryID, blobIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		blobID := strings.TrimSpace(row.BlobID)
		clusterID := strings.TrimSpace(row.AlbumClusterID)
		if blobID == "" || clusterID == "" {
			continue
		}
		if !stringSliceContains(out[blobID], clusterID) {
			out[blobID] = append(out[blobID], clusterID)
		}
	}
	return out, nil
}

func (i *Inspector) blobFiles(blobIDs []string) map[string]any {
	out := make(map[string]any, len(blobIDs))
	for _, blobID := range compactNonEmptyStrings(blobIDs) {
		path, err := i.app.cache.blobPath(blobID)
		if err != nil {
			out[blobID] = map[string]any{
				"available": false,
				"error":     err.Error(),
			}
			continue
		}
		out[blobID] = blobFileMetadata(path)
	}
	return out
}
