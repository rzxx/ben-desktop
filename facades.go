package main

import (
	"context"
	"strings"
	"sync"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type facadeBase struct {
	host *coreHost
}

func (f facadeBase) library() desktopcore.LibraryRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.LibraryRuntime()
}

func (f facadeBase) network() desktopcore.NetworkRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.NetworkRuntime()
}

func (f facadeBase) jobs() desktopcore.JobsRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.JobsRuntime()
}

func (f facadeBase) catalog() desktopcore.CatalogRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.CatalogRuntime()
}

func (f facadeBase) invite() desktopcore.InviteRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.InviteRuntime()
}

func (f facadeBase) cache() desktopcore.CacheRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.CacheRuntime()
}

func (f facadeBase) playback() desktopcore.PlaybackRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.PlaybackRuntime()
}

type LibraryFacade struct {
	facadeBase
}

func NewLibraryFacade(host *coreHost) *LibraryFacade {
	return &LibraryFacade{facadeBase: facadeBase{host: host}}
}

func (s *LibraryFacade) ServiceName() string { return "LibraryFacade" }

func (s *LibraryFacade) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return s.library().ListLibraries(ctx)
}

func (s *LibraryFacade) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return s.library().ActiveLibrary(ctx)
}

func (s *LibraryFacade) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return s.library().CreateLibrary(ctx, name)
}

func (s *LibraryFacade) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return s.library().SelectLibrary(ctx, libraryID)
}

func (s *LibraryFacade) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return s.library().RenameLibrary(ctx, libraryID, name)
}

func (s *LibraryFacade) LeaveLibrary(ctx context.Context, libraryID string) error {
	return s.library().LeaveLibrary(ctx, libraryID)
}

func (s *LibraryFacade) DeleteLibrary(ctx context.Context, libraryID string) error {
	return s.library().DeleteLibrary(ctx, libraryID)
}

func (s *LibraryFacade) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return s.library().ListLibraryMembers(ctx)
}

func (s *LibraryFacade) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return s.library().UpdateLibraryMemberRole(ctx, deviceID, role)
}

func (s *LibraryFacade) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return s.library().RemoveLibraryMember(ctx, deviceID)
}

func (s *LibraryFacade) SetScanRoots(ctx context.Context, roots []string) error {
	return s.library().SetScanRoots(ctx, roots)
}

func (s *LibraryFacade) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return s.library().AddScanRoots(ctx, roots)
}

func (s *LibraryFacade) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return s.library().RemoveScanRoots(ctx, roots)
}

func (s *LibraryFacade) ScanRoots(ctx context.Context) ([]string, error) {
	return s.library().ScanRoots(ctx)
}

func (s *LibraryFacade) StartRescanNow(ctx context.Context) (desktopcore.JobSnapshot, error) {
	return s.library().StartRescanNow(ctx)
}

func (s *LibraryFacade) StartRescanRoot(ctx context.Context, root string) (desktopcore.JobSnapshot, error) {
	return s.library().StartRescanRoot(ctx, root)
}

type NetworkFacade struct {
	facadeBase
}

func NewNetworkFacade(host *coreHost) *NetworkFacade {
	return &NetworkFacade{facadeBase: facadeBase{host: host}}
}

func (s *NetworkFacade) ServiceName() string { return "NetworkFacade" }

func (s *NetworkFacade) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return s.network().EnsureLocalContext(ctx)
}

func (s *NetworkFacade) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return s.network().Inspect(ctx)
}

func (s *NetworkFacade) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return s.network().InspectLibraryOplog(ctx, libraryID)
}

func (s *NetworkFacade) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	return s.network().ActivityStatus(ctx)
}

func (s *NetworkFacade) NetworkStatus() apitypes.NetworkStatus {
	return s.network().NetworkStatus()
}

func (s *NetworkFacade) StartSyncNow(ctx context.Context) (desktopcore.JobSnapshot, error) {
	return s.network().StartSyncNow(ctx)
}

func (s *NetworkFacade) StartConnectPeer(ctx context.Context, peerAddr string) (desktopcore.JobSnapshot, error) {
	return s.network().StartConnectPeer(ctx, peerAddr)
}

