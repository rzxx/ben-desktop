package apitypes

import (
	"context"
	"time"
)

type CatalogTrackAvailabilityState string

const (
	CatalogAvailabilityLocal           CatalogTrackAvailabilityState = "LOCAL"
	CatalogAvailabilityCached          CatalogTrackAvailabilityState = "CACHED"
	CatalogAvailabilityProviderOnline  CatalogTrackAvailabilityState = "PROVIDER_ONLINE"
	CatalogAvailabilityProviderOffline CatalogTrackAvailabilityState = "PROVIDER_OFFLINE"
	CatalogAvailabilityUnavailable     CatalogTrackAvailabilityState = "UNAVAILABLE"
)

type CatalogTrackAvailabilityHint struct {
	State                     CatalogTrackAvailabilityState
	HasLocalSource            bool
	HasCachedLocal            bool
	ProviderDeviceCount       int
	OnlineProviderDeviceCount int
}

type CatalogAggregateAvailabilityHint struct {
	LocalTrackCount           int64
	CachedTrackCount          int64
	ProviderOnlineTrackCount  int64
	ProviderOfflineTrackCount int64
	AvailableTrackCount       int64
	UnavailableTrackCount     int64
}

type AlbumListRequest struct{ PageRequest }

type ArtistListRequest struct{ PageRequest }

type ArtistAlbumListRequest struct {
	ArtistID string
	PageRequest
}

type ArtistListItem struct {
	ArtistID     string
	Name         string
	AlbumCount   int64
	TrackCount   int64
}

type AlbumListItem struct {
	AlbumID        string
	AlbumClusterID string
	Title          string
	Artists        []string
	Year           *int
	TrackCount     int64
	Thumb          ArtworkRef
	VariantCount   int64
	HasVariants    bool
}

type RecordingListRequest struct{ PageRequest }

type RecordingListItem struct {
	TrackClusterID string
	RecordingID    string
	Title          string
	DurationMS     int64
	Artists        []string
	VariantCount   int64
	HasVariants    bool
}

type AlbumTrackListRequest struct {
	AlbumID string
	PageRequest
}

type AlbumTrackItem struct {
	RecordingID  string
	Title        string
	DurationMS   int64
	DiscNo       int
	TrackNo      int
	Artists      []string
}

type RecordingVariantListRequest struct {
	RecordingID string
	PageRequest
}

type RecordingVariantItem struct {
	RecordingID         string
	TrackClusterID      string
	ContentID           string
	Title               string
	DurationMS          int64
	Artists             []string
	AlbumID             string
	AlbumTitle          string
	TrackNo             int
	DiscNo              int
	Container           string
	Codec               string
	Bitrate             int
	SampleRate          int
	Channels            int
	IsLossless          bool
	QualityRank         int
	IsPreferred         bool
	IsExplicitPreferred bool
	IsPresentLocal      bool
	IsCachedLocal       bool
	LocalPath           string
}

type AlbumVariantListRequest struct {
	AlbumID string
	PageRequest
}

type AlbumVariantItem struct {
	AlbumID             string
	AlbumClusterID      string
	Title               string
	Artists             []string
	Year                *int
	Edition             string
	TrackCount          int64
	Thumb               ArtworkRef
	BestQualityRank     int
	LocalTrackCount     int64
	IsPreferred         bool
	IsExplicitPreferred bool
}

type RecordingMatchCandidateListRequest struct {
	RecordingID string
	PageRequest
}

type RecordingMatchCandidateItem struct {
	RecordingID          string
	CandidateRecordingID string
	Score                int
	Status               string
	Title                string
	CandidateTitle       string
	Artists              []string
	CandidateArtists     []string
	DurationMS           int64
	CandidateDurationMS  int64
}

type PlaylistListRequest struct{ PageRequest }

