package desktopcore

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OfflineService struct {
	app *App

	mu    sync.Mutex
	dirty map[string]struct{}
}

type offlineMemberSeedRow struct {
	LibraryRecordingID string
	OfflineSince       time.Time
	HasLocalSource     bool
	HasLocalCached     bool
}

type offlineSummaryRow struct {
	ItemCount  int64
	UpdatedAt  *time.Time
	HasAnyRows bool
}

func newOfflineService(app *App) *OfflineService {
	return &OfflineService{
		app:   app,
		dirty: make(map[string]struct{}),
	}
}

func (s *OfflineService) dirtyKey(libraryID, deviceID string) string {
	return strings.TrimSpace(libraryID) + "|" + strings.TrimSpace(deviceID)
}

func (s *OfflineService) markDirty(libraryID, deviceID string) {
	if s == nil {
		return
	}
	key := s.dirtyKey(libraryID, deviceID)
	if key == "|" {
		return
	}
	s.mu.Lock()
	s.dirty[key] = struct{}{}
	s.mu.Unlock()
}

func (s *OfflineService) clearDirty(libraryID, deviceID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.dirty, s.dirtyKey(libraryID, deviceID))
	s.mu.Unlock()
}

func (s *OfflineService) isDirty(libraryID, deviceID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	_, ok := s.dirty[s.dirtyKey(libraryID, deviceID)]
	s.mu.Unlock()
	return ok
}

func (s *OfflineService) ensureFresh(ctx context.Context, local apitypes.LocalContext) error {
	if s == nil {
		return nil
	}
	if !s.isDirty(local.LibraryID, local.DeviceID) {
		return nil
	}
	changed, err := s.rebuildSnapshot(ctx, local)
	if err != nil {
		return err
	}
	s.clearDirty(local.LibraryID, local.DeviceID)
	if changed {
		s.emitCatalogMutationEvents(local)
	}
	return nil
}

func (s *OfflineService) rebuildSnapshot(ctx context.Context, local apitypes.LocalContext) (bool, error) {
	changed := false
	err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var err error
		changed, err = s.rebuildSnapshotTx(tx, local)
		return err
	})
	return changed, err
}

func (s *OfflineService) rebuildSnapshotTx(tx *gorm.DB, local apitypes.LocalContext) (bool, error) {
	desired, err := s.queryDesiredMembersTx(tx, local, nil)
	if err != nil {
		return false, err
	}
	existing, err := s.loadExistingMembersTx(tx, local, nil)
	if err != nil {
		return false, err
	}
	return s.applyDesiredMembersTx(tx, local, desired, existing)
}

func (s *OfflineService) reconcileLibraryRecordingsTx(tx *gorm.DB, local apitypes.LocalContext, clusterIDs []string) (bool, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return false, nil
	}
	desired, err := s.queryDesiredMembersTx(tx, local, clusterIDs)
	if err != nil {
		return false, err
	}
	existing, err := s.loadExistingMembersTx(tx, local, clusterIDs)
	if err != nil {
		return false, err
	}
	changed, err := s.applyDesiredMembersTx(tx, local, desired, existing)
	if err != nil {
		return false, err
	}
	if changed {
		s.emitCatalogMutationEvents(local)
	}
	return changed, nil
}

func (s *OfflineService) applyDesiredMembersTx(tx *gorm.DB, local apitypes.LocalContext, desired map[string]OfflineMember, existing map[string]OfflineMember) (bool, error) {
	now := time.Now().UTC()
	changed := false
	for libraryRecordingID, member := range desired {
		current, exists := existing[libraryRecordingID]
		if exists {
			member.OfflineSince = current.OfflineSince
			if current.HasLocalSource != member.HasLocalSource || current.HasLocalCached != member.HasLocalCached {
				changed = true
			}
		} else {
			member.OfflineSince = now
			changed = true
		}
		member.UpdatedAt = now
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "device_id"},
				{Name: "library_recording_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"has_local_source", "has_local_cached", "offline_since", "updated_at"}),
		}).Create(&member).Error; err != nil {
			return false, err
		}
		delete(existing, libraryRecordingID)
	}

	for libraryRecordingID := range existing {
		changed = true
		if err := tx.Where(
			"library_id = ? AND device_id = ? AND library_recording_id = ?",
			local.LibraryID,
			local.DeviceID,
			libraryRecordingID,
		).Delete(&OfflineMember{}).Error; err != nil {
			return false, err
		}
	}
	return changed, nil
}