func (s *NetworkFacade) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return s.network().CheckpointStatus(ctx)
}

func (s *NetworkFacade) StartPublishCheckpoint(ctx context.Context) (desktopcore.JobSnapshot, error) {
	return s.network().StartPublishCheckpoint(ctx)
}

func (s *NetworkFacade) StartCompactCheckpoint(ctx context.Context, force bool) (desktopcore.JobSnapshot, error) {
	return s.network().StartCompactCheckpoint(ctx, force)
}

type JobsFacade struct {
	facadeBase

	mu            sync.Mutex
	stopListening func()
}

func NewJobsFacade(host *coreHost) *JobsFacade {
	return &JobsFacade{facadeBase: facadeBase{host: host}}
}

func (s *JobsFacade) ServiceName() string { return "JobsFacade" }

func (s *JobsFacade) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.host == nil {
		return nil
	}
	if err := s.host.Start(ctx); err != nil {
		return err
	}

	app := application.Get()
	if app == nil || app.Event == nil {
		return nil
	}

	stopListening := s.jobs().SubscribeJobSnapshots(func(snapshot desktopcore.JobSnapshot) {
		app.Event.Emit(desktopcore.EventJobSnapshotChanged, snapshot)
	})

	s.mu.Lock()
	s.stopListening = stopListening
	s.mu.Unlock()
	return nil
}

func (s *JobsFacade) ServiceShutdown() error {
	s.mu.Lock()
	stopListening := s.stopListening
	s.stopListening = nil
	s.mu.Unlock()

	if stopListening != nil {
		stopListening()
	}
	return nil
}

func (s *JobsFacade) ListJobs(ctx context.Context, libraryID string) ([]desktopcore.JobSnapshot, error) {
	return s.jobs().ListJobs(ctx, libraryID)
}

func (s *JobsFacade) GetJob(ctx context.Context, jobID string) (desktopcore.JobSnapshot, bool, error) {
	return s.jobs().GetJob(ctx, jobID)
}

func (s *JobsFacade) SubscribeJobEvents() string {
	return desktopcore.EventJobSnapshotChanged
}

type CatalogFacade struct {
	facadeBase

	mu            sync.Mutex
	stopListening func()
}

func NewCatalogFacade(host *coreHost) *CatalogFacade {
	return &CatalogFacade{facadeBase: facadeBase{host: host}}
}

func (s *CatalogFacade) ServiceName() string { return "CatalogFacade" }

func (s *CatalogFacade) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.host == nil {
		return nil
	}
	if err := s.host.Start(ctx); err != nil {
		return err
	}

	app := application.Get()
	if app == nil || app.Event == nil {
		return nil
	}

	stopListening := s.catalog().SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		app.Event.Emit(desktopcore.EventCatalogChanged, event)
	})

	s.mu.Lock()
	s.stopListening = stopListening
	s.mu.Unlock()
	return nil
}

func (s *CatalogFacade) ServiceShutdown() error {
	s.mu.Lock()
	stopListening := s.stopListening
	s.stopListening = nil
	s.mu.Unlock()
	if stopListening != nil {
		stopListening()
	}
	return nil
}

func (s *CatalogFacade) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return s.catalog().ListArtists(ctx, req)
}

func (s *CatalogFacade) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return s.catalog().GetArtist(ctx, artistID)
}

func (s *CatalogFacade) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return s.catalog().ListArtistAlbums(ctx, req)
}

func (s *CatalogFacade) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return s.catalog().ListAlbums(ctx, req)
}

func (s *CatalogFacade) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return s.catalog().GetAlbum(ctx, albumID)
}

func (s *CatalogFacade) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return s.catalog().ListRecordings(ctx, req)
}

func (s *CatalogFacade) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return s.catalog().GetRecording(ctx, recordingID)
}

func (s *CatalogFacade) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return s.catalog().ListRecordingVariants(ctx, req)
}

func (s *CatalogFacade) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return s.catalog().ListAlbumVariants(ctx, req)
}

func (s *CatalogFacade) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return s.catalog().SetPreferredRecordingVariant(ctx, recordingID, variantRecordingID)
}

func (s *CatalogFacade) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return s.catalog().SetPreferredAlbumVariant(ctx, albumID, variantAlbumID)
}

