package desktopcore

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
)

type CacheService struct {
	app *App
}

type cacheBlobEntry struct {
	BlobID           string
	Kind             apitypes.CacheKind
	SizeBytes        int64
	LastAccessed     time.Time
	Pinned           bool
	PinCount         int
	PinScopes        []apitypes.CachePinScopeRef
	EncodingID       string
	Profile          string
	RecordingID      string
	AlbumID          string
	PlaylistID       string
	ThumbnailScope   string
	ThumbnailScopeID string
	ArtworkFileExt   string
}

func (s *CacheService) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.CacheOverview{}, err
	}

	entries, err := s.listEntries(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return apitypes.CacheOverview{}, err
	}

	out := apitypes.CacheOverview{
		LimitBytes: s.app.cfg.CacheBytes,
		ByKind: []apitypes.CacheUsageBreakdown{
			{Kind: apitypes.CacheKindOptimizedAudio},
			{Kind: apitypes.CacheKindThumbnail},
			{Kind: apitypes.CacheKindUnknown},
		},
	}
	usage := map[apitypes.CacheKind]*apitypes.CacheUsageBreakdown{
		apitypes.CacheKindOptimizedAudio: &out.ByKind[0],
		apitypes.CacheKindThumbnail:      &out.ByKind[1],
		apitypes.CacheKindUnknown:        &out.ByKind[2],
	}
	scopeSummary := make(map[string]*apitypes.CachePinScopeSummary)

	for _, entry := range entries {
		out.UsedBytes += entry.SizeBytes
		out.EntryCount++
		row, ok := usage[entry.Kind]
		if !ok {
			row = usage[apitypes.CacheKindUnknown]
		}
		row.Bytes += entry.SizeBytes
		row.Entries++

		if entry.Pinned {
			out.PinnedBytes += entry.SizeBytes
			out.PinnedEntries++
			row.PinnedBytes += entry.SizeBytes
		} else {
			out.UnpinnedBytes += entry.SizeBytes
			out.UnpinnedEntries++
		}

		for _, scope := range entry.PinScopes {
			key := strings.TrimSpace(scope.Scope) + "|" + strings.TrimSpace(scope.ScopeID)
			if key == "|" {
				continue
			}
			summary, ok := scopeSummary[key]
			if !ok {
				summary = &apitypes.CachePinScopeSummary{
					Scope:   strings.TrimSpace(scope.Scope),
					ScopeID: strings.TrimSpace(scope.ScopeID),
					Durable: scope.Durable,
				}
				scopeSummary[key] = summary
			}
			summary.BlobCount++
			summary.Bytes += entry.SizeBytes
		}
	}

	out.FreeBytes = maxInt64(0, out.LimitBytes-out.UsedBytes)
	out.ReclaimableBytes = out.UnpinnedBytes

	if len(scopeSummary) > 0 {
		out.PinScopes = make([]apitypes.CachePinScopeSummary, 0, len(scopeSummary))
		for _, item := range scopeSummary {
			out.PinScopes = append(out.PinScopes, *item)
		}
		sort.Slice(out.PinScopes, func(i, j int) bool {
			if out.PinScopes[i].Scope != out.PinScopes[j].Scope {
				return out.PinScopes[i].Scope < out.PinScopes[j].Scope
			}
			return out.PinScopes[i].ScopeID < out.PinScopes[j].ScopeID
		})
	}

	return out, nil
}

func (s *CacheService) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.CacheEntryItem]{}, err
	}

	entries, err := s.listEntries(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return apitypes.Page[apitypes.CacheEntryItem]{}, err
	}

	items := make([]apitypes.CacheEntryItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, apitypes.CacheEntryItem{
			BlobID:           entry.BlobID,
			Kind:             entry.Kind,
			SizeBytes:        entry.SizeBytes,
			LastAccessed:     entry.LastAccessed,
			Pinned:           entry.Pinned,
			PinCount:         entry.PinCount,
			PinScopes:        append([]apitypes.CachePinScopeRef(nil), entry.PinScopes...),
			EncodingID:       entry.EncodingID,
			Profile:          entry.Profile,
			RecordingID:      entry.RecordingID,
			AlbumID:          entry.AlbumID,
			PlaylistID:       entry.PlaylistID,
			ThumbnailScope:   entry.ThumbnailScope,
			ThumbnailScopeID: entry.ThumbnailScopeID,
		})
	}

	return paginateItems(items, req.PageRequest), nil
}

