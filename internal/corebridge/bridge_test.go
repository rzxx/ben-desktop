package corebridge

import (
	"path/filepath"
	"testing"

	"ben/desktop/internal/settings"
)

func TestConfigFromSettingsAllowsMissingDBPath(t *testing.T) {
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

func TestConfigFromSettingsDerivesPathsFromStoredDBPath(t *testing.T) {
	dbPath := filepath.Join(`C:\Users\tester\AppData\Roaming\ben\v2`, "library.db")

	cfg := configFromSettings(settings.CoreRuntimeSettings{DBPath: dbPath})

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

func TestConfigFromSettingsUsesStoredValues(t *testing.T) {
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
