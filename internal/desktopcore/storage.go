package desktopcore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

func openSQLite(path string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}

	db, err := gorm.Open(sqlite.Dialector{DriverName: "sqlite", DSN: path}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := configureSQLiteRuntime(db); err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// Keep SQLite on a single shared connection to avoid concurrent writer
	// contention in the embedded desktop runtime.
	sqlDB.SetMaxOpenConns(1)
	return db, nil
}

func configureSQLiteRuntime(db *gorm.DB) error {
	if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		return fmt.Errorf("set journal_mode wal: %w", err)
	}
	mode, err := sqliteJournalMode(db)
	if err != nil {
		return err
	}
	if mode != "wal" {
		return fmt.Errorf("unexpected sqlite journal mode %q", mode)
	}
	if err := db.Exec("PRAGMA synchronous=NORMAL;").Error; err != nil {
		return fmt.Errorf("set synchronous normal: %w", err)
	}
	if err := db.Exec("PRAGMA busy_timeout=5000;").Error; err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	return nil
}

func sqliteJournalMode(db *gorm.DB) (string, error) {
	var mode string
	if err := db.Raw("PRAGMA journal_mode;").Scan(&mode).Error; err != nil {
		return "", fmt.Errorf("query journal_mode pragma: %w", err)
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "", fmt.Errorf("journal mode is empty")
	}
	return mode, nil
}

func closeSQL(database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Library{},
		&Device{},
		&LocalSetting{},
		&Membership{},
		&ScanRoot{},
		&LocalSourcePath{},
		&LocalArtworkSourceRef{},
		&ScanMaintenanceState{},
		&PinRoot{},
		&PinMember{},
		&PinBlobRef{},
		&AdmissionAuthority{},
		&MembershipCert{},
		&MembershipCertRevocation{},
		&MembershipRecovery{},
		&InviteJoinRequest{},
		&InviteTokenRedemption{},
		&IssuedInvite{},
		&JoinSession{},
		&Artist{},
		&Credit{},
		&AlbumVariantModel{},
		&TrackVariantModel{},
		&AlbumTrack{},
		&DeviceVariantPreference{},
		&SourceFileModel{},
		&OptimizedAssetModel{},
		&DeviceAssetCacheModel{},
		&ArtworkVariant{},
		&Playlist{},
		&PlaylistItem{},
		&OplogEntry{},
		&DeviceClock{},
		&PeerSyncState{},
		&LibraryCheckpoint{},
		&LibraryCheckpointChunk{},
		&DeviceCheckpointAck{},
	)
}

const (
	pathPrivacyEpoch                 = "2"
	contextIdentityEpoch             = "3"
	catalogMaterializationEpoch      = "1"
	localSettingCatalogIdentityEpoch = "catalog_identity_epoch"
	localSettingCatalogMaterialEpoch = "catalog_materialization_epoch"
)

