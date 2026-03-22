package apitypes

import (
	"context"
	"time"
)

type PlaylistKind string

const (
	PlaylistKindNormal PlaylistKind = "normal"
	PlaylistKindLiked  PlaylistKind = "liked"
)

type PlaylistRecord struct {
	LibraryID      string
	PlaylistID     string
	Name           string
	Kind           PlaylistKind
	IsReserved     bool
	Thumb          ArtworkRef
	HasCustomCover bool
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PlaylistItemRecord struct {
	LibraryID          string
	PlaylistID         string
	ItemID             string
	LibraryRecordingID string
	RecordingID        string
	AddedAt            time.Time
	UpdatedAt          time.Time
}

type PlaylistAddItemRequest struct {
	PlaylistID         string
	LibraryRecordingID string
	RecordingID        string
	AfterItemID        string
	BeforeItemID       string
}

type PlaylistMoveItemRequest struct {
	PlaylistID   string
	ItemID       string
	AfterItemID  string
	BeforeItemID string
}

type PlaylistCoverVariant struct {
	Variant string
	BlobID  string
	MIME    string
	FileExt string
	W       int
	H       int
	Bytes   int64
}

type PlaylistCoverRecord struct {
	PlaylistID     string
	HasCustomCover bool
	Thumb          ArtworkRef
	UpdatedAt      time.Time
	Variants       []PlaylistCoverVariant
}

type PlaylistCoverUploadRequest struct {
	PlaylistID string
	Bytes      []byte
	MIME       string
}

type PlaylistSurface interface {
	CreatePlaylist(ctx context.Context, name, kind string) (PlaylistRecord, error)
	RenamePlaylist(ctx context.Context, playlistID, name string) (PlaylistRecord, error)
	DeletePlaylist(ctx context.Context, playlistID string) error
	AddPlaylistItem(ctx context.Context, req PlaylistAddItemRequest) (PlaylistItemRecord, error)
	MovePlaylistItem(ctx context.Context, req PlaylistMoveItemRequest) (PlaylistItemRecord, error)
	RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error
	GetPlaylistCover(ctx context.Context, playlistID string) (PlaylistCoverRecord, bool, error)
	SetPlaylistCover(ctx context.Context, req PlaylistCoverUploadRequest) (PlaylistCoverRecord, error)
	ClearPlaylistCover(ctx context.Context, playlistID string) error
	LikeRecording(ctx context.Context, libraryRecordingID string) error
	UnlikeRecording(ctx context.Context, libraryRecordingID string) error
	IsRecordingLiked(ctx context.Context, libraryRecordingID string) (bool, error)
}
