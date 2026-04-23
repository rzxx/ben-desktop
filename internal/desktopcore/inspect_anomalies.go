package desktopcore

import (
	"context"
	"os"
	"sort"
	"strings"
)

const (
	anomalyRecordingClusterSpansMultipleAlbumClusters     = "recording_cluster_spans_multiple_album_clusters"
	anomalyRecordingVariantLinkedToMultipleAlbumVariants  = "recording_variant_linked_to_multiple_album_variants"
	anomalyAlbumContextEntryResolvesToForeignAlbumVariant = "album_context_entry_resolves_to_foreign_album_variant"
	anomalyCacheBlobAssociatedWithMultipleAlbumClusters   = "cache_blob_associated_with_multiple_album_clusters"
	anomalyCacheAssociationDependsOnMinAlbumVariantID     = "cache_album_association_depends_on_min_album_variant_id"
	anomalyExplicitVariantDiffersFromPreferredResolution  = "explicit_variant_differs_from_preferred_resolution"
	anomalyRequestedExactShadowedByClusterResolution      = "requested_exact_variant_would_be_shadowed_by_cluster_resolution"
)

func inspectAnomaly(code, severity, message string, evidence map[string]any) InspectAnomaly {
	return InspectAnomaly{
		Code:     strings.TrimSpace(code),
		Severity: strings.TrimSpace(severity),
		Message:  strings.TrimSpace(message),
		Evidence: evidence,
	}
}

func recordingComparatorInputs(row recordingVariantRow, explicitPreferredID string) map[string]any {
	return map[string]any{
		"track_variant_id":   strings.TrimSpace(row.TrackVariantID),
		"track_cluster_id":   strings.TrimSpace(row.TrackClusterID),
		"explicit_preferred": strings.TrimSpace(row.TrackVariantID) == strings.TrimSpace(explicitPreferredID),
		"is_present_local":   row.IsPresentLocal,
		"is_cached_local":    row.IsCachedLocal,
		"quality_rank":       row.QualityRank,
		"bitrate":            row.Bitrate,
		"album_variant_id":   strings.TrimSpace(row.AlbumVariantID),
		"source_file_id":     strings.TrimSpace(row.SourceFileID),
	}
}

func albumComparatorInputs(row albumVariantRow, explicitPreferredID string) map[string]any {
	return map[string]any{
		"album_variant_id":   strings.TrimSpace(row.AlbumVariantID),
		"album_cluster_id":   strings.TrimSpace(row.AlbumClusterID),
		"explicit_preferred": strings.TrimSpace(row.AlbumVariantID) == strings.TrimSpace(explicitPreferredID),
		"local_track_count":  row.LocalTrackCount,
		"track_count":        row.TrackCount,
		"best_quality_rank":  row.BestQualityRank,
		"title":              strings.TrimSpace(row.Title),
	}
}

func sortStrings(values []string) []string {
	out := compactNonEmptyStrings(values)
	sort.Strings(out)
	return out
}

func blobFileMetadata(path string) map[string]any {
	path = strings.TrimSpace(path)
	if path == "" {
		return map[string]any{"available": false}
	}
	info, err := os.Stat(path)
	if err != nil {
		return map[string]any{
			"available": false,
			"path":      path,
		}
	}
	return map[string]any{
		"available":  true,
		"path":       path,
		"size_bytes": info.Size(),
	}
}

func (i *Inspector) recordingIDKind(ctx context.Context, libraryID, recordingID string) (string, string, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "unknown", "", nil
	}
	if exactID, ok, err := i.app.playback.trackVariantExists(ctx, libraryID, recordingID); err != nil {
		return "", "", err
	} else if ok {
		clusterID, _, err := i.app.catalog.trackClusterIDForVariant(ctx, libraryID, exactID)
		return "variant", clusterID, err
	}
	clusterID, ok, err := i.app.catalog.trackClusterIDForVariant(ctx, libraryID, recordingID)
	if err != nil {
		return "", "", err
	}
	if ok {
		return "cluster", clusterID, nil
	}
	return "unknown", "", nil
}

func (i *Inspector) albumIDKind(ctx context.Context, libraryID, albumID string) (string, string, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return "unknown", "", nil
	}
	var exact AlbumVariantModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).
		Take(&exact).Error; err == nil {
		return "variant", strings.TrimSpace(exact.AlbumClusterID), nil
	}
	clusterID, ok, err := i.app.catalog.albumClusterIDForVariant(ctx, libraryID, albumID)
	if err != nil {
		return "", "", err
	}
	if ok {
		return "cluster", clusterID, nil
	}
	return "unknown", "", nil
}

