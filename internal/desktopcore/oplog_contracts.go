package desktopcore

import "strings"

const (
	entityTypeLibrary      = "library"
	entityTypeScanRoots    = "scan_roots"
	entityTypeSourceFile   = "source_file"
	entityTypePlaylist     = "playlist"
	entityTypePlaylistItem = "playlist_item"
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
	DeviceID     string `json:"deviceId"`
	SourceFileID string `json:"sourceFileId"`
	LibraryID    string `json:"libraryId"`
	LocalPath    string `json:"localPath"`
	MTimeNS      int64  `json:"mTimeNs"`
	SizeBytes    int64  `json:"sizeBytes"`
	HashAlgo     string `json:"hashAlgo"`
	HashHex      string `json:"hashHex"`
	Tags         Tags   `json:"tags"`
	IsPresent    bool   `json:"isPresent"`
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
