package desktopcore

import (
	"context"

	"gorm.io/gorm"
)

type DBService struct {
	db *gorm.DB
}

func OpenDBService(path string) (*DBService, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := autoMigrate(db); err != nil {
		_ = closeSQL(db)
		return nil, err
	}
	return &DBService{db: db}, nil
}

func NewDBService(db *gorm.DB) *DBService {
	if db == nil {
		return nil
	}
	return &DBService{db: db}
}

func (s *DBService) DB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *DBService) WithContext(ctx context.Context) *gorm.DB {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx)
}

func (s *DBService) Transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(fn)
}

func (s *DBService) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return closeSQL(s.db)
}
