package corebridge

import (
	"path/filepath"
	"testing"

	"ben/desktop/internal/settings"
)

func TestConfigFromSettingsAllowsMissingDBPath(t *testing.T) {
	t.Setenv("BEN_CORE_DB_PATH", "")
	t.Setenv("BEN_CORE_BLOB_ROOT", "")
	t.Setenv("BEN_CORE_IDENTITY_KEY_PATH", "")
	t.Setenv("BEN_CORE_FFMPEG_PATH", "")
	t.Setenv("BEN_CORE_TRANSCODE_PROFILE", "")

	cfg := configFromSettings(settings.CoreRuntimeSettings{})

	if cfg.Core.DBPath != "" {
		t.Fatalf("expected empty db path, got %q", cfg.Core.DBPath)
	}
	if cfg.Core.BlobRoot != "" {
		t.Fatalf("expected empty blob root, got %q", cfg.Core.BlobRoot)
	}
	if cfg.Core.IdentityKeyPath != "" {
		t.Fatalf("expected empty identity key path, got %q", cfg.Core.IdentityKeyPath)
	}
	if cfg.Core.Runtime.AutoStart == nil || !*cfg.Core.Runtime.AutoStart {
		t.Fatalf("expected auto start to be enabled")
	}
}

func TestConfigFromSettingsDerivesPathsFromDBPath(t *testing.T) {
	dbPath := filepath.Join(`C:\Users\tester\AppData\Roaming\ben\v2`, "library.db")
	t.Setenv("BEN_CORE_DB_PATH", dbPath)
	t.Setenv("BEN_CORE_BLOB_ROOT", "")
	t.Setenv("BEN_CORE_IDENTITY_KEY_PATH", "")

	cfg := configFromSettings(settings.CoreRuntimeSettings{})

	if cfg.Core.DBPath != dbPath {
		t.Fatalf("expected db path %q, got %q", dbPath, cfg.Core.DBPath)
	}
	wantBlobRoot := filepath.Join(filepath.Dir(dbPath), "blobs")
	if cfg.Core.BlobRoot != wantBlobRoot {
		t.Fatalf("expected blob root %q, got %q", wantBlobRoot, cfg.Core.BlobRoot)
	}
	wantIdentityPath := filepath.Join(filepath.Dir(dbPath), "identity.key")
	if cfg.Core.IdentityKeyPath != wantIdentityPath {
		t.Fatalf("expected identity key path %q, got %q", wantIdentityPath, cfg.Core.IdentityKeyPath)
	}
}

func TestConfigFromSettingsPreservesEnvOverrides(t *testing.T) {
	dbPath := filepath.Join(`D:\ben\data`, "library.db")
	blobRoot := filepath.Join(`E:\ben-cache`, "blobs")
	identityKeyPath := filepath.Join(`F:\ben-keys`, "identity.key")
	ffmpegPath := filepath.Join(`C:\tools`, "ffmpeg.exe")
	t.Setenv("BEN_CORE_DB_PATH", dbPath)
	t.Setenv("BEN_CORE_BLOB_ROOT", blobRoot)
	t.Setenv("BEN_CORE_IDENTITY_KEY_PATH", identityKeyPath)
	t.Setenv("BEN_CORE_FFMPEG_PATH", ffmpegPath)
	t.Setenv("BEN_CORE_TRANSCODE_PROFILE", "desktop")

	cfg := configFromSettings(settings.CoreRuntimeSettings{})

	if cfg.Core.DBPath != dbPath {
		t.Fatalf("expected db path %q, got %q", dbPath, cfg.Core.DBPath)
	}
	if cfg.Core.BlobRoot != blobRoot {
		t.Fatalf("expected blob root %q, got %q", blobRoot, cfg.Core.BlobRoot)
	}
	if cfg.Core.IdentityKeyPath != identityKeyPath {
		t.Fatalf("expected identity key path %q, got %q", identityKeyPath, cfg.Core.IdentityKeyPath)
	}
	if cfg.Core.FFmpegPath != ffmpegPath {
		t.Fatalf("expected ffmpeg path %q, got %q", ffmpegPath, cfg.Core.FFmpegPath)
	}
	if cfg.Core.TranscodeProfile != "desktop" {
		t.Fatalf("expected transcode profile desktop, got %q", cfg.Core.TranscodeProfile)
	}
}

func TestConfigFromSettingsUsesStoredValuesWhenEnvMissing(t *testing.T) {
	t.Setenv("BEN_CORE_DB_PATH", "")
	t.Setenv("BEN_CORE_BLOB_ROOT", "")
	t.Setenv("BEN_CORE_IDENTITY_KEY_PATH", "")
	t.Setenv("BEN_CORE_FFMPEG_PATH", "")
	t.Setenv("BEN_CORE_TRANSCODE_PROFILE", "")

	stored := settings.CoreRuntimeSettings{
		DBPath:           filepath.Join(`D:\ben\data`, "library.db"),
		BlobRoot:         filepath.Join(`D:\ben\data`, "blobs"),
		IdentityKeyPath:  filepath.Join(`D:\ben\data`, "identity.key"),
		FFmpegPath:       filepath.Join(`C:\tools`, "ffmpeg.exe"),
		TranscodeProfile: "desktop",
	}

	cfg := configFromSettings(stored)

	if cfg.Core.DBPath != stored.DBPath {
		t.Fatalf("expected db path %q, got %q", stored.DBPath, cfg.Core.DBPath)
	}
	if cfg.Core.BlobRoot != stored.BlobRoot {
		t.Fatalf("expected blob root %q, got %q", stored.BlobRoot, cfg.Core.BlobRoot)
	}
	if cfg.Core.IdentityKeyPath != stored.IdentityKeyPath {
		t.Fatalf("expected identity key path %q, got %q", stored.IdentityKeyPath, cfg.Core.IdentityKeyPath)
	}
	if cfg.Core.FFmpegPath != stored.FFmpegPath {
		t.Fatalf("expected ffmpeg path %q, got %q", stored.FFmpegPath, cfg.Core.FFmpegPath)
	}
	if cfg.Core.TranscodeProfile != stored.TranscodeProfile {
		t.Fatalf("expected transcode profile %q, got %q", stored.TranscodeProfile, cfg.Core.TranscodeProfile)
	}
}