func (s *CacheService) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.CacheCleanupResult{}, err
	}

	entries, err := s.listEntries(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return apitypes.CacheCleanupResult{}, err
	}

	var victims []cacheBlobEntry
	switch req.Mode {
	case apitypes.CacheCleanupOverLimitOnly:
		usedBytes := int64(0)
		for _, entry := range entries {
			usedBytes += entry.SizeBytes
		}
		if s.app.cfg.CacheBytes <= 0 || usedBytes <= s.app.cfg.CacheBytes {
			return apitypes.CacheCleanupResult{}, nil
		}
		unpinned := unpinnedEntries(entries)
		var freed int64
		for _, entry := range unpinned {
			if usedBytes-freed <= s.app.cfg.CacheBytes {
				break
			}
			victims = append(victims, entry)
			freed += entry.SizeBytes
		}
	case apitypes.CacheCleanupAllUnpinned:
		victims = unpinnedEntries(entries)
	case apitypes.CacheCleanupBlobIDs:
		want := make(map[string]struct{}, len(req.BlobIDs))
		for _, blobID := range req.BlobIDs {
			blobID = strings.TrimSpace(blobID)
			if blobID != "" {
				want[blobID] = struct{}{}
			}
		}
		for _, entry := range entries {
			if entry.Pinned {
				continue
			}
			if _, ok := want[entry.BlobID]; ok {
				victims = append(victims, entry)
			}
		}
	default:
		return apitypes.CacheCleanupResult{}, fmt.Errorf("unsupported cleanup mode %q", req.Mode)
	}

	result := apitypes.CacheCleanupResult{}
	for _, victim := range victims {
		deleted, err := s.cleanupEntry(ctx, local, victim)
		if err != nil {
			return apitypes.CacheCleanupResult{}, err
		}
		if deleted {
			result.DeletedBlobs = append(result.DeletedBlobs, victim.BlobID)
			result.DeletedBytes += victim.SizeBytes
		}
	}
	sort.Strings(result.DeletedBlobs)

	remaining, err := s.listEntries(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return apitypes.CacheCleanupResult{}, err
	}
	for _, entry := range remaining {
		result.RemainingBytes += entry.SizeBytes
	}
	return result, nil
}

func (s *CacheService) listEntries(ctx context.Context, libraryID, deviceID string) ([]cacheBlobEntry, error) {
	entries := make(map[string]*cacheBlobEntry)
	if err := s.addOptimizedEntries(ctx, entries, libraryID, deviceID); err != nil {
		return nil, err
	}
	if err := s.addArtworkEntries(ctx, entries, libraryID); err != nil {
		return nil, err
	}
	if err := s.applyPinnedBlobRefs(ctx, entries, libraryID, deviceID); err != nil {
		return nil, err
	}

	out := make([]cacheBlobEntry, 0, len(entries))
	for _, entry := range entries {
		sort.Slice(entry.PinScopes, func(i, j int) bool {
			if entry.PinScopes[i].Scope != entry.PinScopes[j].Scope {
				return entry.PinScopes[i].Scope < entry.PinScopes[j].Scope
			}
			return entry.PinScopes[i].ScopeID < entry.PinScopes[j].ScopeID
		})
		entry.PinCount = len(entry.PinScopes)
		entry.Pinned = entry.PinCount > 0
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastAccessed.Equal(out[j].LastAccessed) {
			return out[i].BlobID < out[j].BlobID
		}
		return out[i].LastAccessed.After(out[j].LastAccessed)
	})
	return out, nil
}

