package apitypes

import (
	"context"
	"time"
)

type PlaybackSourceKind string

const (
	PlaybackSourceLocalFile PlaybackSourceKind = "local_file"
	PlaybackSourceCachedOpt PlaybackSourceKind = "cached_opt"
	PlaybackSourceRemoteOpt PlaybackSourceKind = "remote_opt"
)

type PlaybackUnavailableReason string

const (
	PlaybackUnavailableProviderOffline PlaybackUnavailableReason = "provider_offline"
	PlaybackUnavailableOwnerOffline    PlaybackUnavailableReason = PlaybackUnavailableProviderOffline
	PlaybackUnavailableNoPath          PlaybackUnavailableReason = "no_path"
	PlaybackUnavailableNetworkOff      PlaybackUnavailableReason = "network_off"
)

type RecordingAvailabilityState string

const (
	AvailabilityPlayableLocalFile        RecordingAvailabilityState = "PLAYABLE:LOCAL_FILE"
	AvailabilityPlayableCachedOpt        RecordingAvailabilityState = "PLAYABLE:CACHED_OPT"
	AvailabilityPlayableRemoteOpt        RecordingAvailabilityState = "PLAYABLE:REMOTE_OPT"
	AvailabilityWaitingProviderTranscode RecordingAvailabilityState = "WAITING:PROVIDER_TRANSCODE"
	AvailabilityWaitingTranscode         RecordingAvailabilityState = AvailabilityWaitingProviderTranscode
	AvailabilityUnavailableProvider      RecordingAvailabilityState = "UNAVAILABLE:PROVIDER_OFFLINE"
	AvailabilityUnavailableOwner         RecordingAvailabilityState = AvailabilityUnavailableProvider
	AvailabilityUnavailableNoPath        RecordingAvailabilityState = "UNAVAILABLE:NO_PATH"
)

type PlaybackRecordingResult struct {
	EncodingID string
	BlobID     string
	Profile    string
	Bitrate    int
	Bytes      int
	FromLocal  bool
	SourceKind PlaybackSourceKind
	LocalPath  string
	Reason     PlaybackUnavailableReason
}

type PlaybackResolveResult struct {
	RecordingID string
	State       RecordingAvailabilityState
	SourceKind  PlaybackSourceKind
	Reason      PlaybackUnavailableReason
	PlayableURI string
	EncodingID  string
	BlobID      string
	Profile     string
}

type PlaybackPreparationPurpose string

const (
	PlaybackPreparationPlayNow     PlaybackPreparationPurpose = "play_now"
	PlaybackPreparationPreloadNext PlaybackPreparationPurpose = "preload_next"
)

type PlaybackPreparationPhase string

const (
	PlaybackPreparationReady              PlaybackPreparationPhase = "ready"
	PlaybackPreparationPreparingFetch     PlaybackPreparationPhase = "preparing_fetch"
	PlaybackPreparationPreparingTranscode PlaybackPreparationPhase = "preparing_transcode"
	PlaybackPreparationUnavailable        PlaybackPreparationPhase = "unavailable"
	PlaybackPreparationFailed             PlaybackPreparationPhase = "failed"
)

