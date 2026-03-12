package desktopcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		&OfflinePin{},
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
