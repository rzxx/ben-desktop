package desktopcore

import (
	"context"
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
	return openSQLiteWithConns(path, 1)
}

func openSQLiteWithConns(path string, maxOpenConns int) (*gorm.DB, error) {
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
	if maxOpenConns <= 0 {
		maxOpenConns = 1
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
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
	if err := db.AutoMigrate(
		&Library{},
		&Device{},
		&LocalSetting{},
		&Membership{},
		&ScanRoot{},
		&LocalSourcePath{},
		&LocalArtworkSourceRef{},
		&ScanMaintenanceState{},
		&OfflineMember{},
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
	); err != nil {
		return err
	}
	return ensureReadOptimizedIndexes(db)
}

func ensureReadOptimizedIndexes(db *gorm.DB) error {
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_source_files_playback_device ON source_files(library_id, device_id, track_variant_id, is_present, last_seen_at DESC, quality_rank DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_source_files_playback_variant ON source_files(library_id, track_variant_id, is_present, device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_optimized_assets_playback_profile ON optimized_assets(library_id, track_variant_id, profile, created_by_device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_device_asset_caches_playback ON device_asset_caches(library_id, device_id, optimized_asset_id, is_cached)`,
		`CREATE INDEX IF NOT EXISTS idx_playlist_items_position ON playlist_items(library_id, playlist_id, deleted_at, position_key, item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_playlist_items_added_at ON playlist_items(library_id, playlist_id, deleted_at, added_at DESC, item_id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_offline_members_paging ON offline_members(library_id, device_id, offline_since DESC, library_recording_id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_offline_members_updated ON offline_members(library_id, device_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_track_variants_browse_title ON track_variants(library_id, LOWER(title), track_cluster_id, track_variant_id)`,
	}
	for _, query := range indexes {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *App) ensureDatabaseBaseline(ctx context.Context) error {
	if a == nil || a.storage == nil {
		return nil
	}

	var setting LocalSetting
	err := a.storage.WithContext(ctx).Where("key = ?", localSettingDBBaselineEpoch).Take(&setting).Error
	switch {
	case err == nil && strings.TrimSpace(setting.Value) == dbBaselineEpoch:
		return nil
	case err != nil && err != gorm.ErrRecordNotFound:
		return err
	}

	incompatible, err := databasePredatesCurrentBaseline(a.storage.WithContext(ctx))
	if err != nil {
		return err
	}
	if incompatible {
		return fmt.Errorf("database predates the current development baseline %q; old development databases are no longer migrated; delete the local SQLite database and rebuild from scratch", dbBaselineEpoch)
	}

	now := time.Now().UTC()
	return a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		return upsertLocalSettingTx(tx, localSettingDBBaselineEpoch, dbBaselineEpoch, now)
	})
}

func databasePredatesCurrentBaseline(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, nil
	}

	exists, err := sqliteTableExists(db, "offline_pins")
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}

	for _, table := range []string{
		"libraries",
		"source_files",
		"track_variants",
		"album_variants",
		"playlist_items",
		"pin_roots",
		"oplog_entries",
	} {
		hasRows, err := sqliteTableHasRows(db, table)
		if err != nil {
			return false, err
		}
		if hasRows {
			return true, nil
		}
	}
	return false, nil
}

func sqliteTableExists(db *gorm.DB, table string) (bool, error) {
	var count int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		strings.TrimSpace(table),
	).Scan(&count).Error; err != nil {
		return false, fmt.Errorf("query sqlite_master for table %q: %w", table, err)
	}
	return count > 0, nil
}

func sqliteTableHasRows(db *gorm.DB, table string) (bool, error) {
	var count int64
	if err := db.Table(strings.TrimSpace(table)).Count(&count).Error; err != nil {
		return false, fmt.Errorf("count rows in %q: %w", table, err)
	}
	return count > 0, nil
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
