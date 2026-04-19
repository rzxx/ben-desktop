package playback

import (
	"context"
	"errors"
	"strings"

	apitypes "ben/desktop/api/types"
)

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
	ContextKindTracks    ContextKind = "tracks"
	ContextKindRecording ContextKind = "recording"
	ContextKindCustom    ContextKind = "custom"
)

type ContextRebasePolicy string

const (
	ContextRebaseFrozen ContextRebasePolicy = "frozen"
	ContextRebaseLive   ContextRebasePolicy = "live"
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

var ErrUnsupportedPreloadActivation = errors.New("preloaded activation is unsupported")

type ResolutionMode string

const (
	ResolutionModeLibrary  ResolutionMode = "library"
	ResolutionModeExplicit ResolutionMode = "explicit"
)

type PlaybackTargetResolution string

const (
	PlaybackTargetResolutionPreferred PlaybackTargetResolution = "preferred"
	PlaybackTargetResolutionExact     PlaybackTargetResolution = "exact"
)

type CurrentLane string

const (
	CurrentLaneContext CurrentLane = "context"
	CurrentLaneUser    CurrentLane = "user"
)

type PlaybackTargetRef struct {
	LogicalRecordingID      string                   `json:"logicalRecordingId,omitempty"`
	ExactVariantRecordingID string                   `json:"exactVariantRecordingId,omitempty"`
	ResolutionPolicy        PlaybackTargetResolution `json:"resolutionPolicy,omitempty"`
}

type PlaybackSourceAnchor struct {
	EntryKey     string `json:"entryKey,omitempty"`
	RecordingID  string `json:"recordingId,omitempty"`
	SourceItemID string `json:"sourceItemId,omitempty"`
}

type PlaybackSourceDescriptor struct {
	Kind         ContextKind         `json:"kind"`
	ID           string              `json:"id"`
	Title        string              `json:"title,omitempty"`
	RebasePolicy ContextRebasePolicy `json:"rebasePolicy,omitempty"`
	Live         bool                `json:"live,omitempty"`
}

type PlaybackSourceRequest struct {
	Descriptor PlaybackSourceDescriptor `json:"descriptor"`
	Anchor     PlaybackSourceAnchor     `json:"anchor"`
}

type PlaybackSourceCandidate struct {
	Key  string
	Item SessionItem
}

type SessionItem struct {
	LibraryRecordingID string            `json:"libraryRecordingId,omitempty"`
	VariantRecordingID string            `json:"variantRecordingId,omitempty"`
	RecordingID        string            `json:"recordingId"`
	Title              string            `json:"title"`
	Subtitle           string            `json:"subtitle"`
	DurationMS         int64             `json:"durationMs"`
	ArtworkRef         string            `json:"artworkRef"`
	SourceKind         string            `json:"sourceKind"`
	SourceID           string            `json:"sourceId"`
	SourceItemID       string            `json:"sourceItemId"`
	AlbumID            string            `json:"albumId,omitempty"`
	VariantAlbumID     string            `json:"variantAlbumId,omitempty"`
	ResolutionMode     ResolutionMode    `json:"resolutionMode,omitempty"`
	Target             PlaybackTargetRef `json:"target,omitempty"`
}

type SessionEntry struct {
	EntryID      string      `json:"entryId"`
	Origin       EntryOrigin `json:"origin"`
	ContextIndex int         `json:"contextIndex,omitempty"`
	Item         SessionItem `json:"item"`
}

type ContextWindowEntry = SessionEntry

type ContextQueue struct {
	Kind          ContextKind               `json:"kind"`
	ID            string                    `json:"id"`
	Title         string                    `json:"title,omitempty"`
	Entries       []ContextWindowEntry      `json:"entries"`
	StartIndex    int                       `json:"startIndex"`
	CurrentIndex  int                       `json:"currentIndex"`
	ResumeIndex   int                       `json:"resumeIndex"`
	ShuffleBag    []int                     `json:"shuffleBag,omitempty"`
	HasBefore     bool                      `json:"hasBefore,omitempty"`
	HasAfter      bool                      `json:"hasAfter,omitempty"`
	WindowStart   int                       `json:"windowStart,omitempty"`
	WindowCount   int                       `json:"windowCount,omitempty"`
	TotalCount    int                       `json:"totalCount,omitempty"`
	Live          bool                      `json:"live,omitempty"`
	Loading       bool                      `json:"loading,omitempty"`
	SourceVersion int64                     `json:"sourceVersion,omitempty"`
	ShuffleSeed   uint64                    `json:"shuffleSeed,omitempty"`
	Source        *PlaybackSourceDescriptor `json:"source,omitempty"`
	Anchor        *PlaybackSourceAnchor     `json:"anchor,omitempty"`
	allEntries    []SessionEntry
}

type QueuePlan struct {
	Entry   *SessionEntry `json:"entry,omitempty"`
	Lane    CurrentLane   `json:"lane,omitempty"`
	Planned bool          `json:"planned"`
}

type TargetAvailability struct {
	Target PlaybackTargetRef                      `json:"target"`
	Status apitypes.RecordingPlaybackAvailability `json:"status"`
}

type TargetAvailabilityRequest struct {
	Targets          []PlaybackTargetRef `json:"targets"`
	PreferredProfile string              `json:"preferredProfile"`
}

type SessionSnapshot struct {
	ContextQueue         *ContextQueue                                     `json:"contextQueue,omitempty"`
	UserQueue            []SessionEntry                                    `json:"userQueue"`
	CurrentEntryID       string                                            `json:"currentEntryId,omitempty"`
	CurrentEntry         *SessionEntry                                     `json:"currentEntry,omitempty"`
	CurrentItem          *SessionItem                                      `json:"currentItem,omitempty"`
	LoadingEntry         *SessionEntry                                     `json:"loadingEntry,omitempty"`
	LoadingItem          *SessionItem                                      `json:"loadingItem,omitempty"`
	UpcomingEntries      []SessionEntry                                    `json:"upcomingEntries"`
	CurrentLane          CurrentLane                                       `json:"currentLane,omitempty"`
	NextPlanned          *QueuePlan                                        `json:"nextPlanned,omitempty"`
	PreloadedPlan        *QueuePlan                                        `json:"preloadedPlan,omitempty"`
	RepeatMode           RepeatMode                                        `json:"repeatMode"`
	Shuffle              bool                                              `json:"shuffle"`
	Volume               int                                               `json:"volume"`
	Status               Status                                            `json:"status"`
	PositionMS           int64                                             `json:"positionMs"`
	PositionCapturedAtMS int64                                             `json:"positionCapturedAtMs,omitempty"`
	DurationMS           *int64                                            `json:"durationMs,omitempty"`
	UpdatedAt            string                                            `json:"updatedAt"`
	LastError            string                                            `json:"lastError,omitempty"`
	CurrentSourceKind    apitypes.PlaybackSourceKind                       `json:"currentSourceKind,omitempty"`
	CurrentPreparation   *EntryPreparation                                 `json:"currentPreparation,omitempty"`
	LoadingPreparation   *EntryPreparation                                 `json:"loadingPreparation,omitempty"`
	NextPreparation      *EntryPreparation                                 `json:"nextPreparation,omitempty"`
	EntryAvailability    map[string]apitypes.RecordingPlaybackAvailability `json:"entryAvailability,omitempty"`
	LastSkipEvent        *PlaybackSkipEvent                                `json:"lastSkipEvent,omitempty"`
	QueueLength          int                                               `json:"queueLength"`
	NextEntrySeq         int64                                             `json:"nextEntrySeq,omitempty"`
	QueueVersion         int64                                             `json:"queueVersion,omitempty"`
}

type EntryPreparation struct {
	EntryID string                             `json:"entryId"`
	Status  apitypes.PlaybackPreparationStatus `json:"status"`
}

type PlaybackSkipEvent struct {
	EventID    string        `json:"eventId"`
	Message    string        `json:"message"`
	Count      int           `json:"count"`
	Stopped    bool          `json:"stopped"`
	FirstEntry *SessionEntry `json:"firstEntry,omitempty"`
	OccurredAt string        `json:"occurredAt"`
}

type PlaybackCore interface {
	Close() error
	ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error)
	ListRecordingsCursor(ctx context.Context, req apitypes.RecordingCursorRequest) (apitypes.CursorPage[apitypes.RecordingListItem], error)
	GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error)
	GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error)
	ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error)
	GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error)
	ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error)
	ListPlaylistTracksCursor(ctx context.Context, req apitypes.PlaylistTrackCursorRequest) (apitypes.CursorPage[apitypes.PlaylistTrackItem], error)
	ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error)
	ListLikedRecordingsCursor(ctx context.Context, req apitypes.LikedRecordingCursorRequest) (apitypes.CursorPage[apitypes.LikedRecordingItem], error)
	SubscribeCatalogChanges(listener func(apitypes.CatalogChangeEvent)) func()
	InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error)
	GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error)
	PreparePlaybackTarget(ctx context.Context, target PlaybackTargetRef, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error)
	GetPlaybackTargetPreparation(ctx context.Context, target PlaybackTargetRef, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	GetPlaybackTargetAvailability(ctx context.Context, target PlaybackTargetRef, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error)
	ListPlaybackTargetAvailability(ctx context.Context, req TargetAvailabilityRequest) ([]TargetAvailability, error)
	ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error)
	ResolveAlbumArtwork(ctx context.Context, albumID, variant string) (apitypes.RecordingArtworkResult, error)
	ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error)
	GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error)
	ListRecordingPlaybackAvailability(ctx context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error)
}