type PlaylistListItem struct {
	PlaylistID     string
	Name           string
	Kind           PlaylistKind
	IsReserved     bool
	Thumb          ArtworkRef
	HasCustomCover bool
	CreatedBy      string
	UpdatedAt      time.Time
	ItemCount      int64
}

type PlaylistTrackListRequest struct {
	PlaylistID string
	PageRequest
}

type PlaylistTrackItem struct {
	ItemID       string
	RecordingID  string
	Title        string
	DurationMS   int64
	Artists      []string
	AddedAt      time.Time
}

type LikedRecordingListRequest struct{ PageRequest }

type LikedRecordingItem struct {
	RecordingID  string
	Title        string
	DurationMS   int64
	Artists      []string
	AddedAt      time.Time
}

type CatalogChangeKind string

const (
	CatalogChangeInvalidateBase         CatalogChangeKind = "invalidate_base"
	CatalogChangeInvalidateAvailability CatalogChangeKind = "invalidate_availability"
)

type CatalogChangeEntity string

const (
	CatalogChangeEntityAlbums         CatalogChangeEntity = "albums"
	CatalogChangeEntityAlbum          CatalogChangeEntity = "album"
	CatalogChangeEntityArtists        CatalogChangeEntity = "artists"
	CatalogChangeEntityArtistAlbums   CatalogChangeEntity = "artist_albums"
	CatalogChangeEntityTracks         CatalogChangeEntity = "tracks"
	CatalogChangeEntityAlbumTracks    CatalogChangeEntity = "album_tracks"
	CatalogChangeEntityPlaylists      CatalogChangeEntity = "playlists"
	CatalogChangeEntityPlaylistTracks CatalogChangeEntity = "playlist_tracks"
	CatalogChangeEntityLiked          CatalogChangeEntity = "liked"
)

type CatalogChangeEvent struct {
	Kind         CatalogChangeKind
	Entity       CatalogChangeEntity
	EntityID     string
	QueryKey     string
	RecordingIDs []string
	AlbumIDs     []string
	InvalidateAll bool
	OccurredAt   time.Time
}

type CatalogSurface interface {
	ListArtists(ctx context.Context, req ArtistListRequest) (Page[ArtistListItem], error)
	GetArtist(ctx context.Context, artistID string) (ArtistListItem, error)
	ListArtistAlbums(ctx context.Context, req ArtistAlbumListRequest) (Page[AlbumListItem], error)
	ListAlbums(ctx context.Context, req AlbumListRequest) (Page[AlbumListItem], error)
	GetAlbum(ctx context.Context, albumID string) (AlbumListItem, error)
	ListRecordings(ctx context.Context, req RecordingListRequest) (Page[RecordingListItem], error)
	GetRecording(ctx context.Context, recordingID string) (RecordingListItem, error)
	ListAlbumTracks(ctx context.Context, req AlbumTrackListRequest) (Page[AlbumTrackItem], error)
	ListRecordingVariants(ctx context.Context, req RecordingVariantListRequest) (Page[RecordingVariantItem], error)
	ListAlbumVariants(ctx context.Context, req AlbumVariantListRequest) (Page[AlbumVariantItem], error)
	SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error
	SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error
	ListRecordingMatchCandidates(ctx context.Context, req RecordingMatchCandidateListRequest) (Page[RecordingMatchCandidateItem], error)
	MergeRecordingVariants(ctx context.Context, recordingID, candidateRecordingID string) error
	SplitRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error
	ListPlaylists(ctx context.Context, req PlaylistListRequest) (Page[PlaylistListItem], error)
	GetPlaylistSummary(ctx context.Context, playlistID string) (PlaylistListItem, error)
	ListPlaylistTracks(ctx context.Context, req PlaylistTrackListRequest) (Page[PlaylistTrackItem], error)
	ListLikedRecordings(ctx context.Context, req LikedRecordingListRequest) (Page[LikedRecordingItem], error)
	SubscribeCatalogChanges(listener func(CatalogChangeEvent)) func()
}