func (s *CacheService) addOptimizedEntries(ctx context.Context, entries map[string]*cacheBlobEntry, libraryID, deviceID string) error {
	type row struct {
		BlobID           string
		OptimizedAssetID string
		Profile          string
		TrackVariantID   string
		AlbumVariantID   string
		LastAccessed     string
	}
	query := `
SELECT
	oa.blob_id,
	oa.optimized_asset_id AS optimized_asset_id,
	oa.profile,
	oa.track_variant_id,
	COALESCE(MIN(at.album_variant_id), '') AS album_variant_id,
	MAX(COALESCE(dac.last_verified_at, dac.updated_at, oa.updated_at)) AS last_accessed
FROM device_asset_caches dac
JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
LEFT JOIN album_tracks at ON at.library_id = oa.library_id AND at.track_variant_id = oa.track_variant_id
WHERE dac.library_id = ? AND dac.device_id = ? AND dac.is_cached = 1
GROUP BY oa.blob_id, oa.optimized_asset_id, oa.profile, oa.track_variant_id
ORDER BY last_accessed DESC, oa.optimized_asset_id ASC`

	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, deviceID).Scan(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		sizeBytes, ok, err := s.blobSize(row.BlobID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}

		lastAccessed := parseSQLiteTime(row.LastAccessed)
		entry := ensureCacheEntry(entries, strings.TrimSpace(row.BlobID))
		entry.Kind = apitypes.CacheKindOptimizedAudio
		entry.SizeBytes = sizeBytes
		if lastAccessed.After(entry.LastAccessed) {
			entry.LastAccessed = lastAccessed
		}
		if strings.TrimSpace(entry.EncodingID) == "" {
			entry.EncodingID = strings.TrimSpace(row.OptimizedAssetID)
			entry.Profile = strings.TrimSpace(row.Profile)
			entry.RecordingID = strings.TrimSpace(row.TrackVariantID)
			entry.AlbumID = strings.TrimSpace(row.AlbumVariantID)
		}
	}

	return nil
}

func (s *CacheService) addArtworkEntries(ctx context.Context, entries map[string]*cacheBlobEntry, libraryID string) error {
	type row struct {
		BlobID    string
		ScopeType string
		ScopeID   string
		FileExt   string
		MIME      string
		UpdatedAt time.Time
	}
	var rows []row
	if err := s.app.storage.WithContext(ctx).
		Table("artwork_variants").
		Select("blob_id, scope_type AS scope_type, scope_id, file_ext AS file_ext, mime, updated_at").
		Where("library_id = ?", libraryID).
		Order("updated_at DESC, scope_type ASC, scope_id ASC").
		Scan(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		fileExt := normalizeArtworkFileExt(row.FileExt, row.MIME)
		if fileExt == "" {
			continue
		}
		sizeBytes, ok, err := s.artworkBlobSize(row.BlobID, fileExt)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}

		entry := ensureCacheEntry(entries, strings.TrimSpace(row.BlobID))
		if entry.Kind == "" || entry.Kind == apitypes.CacheKindUnknown {
			entry.Kind = apitypes.CacheKindThumbnail
		}
		if sizeBytes > entry.SizeBytes {
			entry.SizeBytes = sizeBytes
		}
		if row.UpdatedAt.After(entry.LastAccessed) {
			entry.LastAccessed = row.UpdatedAt.UTC()
		}
		scopeType := strings.TrimSpace(row.ScopeType)
		scopeID := strings.TrimSpace(row.ScopeID)
		if scopeType == "album" {
			if clusterID, ok, err := s.app.catalog.albumClusterIDForVariant(ctx, libraryID, scopeID); err != nil {
				return err
			} else if ok && strings.TrimSpace(clusterID) != "" {
				scopeID = strings.TrimSpace(clusterID)
			}
		}
		if strings.TrimSpace(entry.ThumbnailScope) == "" {
			entry.ThumbnailScope = scopeType
			entry.ThumbnailScopeID = scopeID
		}
		if strings.TrimSpace(entry.ArtworkFileExt) == "" {
			entry.ArtworkFileExt = fileExt
		}
	}

	return nil
}

