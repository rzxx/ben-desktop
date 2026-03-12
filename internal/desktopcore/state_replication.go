package desktopcore

import (
	"strings"
	"time"

	apitypes "ben/core/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) upsertOptimizedAssetTx(tx *gorm.DB, local apitypes.LocalContext, row OptimizedAssetModel) error {
	now := time.Now().UTC()
	row.LibraryID = strings.TrimSpace(local.LibraryID)
	row.OptimizedAssetID = strings.TrimSpace(row.OptimizedAssetID)
	row.SourceFileID = strings.TrimSpace(row.SourceFileID)
	row.TrackVariantID = strings.TrimSpace(row.TrackVariantID)
	row.Profile = strings.TrimSpace(row.Profile)
	row.BlobID = strings.TrimSpace(row.BlobID)
	row.MIME = strings.TrimSpace(row.MIME)
	row.Codec = strings.TrimSpace(row.Codec)
	row.Container = strings.TrimSpace(row.Container)
	row.CreatedByDeviceID = strings.TrimSpace(row.CreatedByDeviceID)
	if row.CreatedByDeviceID == "" {
		row.CreatedByDeviceID = strings.TrimSpace(local.DeviceID)
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "optimized_asset_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"source_file_id", "track_variant_id", "profile", "blob_id", "mime", "duration_ms", "bitrate", "codec", "container", "created_by_device_id", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	_, err := a.appendLocalOplogTx(tx, local, entityTypeOptimizedAsset, optimizedAssetEntityID(row.OptimizedAssetID), "upsert", optimizedAssetOplogPayload{
		OptimizedAssetID:  row.OptimizedAssetID,
		SourceFileID:      row.SourceFileID,
		TrackVariantID:    row.TrackVariantID,
		Profile:           row.Profile,
		BlobID:            row.BlobID,
		MIME:              row.MIME,
		DurationMS:        row.DurationMS,
		Bitrate:           row.Bitrate,
		Codec:             row.Codec,
		Container:         row.Container,
		CreatedByDeviceID: row.CreatedByDeviceID,
		CreatedAtNS:       row.CreatedAt.UTC().UnixNano(),
		UpdatedAtNS:       row.UpdatedAt.UTC().UnixNano(),
	})
	return err
}

func (a *App) deleteOptimizedAssetTx(tx *gorm.DB, local apitypes.LocalContext, optimizedAssetID string) error {
	optimizedAssetID = strings.TrimSpace(optimizedAssetID)
	if optimizedAssetID == "" {
		return nil
	}
	if err := tx.Where("library_id = ? AND optimized_asset_id = ?", local.LibraryID, optimizedAssetID).Delete(&DeviceAssetCacheModel{}).Error; err != nil {
		return err
	}
	result := tx.Where("library_id = ? AND optimized_asset_id = ?", local.LibraryID, optimizedAssetID).Delete(&OptimizedAssetModel{})
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	_, err := a.appendLocalOplogTx(tx, local, entityTypeOptimizedAsset, optimizedAssetEntityID(optimizedAssetID), "delete", optimizedAssetDeleteOplogPayload{
		OptimizedAssetID: optimizedAssetID,
	})
	return err
}

func (a *App) upsertDeviceAssetCacheTx(tx *gorm.DB, local apitypes.LocalContext, row DeviceAssetCacheModel) error {
	now := time.Now().UTC()
	row.LibraryID = strings.TrimSpace(local.LibraryID)
	row.DeviceID = strings.TrimSpace(row.DeviceID)
	row.OptimizedAssetID = strings.TrimSpace(row.OptimizedAssetID)
	if row.DeviceID == "" {
		row.DeviceID = strings.TrimSpace(local.DeviceID)
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
			{Name: "optimized_asset_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"is_cached", "last_verified_at", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	payload := deviceAssetCacheOplogPayload{
		DeviceID:         row.DeviceID,
		OptimizedAssetID: row.OptimizedAssetID,
		IsCached:         row.IsCached,
		UpdatedAtNS:      row.UpdatedAt.UTC().UnixNano(),
	}
	if row.LastVerifiedAt != nil {
		payload.HasLastVerifiedAt = true
		payload.LastVerifiedAtNS = row.LastVerifiedAt.UTC().UnixNano()
	}
	_, err := a.appendLocalOplogTx(tx, local, entityTypeDeviceAssetCache, deviceAssetCacheEntityID(row.DeviceID, row.OptimizedAssetID), "upsert", payload)
	return err
}

func (a *App) upsertArtworkVariantTx(tx *gorm.DB, local apitypes.LocalContext, row ArtworkVariant) error {
	now := time.Now().UTC()
	row.LibraryID = strings.TrimSpace(local.LibraryID)
	row.ScopeType = strings.TrimSpace(row.ScopeType)
	row.ScopeID = strings.TrimSpace(row.ScopeID)
	row.Variant = strings.TrimSpace(row.Variant)
	row.BlobID = strings.TrimSpace(row.BlobID)
	row.MIME = strings.TrimSpace(row.MIME)
	row.FileExt = normalizeArtworkFileExt(row.FileExt, row.MIME)
	row.ChosenSource = strings.TrimSpace(row.ChosenSource)
	row.ChosenSourceRef = strings.TrimSpace(row.ChosenSourceRef)
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "scope_type"},
			{Name: "scope_id"},
			{Name: "variant"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"blob_id", "mime", "file_ext", "w", "h", "bytes", "chosen_source", "chosen_source_ref", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	_, err := a.appendLocalOplogTx(tx, local, entityTypeArtworkVariant, artworkVariantEntityID(row.ScopeType, row.ScopeID, row.Variant), "upsert", artworkVariantOplogPayload{
		ScopeType:       row.ScopeType,
		ScopeID:         row.ScopeID,
		Variant:         row.Variant,
		BlobID:          row.BlobID,
		MIME:            row.MIME,
		FileExt:         row.FileExt,
		W:               row.W,
		H:               row.H,
		Bytes:           row.Bytes,
		ChosenSource:    row.ChosenSource,
		ChosenSourceRef: row.ChosenSourceRef,
		UpdatedAtNS:     row.UpdatedAt.UTC().UnixNano(),
	})
	return err
}

func (a *App) deleteArtworkScopeTx(tx *gorm.DB, local apitypes.LocalContext, scopeType, scopeID string) error {
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	if scopeType == "" || scopeID == "" {
		return nil
	}
	var rows []ArtworkVariant
	if err := tx.Where("library_id = ? AND scope_type = ? AND scope_id = ?", local.LibraryID, scopeType, scopeID).
		Order("variant ASC").
		Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := tx.Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", local.LibraryID, scopeType, scopeID, row.Variant).
			Delete(&ArtworkVariant{}).Error; err != nil {
			return err
		}
		if _, err := a.appendLocalOplogTx(tx, local, entityTypeArtworkVariant, artworkVariantEntityID(scopeType, scopeID, row.Variant), "delete", artworkVariantDeleteOplogPayload{
			ScopeType: scopeType,
			ScopeID:   scopeID,
			Variant:   strings.TrimSpace(row.Variant),
		}); err != nil {
			return err
		}
	}
	return nil
}
