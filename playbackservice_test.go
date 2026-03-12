package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/settings"
)

type passthroughBridgeStub struct {
	*desktopcore.UnavailableCore

	ensureLocalContextFn        func(context.Context) (apitypes.LocalContext, error)
	inspectFn                   func(context.Context) (apitypes.InspectSummary, error)
	inspectLibraryOplogFn       func(context.Context, string) (apitypes.LibraryOplogDiagnostics, error)
	activityStatusFn            func(context.Context) (apitypes.ActivityStatus, error)
	networkStatusFn             func() apitypes.NetworkStatus
	syncNowFn                   func(context.Context) error
	startSyncNowFn              func(context.Context) (desktopcore.JobSnapshot, error)
	connectPeerFn               func(context.Context, string) error
	checkpointStatusFn          func(context.Context) (apitypes.LibraryCheckpointStatus, error)
	publishCheckpointFn         func(context.Context) (apitypes.LibraryCheckpointManifest, error)
	startPublishCheckpointFn    func(context.Context) (desktopcore.JobSnapshot, error)
	compactCheckpointFn         func(context.Context, bool) (apitypes.CheckpointCompactionResult, error)
	startCompactCheckpointFn    func(context.Context, bool) (desktopcore.JobSnapshot, error)
	listJobsFn                  func(context.Context, string) ([]desktopcore.JobSnapshot, error)
	getJobFn                    func(context.Context, string) (desktopcore.JobSnapshot, bool, error)
	listLibrariesFn             func(context.Context) ([]apitypes.LibrarySummary, error)
	activeLibraryFn             func(context.Context) (apitypes.LibrarySummary, bool, error)
	createLibraryFn             func(context.Context, string) (apitypes.LibrarySummary, error)
	selectLibraryFn             func(context.Context, string) (apitypes.LibrarySummary, error)
	renameLibraryFn             func(context.Context, string, string) (apitypes.LibrarySummary, error)
	leaveLibraryFn              func(context.Context, string) error
	deleteLibraryFn             func(context.Context, string) error
	listLibraryMembersFn        func(context.Context) ([]apitypes.LibraryMemberStatus, error)
	updateLibraryMemberRoleFn   func(context.Context, string, string) error
	removeLibraryMemberFn       func(context.Context, string) error
	setScanRootsFn              func(context.Context, []string) error
	addScanRootsFn              func(context.Context, []string) ([]string, error)
	removeScanRootsFn           func(context.Context, []string) ([]string, error)
	scanRootsFn                 func(context.Context) ([]string, error)
	rescanNowFn                 func(context.Context) (apitypes.ScanStats, error)
	startRescanNowFn            func(context.Context) (desktopcore.JobSnapshot, error)
	rescanRootFn                func(context.Context, string) (apitypes.ScanStats, error)
	startRescanRootFn           func(context.Context, string) (desktopcore.JobSnapshot, error)
	listArtistsFn               func(context.Context, apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error)
	getArtistFn                 func(context.Context, string) (apitypes.ArtistListItem, error)
	listArtistAlbumsFn          func(context.Context, apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	listAlbumsFn                func(context.Context, apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	getAlbumFn                  func(context.Context, string) (apitypes.AlbumListItem, error)
	listRecordingsFn            func(context.Context, apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error)
	getRecordingFn              func(context.Context, string) (apitypes.RecordingListItem, error)
	listRecordingVariantsFn     func(context.Context, apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error)
	listAlbumVariantsFn         func(context.Context, apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error)
	setPreferredRecordingFn     func(context.Context, string, string) error
	setPreferredAlbumFn         func(context.Context, string, string) error
	listAlbumTracksFn           func(context.Context, apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error)
	listPlaylistsFn             func(context.Context, apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error)
	getPlaylistSummaryFn        func(context.Context, string) (apitypes.PlaylistListItem, error)
	listPlaylistTracksFn        func(context.Context, apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error)
	listLikedRecordingsFn       func(context.Context, apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error)
	createPlaylistFn            func(context.Context, string, string) (apitypes.PlaylistRecord, error)
	renamePlaylistFn            func(context.Context, string, string) (apitypes.PlaylistRecord, error)
	deletePlaylistFn            func(context.Context, string) error
	addPlaylistItemFn           func(context.Context, apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error)
	movePlaylistItemFn          func(context.Context, apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error)
	removePlaylistItemFn        func(context.Context, string, string) error
	likeRecordingFn             func(context.Context, string) error
	unlikeRecordingFn           func(context.Context, string) error
	isRecordingLikedFn          func(context.Context, string) (bool, error)
	createInviteCodeFn          func(context.Context, apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error)
	listIssuedInvitesFn         func(context.Context, string) ([]apitypes.IssuedInviteRecord, error)
	revokeIssuedInviteFn        func(context.Context, string, string) error
	startJoinFromInviteFn       func(context.Context, apitypes.JoinFromInviteInput) (apitypes.JoinSession, error)
	getJoinSessionFn            func(context.Context, string) (apitypes.JoinSession, error)
	finalizeJoinSessionFn       func(context.Context, string) (apitypes.JoinLibraryResult, error)
	startFinalizeJoinSessionFn  func(context.Context, string) (desktopcore.JobSnapshot, error)
	cancelJoinSessionFn         func(context.Context, string) error
	listJoinRequestsFn          func(context.Context, string) ([]apitypes.InviteJoinRequestRecord, error)
	approveJoinRequestFn        func(context.Context, string, string) error
	rejectJoinRequestFn         func(context.Context, string, string) error
	getCacheOverviewFn          func(context.Context) (apitypes.CacheOverview, error)
	listCacheEntriesFn          func(context.Context, apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error)
	cleanupCacheFn              func(context.Context, apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error)
	pinRecordingOfflineFn       func(context.Context, string, string) (apitypes.PlaybackRecordingResult, error)
	unpinRecordingOfflineFn     func(context.Context, string) error
	pinAlbumOfflineFn           func(context.Context, string, string) (apitypes.PlaybackBatchResult, error)
	unpinAlbumOfflineFn         func(context.Context, string) error
	pinPlaylistOfflineFn        func(context.Context, string, string) (apitypes.PlaybackBatchResult, error)
	unpinPlaylistOfflineFn      func(context.Context, string) error
	pinLikedOfflineFn           func(context.Context, string) (apitypes.PlaybackBatchResult, error)
	unpinLikedOfflineFn         func(context.Context) error
	inspectPlaybackRecordingFn  func(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error)
	preparePlaybackRecordingFn  func(context.Context, string, string, apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error)
	startPreparePlaybackFn      func(context.Context, string, string, apitypes.PlaybackPreparationPurpose) (desktopcore.JobSnapshot, error)
	getPlaybackPreparationFn    func(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error)
	resolvePlaybackRecordingFn  func(context.Context, string, string) (apitypes.PlaybackResolveResult, error)
	resolveArtworkRefFn         func(context.Context, apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error)
	resolveRecordingArtworkFn   func(context.Context, string, string) (apitypes.RecordingArtworkResult, error)
	listRecordingAvailabilityFn func(context.Context, string, string) ([]apitypes.RecordingAvailabilityItem, error)
	recordingAvailabilityOVFn   func(context.Context, string, string) (apitypes.RecordingAvailabilityOverview, error)
	getRecordingAvailabilityFn  func(context.Context, string, string) (apitypes.RecordingPlaybackAvailability, error)
	albumAvailabilityOVFn       func(context.Context, string, string) (apitypes.AlbumAvailabilityOverview, error)
}

func (b *passthroughBridgeStub) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return b.ensureLocalContextFn(ctx)
}

func (b *passthroughBridgeStub) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return b.inspectFn(ctx)
}

