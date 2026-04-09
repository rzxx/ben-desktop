package desktopcore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenStampsBaselineOnFreshDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	app := openBaselineTestApp(t, ctx, root)
	defer closeBaselineTestApp(t, app)

	var setting LocalSetting
	if err := app.db.WithContext(ctx).Where("key = ?", localSettingDBBaselineEpoch).Take(&setting).Error; err != nil {
		t.Fatalf("load baseline setting: %v", err)
	}
	if strings.TrimSpace(setting.Value) != dbBaselineEpoch {
		t.Fatalf("baseline setting = %q, want %q", setting.Value, dbBaselineEpoch)
	}
}

func TestOpenAllowsReopenOfStampedDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	app := openBaselineTestApp(t, ctx, root)
	closeBaselineTestApp(t, app)

	app = openBaselineTestApp(t, ctx, root)
	defer closeBaselineTestApp(t, app)
}

func TestOpenRejectsLegacyPopulatedDatabaseWithoutBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "library.db")

	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	now := time.Now().UTC()
	if err := db.Create(&Library{
		LibraryID: "library-legacy",
		Name:      "legacy",
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed library: %v", err)
	}
	if err := closeSQL(db); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	_, err = Open(ctx, Config{
		DBPath:           dbPath,
		BlobRoot:         filepath.Join(root, "blobs"),
		IdentityKeyPath:  filepath.Join(root, "identity.key"),
		CacheBytes:       1024,
		TranscodeBuilder: &fakeAACBuilder{result: []byte("test-encoded")},
	})
	if err == nil {
		t.Fatalf("expected open to fail for legacy populated database")
	}
	if !strings.Contains(err.Error(), "database predates the current development baseline") {
		t.Fatalf("open error = %v, want baseline incompatibility", err)
	}
	if !strings.Contains(err.Error(), "delete the local SQLite database and rebuild from scratch") {
		t.Fatalf("open error = %v, want explicit reset guidance", err)
	}
}

func TestOpenRejectsDatabaseWithOfflinePinsTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "library.db")

	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("CREATE TABLE offline_pins (id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create offline_pins: %v", err)
	}
	if err := closeSQL(db); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	_, err = Open(ctx, Config{
		DBPath:           dbPath,
		BlobRoot:         filepath.Join(root, "blobs"),
		IdentityKeyPath:  filepath.Join(root, "identity.key"),
		CacheBytes:       1024,
		TranscodeBuilder: &fakeAACBuilder{result: []byte("test-encoded")},
	})
	if err == nil {
		t.Fatalf("expected open to fail when offline_pins exists")
	}
	if !strings.Contains(err.Error(), "database predates the current development baseline") {
		t.Fatalf("open error = %v, want baseline incompatibility", err)
	}
}

func openBaselineTestApp(t *testing.T, ctx context.Context, root string) *App {
	t.Helper()

	app, err := Open(ctx, Config{
		DBPath:           filepath.Join(root, "library.db"),
		BlobRoot:         filepath.Join(root, "blobs"),
		IdentityKeyPath:  filepath.Join(root, "identity.key"),
		CacheBytes:       1024,
		TranscodeBuilder: &fakeAACBuilder{result: []byte("test-encoded")},
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	return app
}

func closeBaselineTestApp(t *testing.T, app *App) {
	t.Helper()

	if app == nil {
		return
	}
	if err := app.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
}
