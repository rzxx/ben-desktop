package main

import (
	"context"
	"encoding/base64"
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

func TestResolveThumbnailURLReturnsDataURLWhenBlobExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hashHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	blobPath := filepath.Join(root, "b3", hashHex[:2], hashHex[2:4], hashHex)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatalf("mkdir blob dir: %v", err)
	}
	payload := []byte("art")
	if err := os.WriteFile(blobPath, payload, 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	service := &PlaybackService{blobRoot: root}
	got, err := service.ResolveThumbnailURL(apitypes.ArtworkRef{
		BlobID:  "b3:" + hashHex,
		MIME:    "image/webp",
		Variant: "320_webp",
	})
	if err != nil {
		t.Fatalf("resolve thumbnail url: %v", err)
	}
	want := "data:image/webp;base64," + base64.StdEncoding.EncodeToString(payload)
	if got != want {
		t.Fatalf("thumbnail url = %q, want %q", got, want)
	}
}

func TestResolveThumbnailURLReturnsEmptyForMissingBlob(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{blobRoot: t.TempDir()}
	got, err := service.ResolveThumbnailURL(apitypes.ArtworkRef{
		BlobID: "b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MIME:   "image/jpeg",
	})
	if err != nil {
		t.Fatalf("resolve thumbnail url: %v", err)
	}
	if got != "" {
		t.Fatalf("thumbnail url = %q, want empty", got)
	}
}

func TestResolveThumbnailURLRejectsMissingMIME(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{blobRoot: t.TempDir()}
	if _, err := service.ResolveThumbnailURL(apitypes.ArtworkRef{
		BlobID: "b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}); err == nil {
		t.Fatalf("expected missing mime to fail")
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

func TestResolvedBlobRootUsesCoreDefaultsWhenSettingsEmpty(t *testing.T) {
	t.Parallel()

	configRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}

	got := resolvedBlobRoot(settings.CoreRuntimeSettings{})
	want := filepath.Join(configRoot, "ben", "v2", "blobs")
	if got != want {
		t.Fatalf("resolved blob root = %q, want %q", got, want)
	}
}
