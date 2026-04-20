package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ben/desktop/internal/desktopcore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

type inspectCLIFixture struct {
	root               string
	dbPath             string
	blobRoot           string
	libraryID          string
	deviceID           string
	recordingVariantID string
	recordingClusterID string
	albumID            string
	blobID             string
}

func TestCLIJSONGoldens(t *testing.T) {
	fixture := createInspectCLIFixture(t)
	baseArgs := []string{
		"--db", fixture.dbPath,
		"--blob-root", fixture.blobRoot,
		"--library-id", fixture.libraryID,
		"--device-id", fixture.deviceID,
		"--profile", "desktop",
	}

	testCases := []struct {
		name string
		path string
		args []string
	}{
		{
			name: "trace-recording",
			path: filepath.Join("testdata", "trace-recording.golden.json"),
			args: append([]string{"music", "trace-recording"}, append(baseArgs, "--id", fixture.recordingVariantID)...),
		},
		{
			name: "trace-album",
			path: filepath.Join("testdata", "trace-album.golden.json"),
			args: append([]string{"music", "trace-album"}, append(baseArgs, "--id", fixture.albumID)...),
		},
		{
			name: "trace-context",
			path: filepath.Join("testdata", "trace-context.golden.json"),
			args: append([]string{"music", "trace-context"}, append(baseArgs, "--kind", "album", "--id", fixture.albumID)...),
		},
		{
			name: "trace-cache",
			path: filepath.Join("testdata", "trace-cache.golden.json"),
			args: append([]string{"cache", "trace-recording"}, append(baseArgs, "--id", fixture.recordingClusterID)...),
		},
		{
			name: "health-check",
			path: filepath.Join("testdata", "health-check.golden.json"),
			args: append([]string{"music", "health-check"}, append(baseArgs, "--date", "2026-01-02", "--limit", "2", "--decode=false")...),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeCLIJSON(t, fixture, captureRunOutput(t, tc.args))
			assertGolden(t, tc.path, got)
		})
	}
}

func captureRunOutput(t *testing.T, args []string) []byte {
	t.Helper()

	oldStdout := os.Stdout
	tempFile, err := os.CreateTemp(t.TempDir(), "beninspect-stdout-*.json")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	os.Stdout = tempFile
	defer func() {
		os.Stdout = oldStdout
	}()

	exitCode := run(args)
	if err := tempFile.Close(); err != nil {
		t.Fatalf("close stdout temp file: %v", err)
	}
	output, err := os.ReadFile(tempFile.Name())
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	if exitCode != 0 {
		t.Fatalf("run(%v) exit=%d output=%s", args, exitCode, string(output))
	}
	return output
}

func normalizeCLIJSON(t *testing.T, fixture inspectCLIFixture, raw []byte) []byte {
	t.Helper()

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal cli json: %v\n%s", err, string(raw))
	}
	normalized := normalizeJSONValue(payload, fixture)
	out, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		t.Fatalf("marshal normalized json: %v", err)
	}
	return append(out, '\n')
}

func normalizeJSONValue(value any, fixture inspectCLIFixture) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = normalizeJSONValue(child, fixture)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, normalizeJSONValue(child, fixture))
		}
		return out
	case string:
		replacer := strings.NewReplacer(
			fixture.root, "<TEST_ROOT>",
			filepath.ToSlash(fixture.root), "<TEST_ROOT>",
		)
		return replacer.Replace(typed)
	default:
		return value
	}
}

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	got = normalizeGoldenNewlines(got)
	want = normalizeGoldenNewlines(want)
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, string(got), string(want))
	}
}

func normalizeGoldenNewlines(in []byte) []byte {
	return bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n"))
}