func (s *CacheService) applyPinnedBlobRefs(ctx context.Context, entries map[string]*cacheBlobEntry, libraryID, deviceID string) error {
	type row struct {
		BlobID      string
		Scope       string
		ScopeID     string
		RefKind     string
		RecordingID string
	}
	var rows []row
	if err := s.app.storage.WithContext(ctx).
		Table("pin_blob_refs").
		Select("blob_id, scope, scope_id, ref_kind AS ref_kind, recording_id AS recording_id").
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Order("scope ASC, scope_id ASC, blob_id ASC").
		Scan(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		entry, ok := entries[strings.TrimSpace(row.BlobID)]
		if !ok {
			continue
		}
		switch strings.TrimSpace(row.Scope) {
		case "recording":
			if entry.RecordingID == "" {
				entry.RecordingID = firstNonEmpty(strings.TrimSpace(row.RecordingID), strings.TrimSpace(row.ScopeID))
			}
		case "album":
			if entry.AlbumID == "" {
				entry.AlbumID = strings.TrimSpace(row.ScopeID)
			}
		case "playlist":
			if entry.PlaylistID == "" {
				entry.PlaylistID = strings.TrimSpace(row.ScopeID)
			}
		}
		addPinScope(entry, apitypes.CachePinScopeRef{
			Scope:   strings.TrimSpace(row.Scope),
			ScopeID: strings.TrimSpace(row.ScopeID),
			Durable: true,
		})
		if strings.EqualFold(strings.TrimSpace(row.RefKind), "artwork") && strings.TrimSpace(entry.ThumbnailScopeID) == "" {
			entry.ThumbnailScope = strings.TrimSpace(row.Scope)
			entry.ThumbnailScopeID = strings.TrimSpace(row.ScopeID)
		}
	}

	return nil
}

func (s *CacheService) cachedBlobIDsForResolvedRecordings(ctx context.Context, libraryID, deviceID string, recordingIDs []string, profile string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}

	type row struct {
		BlobID string
	}
	var rows []row
	aliasProfile := normalizedPlaybackProfileAlias(profile)
	query := `
SELECT DISTINCT oa.blob_id
FROM device_asset_caches dac
JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
WHERE dac.library_id = ? AND dac.device_id = ? AND dac.is_cached = 1 AND oa.track_variant_id IN ? AND (? = '' OR oa.profile = ? OR oa.profile = ?)
ORDER BY oa.blob_id ASC`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, deviceID, recordingIDs, profile, profile, aliasProfile).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		blobID := strings.TrimSpace(row.BlobID)
		if blobID == "" {
			continue
		}
		if _, ok := seen[blobID]; ok {
			continue
		}
		seen[blobID] = struct{}{}
		out = append(out, blobID)
	}
	return out, nil
}

