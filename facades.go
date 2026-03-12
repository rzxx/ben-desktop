package main

import (
	"context"
	"fmt"
	"strings"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/desktopcore"
)

type facadeBase struct {
	host *coreHost
}

func (f facadeBase) bridge() hostBridge {
	if f.host == nil {
		return desktopcore.NewUnavailableCore(fmt.Errorf("core bridge is not available"))
	}
	return f.host.Bridge()
}

func (f facadeBase) blobRoot() string {
	if f.host == nil {
		return ""
	}
	return f.host.BlobRoot()
}

type LibraryFacade struct {
	facadeBase
}

func NewLibraryFacade(host *coreHost) *LibraryFacade {
	return &LibraryFacade{facadeBase: facadeBase{host: host}}
}

func (s *LibraryFacade) ServiceName() string { return "LibraryFacade" }

func (s *LibraryFacade) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return s.bridge().ListLibraries(ctx)
}

func (s *LibraryFacade) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return s.bridge().ActiveLibrary(ctx)
}

func (s *LibraryFacade) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return s.bridge().CreateLibrary(ctx, name)
}

func (s *LibraryFacade) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return s.bridge().SelectLibrary(ctx, libraryID)
}

func (s *LibraryFacade) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return s.bridge().RenameLibrary(ctx, libraryID, name)
}

func (s *LibraryFacade) LeaveLibrary(ctx context.Context, libraryID string) error {
	return s.bridge().LeaveLibrary(ctx, libraryID)
}

func (s *LibraryFacade) DeleteLibrary(ctx context.Context, libraryID string) error {
	return s.bridge().DeleteLibrary(ctx, libraryID)
}

func (s *LibraryFacade) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return s.bridge().ListLibraryMembers(ctx)
}

func (s *LibraryFacade) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return s.bridge().UpdateLibraryMemberRole(ctx, deviceID, role)
}

func (s *LibraryFacade) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return s.bridge().RemoveLibraryMember(ctx, deviceID)
}

func (s *LibraryFacade) SetScanRoots(ctx context.Context, roots []string) error {
	return s.bridge().SetScanRoots(ctx, roots)
}

func (s *LibraryFacade) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return s.bridge().AddScanRoots(ctx, roots)
}

func (s *LibraryFacade) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return s.bridge().RemoveScanRoots(ctx, roots)
}

func (s *LibraryFacade) ScanRoots(ctx context.Context) ([]string, error) {
	return s.bridge().ScanRoots(ctx)
}

func (s *LibraryFacade) RescanNow(ctx context.Context) (apitypes.ScanStats, error) {
	return s.bridge().RescanNow(ctx)
}

func (s *LibraryFacade) RescanRoot(ctx context.Context, root string) (apitypes.ScanStats, error) {
	return s.bridge().RescanRoot(ctx, root)
}

type NetworkFacade struct {
	facadeBase
}

func NewNetworkFacade(host *coreHost) *NetworkFacade {
	return &NetworkFacade{facadeBase: facadeBase{host: host}}
}

func (s *NetworkFacade) ServiceName() string { return "NetworkFacade" }

func (s *NetworkFacade) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return s.bridge().EnsureLocalContext(ctx)
}

func (s *NetworkFacade) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return s.bridge().Inspect(ctx)
}

func (s *NetworkFacade) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return s.bridge().InspectLibraryOplog(ctx, libraryID)
}

func (s *NetworkFacade) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	return s.bridge().ActivityStatus(ctx)
}

func (s *NetworkFacade) NetworkStatus() apitypes.NetworkStatus {
	return s.bridge().NetworkStatus()
}

func (s *NetworkFacade) SyncNow(ctx context.Context) error {
	return s.bridge().SyncNow(ctx)
}

func (s *NetworkFacade) ConnectPeer(ctx context.Context, peerAddr string) error {
	return s.bridge().ConnectPeer(ctx, peerAddr)
}

func (s *NetworkFacade) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return s.bridge().CheckpointStatus(ctx)
}

func (s *NetworkFacade) PublishCheckpoint(ctx context.Context) (apitypes.LibraryCheckpointManifest, error) {
	return s.bridge().PublishCheckpoint(ctx)
}

func (s *NetworkFacade) CompactCheckpoint(ctx context.Context, force bool) (apitypes.CheckpointCompactionResult, error) {
	return s.bridge().CompactCheckpoint(ctx, force)
}

type JobsFacade struct {
	facadeBase
}

