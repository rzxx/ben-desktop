package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/corebridge"
	"ben/desktop/internal/settings"
)

func TestResolveBlobURLReturnsFileURLWhenBlobExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hashHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	blobPath := filepath.Join(root, "b3", hashHex[:2], hashHex[2:4], hashHex)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatalf("mkdir blob dir: %v", err)
	}
	if err := os.WriteFile(blobPath, []byte("art"), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	service := &PlaybackService{blobRoot: root}
	got, err := service.ResolveBlobURL("b3:" + hashHex)
	if err != nil {
		t.Fatalf("resolve blob url: %v", err)
	}
	want, err := fileURLFromPath(blobPath)
	if err != nil {
		t.Fatalf("file url from path: %v", err)
	}
	if got != want {
		t.Fatalf("blob url = %q, want %q", got, want)
	}
}

func TestResolveBlobURLReturnsEmptyForMissingBlob(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{blobRoot: t.TempDir()}
	got, err := service.ResolveBlobURL("b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("resolve blob url: %v", err)
	}
	if got != "" {
		t.Fatalf("blob url = %q, want empty", got)
	}
}

func TestListAlbumsReturnsBridgeErrorWhenUnavailable(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{
		bridge: corebridge.NewUnavailableBridge(errors.New("core unavailable")),
	}
	_, err := service.ListAlbums(context.Background(), apitypes.AlbumListRequest{})
	if err == nil || err.Error() != "core unavailable" {
		t.Fatalf("list albums error = %v, want core unavailable", err)
	}
}

func TestPreferredProfileDefaultsToSupportedAACProfile(t *testing.T) {
	t.Parallel()

	if got := preferredProfile(settings.CoreRuntimeSettings{}); got != settings.DefaultTranscodeProfile {
		t.Fatalf("preferred profile = %q, want %q", got, settings.DefaultTranscodeProfile)
	}
}

func TestPreferredProfileNormalizesLegacyDesktopProfile(t *testing.T) {
	t.Parallel()

	got := preferredProfile(settings.CoreRuntimeSettings{TranscodeProfile: " desktop "})
	if got != settings.DefaultTranscodeProfile {
		t.Fatalf("preferred profile = %q, want %q", got, settings.DefaultTranscodeProfile)
	}
}
