package playback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

const (
	playbackSessionStateRowID    = 1
	playbackSessionSchemaVersion = 5
)

type SQLiteStore struct {
	path string
	db   *gorm.DB
	mu   sync.Mutex
}

type sqliteSessionRow struct {
	ID            int       `gorm:"primaryKey;column:id"`
	SchemaVersion int       `gorm:"column:schema_version"`
	SnapshotJSON  string    `gorm:"column:snapshot_json;type:text"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (sqliteSessionRow) TableName() string {
	return "playback_session_state"
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&sqliteSessionRow{}); err != nil {
		return nil, err
	}
	return &SQLiteStore{path: path, db: db}, nil
}

func DefaultStorePath(appName string) (string, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(configRoot, appName)
	return filepath.Join(appDir, "playback-state.db"), nil
}

func (s *SQLiteStore) Load(ctx context.Context) (SessionSnapshot, error) {
	defaultSnapshot := normalizeSnapshot(defaultSessionSnapshot())
	if s == nil || s.db == nil {
		return defaultSnapshot, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var row sqliteSessionRow
	err := s.db.WithContext(ctx).First(&row, "id = ?", playbackSessionStateRowID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaultSnapshot, nil
	}
	if err != nil {
		return SessionSnapshot{}, err
	}
	if row.SchemaVersion != playbackSessionSchemaVersion {
		_ = s.db.WithContext(ctx).Delete(&sqliteSessionRow{}, "id = ?", playbackSessionStateRowID).Error
		return defaultSnapshot, nil
	}

	snapshot := defaultSessionSnapshot()
	if strings.TrimSpace(row.SnapshotJSON) != "" {
		if err := json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
			return defaultSnapshot, nil
		}
	}
	if snapshot.UpdatedAt == "" {
		snapshot.UpdatedAt = formatTimestamp(row.UpdatedAt)
	}
	if snapshot.Status == StatusPlaying {
		snapshot.Status = StatusPaused
	}
	return normalizeSnapshot(snapshot), nil
}

func (s *SQLiteStore) Save(ctx context.Context, snapshot SessionSnapshot) error {
	if s == nil || s.db == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := normalizeSnapshot(snapshot)
	if normalized.Status == StatusPlaying {
		normalized.Status = StatusPaused
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return err
	}

	row := sqliteSessionRow{
		ID:            playbackSessionStateRowID,
		SchemaVersion: playbackSessionSchemaVersion,
		SnapshotJSON:  string(payload),
		UpdatedAt:     rowUpdatedAt(normalized),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"snapshot_json", "updated_at"}),
	}).Create(&row).Error
}

func rowUpdatedAt(snapshot SessionSnapshot) time.Time {
	if updatedAt, ok := parseTimestamp(snapshot.UpdatedAt); ok {
		return updatedAt
	}
	return time.Now().UTC()
}

func (s *SQLiteStore) Clear(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.WithContext(ctx).Delete(&sqliteSessionRow{}, "id = ?", playbackSessionStateRowID).Error
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

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
	if err := db.Exec("PRAGMA synchronous=NORMAL;").Error; err != nil {
		return fmt.Errorf("set synchronous normal: %w", err)
	}
	if err := db.Exec("PRAGMA busy_timeout=5000;").Error; err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	return nil
}