func NewJobsFacade(host *coreHost) *JobsFacade {
	return &JobsFacade{facadeBase: facadeBase{host: host}}
}

func (s *JobsFacade) ServiceName() string { return "JobsFacade" }

func (s *JobsFacade) ListJobs(ctx context.Context, libraryID string) ([]desktopcore.JobSnapshot, error) {
	return s.bridge().ListJobs(ctx, libraryID)
}

func (s *JobsFacade) GetJob(ctx context.Context, jobID string) (desktopcore.JobSnapshot, bool, error) {
	return s.bridge().GetJob(ctx, jobID)
}

type CatalogFacade struct {
	facadeBase
}

func NewCatalogFacade(host *coreHost) *CatalogFacade {
	return &CatalogFacade{facadeBase: facadeBase{host: host}}
}

func (s *CatalogFacade) ServiceName() string { return "CatalogFacade" }

func (s *CatalogFacade) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return s.bridge().ListArtists(ctx, req)
}

func (s *CatalogFacade) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return s.bridge().GetArtist(ctx, artistID)
}

func (s *CatalogFacade) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return s.bridge().ListArtistAlbums(ctx, req)
}

func (s *CatalogFacade) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return s.bridge().ListAlbums(ctx, req)
}

func (s *CatalogFacade) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return s.bridge().GetAlbum(ctx, albumID)
}

func (s *CatalogFacade) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return s.bridge().ListRecordings(ctx, req)
}

func (s *CatalogFacade) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return s.bridge().GetRecording(ctx, recordingID)
}

func (s *CatalogFacade) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return s.bridge().ListRecordingVariants(ctx, req)
}

func (s *CatalogFacade) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return s.bridge().ListAlbumVariants(ctx, req)
}

func (s *CatalogFacade) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return s.bridge().SetPreferredRecordingVariant(ctx, recordingID, variantRecordingID)
}

func (s *CatalogFacade) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return s.bridge().SetPreferredAlbumVariant(ctx, albumID, variantAlbumID)
}

func (s *CatalogFacade) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return s.bridge().ListAlbumTracks(ctx, req)
}

func (s *CatalogFacade) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return s.bridge().ListPlaylists(ctx, req)
}

func (s *CatalogFacade) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return s.bridge().GetPlaylistSummary(ctx, playlistID)
}

func (s *CatalogFacade) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return s.bridge().ListPlaylistTracks(ctx, req)
}

func (s *CatalogFacade) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return s.bridge().ListLikedRecordings(ctx, req)
}

func (s *CatalogFacade) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return s.bridge().CreatePlaylist(ctx, name, kind)
}

func (s *CatalogFacade) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return s.bridge().RenamePlaylist(ctx, playlistID, name)
}

func (s *CatalogFacade) DeletePlaylist(ctx context.Context, playlistID string) error {
	return s.bridge().DeletePlaylist(ctx, playlistID)
}

func (s *CatalogFacade) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return s.bridge().AddPlaylistItem(ctx, req)
}

func (s *CatalogFacade) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return s.bridge().MovePlaylistItem(ctx, req)
}

func (s *CatalogFacade) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return s.bridge().RemovePlaylistItem(ctx, playlistID, itemID)
}

func (s *CatalogFacade) LikeRecording(ctx context.Context, recordingID string) error {
	return s.bridge().LikeRecording(ctx, recordingID)
}

func (s *CatalogFacade) UnlikeRecording(ctx context.Context, recordingID string) error {
	return s.bridge().UnlikeRecording(ctx, recordingID)
}

func (s *CatalogFacade) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return s.bridge().IsRecordingLiked(ctx, recordingID)
}

type InviteFacade struct {
	facadeBase
}

func NewInviteFacade(host *coreHost) *InviteFacade {
	return &InviteFacade{facadeBase: facadeBase{host: host}}
}

func (s *InviteFacade) ServiceName() string { return "InviteFacade" }

func (s *InviteFacade) CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	return s.bridge().CreateInviteCode(ctx, req)
}

func (s *InviteFacade) ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error) {
	return s.bridge().ListIssuedInvites(ctx, status)
}

func (s *InviteFacade) RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error {
	return s.bridge().RevokeIssuedInvite(ctx, inviteID, reason)
}

func (s *InviteFacade) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	return s.bridge().StartJoinFromInvite(ctx, req)
}

func (s *InviteFacade) GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error) {
	return s.bridge().GetJoinSession(ctx, sessionID)
}

