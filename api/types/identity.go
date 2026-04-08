package apitypes

import (
	"context"
	"time"
)

type LocalContext struct {
	LibraryID string
	DeviceID  string
	Device    string
	Role      string
	PeerID    string
}

type InspectSummary struct {
	Libraries            int64
	Devices              int64
	Memberships          int64
	Artists              int64
	Credits              int64
	Albums               int64
	Recordings           int64
	DeviceVariantPrefs   int64
	Content              int64
	DeviceContentPresent int64
	Encodings            int64
	DeviceEncodings      int64
	AlbumTracks          int64
	ArtworkVariants      int64
	Playlists            int64
	PlaylistItems        int64
	OplogEntries         int64
	DeviceClocks         int64
}

type OplogDiagnosticsGroup struct {
	Key   string
	Count int64
}

type OplogRecencyBucket struct {
	Bucket string
	Count  int64
}

type LibraryMaterializedCounts struct {
	Artists            int64
	Credits            int64
	Albums             int64
	Recordings         int64
	Contents           int64
	DeviceContentCount int64
	AlbumTracks        int64
	Encodings          int64
	DeviceEncodings    int64
	ArtworkVariants    int64
	Playlists          int64
	PlaylistItems      int64
	OplogEntries       int64
	DeviceClocks       int64
}

type TranscodeOplogDiagnostics struct {
	OplogEncodings       int64
	OplogDeviceEncodings int64
	Encodings            int64
	DeviceEncodings      int64
	ArtworkVariants      int64
}

type LibraryOplogDiagnostics struct {
	LibraryID         string
	GeneratedAt       time.Time
	Maintenance       ScanMaintenanceStatus
	OplogByEntityType []OplogDiagnosticsGroup
	OplogByDeviceID   []OplogDiagnosticsGroup
	OplogByRecency    []OplogRecencyBucket
	Materialized      LibraryMaterializedCounts
	Transcode         TranscodeOplogDiagnostics
}

type IdentitySurface interface {
	EnsureLocalContext(ctx context.Context) (LocalContext, error)
}

// IdentityDiagnosticsSurface exposes inspection helpers intended for
// diagnostics and maintenance tooling.
type IdentityDiagnosticsSurface interface {
	Inspect(ctx context.Context) (InspectSummary, error)
	InspectLibraryOplog(ctx context.Context, libraryID string) (LibraryOplogDiagnostics, error)
}