func (b *passthroughBridgeStub) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return b.inspectLibraryOplogFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	return b.activityStatusFn(ctx)
}

func (b *passthroughBridgeStub) NetworkStatus() apitypes.NetworkStatus {
	return b.networkStatusFn()
}

func (b *passthroughBridgeStub) SyncNow(ctx context.Context) error {
	return b.syncNowFn(ctx)
}

func (b *passthroughBridgeStub) StartSyncNow(ctx context.Context) (desktopcore.JobSnapshot, error) {
	return b.startSyncNowFn(ctx)
}

func (b *passthroughBridgeStub) ConnectPeer(ctx context.Context, peerAddr string) error {
	return b.connectPeerFn(ctx, peerAddr)
}

func (b *passthroughBridgeStub) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return b.checkpointStatusFn(ctx)
}

func (b *passthroughBridgeStub) PublishCheckpoint(ctx context.Context) (apitypes.LibraryCheckpointManifest, error) {
	return b.publishCheckpointFn(ctx)
}

func (b *passthroughBridgeStub) StartPublishCheckpoint(ctx context.Context) (desktopcore.JobSnapshot, error) {
	return b.startPublishCheckpointFn(ctx)
}

func (b *passthroughBridgeStub) CompactCheckpoint(ctx context.Context, force bool) (apitypes.CheckpointCompactionResult, error) {
	return b.compactCheckpointFn(ctx, force)
}

func (b *passthroughBridgeStub) StartCompactCheckpoint(ctx context.Context, force bool) (desktopcore.JobSnapshot, error) {
	return b.startCompactCheckpointFn(ctx, force)
}