func (s *OfflineService) loadExistingMembersTx(tx *gorm.DB, local apitypes.LocalContext, clusterIDs []string) (map[string]OfflineMember, error) {
	query := tx.Where("library_id = ? AND device_id = ?", local.LibraryID, local.DeviceID)
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) > 0 {
		query = query.Where("library_recording_id IN ?", clusterIDs)
	}
	var rows []OfflineMember
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]OfflineMember, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.LibraryRecordingID)] = row
	}
	return out, nil
}

func (s *OfflineService) queryDesiredMembersTx(tx *gorm.DB, local apitypes.LocalContext, clusterIDs []string) (map[string]OfflineMember, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	args := []any{local.LibraryID, local.DeviceID}
	sourceFilter := ""
	cacheFilter := ""
	if len(clusterIDs) > 0 {
		sourceFilter = " AND COALESCE(NULLIF(tv.track_cluster_id, ''), tv.track_variant_id) IN ?"
		cacheFilter = " AND COALESCE(NULLIF(tv.track_cluster_id, ''), tv.track_variant_id) IN ?"
		args = append(args, clusterIDs)
	}
	args = append(args, local.LibraryID, local.DeviceID)
	if len(clusterIDs) > 0 {
		args = append(args, clusterIDs)
	}
	query := `
SELECT
	grouped.library_recording_id,
	MAX(grouped.has_local_source) AS has_local_source,
	MAX(grouped.has_local_cached) AS has_local_cached
FROM (
	SELECT
		COALESCE(NULLIF(tv.track_cluster_id, ''), tv.track_variant_id) AS library_recording_id,
		1 AS has_local_source,
		0 AS has_local_cached
	FROM source_files sf
	JOIN track_variants tv ON tv.library_id = sf.library_id AND tv.track_variant_id = sf.track_variant_id
	WHERE sf.library_id = ? AND sf.device_id = ? AND sf.is_present = 1` + sourceFilter + `
	UNION ALL
	SELECT
		COALESCE(NULLIF(tv.track_cluster_id, ''), tv.track_variant_id) AS library_recording_id,
		0 AS has_local_source,
		1 AS has_local_cached
	FROM device_asset_caches dac
	JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
	JOIN track_variants tv ON tv.library_id = oa.library_id AND tv.track_variant_id = oa.track_variant_id
	WHERE dac.library_id = ? AND dac.device_id = ? AND dac.is_cached = 1` + cacheFilter + `
) grouped
GROUP BY grouped.library_recording_id`
	type row struct {
		LibraryRecordingID string
		HasLocalSource     int
		HasLocalCached     int
	}
	var rows []row
	if err := tx.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]OfflineMember, len(rows))
	for _, row := range rows {
		libraryRecordingID := strings.TrimSpace(row.LibraryRecordingID)
		if libraryRecordingID == "" {
			continue
		}
		out[libraryRecordingID] = OfflineMember{
			LibraryID:          local.LibraryID,
			DeviceID:           local.DeviceID,
			LibraryRecordingID: libraryRecordingID,
			HasLocalSource:     row.HasLocalSource > 0,
			HasLocalCached:     row.HasLocalCached > 0,
		}
	}
	return out, nil
}

