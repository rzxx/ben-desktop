package desktopcore

import (
	"path/filepath"
	"strconv"
	"strings"
)

func sourceFileIDForDevicePath(deviceID, path string) string {
	pathKey := localPathKey(path)
	if pathKey == "" {
		return ""
	}
	return stableNameID("source_file_path", strings.TrimSpace(deviceID)+"|"+pathKey)
}

func explicitTrackVariantID(recordingKey, editionScopeKey string, discNo, trackNo int) string {
	return stableNameID(
		"recording_variant",
		strings.Join([]string{
			strings.TrimSpace(recordingKey),
			strings.TrimSpace(editionScopeKey),
			strings.TrimSpace(intKey(maxTrackNumber(discNo))),
			strings.TrimSpace(intKey(maxTrackNumber(trackNo))),
		}, "|"),
	)
}

func explicitAlbumVariantID(albumKey, editionScopeKey string) string {
	return stableNameID("album_variant", strings.TrimSpace(albumKey)+"|"+strings.TrimSpace(editionScopeKey))
}

func editionScopeKeyForPath(root, path string, tags Tags) string {
	root = filepath.Clean(strings.TrimSpace(root))
	path = filepath.Clean(strings.TrimSpace(path))
	relativeDir := ""
	if root != "" && path != "" {
		if rel, err := filepath.Rel(root, filepath.Dir(path)); err == nil {
			relativeDir = normalizeEditionRelativeDir(rel)
		}
	}
	if relativeDir == "" {
		relativeDir = normalizeEditionRelativeDir(filepath.Base(filepath.Dir(path)))
	}
	albumRoot := editionRootDir(relativeDir)
	primaryArtist := firstArtist(tags.Artists)
	return normalizeCatalogKey(strings.Join([]string{
		firstNonEmpty(tags.AlbumArtist, primaryArtist),
		tags.Album,
		albumRoot,
	}, "|"))
}

func normalizeEditionRelativeDir(value string) string {
	value = filepath.Clean(strings.TrimSpace(value))
	switch value {
	case "", ".", string(filepath.Separator):
		return ""
	default:
		return strings.ToLower(filepath.ToSlash(value))
	}
}

func editionRootDir(relativeDir string) string {
	relativeDir = normalizeEditionRelativeDir(relativeDir)
	if relativeDir == "" {
		return ""
	}
	parts := strings.Split(relativeDir, "/")
	if len(parts) >= 2 && isDiscDirectory(parts[len(parts)-1]) {
		return strings.Join(parts[:len(parts)-1], "/")
	}
	return relativeDir
}

func isDiscDirectory(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, prefix := range []string{"cd", "disc", "disk"} {
		if strings.HasPrefix(value, prefix) {
			suffix := strings.TrimSpace(strings.TrimPrefix(value, prefix))
			if suffix == "" {
				return true
			}
			if suffix[0] == '-' || suffix[0] == '_' || suffix[0] == ' ' {
				suffix = strings.TrimSpace(suffix[1:])
			}
			if suffix != "" {
				return true
			}
		}
	}
	return false
}

func intKey(value int) string {
	return strconv.Itoa(value)
}