func (b *passthroughBridgeStub) ListJobs(ctx context.Context, libraryID string) ([]desktopcore.JobSnapshot, error) {
	return b.listJobsFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) GetJob(ctx context.Context, jobID string) (desktopcore.JobSnapshot, bool, error) {
	return b.getJobFn(ctx, jobID)
}

func (b *passthroughBridgeStub) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return b.listLibrariesFn(ctx)
}

func (b *passthroughBridgeStub) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return b.activeLibraryFn(ctx)
}

func (b *passthroughBridgeStub) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return b.createLibraryFn(ctx, name)
}

func (b *passthroughBridgeStub) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return b.selectLibraryFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return b.renameLibraryFn(ctx, libraryID, name)
}

func (b *passthroughBridgeStub) LeaveLibrary(ctx context.Context, libraryID string) error {
	return b.leaveLibraryFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) DeleteLibrary(ctx context.Context, libraryID string) error {
	return b.deleteLibraryFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return b.listLibraryMembersFn(ctx)
}

func (b *passthroughBridgeStub) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return b.updateLibraryMemberRoleFn(ctx, deviceID, role)
}

func (b *passthroughBridgeStub) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return b.removeLibraryMemberFn(ctx, deviceID)
}

func (b *passthroughBridgeStub) SetScanRoots(ctx context.Context, roots []string) error {
	return b.setScanRootsFn(ctx, roots)
}

func (b *passthroughBridgeStub) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return b.addScanRootsFn(ctx, roots)
}

func (b *passthroughBridgeStub) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return b.removeScanRootsFn(ctx, roots)
}

func (b *passthroughBridgeStub) ScanRoots(ctx context.Context) ([]string, error) {
	return b.scanRootsFn(ctx)
}

func (b *passthroughBridgeStub) RescanNow(ctx context.Context) (apitypes.ScanStats, error) {
	return b.rescanNowFn(ctx)
}

func (b *passthroughBridgeStub) StartRescanNow(ctx context.Context) (desktopcore.JobSnapshot, error) {
	return b.startRescanNowFn(ctx)
}

func (b *passthroughBridgeStub) RescanRoot(ctx context.Context, root string) (apitypes.ScanStats, error) {
	return b.rescanRootFn(ctx, root)
}

func (b *passthroughBridgeStub) StartRescanRoot(ctx context.Context, root string) (desktopcore.JobSnapshot, error) {
	return b.startRescanRootFn(ctx, root)
}

func (b *passthroughBridgeStub) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return b.listArtistsFn(ctx, req)
}

func (b *passthroughBridgeStub) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return b.getArtistFn(ctx, artistID)
}

func (b *passthroughBridgeStub) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return b.listArtistAlbumsFn(ctx, req)
}

func (b *passthroughBridgeStub) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return b.listAlbumsFn(ctx, req)
}

func (b *passthroughBridgeStub) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return b.getAlbumFn(ctx, albumID)
}

func (b *passthroughBridgeStub) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return b.listRecordingsFn(ctx, req)
}

func (b *passthroughBridgeStub) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return b.getRecordingFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return b.listRecordingVariantsFn(ctx, req)
}

func (b *passthroughBridgeStub) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return b.listAlbumVariantsFn(ctx, req)
}

func (b *passthroughBridgeStub) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return b.setPreferredRecordingFn(ctx, recordingID, variantRecordingID)
}

func (b *passthroughBridgeStub) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return b.setPreferredAlbumFn(ctx, albumID, variantAlbumID)
}

func (b *passthroughBridgeStub) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return b.listAlbumTracksFn(ctx, req)
}

func (b *passthroughBridgeStub) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return b.listPlaylistsFn(ctx, req)
}

func (b *passthroughBridgeStub) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return b.getPlaylistSummaryFn(ctx, playlistID)
}

func (b *passthroughBridgeStub) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return b.listPlaylistTracksFn(ctx, req)
}

func (b *passthroughBridgeStub) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return b.listLikedRecordingsFn(ctx, req)
}

func (b *passthroughBridgeStub) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return b.createPlaylistFn(ctx, name, kind)
}

func (b *passthroughBridgeStub) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return b.renamePlaylistFn(ctx, playlistID, name)
}

func (b *passthroughBridgeStub) DeletePlaylist(ctx context.Context, playlistID string) error {
	return b.deletePlaylistFn(ctx, playlistID)
}

func (b *passthroughBridgeStub) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return b.addPlaylistItemFn(ctx, req)
}

func (b *passthroughBridgeStub) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return b.movePlaylistItemFn(ctx, req)
}

func (b *passthroughBridgeStub) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return b.removePlaylistItemFn(ctx, playlistID, itemID)
}

