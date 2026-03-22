package playback

import (
	"context"
	"strings"

	apitypes "ben/desktop/api/types"
)

const EventSnapshotChanged = "playback:snapshot"

type RepeatMode string

const (
	RepeatOff RepeatMode = "off"
	RepeatAll RepeatMode = "all"
	RepeatOne RepeatMode = "one"
)

type Status string

const (
	StatusIdle    Status = "idle"
	StatusPaused  Status = "paused"
	StatusPlaying Status = "playing"
	StatusPending Status = "pending"
)

type ContextKind string

const (
	ContextKindAlbum     ContextKind = "album"
	ContextKindPlaylist  ContextKind = "playlist"
	ContextKindLiked     ContextKind = "liked"
	ContextKindRecording ContextKind = "recording"
	ContextKindCustom    ContextKind = "custom"
)

type EntryOrigin string

const (
	EntryOriginContext EntryOrigin = "context"
	EntryOriginQueued  EntryOrigin = "queued"
)

type QueueInsertMode string

const (
	QueueInsertNext QueueInsertMode = "next"
	QueueInsertLast QueueInsertMode = "last"
)

const DefaultVolume = 80

type ResolutionMode string

const (
	ResolutionModeLibrary  ResolutionMode = "library"
	ResolutionModeExplicit ResolutionMode = "explicit"
)

type SessionItem struct {
	LibraryRecordingID string         `json:"libraryRecordingId,omitempty"`
	VariantRecordingID string         `json:"variantRecordingId,omitempty"`
	RecordingID        string         `json:"recordingId"`
	Title              string         `json:"title"`
	Subtitle           string         `json:"subtitle"`
	DurationMS         int64          `json:"durationMs"`
	ArtworkRef         string         `json:"artworkRef"`
	SourceKind         string         `json:"sourceKind"`
	SourceID           string         `json:"sourceId"`
	SourceItemID       string         `json:"sourceItemId"`
	AlbumID            string         `json:"albumId,omitempty"`
	VariantAlbumID     string         `json:"variantAlbumId,omitempty"`
	ResolutionMode     ResolutionMode `json:"resolutionMode,omitempty"`
}

type SessionEntry struct {
	EntryID      string      `json:"entryId"`
	Origin       EntryOrigin `json:"origin"`
	ContextIndex int         `json:"contextIndex,omitempty"`
	Item         SessionItem `json:"item"`
}

type PlaybackContext struct {
	Kind    ContextKind    `json:"kind"`
	ID      string         `json:"id"`
	Title   string         `json:"title,omitempty"`
	Entries []SessionEntry `json:"entries"`
}

type HistoryEntry struct {
	Entry    SessionEntry `json:"entry"`
	PlayedAt string       `json:"playedAt"`
}

type SessionSnapshot struct {
	Context             *PlaybackContext            `json:"context,omitempty"`
	QueuedEntries       []SessionEntry              `json:"queuedEntries"`
	History             []HistoryEntry              `json:"history"`
	CurrentEntryID      string                      `json:"currentEntryId,omitempty"`
	CurrentEntry        *SessionEntry               `json:"currentEntry,omitempty"`
	CurrentItem         *SessionItem                `json:"currentItem,omitempty"`
	LoadingEntry        *SessionEntry               `json:"loadingEntry,omitempty"`
	LoadingItem         *SessionItem                `json:"loadingItem,omitempty"`
	UpcomingEntries     []SessionEntry              `json:"upcomingEntries"`
	CurrentOrigin       EntryOrigin                 `json:"currentOrigin,omitempty"`
	CurrentContextIndex int                         `json:"currentContextIndex"`
	ResumeContextIndex  int                         `json:"resumeContextIndex"`
	ShuffleCycle        []int                       `json:"shuffleCycle,omitempty"`
	RepeatMode          RepeatMode                  `json:"repeatMode"`
	Shuffle             bool                        `json:"shuffle"`
	Volume              int                         `json:"volume"`
	Status              Status                      `json:"status"`
	PositionMS          int64                       `json:"positionMs"`
	DurationMS          *int64                      `json:"durationMs,omitempty"`
	UpdatedAt           string                      `json:"updatedAt"`
	LastError           string                      `json:"lastError,omitempty"`
	CurrentSourceKind   apitypes.PlaybackSourceKind `json:"currentSourceKind,omitempty"`
	CurrentPreparation  *EntryPreparation           `json:"currentPreparation,omitempty"`
	LoadingPreparation  *EntryPreparation           `json:"loadingPreparation,omitempty"`
	NextPreparation     *EntryPreparation           `json:"nextPreparation,omitempty"`
	QueueLength         int                         `json:"queueLength"`
	NextEntrySeq        int64                       `json:"nextEntrySeq,omitempty"`
}

type EntryPreparation struct {
	EntryID string                             `json:"entryId"`
	Status  apitypes.PlaybackPreparationStatus `json:"status"`
}

type PlaybackCore interface {
	Close() error
	ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error)
	GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error)
	ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error)
	ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error)
	ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error)
	InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error)
	GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error)
	ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error)
	ResolveAlbumArtwork(ctx context.Context, albumID, variant string) (apitypes.RecordingArtworkResult, error)
	ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error)
	GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error)
}

type SessionStore interface {
	Load(ctx context.Context) (SessionSnapshot, error)
	Save(ctx context.Context, snapshot SessionSnapshot) error
	Clear(ctx context.Context) error
}

type Backend interface {
	Load(ctx context.Context, uri string) error
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	Stop(ctx context.Context) error
	SeekTo(ctx context.Context, positionMS int64) error
	SetVolume(ctx context.Context, volume int) error
	PositionMS() (int64, error)
	DurationMS() (*int64, error)
	Events() <-chan BackendEvent
	SupportsPreload() bool
	PreloadNext(ctx context.Context, uri string) error
	ClearPreloaded(ctx context.Context) error
	Close() error
}

type PlatformController interface {
	Start() error
	Stop() error
	HandlePlaybackSnapshot(snapshot SessionSnapshot)
}

type BackendEventType string

const (
	BackendEventTrackEnd BackendEventType = "track_end"
	BackendEventShutdown BackendEventType = "shutdown"
	BackendEventError    BackendEventType = "error"
)

const (
	TrackEndReasonEOF      = "eof"
	TrackEndReasonStop     = "stop"
	TrackEndReasonQuit     = "quit"
	TrackEndReasonError    = "error"
	TrackEndReasonRedirect = "redirect"
)

type BackendEvent struct {
	Type   BackendEventType
	Reason string
	Err    error
}

type PlaybackContextInput struct {
	Kind       ContextKind
	ID         string
	Title      string
	Items      []SessionItem
	StartIndex int
}

func ParseRepeatMode(value string) (RepeatMode, bool) {
	switch RepeatMode(strings.ToLower(strings.TrimSpace(value))) {
	case RepeatOff:
		return RepeatOff, true
	case RepeatAll:
		return RepeatAll, true
	case RepeatOne:
		return RepeatOne, true
	default:
		return "", false
	}
}

func ParseQueueInsertMode(value string) QueueInsertMode {
	switch QueueInsertMode(strings.ToLower(strings.TrimSpace(value))) {
	case QueueInsertNext:
		return QueueInsertNext
	default:
		return QueueInsertLast
	}
}