func (i *Inspector) loadTrackVariants(ctx context.Context, libraryID string, ids []string) ([]TrackVariantModel, error) {
	ids = compactNonEmptyStrings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []TrackVariantModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND track_variant_id IN ?", libraryID, ids).
		Order("track_variant_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadAlbumVariants(ctx context.Context, libraryID string, ids []string) ([]AlbumVariantModel, error) {
	ids = compactNonEmptyStrings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []AlbumVariantModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND album_variant_id IN ?", libraryID, ids).
		Order("album_variant_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadAlbumVariantsByCluster(ctx context.Context, libraryID string, clusterID string) ([]AlbumVariantModel, error) {
	var rows []AlbumVariantModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND album_cluster_id = ?", libraryID, strings.TrimSpace(clusterID)).
		Order("album_variant_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadAlbumTracksByAlbumIDs(ctx context.Context, libraryID string, albumIDs []string) ([]AlbumTrack, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return nil, nil
	}
	var rows []AlbumTrack
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND album_variant_id IN ?", libraryID, albumIDs).
		Order("album_variant_id ASC, disc_no ASC, track_no ASC, track_variant_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadAlbumTracksByTrackIDs(ctx context.Context, libraryID string, trackIDs []string) ([]AlbumTrack, error) {
	trackIDs = compactNonEmptyStrings(trackIDs)
	if len(trackIDs) == 0 {
		return nil, nil
	}
	var rows []AlbumTrack
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND track_variant_id IN ?", libraryID, trackIDs).
		Order("album_variant_id ASC, disc_no ASC, track_no ASC, track_variant_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadSourceFilesByTrackIDs(ctx context.Context, libraryID string, trackIDs []string) ([]SourceFileModel, error) {
	trackIDs = compactNonEmptyStrings(trackIDs)
	if len(trackIDs) == 0 {
		return nil, nil
	}
	var rows []SourceFileModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND track_variant_id IN ?", libraryID, trackIDs).
		Order("device_id ASC, source_file_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadOptimizedAssetsBySourceIDs(ctx context.Context, libraryID string, sourceIDs []string) ([]OptimizedAssetModel, error) {
	sourceIDs = compactNonEmptyStrings(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	var rows []OptimizedAssetModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND source_file_id IN ?", libraryID, sourceIDs).
		Order("optimized_asset_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadOptimizedAssetsByBlobID(ctx context.Context, libraryID, blobID string) ([]OptimizedAssetModel, error) {
	var rows []OptimizedAssetModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND blob_id = ?", libraryID, strings.TrimSpace(blobID)).
		Order("optimized_asset_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadDeviceAssetCaches(ctx context.Context, libraryID string, assetIDs []string) ([]DeviceAssetCacheModel, error) {
	assetIDs = compactNonEmptyStrings(assetIDs)
	if len(assetIDs) == 0 {
		return nil, nil
	}
	var rows []DeviceAssetCacheModel
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND optimized_asset_id IN ?", libraryID, assetIDs).
		Order("device_id ASC, optimized_asset_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadPreferences(ctx context.Context, libraryID, deviceID, scopeType string, clusterIDs []string) ([]DeviceVariantPreference, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return nil, nil
	}
	var rows []DeviceVariantPreference
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id IN ?", libraryID, deviceID, strings.TrimSpace(scopeType), clusterIDs).
		Order("cluster_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadPinMembersByRecordings(ctx context.Context, libraryID string, recordingIDs []string) ([]PinMember, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}
	var rows []PinMember
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ? AND (library_recording_id IN ? OR variant_recording_id IN ?)", libraryID, recordingIDs, recordingIDs).
		Order("device_id ASC, scope ASC, scope_id ASC, variant_recording_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadPinBlobRefsByBlobIDs(ctx context.Context, blobIDs []string) ([]PinBlobRef, error) {
	blobIDs = compactNonEmptyStrings(blobIDs)
	if len(blobIDs) == 0 {
		return nil, nil
	}
	var rows []PinBlobRef
	if err := i.app.storage.WithContext(ctx).
		Where("blob_id IN ?", blobIDs).
		Order("library_id ASC, device_id ASC, scope ASC, scope_id ASC, blob_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadMemberships(ctx context.Context, libraryID string) ([]Membership, error) {
	var rows []Membership
	if err := i.app.storage.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("device_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (i *Inspector) loadDevices(ctx context.Context, deviceIDs []string) ([]Device, error) {
	deviceIDs = compactNonEmptyStrings(deviceIDs)
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	var rows []Device
	if err := i.app.storage.WithContext(ctx).
		Where("device_id IN ?", deviceIDs).
		Order("device_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