type SessionStore interface {
	Load(ctx context.Context) (SessionSnapshot, error)
	Save(ctx context.Context, snapshot SessionSnapshot) error
	Clear(ctx context.Context) error
}

type Backend interface {
	Load(ctx context.Context, uri string) error
	ActivatePreloaded(ctx context.Context, uri string) (BackendActivationRef, error)
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

type BackendActivationRef struct {
	URI             string
	PlaylistEntryID int64
	PlaylistPos     int64
	AttemptID       uint64
}

type PlatformController interface {
	Start() error
	Stop() error
	HandlePlaybackSnapshot(snapshot SessionSnapshot)
}

type BackendEventType string

const (
	BackendEventTrackEnd   BackendEventType = "track_end"
	BackendEventFileLoaded BackendEventType = "file_loaded"
	BackendEventShutdown   BackendEventType = "shutdown"
	BackendEventError      BackendEventType = "error"
)

const (
	TrackEndReasonEOF      = "eof"
	TrackEndReasonStop     = "stop"
	TrackEndReasonQuit     = "quit"
	TrackEndReasonError    = "error"
	TrackEndReasonRedirect = "redirect"
)

type BackendEvent struct {
	Type                  BackendEventType
	Reason                string
	Err                   error
	EndedURI              string
	ActiveURI             string
	ActivePlaylistEntryID int64
	ActivePlaylistPos     int64
	ActiveAttemptID       uint64
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