func (s *CatalogFacade) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return s.catalog().ListAlbumTracks(ctx, req)
}

func (s *CatalogFacade) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return s.catalog().ListPlaylists(ctx, req)
}

func (s *CatalogFacade) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return s.catalog().GetPlaylistSummary(ctx, playlistID)
}

func (s *CatalogFacade) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return s.catalog().ListPlaylistTracks(ctx, req)
}

func (s *CatalogFacade) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return s.catalog().ListLikedRecordings(ctx, req)
}

func (s *CatalogFacade) SubscribeCatalogEvents() string {
	return desktopcore.EventCatalogChanged
}

func (s *CatalogFacade) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return s.catalog().CreatePlaylist(ctx, name, kind)
}

func (s *CatalogFacade) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return s.catalog().RenamePlaylist(ctx, playlistID, name)
}

func (s *CatalogFacade) DeletePlaylist(ctx context.Context, playlistID string) error {
	return s.catalog().DeletePlaylist(ctx, playlistID)
}

func (s *CatalogFacade) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return s.catalog().AddPlaylistItem(ctx, req)
}

func (s *CatalogFacade) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return s.catalog().MovePlaylistItem(ctx, req)
}

func (s *CatalogFacade) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return s.catalog().RemovePlaylistItem(ctx, playlistID, itemID)
}

func (s *CatalogFacade) LikeRecording(ctx context.Context, recordingID string) error {
	return s.catalog().LikeRecording(ctx, recordingID)
}

func (s *CatalogFacade) UnlikeRecording(ctx context.Context, recordingID string) error {
	return s.catalog().UnlikeRecording(ctx, recordingID)
}

func (s *CatalogFacade) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return s.catalog().IsRecordingLiked(ctx, recordingID)
}

type InviteFacade struct {
	facadeBase
}

func NewInviteFacade(host *coreHost) *InviteFacade {
	return &InviteFacade{facadeBase: facadeBase{host: host}}
}

func (s *InviteFacade) ServiceName() string { return "InviteFacade" }

func (s *InviteFacade) CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	return s.invite().CreateInviteCode(ctx, req)
}

func (s *InviteFacade) ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error) {
	return s.invite().ListIssuedInvites(ctx, status)
}

func (s *InviteFacade) RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error {
	return s.invite().RevokeIssuedInvite(ctx, inviteID, reason)
}

func (s *InviteFacade) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	return s.invite().StartJoinFromInvite(ctx, req)
}

func (s *InviteFacade) GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error) {
	return s.invite().GetJoinSession(ctx, sessionID)
}

func (s *InviteFacade) StartFinalizeJoinSession(ctx context.Context, sessionID string) (desktopcore.JobSnapshot, error) {
	return s.invite().StartFinalizeJoinSession(ctx, sessionID)
}

func (s *InviteFacade) CancelJoinSession(ctx context.Context, sessionID string) error {
	return s.invite().CancelJoinSession(ctx, sessionID)
}

func (s *InviteFacade) ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error) {
	return s.invite().ListJoinRequests(ctx, status)
}

func (s *InviteFacade) ApproveJoinRequest(ctx context.Context, requestID, role string) error {
	return s.invite().ApproveJoinRequest(ctx, requestID, role)
}

func (s *InviteFacade) RejectJoinRequest(ctx context.Context, requestID, reason string) error {
	return s.invite().RejectJoinRequest(ctx, requestID, reason)
}

type CacheFacade struct {
	facadeBase
}

func NewCacheFacade(host *coreHost) *CacheFacade {
	return &CacheFacade{facadeBase: facadeBase{host: host}}
}

func (s *CacheFacade) ServiceName() string { return "CacheFacade" }

func (s *CacheFacade) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return s.cache().GetCacheOverview(ctx)
}

func (s *CacheFacade) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return s.cache().ListCacheEntries(ctx, req)
}

func (s *CacheFacade) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return s.cache().CleanupCache(ctx, req)
}

type PlaybackFacade struct {
	facadeBase
}

func NewPlaybackFacade(host *coreHost) *PlaybackFacade {
	return &PlaybackFacade{facadeBase: facadeBase{host: host}}
}