func (a *App) runPathPrivacyMigration(ctx context.Context) error {
	if a == nil || a.storage == nil {
		return nil
	}

	device, err := a.ensureCurrentDevice(ctx)
	if err != nil {
		return fmt.Errorf("ensure current device for path privacy migration: %w", err)
	}
	currentDeviceID := strings.TrimSpace(device.DeviceID)
	if currentDeviceID == "" {
		return fmt.Errorf("current device id is required for path privacy migration")
	}

	var setting LocalSetting
	err = a.storage.WithContext(ctx).Where("key = ?", localSettingPathPrivacyEpoch).Take(&setting).Error
	switch {
	case err == nil && strings.TrimSpace(setting.Value) == pathPrivacyEpoch:
		return nil
	case err != nil && err != gorm.ErrRecordNotFound:
		return err
	}

	now := time.Now().UTC()
	return a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var sources []SourceFileModel
		if err := tx.Order("library_id ASC, device_id ASC, source_file_id ASC").Find(&sources).Error; err != nil {
			return err
		}
		for _, row := range sources {
			if strings.TrimSpace(row.DeviceID) == currentDeviceID && strings.TrimSpace(row.LocalPath) != "" {
				if err := tx.Save(&LocalSourcePath{
					LibraryID:    strings.TrimSpace(row.LibraryID),
					DeviceID:     currentDeviceID,
					SourceFileID: strings.TrimSpace(row.SourceFileID),
					LocalPath:    filepath.Clean(strings.TrimSpace(row.LocalPath)),
					PathKey:      localPathKey(row.LocalPath),
					UpdatedAt:    now,
				}).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Model(&SourceFileModel{}).
				Where("library_id = ? AND device_id = ? AND source_file_id = ?", row.LibraryID, row.DeviceID, row.SourceFileID).
				Updates(map[string]any{
					"local_path": "",
					"path_key":   opaqueSourcePathKey(strings.TrimSpace(row.SourceFileID)),
				}).Error; err != nil {
				return err
			}
		}

		var artwork []ArtworkVariant
		if err := tx.Order("library_id ASC, scope_type ASC, scope_id ASC, variant ASC").Find(&artwork).Error; err != nil {
			return err
		}
		for _, row := range artwork {
			if strings.TrimSpace(row.ChosenSourceRef) == "" {
				continue
			}
			if err := tx.Save(&LocalArtworkSourceRef{
				LibraryID:       strings.TrimSpace(row.LibraryID),
				ScopeType:       strings.TrimSpace(row.ScopeType),
				ScopeID:         strings.TrimSpace(row.ScopeID),
				Variant:         strings.TrimSpace(row.Variant),
				ChosenSource:    strings.TrimSpace(row.ChosenSource),
				ChosenSourceRef: filepath.Clean(strings.TrimSpace(row.ChosenSourceRef)),
				UpdatedAt:       now,
			}).Error; err != nil {
				return err
			}
			if err := tx.Model(&ArtworkVariant{}).
				Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", row.LibraryID, row.ScopeType, row.ScopeID, row.Variant).
				Update("chosen_source_ref", "").Error; err != nil {
				return err
			}
		}

		if err := scrubPathBearingOplogRowsTx(tx); err != nil {
			return err
		}
		if err := clearPathBearingCheckpointStateTx(tx); err != nil {
			return err
		}
		return upsertLocalSettingTx(tx, localSettingPathPrivacyEpoch, pathPrivacyEpoch, now)
	})
}

func (a *App) runContextIdentityMigration(ctx context.Context) error {
	if a == nil || a.storage == nil {
		return nil
	}

	var setting LocalSetting
	err := a.storage.WithContext(ctx).Where("key = ?", localSettingCatalogIdentityEpoch).Take(&setting).Error
	switch {
	case err == nil && strings.TrimSpace(setting.Value) == contextIdentityEpoch:
		return nil
	case err != nil && err != gorm.ErrRecordNotFound:
		return err
	}

	now := time.Now().UTC()
	return a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		for _, stmt := range []string{
			"DROP INDEX IF EXISTS idx_source_file_fingerprint",
			"DROP INDEX IF EXISTS idx_album_variant_key",
			"DROP INDEX IF EXISTS idx_track_variant_key",
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}

		for _, model := range []any{
			&PlaylistItem{},
			&PinBlobRef{},
			&PinMember{},
			&PinRoot{},
			&DeviceVariantPreference{},
			&ArtworkVariant{},
			&LocalArtworkSourceRef{},
			&AlbumTrack{},
			&AlbumVariantModel{},
			&TrackVariantModel{},
			&OptimizedAssetModel{},
			&DeviceAssetCacheModel{},
			&SourceFileModel{},
			&LocalSourcePath{},
			&OplogEntry{},
			&DeviceClock{},
			&PeerSyncState{},
			&LibraryCheckpointChunk{},
			&LibraryCheckpoint{},
			&DeviceCheckpointAck{},
		} {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(model).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&Playlist{}).
			Where("kind = ?", playlistKindLiked).
			Update("deleted_at", now).Error; err != nil {
			return err
		}
		return upsertLocalSettingTx(tx, localSettingCatalogIdentityEpoch, contextIdentityEpoch, now)
	})
}