type PlaybackPreparationStatus struct {
	RecordingID      string                     `json:"recordingId"`
	PreferredProfile string                     `json:"preferredProfile"`
	Purpose          PlaybackPreparationPurpose `json:"purpose"`
	Phase            PlaybackPreparationPhase   `json:"phase"`
	SourceKind       PlaybackSourceKind         `json:"sourceKind,omitempty"`
	Reason           PlaybackUnavailableReason  `json:"reason,omitempty"`
	PlayableURI      string                     `json:"playableUri,omitempty"`
	EncodingID       string                     `json:"encodingId,omitempty"`
	BlobID           string                     `json:"blobId,omitempty"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
}

type RecordingArtworkResult struct {
	RecordingID string
	AlbumID     string
	Artwork     ArtworkRef
	LocalPath   string
	Available   bool
}

type ArtworkResolveResult struct {
	Artwork   ArtworkRef
	LocalPath string
	Available bool
}

type PlaybackBatchResult struct {
	Tracks        int
	TotalBytes    int64
	LocalHits     int
	RemoteFetches int
}

type EnsureEncodingBatchResult struct {
	Recordings int
	Created    int
	Skipped    int
}

type RecordingAvailabilityItem struct {
	DeviceID          string
	Role              string
	PeerID            string
	LastSeenAt        *time.Time
	LastSyncSuccessAt *time.Time
	SourcePresent     bool
	OptimizedPresent  bool
	CachedOptimized   bool
}

type RecordingPlaybackAvailability struct {
	RecordingID      string
	PreferredProfile string
	State            RecordingAvailabilityState
	SourceKind       PlaybackSourceKind
	LocalPath        string
	Reason           PlaybackUnavailableReason
}

type RecordingPlaybackAvailabilityListRequest struct {
	RecordingIDs      []string
	PreferredProfile string
}

type AlbumAvailabilitySummaryListRequest struct {
	AlbumIDs          []string
	PreferredProfile string
}

type AlbumAvailabilitySummaryItem struct {
	AlbumID           string
	PreferredProfile string
	Availability      AggregateAvailabilitySummary
}

type TrackAvailabilitySummary struct {
	State                    RecordingAvailabilityState
	SourceKind               PlaybackSourceKind
	Reason                   PlaybackUnavailableReason
	IsLocal                  bool
	HasLocalSource           bool
	HasLocalCachedOptimized  bool
	HasRemoteSource          bool
	HasRemoteCachedOptimized bool
	LocalDeviceCount         int
	RemoteDeviceCount        int
	AvailableDeviceCount     int
}

type AggregateAvailabilitySummary struct {
	IsLocal               bool
	HasRemote             bool
	LocalTrackCount       int64
	CachedTrackCount      int64
	RemoteTrackCount      int64
	AvailableTrackCount   int64
	UnavailableTrackCount int64
}

type RecordingVariantAvailabilityOverview struct {
	Variant RecordingVariantItem
	Devices []RecordingAvailabilityItem
}

type RecordingAvailabilityOverview struct {
	RecordingID      string
	PreferredProfile string
	Playback         RecordingPlaybackAvailability
	Availability     TrackAvailabilitySummary
	Devices          []RecordingAvailabilityItem
	Variants         []RecordingVariantAvailabilityOverview
}

type AlbumTrackAvailabilityOverview struct {
	Track AlbumTrackItem
}

type AlbumVariantAvailabilityOverview struct {
	Variant AlbumVariantItem
}

type AlbumAvailabilityOverview struct {
	AlbumID          string
	PreferredProfile string
	Availability     AggregateAvailabilitySummary
	Tracks           []AlbumTrackAvailabilityOverview
	Variants         []AlbumVariantAvailabilityOverview
}

type PlaybackSurface interface {
	EnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (bool, error)
	EnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (EnsureEncodingBatchResult, error)
	EnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (EnsureEncodingBatchResult, error)
	EnsurePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (PlaybackRecordingResult, error)
	InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (PlaybackPreparationStatus, error)
	PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose PlaybackPreparationPurpose) (PlaybackPreparationStatus, error)
	GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (PlaybackPreparationStatus, error)
	ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (PlaybackResolveResult, error)
	ResolveArtworkRef(ctx context.Context, artwork ArtworkRef) (ArtworkResolveResult, error)
	ResolveAlbumArtwork(ctx context.Context, albumID, variant string) (RecordingArtworkResult, error)
	ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (RecordingArtworkResult, error)
	EnsurePlaybackAlbum(ctx context.Context, albumID, preferredProfile string) (PlaybackBatchResult, error)
	EnsurePlaybackPlaylist(ctx context.Context, playlistID, preferredProfile string) (PlaybackBatchResult, error)
	PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (PlaybackBatchResult, error)
	PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (PlaybackBatchResult, error)
	PinLikedOffline(ctx context.Context, preferredProfile string) (PlaybackBatchResult, error)
	UnpinPlaylistOffline(ctx context.Context, playlistID string) error
	PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (PlaybackRecordingResult, error)
	UnpinRecordingOffline(ctx context.Context, recordingID string) error
	UnpinAlbumOffline(ctx context.Context, albumID string) error
	UnpinLikedOffline(ctx context.Context) error
	ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]RecordingAvailabilityItem, error)
	GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (RecordingPlaybackAvailability, error)
	ListRecordingPlaybackAvailability(ctx context.Context, req RecordingPlaybackAvailabilityListRequest) ([]RecordingPlaybackAvailability, error)
	ListAlbumAvailabilitySummaries(ctx context.Context, req AlbumAvailabilitySummaryListRequest) ([]AlbumAvailabilitySummaryItem, error)
	GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (RecordingAvailabilityOverview, error)
	GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (AlbumAvailabilityOverview, error)
}