func (s *PlaybackFacade) ServiceName() string { return "PlaybackFacade" }

func (s *PlaybackFacade) StartEnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (desktopcore.JobSnapshot, error) {
	return s.playback().StartEnsureRecordingEncoding(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) StartEnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (desktopcore.JobSnapshot, error) {
	return s.playback().StartEnsureAlbumEncodings(ctx, albumID, preferredProfile)
}

func (s *PlaybackFacade) StartEnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (desktopcore.JobSnapshot, error) {
	return s.playback().StartEnsurePlaylistEncodings(ctx, playlistID, preferredProfile)
}

func (s *PlaybackFacade) EnsurePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return s.playback().EnsurePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.playback().InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) StartPreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (desktopcore.JobSnapshot, error) {
	return s.playback().StartPreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (s *PlaybackFacade) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.playback().GetPlaybackPreparation(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return s.playback().ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) ResolveThumbnailURL(artwork apitypes.ArtworkRef) (string, error) {
	artwork.BlobID = strings.TrimSpace(artwork.BlobID)
	if artwork.BlobID == "" {
		return "", nil
	}

	resolved, err := s.playback().ResolveArtworkRef(context.Background(), artwork)
	if err != nil {
		return "", err
	}
	if !resolved.Available || strings.TrimSpace(resolved.LocalPath) == "" {
		return "", nil
	}
	return artworkAssetURL(resolved.Artwork), nil
}

func (s *PlaybackFacade) ResolveAlbumArtworkURL(ctx context.Context, albumID, variant string) (string, error) {
	result, err := s.playback().ResolveAlbumArtwork(ctx, albumID, variant)
	if err != nil {
		return "", err
	}
	if !result.Available || strings.TrimSpace(result.LocalPath) == "" {
		return "", nil
	}
	return artworkAssetURL(result.Artwork), nil
}

func (s *PlaybackFacade) ResolveRecordingArtworkURL(ctx context.Context, recordingID, variant string) (string, error) {
	result, err := s.playback().ResolveRecordingArtwork(ctx, recordingID, variant)
	if err != nil {
		return "", err
	}
	if !result.Available || strings.TrimSpace(result.LocalPath) == "" {
		return "", nil
	}
	return artworkAssetURL(result.Artwork), nil
}

func (s *PlaybackFacade) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return s.playback().PinRecordingOffline(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) EnsurePlaybackAlbum(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.playback().EnsurePlaybackAlbum(ctx, albumID, preferredProfile)
}

func (s *PlaybackFacade) EnsurePlaybackPlaylist(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.playback().EnsurePlaybackPlaylist(ctx, playlistID, preferredProfile)
}

func (s *PlaybackFacade) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	return s.playback().UnpinRecordingOffline(ctx, recordingID)
}

func (s *PlaybackFacade) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.playback().PinAlbumOffline(ctx, albumID, preferredProfile)
}

func (s *PlaybackFacade) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	return s.playback().UnpinAlbumOffline(ctx, albumID)
}

func (s *PlaybackFacade) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.playback().PinPlaylistOffline(ctx, playlistID, preferredProfile)
}

func (s *PlaybackFacade) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	return s.playback().UnpinPlaylistOffline(ctx, playlistID)
}

func (s *PlaybackFacade) PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.playback().PinLikedOffline(ctx, preferredProfile)
}

func (s *PlaybackFacade) UnpinLikedOffline(ctx context.Context) error {
	return s.playback().UnpinLikedOffline(ctx)
}

func (s *PlaybackFacade) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return s.playback().ListRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return s.playback().GetRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) ListRecordingPlaybackAvailability(ctx context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error) {
	return s.playback().ListRecordingPlaybackAvailability(ctx, req)
}

func (s *PlaybackFacade) ListAlbumAvailabilitySummaries(ctx context.Context, req apitypes.AlbumAvailabilitySummaryListRequest) ([]apitypes.AlbumAvailabilitySummaryItem, error) {
	return s.playback().ListAlbumAvailabilitySummaries(ctx, req)
}

func (s *PlaybackFacade) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return s.playback().GetRecordingAvailabilityOverview(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return s.playback().GetAlbumAvailabilityOverview(ctx, albumID, preferredProfile)
}