func createInspectCLIFixture(t *testing.T) inspectCLIFixture {
	t.Helper()

	root := t.TempDir()
	cfg := desktopcore.Config{
		DBPath:          filepath.Join(root, "library.db"),
		BlobRoot:        filepath.Join(root, "blobs"),
		IdentityKeyPath: filepath.Join(root, "identity.key"),
		CacheBytes:      1024 * 1024,
	}
	app, err := desktopcore.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open app for fixture schema: %v", err)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("close app after schema init: %v", err)
	}

	db := openFixtureSQLite(t, cfg.DBPath)
	defer closeFixtureSQLite(t, db)

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fixture := inspectCLIFixture{
		root:               root,
		dbPath:             cfg.DBPath,
		blobRoot:           cfg.BlobRoot,
		libraryID:          "library-inspect",
		deviceID:           "device-inspect",
		recordingVariantID: "recording-alpha",
		recordingClusterID: "cluster-shared",
		albumID:            "album-alpha",
		blobID:             "b3:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	mustCreateRows(t, db,
		&desktopcore.Library{
			LibraryID: fixture.libraryID,
			Name:      "Inspect Library",
			CreatedAt: now,
		},
		&desktopcore.Device{
			DeviceID:        fixture.deviceID,
			Name:            "Inspect Device",
			PeerID:          "peer-inspect",
			ActiveLibraryID: ptr(fixture.libraryID),
			JoinedAt:        now,
			LastSeenAt:      ptr(now),
		},
		&desktopcore.Membership{
			LibraryID:        fixture.libraryID,
			DeviceID:         fixture.deviceID,
			Role:             "owner",
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		},
		&desktopcore.AlbumVariantModel{
			LibraryID:      fixture.libraryID,
			AlbumVariantID: "album-alpha",
			AlbumClusterID: "album-cluster-alpha",
			KeyNorm:        "album-alpha",
			Title:          "Album Alpha",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		&desktopcore.AlbumVariantModel{
			LibraryID:      fixture.libraryID,
			AlbumVariantID: "album-beta",
			AlbumClusterID: "album-cluster-beta",
			KeyNorm:        "album-beta",
			Title:          "Album Beta",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		&desktopcore.TrackVariantModel{
			LibraryID:      fixture.libraryID,
			TrackVariantID: "recording-alpha",
			TrackClusterID: "cluster-shared",
			KeyNorm:        "opening",
			Title:          "Opening",
			DurationMS:     180000,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		&desktopcore.TrackVariantModel{
			LibraryID:      fixture.libraryID,
			TrackVariantID: "recording-alpha-bonus",
			TrackClusterID: "cluster-alpha-bonus",
			KeyNorm:        "bonus",
			Title:          "Bonus",
			DurationMS:     200000,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		&desktopcore.TrackVariantModel{
			LibraryID:      fixture.libraryID,
			TrackVariantID: "recording-beta",
			TrackClusterID: "cluster-shared",
			KeyNorm:        "opening",
			Title:          "Opening",
			DurationMS:     180000,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		&desktopcore.AlbumTrack{
			LibraryID:      fixture.libraryID,
			AlbumVariantID: "album-alpha",
			TrackVariantID: "recording-alpha",
			DiscNo:         1,
			TrackNo:        1,
		},
		&desktopcore.AlbumTrack{
			LibraryID:      fixture.libraryID,
			AlbumVariantID: "album-alpha",
			TrackVariantID: "recording-alpha-bonus",
			DiscNo:         1,
			TrackNo:        2,
		},
		&desktopcore.AlbumTrack{
			LibraryID:      fixture.libraryID,
			AlbumVariantID: "album-beta",
			TrackVariantID: "recording-beta",
			DiscNo:         1,
			TrackNo:        1,
		},
		&desktopcore.DeviceVariantPreference{
			LibraryID:       fixture.libraryID,
			DeviceID:        fixture.deviceID,
			ScopeType:       "track",
			ClusterID:       "cluster-shared",
			ChosenVariantID: "recording-beta",
			UpdatedAt:       now,
		},
	)

	createFixtureSourceFile(t, root, "media", "recording-alpha.flac", []byte("recording-alpha"))
	createFixtureSourceFile(t, root, "media", "recording-alpha-bonus.flac", []byte("recording-alpha-bonus"))
	createFixtureSourceFile(t, root, "media", "recording-beta.flac", []byte("recording-beta"))

	alphaPath := filepath.Join(root, "media", "recording-alpha.flac")
	alphaBonusPath := filepath.Join(root, "media", "recording-alpha-bonus.flac")
	betaPath := filepath.Join(root, "media", "recording-beta.flac")

	mustCreateRows(t, db,
		&desktopcore.SourceFileModel{
			LibraryID:         fixture.libraryID,
			DeviceID:          fixture.deviceID,
			SourceFileID:      "source-alpha",
			TrackVariantID:    "recording-alpha",
			LocalPath:         alphaPath,
			PathKey:           alphaPath,
			SourceFingerprint: "source-alpha-fp",
			HashAlgo:          "b3",
			HashHex:           strings.TrimPrefix(fixture.blobID, "b3:"),
			MTimeNS:           now.UnixNano(),
			SizeBytes:         2048,
			Container:         "flac",
			Codec:             "flac",
			Bitrate:           1400000,
			SampleRate:        44100,
			Channels:          2,
			IsLossless:        true,
			QualityRank:       220,
			DurationMS:        180000,
			TagsJSON:          "{}",
			LastSeenAt:        now,
			IsPresent:         true,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		&desktopcore.SourceFileModel{
			LibraryID:         fixture.libraryID,
			DeviceID:          fixture.deviceID,
			SourceFileID:      "source-alpha-bonus",
			TrackVariantID:    "recording-alpha-bonus",
			LocalPath:         alphaBonusPath,
			PathKey:           alphaBonusPath,
			SourceFingerprint: "source-alpha-bonus-fp",
			HashAlgo:          "b3",
			HashHex:           strings.TrimPrefix(fixture.blobID, "b3:"),
			MTimeNS:           now.UnixNano(),
			SizeBytes:         1024,
			Container:         "flac",
			Codec:             "flac",
			Bitrate:           900000,
			SampleRate:        44100,
			Channels:          2,
			IsLossless:        true,
			QualityRank:       180,
			DurationMS:        200000,
			TagsJSON:          "{}",
			LastSeenAt:        now,
			IsPresent:         true,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		&desktopcore.SourceFileModel{
			LibraryID:         fixture.libraryID,
			DeviceID:          fixture.deviceID,
			SourceFileID:      "source-beta",
			TrackVariantID:    "recording-beta",
			LocalPath:         betaPath,
			PathKey:           betaPath,
			SourceFingerprint: "source-beta-fp",
			HashAlgo:          "b3",
			HashHex:           strings.TrimPrefix(fixture.blobID, "b3:"),
			MTimeNS:           now.UnixNano(),
			SizeBytes:         1800,
			Container:         "flac",
			Codec:             "flac",
			Bitrate:           700000,
			SampleRate:        44100,
			Channels:          2,
			IsLossless:        true,
			QualityRank:       120,
			DurationMS:        180000,
			TagsJSON:          "{}",
			LastSeenAt:        now,
			IsPresent:         true,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		&desktopcore.OptimizedAssetModel{
			LibraryID:         fixture.libraryID,
			OptimizedAssetID:  "asset-alpha",
			SourceFileID:      "source-alpha",
			TrackVariantID:    "recording-alpha",
			Profile:           "desktop",
			BlobID:            fixture.blobID,
			MIME:              "audio/mp4",
			DurationMS:        180000,
			Bitrate:           128000,
			Codec:             "aac",
			Container:         "m4a",
			CreatedByDeviceID: fixture.deviceID,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		&desktopcore.OptimizedAssetModel{
			LibraryID:         fixture.libraryID,
			OptimizedAssetID:  "asset-beta",
			SourceFileID:      "source-beta",
			TrackVariantID:    "recording-beta",
			Profile:           "desktop",
			BlobID:            fixture.blobID,
			MIME:              "audio/mp4",
			DurationMS:        180000,
			Bitrate:           96000,
			Codec:             "aac",
			Container:         "m4a",
			CreatedByDeviceID: fixture.deviceID,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		&desktopcore.DeviceAssetCacheModel{
			LibraryID:        fixture.libraryID,
			DeviceID:         fixture.deviceID,
			OptimizedAssetID: "asset-alpha",
			IsCached:         true,
			LastVerifiedAt:   ptr(now),
			UpdatedAt:        now,
		},
		&desktopcore.DeviceAssetCacheModel{
			LibraryID:        fixture.libraryID,
			DeviceID:         fixture.deviceID,
			OptimizedAssetID: "asset-beta",
			IsCached:         true,
			LastVerifiedAt:   ptr(now),
			UpdatedAt:        now,
		},
	)

	blobStore := desktopcore.NewBlobStoreService(cfg.BlobRoot)
	blobPath, err := blobStore.Path(fixture.blobID)
	if err != nil {
		t.Fatalf("resolve blob path: %v", err)
	}
	createFixtureSourceFile(t, filepath.Dir(blobPath), "", filepath.Base(blobPath), bytes.Repeat([]byte("a"), 64))

	return fixture
}

func openFixtureSQLite(t *testing.T, path string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Dialector{DriverName: "sqlite", DSN: path}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open fixture sqlite: %v", err)
	}
	return db
}

func closeFixtureSQLite(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
}

func createFixtureSourceFile(t *testing.T, root, dir, name string, data []byte) {
	t.Helper()

	path := filepath.Join(root, dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture file dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func mustCreateRows(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()

	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create row %T: %v", row, err)
		}
	}
}

func ptr[T any](value T) *T {
	return &value
}