func (s *OfflineService) summaryForLocal(ctx context.Context, local apitypes.LocalContext) (apitypes.PlaylistListItem, error) {
	if err := s.ensureFresh(ctx, local); err != nil {
		return apitypes.PlaylistListItem{}, err
	}
	row, err := s.summaryRowForLocal(ctx, local)
	if err != nil {
		return apitypes.PlaylistListItem{}, err
	}
	updatedAt := time.Time{}
	if row.UpdatedAt != nil {
		updatedAt = row.UpdatedAt.UTC()
	}
	return apitypes.PlaylistListItem{
		PlaylistID:     offlinePlaylistIDForDevice(local.LibraryID, local.DeviceID),
		Name:           "Offline",
		Kind:           apitypes.PlaylistKindOffline,
		IsReserved:     true,
		ScopePinned:    false,
		HasCustomCover: false,
		CreatedBy:      local.DeviceID,
		UpdatedAt:      updatedAt,
		ItemCount:      row.ItemCount,
	}, nil
}

func (s *OfflineService) summaryRowForLocal(ctx context.Context, local apitypes.LocalContext) (offlineSummaryRow, error) {
	type row struct {
		ItemCount int64
		UpdatedAt sql.NullString
	}
	var result row
	query := `
SELECT
	COUNT(*) AS item_count,
	MAX(updated_at) AS updated_at
FROM offline_members
WHERE library_id = ? AND device_id = ?`
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, local.LibraryID, local.DeviceID).Scan(&result).Error; err != nil {
		return offlineSummaryRow{}, err
	}
	var updatedAt *time.Time
	if result.UpdatedAt.Valid {
		if parsed := parseSQLiteTime(result.UpdatedAt.String); !parsed.IsZero() {
			updatedAt = &parsed
		}
	}
	return offlineSummaryRow{
		ItemCount:  result.ItemCount,
		UpdatedAt:  updatedAt,
		HasAnyRows: result.ItemCount > 0,
	}, nil
}

func (s *OfflineService) listSeedsPage(ctx context.Context, local apitypes.LocalContext, req apitypes.PageRequest) ([]offlineMemberSeedRow, apitypes.PageInfo, error) {
	if err := s.ensureFresh(ctx, local); err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	limit, offset := normalizePageRequest(req)
	var total int64
	if err := s.app.storage.ReadWithContext(ctx).
		Model(&OfflineMember{}).
		Where("library_id = ? AND device_id = ?", local.LibraryID, local.DeviceID).
		Count(&total).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	var rows []OfflineMember
	if err := s.app.storage.ReadWithContext(ctx).
		Where("library_id = ? AND device_id = ?", local.LibraryID, local.DeviceID).
		Order("offline_since DESC, library_recording_id DESC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	return offlineSeedRows(rows), newOffsetPageInfo(limit, offset, len(rows), int(total)), nil
}

func (s *OfflineService) listSeedsCursor(ctx context.Context, local apitypes.LocalContext, req apitypes.CursorPageRequest) ([]offlineMemberSeedRow, apitypes.CursorPageInfo, error) {
	if err := s.ensureFresh(ctx, local); err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	}
	limit := normalizeCursorPageRequest(req)
	query := s.app.storage.ReadWithContext(ctx).
		Where("library_id = ? AND device_id = ?", local.LibraryID, local.DeviceID)
	if offlineSince, libraryRecordingID, ok, err := decodeCatalogCursorTimePair(req.Cursor); err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	} else if ok {
		query = query.Where(
			"(offline_since < ? OR (offline_since = ? AND library_recording_id < ?))",
			offlineSince,
			offlineSince,
			libraryRecordingID,
		)
	}
	var rows []OfflineMember
	if err := query.
		Order("offline_since DESC, library_recording_id DESC").
		Limit(limit + 1).
		Find(&rows).Error; err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeCatalogCursor(last.OfflineSince.UTC().Format(time.RFC3339Nano), strings.TrimSpace(last.LibraryRecordingID))
	}
	return offlineSeedRows(rows), apitypes.CursorPageInfo{
		Limit:      limit,
		Returned:   len(rows),
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func offlineSeedRows(rows []OfflineMember) []offlineMemberSeedRow {
	out := make([]offlineMemberSeedRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, offlineMemberSeedRow{
			LibraryRecordingID: strings.TrimSpace(row.LibraryRecordingID),
			OfflineSince:       row.OfflineSince,
			HasLocalSource:     row.HasLocalSource,
			HasLocalCached:     row.HasLocalCached,
		})
	}
	return out
}