func (s *CacheService) cleanupEntry(ctx context.Context, local apitypes.LocalContext, entry cacheBlobEntry) (bool, error) {
	if entry.Pinned {
		return false, nil
	}

	if entry.Kind == apitypes.CacheKindOptimizedAudio {
		if err := s.markBlobEncodingsUncached(ctx, local, entry.BlobID); err != nil {
			return false, err
		}
	}

	retained, err := s.blobHasRetainedReferences(ctx, entry.BlobID)
	if err != nil {
		return false, err
	}
	if retained {
		return false, nil
	}

	var path string
	if entry.Kind == apitypes.CacheKindThumbnail {
		path, err = s.artworkBlobPath(entry.BlobID, entry.ArtworkFileExt)
		if err != nil {
			return false, err
		}
	} else {
		path, err = s.blobPath(entry.BlobID)
		if err != nil {
			return false, err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func (s *CacheService) markBlobEncodingsUncached(ctx context.Context, local apitypes.LocalContext, blobID string) error {
	type row struct {
		OptimizedAssetID string
	}
	now := time.Now().UTC()
	return s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []row
		query := `
SELECT dac.optimized_asset_id AS optimized_asset_id
FROM device_asset_caches dac
JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
WHERE dac.library_id = ? AND dac.device_id = ? AND dac.is_cached = 1 AND oa.blob_id = ?
ORDER BY dac.optimized_asset_id ASC`
		if err := tx.Raw(query, local.LibraryID, local.DeviceID, strings.TrimSpace(blobID)).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			encodingID := strings.TrimSpace(row.OptimizedAssetID)
			if encodingID == "" {
				continue
			}
			if err := s.app.upsertDeviceAssetCacheTx(tx, local, DeviceAssetCacheModel{
				LibraryID:        local.LibraryID,
				DeviceID:         local.DeviceID,
				OptimizedAssetID: encodingID,
				IsCached:         false,
				LastVerifiedAt:   &now,
				UpdatedAt:        now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *CacheService) blobHasRetainedReferences(ctx context.Context, blobID string) (bool, error) {
	type countRow struct {
		Count int64
	}

	var artwork countRow
	if err := s.app.storage.WithContext(ctx).
		Table("artwork_variants").
		Select("COUNT(1) AS count").
		Where("blob_id = ?", strings.TrimSpace(blobID)).
		Scan(&artwork).Error; err != nil {
		return false, err
	}
	if artwork.Count > 0 {
		return true, nil
	}

	var cached countRow
	query := `
SELECT COUNT(1) AS count
FROM device_asset_caches dac
JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
WHERE oa.blob_id = ? AND dac.is_cached = 1`
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(blobID)).Scan(&cached).Error; err != nil {
		return false, err
	}
	return cached.Count > 0, nil
}

func (s *CacheService) blobSize(blobID string) (int64, bool, error) {
	path, err := s.blobPath(blobID)
	if err != nil {
		return 0, false, err
	}
	return fileSize(path)
}

func (s *CacheService) artworkBlobSize(blobID, fileExt string) (int64, bool, error) {
	path, err := s.artworkBlobPath(blobID, fileExt)
	if err != nil {
		return 0, false, err
	}
	return fileSize(path)
}

func fileSize(path string) (int64, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return info.Size(), true, nil
}

func (s *CacheService) blobPath(blobID string) (string, error) {
	return s.app.blobs.Path(blobID)
}

func (s *CacheService) artworkBlobPath(blobID, fileExt string) (string, error) {
	return s.app.blobs.ArtworkPath(blobID, fileExt)
}

func ensureCacheEntry(entries map[string]*cacheBlobEntry, blobID string) *cacheBlobEntry {
	if entry, ok := entries[blobID]; ok {
		return entry
	}
	entry := &cacheBlobEntry{
		BlobID: blobID,
		Kind:   apitypes.CacheKindUnknown,
	}
	entries[blobID] = entry
	return entry
}

func addPinScope(entry *cacheBlobEntry, scope apitypes.CachePinScopeRef) {
	if entry == nil {
		return
	}
	scope.Scope = strings.TrimSpace(scope.Scope)
	scope.ScopeID = strings.TrimSpace(scope.ScopeID)
	if scope.Scope == "" || scope.ScopeID == "" {
		return
	}
	for _, existing := range entry.PinScopes {
		if existing.Scope == scope.Scope && existing.ScopeID == scope.ScopeID {
			return
		}
	}
	entry.PinScopes = append(entry.PinScopes, scope)
}

func unpinnedEntries(entries []cacheBlobEntry) []cacheBlobEntry {
	out := make([]cacheBlobEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Pinned {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastAccessed.Equal(out[j].LastAccessed) {
			return out[i].BlobID < out[j].BlobID
		}
		return out[i].LastAccessed.Before(out[j].LastAccessed)
	})
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func parseSQLiteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
