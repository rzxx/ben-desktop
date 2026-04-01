package desktopcore

import (
	"context"

	apitypes "ben/desktop/api/types"
)

type LibraryRuntime interface {
	ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error)
	ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error)
	CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error)
	SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error)
	RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error)
	LeaveLibrary(ctx context.Context, libraryID string) error
	DeleteLibrary(ctx context.Context, libraryID string) error
	ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error)
	UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error
	RemoveLibraryMember(ctx context.Context, deviceID string) error
	SetScanRoots(ctx context.Context, roots []string) error
	AddScanRoots(ctx context.Context, roots []string) ([]string, error)
	RemoveScanRoots(ctx context.Context, roots []string) ([]string, error)
	ScanRoots(ctx context.Context) ([]string, error)
	StartRescanNow(ctx context.Context) (JobSnapshot, error)
	StartRescanRoot(ctx context.Context, root string) (JobSnapshot, error)
}

type NetworkRuntime interface {
	EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error)
	Inspect(ctx context.Context) (apitypes.InspectSummary, error)
	InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error)
	ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error)
	NetworkStatus() apitypes.NetworkStatus
	StartSyncNow(ctx context.Context) (JobSnapshot, error)
	StartConnectPeer(ctx context.Context, peerAddr string) (JobSnapshot, error)
	CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error)
	StartPublishCheckpoint(ctx context.Context) (JobSnapshot, error)
	StartCompactCheckpoint(ctx context.Context, force bool) (JobSnapshot, error)
}

type JobsRuntime interface {
	ListJobs(ctx context.Context, libraryID string) ([]JobSnapshot, error)
	GetJob(ctx context.Context, jobID string) (JobSnapshot, bool, error)
	SubscribeJobSnapshots(listener func(JobSnapshot)) func()
}

type CatalogRuntime interface {
	ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error)
	GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error)
	ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error)
	ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error)
	GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error)
	ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error)
	ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error)
	SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error
	SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error
	ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error)
	ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error)
	GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error)
	ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error)
	ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error)
	CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error)
	RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error)
	DeletePlaylist(ctx context.Context, playlistID string) error
	AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error)
	MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error)
	RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error
	GetPlaylistCover(ctx context.Context, playlistID string) (apitypes.PlaylistCoverRecord, bool, error)
	SetPlaylistCover(ctx context.Context, req apitypes.PlaylistCoverUploadRequest) (apitypes.PlaylistCoverRecord, error)
	ClearPlaylistCover(ctx context.Context, playlistID string) error
	LikeRecording(ctx context.Context, recordingID string) error
	UnlikeRecording(ctx context.Context, recordingID string) error
	IsRecordingLiked(ctx context.Context, recordingID string) (bool, error)
	SubscribeCatalogChanges(listener func(apitypes.CatalogChangeEvent)) func()
}

type InviteRuntime interface {
	CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error)
	ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error)
	RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error
	StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error)
	GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error)
	StartFinalizeJoinSession(ctx context.Context, sessionID string) (JobSnapshot, error)
	CancelJoinSession(ctx context.Context, sessionID string) error
	ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error)
	ApproveJoinRequest(ctx context.Context, requestID, role string) error
	RejectJoinRequest(ctx context.Context, requestID, reason string) error
}

type CacheRuntime interface {
	GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error)
	ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error)
	CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error)
}

type PlaybackRuntime interface {
	StartEnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (JobSnapshot, error)
	StartEnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (JobSnapshot, error)
	StartEnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (JobSnapshot, error)
	StartPinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (JobSnapshot, error)
	StartPinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (JobSnapshot, error)
	StartPinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (JobSnapshot, error)
	EnsurePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error)
	InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	StartPreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (JobSnapshot, error)
	GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error)
	ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error)
	ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error)
	ResolveAlbumArtwork(ctx context.Context, albumID, variant string) (apitypes.RecordingArtworkResult, error)
	ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error)
	EnsurePlaybackAlbum(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	EnsurePlaybackPlaylist(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error)
	UnpinRecordingOffline(ctx context.Context, recordingID string) error
	PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	UnpinAlbumOffline(ctx context.Context, albumID string) error
	PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	UnpinPlaylistOffline(ctx context.Context, playlistID string) error
	PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	UnpinLikedOffline(ctx context.Context) error
	ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error)
	GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error)
	ListRecordingPlaybackAvailability(ctx context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error)
	ListAlbumAvailabilitySummaries(ctx context.Context, req apitypes.AlbumAvailabilitySummaryListRequest) ([]apitypes.AlbumAvailabilitySummaryItem, error)
	GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error)
	GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error)
}