func (b *passthroughBridgeStub) LikeRecording(ctx context.Context, recordingID string) error {
	return b.likeRecordingFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) UnlikeRecording(ctx context.Context, recordingID string) error {
	return b.unlikeRecordingFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return b.isRecordingLikedFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	return b.createInviteCodeFn(ctx, req)
}

func (b *passthroughBridgeStub) ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error) {
	return b.listIssuedInvitesFn(ctx, status)
}

func (b *passthroughBridgeStub) RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error {
	return b.revokeIssuedInviteFn(ctx, inviteID, reason)
}

func (b *passthroughBridgeStub) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	return b.startJoinFromInviteFn(ctx, req)
}

func (b *passthroughBridgeStub) GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error) {
	return b.getJoinSessionFn(ctx, sessionID)
}

func (b *passthroughBridgeStub) FinalizeJoinSession(ctx context.Context, sessionID string) (apitypes.JoinLibraryResult, error) {
	return b.finalizeJoinSessionFn(ctx, sessionID)
}

func (b *passthroughBridgeStub) StartFinalizeJoinSession(ctx context.Context, sessionID string) (desktopcore.JobSnapshot, error) {
	return b.startFinalizeJoinSessionFn(ctx, sessionID)
}

func (b *passthroughBridgeStub) CancelJoinSession(ctx context.Context, sessionID string) error {
	return b.cancelJoinSessionFn(ctx, sessionID)
}

func (b *passthroughBridgeStub) ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error) {
	return b.listJoinRequestsFn(ctx, status)
}

func (b *passthroughBridgeStub) ApproveJoinRequest(ctx context.Context, requestID, role string) error {
	return b.approveJoinRequestFn(ctx, requestID, role)
}

func (b *passthroughBridgeStub) RejectJoinRequest(ctx context.Context, requestID, reason string) error {
	return b.rejectJoinRequestFn(ctx, requestID, reason)
}

func (b *passthroughBridgeStub) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return b.getCacheOverviewFn(ctx)
}

func (b *passthroughBridgeStub) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return b.listCacheEntriesFn(ctx, req)
}

func (b *passthroughBridgeStub) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return b.cleanupCacheFn(ctx, req)
}

func (b *passthroughBridgeStub) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return b.pinRecordingOfflineFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	return b.unpinRecordingOfflineFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return b.pinAlbumOfflineFn(ctx, albumID, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	return b.unpinAlbumOfflineFn(ctx, albumID)
}

func (b *passthroughBridgeStub) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return b.pinPlaylistOfflineFn(ctx, playlistID, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	return b.unpinPlaylistOfflineFn(ctx, playlistID)
}

func (b *passthroughBridgeStub) PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return b.pinLikedOfflineFn(ctx, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinLikedOffline(ctx context.Context) error {
	return b.unpinLikedOfflineFn(ctx)
}

func (b *passthroughBridgeStub) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.inspectPlaybackRecordingFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return b.preparePlaybackRecordingFn(ctx, recordingID, preferredProfile, purpose)
}

func (b *passthroughBridgeStub) StartPreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (desktopcore.JobSnapshot, error) {
	return b.startPreparePlaybackFn(ctx, recordingID, preferredProfile, purpose)
}

func (b *passthroughBridgeStub) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.getPlaybackPreparationFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return b.resolvePlaybackRecordingFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	return b.resolveArtworkRefFn(ctx, artwork)
}

func (b *passthroughBridgeStub) ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
	return b.resolveRecordingArtworkFn(ctx, recordingID, variant)
}

func (b *passthroughBridgeStub) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return b.listRecordingAvailabilityFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return b.recordingAvailabilityOVFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return b.getRecordingAvailabilityFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return b.albumAvailabilityOVFn(ctx, albumID, preferredProfile)
}

func TestPreferredProfileDefaultsToSupportedAACProfile(t *testing.T) {
	t.Parallel()

	if got := preferredProfile(settings.CoreRuntimeSettings{}); got != settings.DefaultTranscodeProfile {
		t.Fatalf("preferred profile = %q, want %q", got, settings.DefaultTranscodeProfile)
	}
}

func TestPreferredProfileNormalizesLegacyDesktopProfile(t *testing.T) {
	t.Parallel()

	got := preferredProfile(settings.CoreRuntimeSettings{TranscodeProfile: " desktop "})
	if got != settings.DefaultTranscodeProfile {
		t.Fatalf("preferred profile = %q, want %q", got, settings.DefaultTranscodeProfile)
	}
}

func TestResolvedBlobRootUsesCoreDefaultsWhenSettingsEmpty(t *testing.T) {
	t.Parallel()

	configRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}

	got := resolvedBlobRoot(settings.CoreRuntimeSettings{})
	want := filepath.Join(configRoot, "ben", "v2", "blobs")
	if got != want {
		t.Fatalf("resolved blob root = %q, want %q", got, want)
	}
}