func (s *OfflineService) playlistIDForLocal(local apitypes.LocalContext) string {
	return offlinePlaylistIDForDevice(local.LibraryID, local.DeviceID)
}

func (s *OfflineService) emitCatalogMutationEvents(local apitypes.LocalContext) {
	if s == nil || s.app == nil {
		return
	}
	playlistID := s.playlistIDForLocal(local)
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityPlaylists,
		QueryKey:      "playlists",
		EntityID:      playlistID,
		InvalidateAll: true,
	})
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityOffline,
		EntityID:      playlistID,
		QueryKey:      "offline",
		InvalidateAll: true,
	})
}

func (s *OfflineService) resolveLibraryRecordingIDsForEncodingIDsTx(tx *gorm.DB, libraryID, deviceID string, optimizedAssetIDs []string) ([]string, error) {
	optimizedAssetIDs = compactNonEmptyStrings(optimizedAssetIDs)
	if len(optimizedAssetIDs) == 0 {
		return nil, nil
	}
	type row struct{ LibraryRecordingID string }
	var rows []row
	query := `
SELECT DISTINCT COALESCE(NULLIF(tv.track_cluster_id, ''), tv.track_variant_id) AS library_recording_id
FROM device_asset_caches dac
JOIN optimized_assets oa ON oa.library_id = dac.library_id AND oa.optimized_asset_id = dac.optimized_asset_id
JOIN track_variants tv ON tv.library_id = oa.library_id AND tv.track_variant_id = oa.track_variant_id
WHERE dac.library_id = ? AND dac.device_id = ? AND dac.optimized_asset_id IN ?`
	if err := tx.Raw(query, libraryID, deviceID, optimizedAssetIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.LibraryRecordingID))
	}
	return compactNonEmptyStrings(out), nil
}

func (s *OfflineService) resolveLibraryRecordingIDForVariantTx(tx *gorm.DB, libraryID, variantRecordingID string) (string, bool, error) {
	type row struct{ LibraryRecordingID string }
	var result row
	query := `
SELECT COALESCE(NULLIF(track_cluster_id, ''), track_variant_id) AS library_recording_id
FROM track_variants
WHERE library_id = ? AND track_variant_id = ?`
	if err := tx.Raw(query, libraryID, strings.TrimSpace(variantRecordingID)).Scan(&result).Error; err != nil {
		return "", false, err
	}
	if strings.TrimSpace(result.LibraryRecordingID) == "" {
		return "", false, nil
	}
	return strings.TrimSpace(result.LibraryRecordingID), true, nil
}

func (s *OfflineService) resolveLibraryRecordingIDForSourceFileTx(tx *gorm.DB, libraryID, deviceID, sourceFileID string) (string, bool, error) {
	type row struct{ LibraryRecordingID string }
	var result row
	query := `
SELECT COALESCE(NULLIF(tv.track_cluster_id, ''), tv.track_variant_id) AS library_recording_id
FROM source_files sf
JOIN track_variants tv ON tv.library_id = sf.library_id AND tv.track_variant_id = sf.track_variant_id
WHERE sf.library_id = ? AND sf.device_id = ? AND sf.source_file_id = ?`
	if err := tx.Raw(query, libraryID, deviceID, strings.TrimSpace(sourceFileID)).Scan(&result).Error; err != nil {
		return "", false, err
	}
	if strings.TrimSpace(result.LibraryRecordingID) == "" {
		return "", false, nil
	}
	return strings.TrimSpace(result.LibraryRecordingID), true, nil
}

func (s *OfflineService) matchesPlaylistID(local apitypes.LocalContext, playlistID string) bool {
	return strings.TrimSpace(playlistID) != "" && strings.TrimSpace(playlistID) == s.playlistIDForLocal(local)
}
