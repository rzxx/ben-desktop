package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
)

func TestArtworkHTTPServiceServesResolvedArtwork(t *testing.T) {
	t.Parallel()

	blobPath := filepath.Join(t.TempDir(), "cover.webp")
	if err := os.WriteFile(blobPath, []byte("thumb"), 0o644); err != nil {
		t.Fatalf("write artwork: %v", err)
	}

	host := newPassthroughHost(&passthroughRuntimeStub{
		UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
		resolveArtworkRefFn: func(_ context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
			if artwork.BlobID != "b3:"+strings.Repeat("a", 64) {
				t.Fatalf("unexpected blob id: %q", artwork.BlobID)
			}
			return apitypes.ArtworkResolveResult{
				Artwork: apitypes.ArtworkRef{
					BlobID:  artwork.BlobID,
					MIME:    "image/webp",
					FileExt: ".webp",
				},
				LocalPath: blobPath,
				Available: true,
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, artworkServiceRoute+"?blob_id=b3%3A"+strings.Repeat("a", 64)+"&ext=.webp&mime=image%2Fwebp", nil)
	rec := httptest.NewRecorder()

	NewArtworkHTTPService(host).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("content type = %q, want %q", got, "image/webp")
	}
	if got := rec.Body.String(); got != "thumb" {
		t.Fatalf("body = %q, want %q", got, "thumb")
	}
}

func TestArtworkHTTPServiceReturnsNotFoundWhenArtworkUnavailable(t *testing.T) {
	t.Parallel()

	host := newPassthroughHost(&passthroughRuntimeStub{
		UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
		resolveArtworkRefFn: func(_ context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
			return apitypes.ArtworkResolveResult{Artwork: artwork}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, artworkServiceRoute+"?blob_id=b3%3A"+strings.Repeat("b", 64)+"&ext=.webp", nil)
	rec := httptest.NewRecorder()

	NewArtworkHTTPService(host).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
