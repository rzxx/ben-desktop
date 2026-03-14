package desktopcore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"github.com/google/uuid"
)

const (
	artistSeparator              = "|||"
	localSettingCurrentDevice    = "current_device_id"
	localSettingPathPrivacyEpoch = "path_privacy_epoch"
	playlistKindNormal           = "normal"
	playlistKindLiked            = "liked"
	roleOwner                    = "owner"
	roleAdmin                    = "admin"
	roleMember                   = "member"
	roleGuest                    = "guest"
	maxPageLimit                 = 1000
	availabilityOnlineWindow     = 2 * time.Minute
	defaultLibraryName           = "ben library"
	defaultArtworkVariant320     = "320_webp"
	defaultArtworkVariant96      = "96_jpeg"
	defaultArtworkVariant1024    = "1024_avif"
)

func splitArtists(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, artistSeparator)
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case roleOwner, roleAdmin, roleMember, roleGuest:
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return roleMember
	}
}

func canManageLibrary(role string) bool {
	switch normalizeRole(role) {
	case roleOwner, roleAdmin:
		return true
	default:
		return false
	}
}

func canProvideLocalMedia(role string) bool {
	switch normalizeRole(role) {
	case roleOwner, roleAdmin, roleMember:
		return true
	default:
		return false
	}
}

func stableNameID(kind, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind+":"+strings.TrimSpace(key))).String()
}

func normalizeArtworkFileExt(fileExt string, mimeType string) string {
	fileExt = strings.TrimSpace(strings.ToLower(fileExt))
	if fileExt != "" {
		if !strings.HasPrefix(fileExt, ".") {
			fileExt = "." + fileExt
		}
		switch fileExt {
		case ".jpeg", ".jpe":
			return ".jpg"
		default:
			return fileExt
		}
	}

	switch strings.TrimSpace(strings.ToLower(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	}

	extensions, _ := mime.ExtensionsByType(strings.TrimSpace(strings.ToLower(mimeType)))
	for _, ext := range extensions {
		if normalized := normalizeArtworkFileExt(ext, ""); normalized != "" {
			return normalized
		}
	}
	return ""
}

func compactNonEmptyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func pageItems[T any](items []T, req apitypes.PageRequest) ([]T, apitypes.PageInfo) {
	limit := req.Limit
	if limit <= 0 || limit > maxPageLimit {
		limit = 100
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	total := len(items)
	if offset >= total {
		return []T{}, apitypes.PageInfo{
			Limit:      limit,
			Offset:     offset,
			Returned:   0,
			Total:      total,
			HasMore:    false,
			NextOffset: offset,
		}
	}

	end := offset + limit
	if end > total {
		end = total
	}
	paged := append([]T(nil), items[offset:end]...)
	return paged, apitypes.PageInfo{
		Limit:      limit,
		Offset:     offset,
		Returned:   len(paged),
		Total:      total,
		HasMore:    end < total,
		NextOffset: end,
	}
}

func paginateItems[T any](items []T, req apitypes.PageRequest) apitypes.Page[T] {
	paged, info := pageItems(items, req)
	return apitypes.Page[T]{Items: paged, Page: info}
}

func fileURIFromPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	urlPath := filepath.ToSlash(absPath)
	if volume := filepath.VolumeName(absPath); volume != "" && !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	return (&url.URL{
		Scheme: "file",
		Path:   urlPath,
	}).String(), nil
}

func likedPlaylistIDForLibrary(libraryID string) string {
	sum := sha256.Sum256([]byte("liked:" + strings.TrimSpace(libraryID)))
	return "liked-" + hex.EncodeToString(sum[:8])
}
