package apitypes

import (
	"context"
	"time"
)

type ActivityStatus struct {
	Scan        ScanActivityStatus
	Maintenance ScanMaintenanceStatus
	Artwork     ArtworkActivityStatus
	Transcodes  []TranscodeActivityStatus
	UpdatedAt   time.Time
}

type ScanMaintenanceStatus struct {
	RepairRequired bool
	Reason         string
	Detail         string
	UpdatedAt      time.Time
}

type ScanActivityStatus struct {
	Phase         string
	RootsTotal    int
	RootsDone     int
	TracksTotal   int
	TracksDone    int
	CurrentRoot   string
	CurrentPath   string
	Workers       int
	WorkersActive int
	Errors        int
	UpdatedAt     time.Time
}

type ArtworkActivityStatus struct {
	Phase             string
	AlbumsTotal       int
	AlbumsDone        int
	CurrentAlbumID    string
	CurrentSourcePath string
	Workers           int
	WorkersActive     int
	Errors            int
	UpdatedAt         time.Time
}

type TranscodeActivityStatus struct {
	RecordingID       string
	SourceFileID      string
	SourcePath        string
	Profile           string
	RequestKind       string
	RequesterDeviceID string
	Phase             string
	StartedAt         time.Time
}

type ActivitySurface interface {
	ActivityStatus(ctx context.Context) (ActivityStatus, error)
}