func (a *App) runCatalogMaterializationMigration(ctx context.Context) error {
	if a == nil || a.storage == nil {
		return nil
	}

	var setting LocalSetting
	err := a.storage.WithContext(ctx).Where("key = ?", localSettingCatalogMaterialEpoch).Take(&setting).Error
	switch {
	case err == nil && strings.TrimSpace(setting.Value) == catalogMaterializationEpoch:
		return nil
	case err != nil && err != gorm.ErrRecordNotFound:
		return err
	}

	var libraryIDs []string
	if err := a.storage.WithContext(ctx).
		Model(&Library{}).
		Select("library_id").
		Order("library_id ASC").
		Scan(&libraryIDs).Error; err != nil {
		return err
	}
	for _, libraryID := range libraryIDs {
		libraryID = strings.TrimSpace(libraryID)
		if libraryID == "" {
			continue
		}
		if err := a.rebuildCatalogMaterializationFull(ctx, libraryID, nil); err != nil {
			return fmt.Errorf("rebuild catalog materialization for %s: %w", libraryID, err)
		}
	}

	now := time.Now().UTC()
	return a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		return upsertLocalSettingTx(tx, localSettingCatalogMaterialEpoch, catalogMaterializationEpoch, now)
	})
}

func runPinStorageMigration(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.Exec("DROP TABLE IF EXISTS offline_pins").Error
}

func scrubPathBearingOplogRowsTx(tx *gorm.DB) error {
	var rows []OplogEntry
	if err := tx.Where("entity_type IN ?", []string{entityTypeScanRoots, entityTypeSourceFile, entityTypeArtworkVariant}).
		Order("library_id ASC, device_id ASC, seq ASC, op_id ASC").
		Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		switch strings.TrimSpace(row.EntityType) {
		case entityTypeScanRoots:
			if err := tx.Delete(&row).Error; err != nil {
				return err
			}
		case entityTypeSourceFile:
			var payload sourceFileOplogPayload
			if strings.TrimSpace(row.PayloadJSON) != "" && strings.TrimSpace(row.PayloadJSON) != "{}" {
				if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
					return fmt.Errorf("decode source file oplog payload during migration: %w", err)
				}
			}
			payload.LocalPath = ""
			raw, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal scrubbed source file oplog payload: %w", err)
			}
			if err := tx.Model(&OplogEntry{}).
				Where("library_id = ? AND op_id = ?", row.LibraryID, row.OpID).
				Update("payload_json", string(raw)).Error; err != nil {
				return err
			}
		case entityTypeArtworkVariant:
			var payload artworkVariantOplogPayload
			if strings.TrimSpace(row.PayloadJSON) != "" && strings.TrimSpace(row.PayloadJSON) != "{}" {
				if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
					return fmt.Errorf("decode artwork variant oplog payload during migration: %w", err)
				}
			}
			payload.ChosenSourceRef = ""
			raw, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal scrubbed artwork variant oplog payload: %w", err)
			}
			if err := tx.Model(&OplogEntry{}).
				Where("library_id = ? AND op_id = ?", row.LibraryID, row.OpID).
				Update("payload_json", string(raw)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func clearPathBearingCheckpointStateTx(tx *gorm.DB) error {
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&LibraryCheckpointChunk{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&LibraryCheckpoint{}).Error; err != nil {
		return err
	}
	return tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&DeviceCheckpointAck{}).Error
}

func reclaimSQLiteSpace(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
		return fmt.Errorf("truncate wal before vacuum: %w", err)
	}
	if err := db.WithContext(ctx).Exec("VACUUM;").Error; err != nil {
		return fmt.Errorf("vacuum sqlite database: %w", err)
	}
	if err := db.WithContext(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
		return fmt.Errorf("truncate wal after vacuum: %w", err)
	}
	return nil
}
