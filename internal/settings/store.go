package settings

import (
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

const settingsStateRowID = 1

type State struct {
	Core CoreRuntimeSettings `json:"core,omitempty"`
}

type CoreRuntimeSettings struct {
	DBPath           string `json:"dbPath,omitempty"`
	BlobRoot         string `json:"blobRoot,omitempty"`
	IdentityKeyPath  string `json:"identityKeyPath,omitempty"`
	FFmpegPath       string `json:"ffmpegPath,omitempty"`
	TranscodeProfile string `json:"transcodeProfile,omitempty"`
}

type Store struct {
	path string
	db   *gorm.DB
	mu   sync.Mutex
}

type sqliteSettingsRow struct {
	ID          int       `gorm:"primaryKey;column:id"`
	PayloadJSON string    `gorm:"column:payload_json;type:text"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (sqliteSettingsRow) TableName() string {
	return "app_settings_state"
}

func DefaultPath(appName string) (string, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configRoot, appName, "settings.db"), nil
}

func NewStore(path string) (*Store, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&sqliteSettingsRow{}); err != nil {
		return nil, err
	}
	return &Store{path: path, db: db}, nil
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) Load() (State, error) {
	if s == nil || s.db == nil {
		return State{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var row sqliteSettingsRow
	err := s.db.First(&row, "id = ?", settingsStateRowID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}

	if strings.TrimSpace(row.PayloadJSON) == "" {
		return State{}, nil
	}

	var state State
	if err := json.Unmarshal([]byte(row.PayloadJSON), &state); err != nil {
		return State{}, err
	}
	return normalizeState(state), nil
}

func (s *Store) Save(state State) error {
	if s == nil || s.db == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(normalizeState(state))
	if err != nil {
		return err
	}

	row := sqliteSettingsRow{
		ID:          settingsStateRowID,
		PayloadJSON: string(payload),
		UpdatedAt:   time.Now().UTC(),
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload_json", "updated_at"}),
	}).Create(&row).Error
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func normalizeState(state State) State {
	state.Core = normalizeCoreRuntimeSettings(state.Core)
	return state
}

func normalizeCoreRuntimeSettings(settings CoreRuntimeSettings) CoreRuntimeSettings {
	settings.DBPath = strings.TrimSpace(settings.DBPath)
	settings.BlobRoot = strings.TrimSpace(settings.BlobRoot)
	settings.IdentityKeyPath = strings.TrimSpace(settings.IdentityKeyPath)
	settings.FFmpegPath = strings.TrimSpace(settings.FFmpegPath)
	settings.TranscodeProfile = strings.TrimSpace(settings.TranscodeProfile)
	return settings
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
