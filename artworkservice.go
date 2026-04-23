package main

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	apitypes "ben/desktop/api/types"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const artworkServiceRoute = "/artwork/"

type ArtworkHTTPService struct {
	host *coreHost
}

func NewArtworkHTTPService(host *coreHost) *ArtworkHTTPService {
	return &ArtworkHTTPService{host: host}
}

func (s *ArtworkHTTPService) ServiceName() string { return "ArtworkHTTPService" }

func (s *ArtworkHTTPService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.host == nil {
		return nil
	}
	return s.host.Start(ctx)
}

func (s *ArtworkHTTPService) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if s.host == nil {
		http.Error(rw, "artwork service unavailable", http.StatusServiceUnavailable)
		return
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		rw.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	artwork := apitypes.ArtworkRef{
		BlobID:  strings.TrimSpace(req.URL.Query().Get("blob_id")),
		MIME:    strings.TrimSpace(req.URL.Query().Get("mime")),
		FileExt: strings.TrimSpace(req.URL.Query().Get("ext")),
		Variant: strings.TrimSpace(req.URL.Query().Get("variant")),
	}
	if strings.TrimSpace(artwork.BlobID) == "" {
		http.NotFound(rw, req)
		return
	}

	resolved, err := s.host.PlaybackRuntime().ResolveArtworkRef(req.Context(), artwork)
	if err != nil {
		http.Error(rw, "failed to resolve artwork", http.StatusInternalServerError)
		return
	}
	if !resolved.Available || strings.TrimSpace(resolved.LocalPath) == "" {
		http.NotFound(rw, req)
		return
	}
	if _, err := os.Stat(resolved.LocalPath); err != nil {
		if os.IsNotExist(err) {
			http.NotFound(rw, req)
			return
		}
		http.Error(rw, "failed to read artwork", http.StatusInternalServerError)
		return
	}

	contentType := strings.TrimSpace(resolved.Artwork.MIME)
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(resolved.LocalPath))
	}
	if contentType != "" {
		rw.Header().Set("Content-Type", contentType)
	}
	rw.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(rw, req, resolved.LocalPath)
}

func artworkAssetURL(artwork apitypes.ArtworkRef) string {
	artwork.BlobID = strings.TrimSpace(artwork.BlobID)
	if artwork.BlobID == "" {
		return ""
	}

	values := url.Values{}
	values.Set("blob_id", artwork.BlobID)
	if ext := strings.TrimSpace(artwork.FileExt); ext != "" {
		values.Set("ext", ext)
	}
	if mimeType := strings.TrimSpace(artwork.MIME); mimeType != "" {
		values.Set("mime", mimeType)
	}
	if variant := strings.TrimSpace(artwork.Variant); variant != "" {
		values.Set("variant", variant)
	}
	return artworkServiceRoute + "?" + values.Encode()
}
