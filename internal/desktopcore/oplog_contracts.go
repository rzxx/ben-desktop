package desktopcore

import "strings"

const (
	entityTypeLibrary                 = "library"
	entityTypeScanRoots               = "scan_roots"
	entityTypeSourceFile              = "source_file"
	entityTypePlaylist                = "playlist"
	entityTypePlaylistItem            = "playlist_item"
	entityTypeDeviceVariantPreference = "device_variant_preference"
	entityTypeOfflinePin              = "offline_pin"
	entityTypeOptimizedAsset          = "optimized_asset"
	entityTypeDeviceAssetCache        = "device_asset_cache"
	entityTypeArtworkVariant          = "artwork_variant"
	entityTypePlaylistCover           = "playlist_cover"
)

type libraryOplogPayload struct {
	LibraryID string `json:"libraryId"`
	Name      string `json:"name"`
}

type scanRootsOplogPayload struct {
	DeviceID string   `json:"deviceId"`
	Roots    []string `json:"roots"`
}

type sourceFileOplogPayload struct {
	DeviceID        string `json:"deviceId"`
	SourceFileID    string `json:"sourceFileId"`
	LibraryID       string `json:"libraryId"`
	LocalPath       string `json:"localPath"`
	EditionScopeKey string `json:"editionScopeKey"`
	MTimeNS         int64  `json:"mTimeNs"`
	SizeBytes       int64  `json:"sizeBytes"`
	HashAlgo        string `json:"hashAlgo"`
	HashHex         string `json:"hashHex"`
	Tags            Tags   `json:"tags"`
	IsPresent       bool   `json:"isPresent"`
}

type deviceVariantPreferenceOplogPayload struct {
	DeviceID        string `json:"deviceId"`
	ScopeType       string `json:"scopeType"`
	ClusterID       string `json:"clusterId"`
	ChosenVariantID string `json:"chosenVariantId"`
	UpdatedAtNS     int64  `json:"updatedAtNs"`
}

type offlinePinOplogPayload struct {
	DeviceID    string `json:"deviceId"`
	Scope       string `json:"scope"`
	ScopeID     string `json:"scopeId"`
	Profile     string `json:"profile"`
	UpdatedAtNS int64  `json:"updatedAtNs"`
}

type optimizedAssetOplogPayload struct {
	OptimizedAssetID  string `json:"optimizedAssetId"`
	SourceFileID      string `json:"sourceFileId"`
	TrackVariantID    string `json:"trackVariantId"`
	Profile           string `json:"profile"`
	BlobID            string `json:"blobId"`
	MIME              string `json:"mime"`
	DurationMS        int64  `json:"durationMs"`
	Bitrate           int    `json:"bitrate"`
	Codec             string `json:"codec"`
	Container         string `json:"container"`
	CreatedByDeviceID string `json:"createdByDeviceId"`
	CreatedAtNS       int64  `json:"createdAtNs"`
	UpdatedAtNS       int64  `json:"updatedAtNs"`
}

type optimizedAssetDeleteOplogPayload struct {
	OptimizedAssetID string `json:"optimizedAssetId"`
}

type deviceAssetCacheOplogPayload struct {
	DeviceID          string `json:"deviceId"`
	OptimizedAssetID  string `json:"optimizedAssetId"`
	IsCached          bool   `json:"isCached"`
	LastVerifiedAtNS  int64  `json:"lastVerifiedAtNs"`
	HasLastVerifiedAt bool   `json:"hasLastVerifiedAt"`
	UpdatedAtNS       int64  `json:"updatedAtNs"`
}

type artworkVariantOplogPayload struct {
	ScopeType       string `json:"scopeType"`
	ScopeID         string `json:"scopeId"`
	Variant         string `json:"variant"`
	BlobID          string `json:"blobId"`
	MIME            string `json:"mime"`
	FileExt         string `json:"fileExt"`
	W               int    `json:"w"`
	H               int    `json:"h"`
	Bytes           int64  `json:"bytes"`
	ChosenSource    string `json:"chosenSource"`
	ChosenSourceRef string `json:"chosenSourceRef"`
	UpdatedAtNS     int64  `json:"updatedAtNs"`
}

type artworkVariantDeleteOplogPayload struct {
	ScopeType string `json:"scopeType"`
	ScopeID   string `json:"scopeId"`
	Variant   string `json:"variant"`
}

type playlistCoverOplogPayload struct {
	PlaylistID   string `json:"playlistId"`
	BlobID       string `json:"blobId"`
	MIME         string `json:"mime"`
	FileExt      string `json:"fileExt"`
	W            int    `json:"w"`
	H            int    `json:"h"`
	Bytes        int64  `json:"bytes"`
	ChosenSource string `json:"chosenSource"`
	UpdatedAtNS  int64  `json:"updatedAtNs"`
}

type playlistCoverDeleteOplogPayload struct {
	PlaylistID string `json:"playlistId"`
}

func scanRootsEntityID(deviceID string) string {
	return strings.TrimSpace(deviceID)
}

func sourceFileEntityID(deviceID, sourceFileID string) string {
	deviceID = strings.TrimSpace(deviceID)
	sourceFileID = strings.TrimSpace(sourceFileID)
	if deviceID == "" {
		return sourceFileID
	}
	if sourceFileID == "" {
		return deviceID
	}
	return deviceID + ":" + sourceFileID
}

func deviceVariantPreferenceEntityID(deviceID, scopeType, clusterID string) string {
	deviceID = strings.TrimSpace(deviceID)
	scopeType = strings.TrimSpace(scopeType)
	clusterID = strings.TrimSpace(clusterID)
	if deviceID == "" {
		return scopeType + ":" + clusterID
	}
	if scopeType == "" {
		return deviceID + ":" + clusterID
	}
	return deviceID + ":" + scopeType + ":" + clusterID
}

func offlinePinEntityID(deviceID, scope, scopeID string) string {
	deviceID = strings.TrimSpace(deviceID)
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if deviceID == "" {
		return scope + ":" + scopeID
	}
	if scope == "" {
		return deviceID + ":" + scopeID
	}
	return deviceID + ":" + scope + ":" + scopeID
}

func optimizedAssetEntityID(optimizedAssetID string) string {
	return strings.TrimSpace(optimizedAssetID)
}

func deviceAssetCacheEntityID(deviceID, optimizedAssetID string) string {
	deviceID = strings.TrimSpace(deviceID)
	optimizedAssetID = strings.TrimSpace(optimizedAssetID)
	if deviceID == "" {
		return optimizedAssetID
	}
	if optimizedAssetID == "" {
		return deviceID
	}
	return deviceID + ":" + optimizedAssetID
}

func artworkVariantEntityID(scopeType, scopeID, variant string) string {
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	variant = strings.TrimSpace(variant)
	if scopeType == "" {
		return scopeID + ":" + variant
	}
	if scopeID == "" {
		return scopeType + ":" + variant
	}
	return scopeType + ":" + scopeID + ":" + variant
}

func playlistCoverEntityID(playlistID string) string {
	return strings.TrimSpace(playlistID)
}
