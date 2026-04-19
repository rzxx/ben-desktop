package playback

import (
	"net/url"
	"path/filepath"
	"strings"
)

func normalizePlaybackURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if parsed, err := url.Parse(value); err == nil && strings.EqualFold(parsed.Scheme, "file") {
		path := parsed.Path
		if decoded, decodeErr := url.PathUnescape(path); decodeErr == nil {
			path = decoded
		}
		if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		return normalizeLocalPlaybackPath(path)
	}

	if looksLikeLocalPlaybackPath(value) {
		return normalizeLocalPlaybackPath(value)
	}

	return value
}

func normalizeLocalPlaybackPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Clean(value)
	value = filepath.ToSlash(value)
	if len(value) >= 2 && value[1] == ':' {
		return strings.ToLower(value)
	}
	return value
}

func looksLikeLocalPlaybackPath(value string) bool {
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return true
	}
	if strings.Contains(value, `\`) {
		return true
	}
	return len(value) >= 2 && value[1] == ':'
}