func (s *InviteFacade) FinalizeJoinSession(ctx context.Context, sessionID string) (apitypes.JoinLibraryResult, error) {
	return s.bridge().FinalizeJoinSession(ctx, sessionID)
}

func (s *InviteFacade) CancelJoinSession(ctx context.Context, sessionID string) error {
	return s.bridge().CancelJoinSession(ctx, sessionID)
}

func (s *InviteFacade) ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error) {
	return s.bridge().ListJoinRequests(ctx, status)
}

func (s *InviteFacade) ApproveJoinRequest(ctx context.Context, requestID, role string) error {
	return s.bridge().ApproveJoinRequest(ctx, requestID, role)
}

func (s *InviteFacade) RejectJoinRequest(ctx context.Context, requestID, reason string) error {
	return s.bridge().RejectJoinRequest(ctx, requestID, reason)
}

type CacheFacade struct {
	facadeBase
}

func NewCacheFacade(host *coreHost) *CacheFacade {
	return &CacheFacade{facadeBase: facadeBase{host: host}}
}

func (s *CacheFacade) ServiceName() string { return "CacheFacade" }

func (s *CacheFacade) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return s.bridge().GetCacheOverview(ctx)
}

func (s *CacheFacade) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return s.bridge().ListCacheEntries(ctx, req)
}

func (s *CacheFacade) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return s.bridge().CleanupCache(ctx, req)
}

type PlaybackFacade struct {
	facadeBase
}

func NewPlaybackFacade(host *coreHost) *PlaybackFacade {
	return &PlaybackFacade{facadeBase: facadeBase{host: host}}
}

func (s *PlaybackFacade) ServiceName() string { return "PlaybackFacade" }

func (s *PlaybackFacade) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.bridge().InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return s.bridge().PreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (s *PlaybackFacade) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.bridge().GetPlaybackPreparation(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return s.bridge().ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) ResolveBlobURL(blobID string) (string, error) {
	path, ok, err := blobPathForID(s.blobRoot(), blobID)
	if err != nil || !ok {
		return "", err
	}
	return fileURLFromPath(path)
}

func (s *PlaybackFacade) ResolveThumbnailURL(artwork apitypes.ArtworkRef) (string, error) {
	artwork.BlobID = strings.TrimSpace(artwork.BlobID)
	if artwork.BlobID == "" {
		return "", nil
	}

	resolved, err := s.bridge().ResolveArtworkRef(context.Background(), artwork)
	if err != nil {
		return "", err
	}
	if !resolved.Available || strings.TrimSpace(resolved.LocalPath) == "" {
		return "", nil
	}

	fileExt := normalizeArtworkFileExt(strings.TrimSpace(resolved.Artwork.FileExt), strings.TrimSpace(resolved.Artwork.MIME))
	if fileExt == "" {
		return "", fmt.Errorf("thumbnail file extension is required")
	}
	aliasPath, err := ensureTypedBlobAlias(resolved.LocalPath, fileExt)
	if err != nil {
		return "", err
	}
	return fileURLFromPath(aliasPath)
}

func (s *PlaybackFacade) ResolveRecordingArtworkURL(ctx context.Context, recordingID, variant string) (string, error) {
	result, err := s.bridge().ResolveRecordingArtwork(ctx, recordingID, variant)
	if err != nil {
		return "", err
	}
	return s.ResolveThumbnailURL(result.Artwork)
}

func (s *PlaybackFacade) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return s.bridge().PinRecordingOffline(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	return s.bridge().UnpinRecordingOffline(ctx, recordingID)
}

func (s *PlaybackFacade) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.bridge().PinAlbumOffline(ctx, albumID, preferredProfile)
}

func (s *PlaybackFacade) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	return s.bridge().UnpinAlbumOffline(ctx, albumID)
}

func (s *PlaybackFacade) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.bridge().PinPlaylistOffline(ctx, playlistID, preferredProfile)
}

func (s *PlaybackFacade) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	return s.bridge().UnpinPlaylistOffline(ctx, playlistID)
}

func (s *PlaybackFacade) PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return s.bridge().PinLikedOffline(ctx, preferredProfile)
}

func (s *PlaybackFacade) UnpinLikedOffline(ctx context.Context) error {
	return s.bridge().UnpinLikedOffline(ctx)
}

func (s *PlaybackFacade) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return s.bridge().ListRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return s.bridge().GetRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return s.bridge().GetRecordingAvailabilityOverview(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return s.bridge().GetAlbumAvailabilityOverview(ctx, albumID, preferredProfile)
}
