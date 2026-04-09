package desktopcore

import (
	"context"
	"fmt"
	"os"
	"sync"

	apitypes "ben/desktop/api/types"
	playbackcore "ben/desktop/internal/playback"
	"gorm.io/gorm"
)

type App struct {
	cfg        Config
	db         *gorm.DB
	storage    *DBService
	blobs      *BlobStoreService
	identity   *IdentityMembershipService
	operator   *OperatorService
	sync       *SyncService
	checkpoint *CheckpointService
	scanner    *ScannerService

	activityMu                            sync.RWMutex
	activity                              apitypes.ActivityStatus
	tagReader                             TagReader
	rebuildCatalogMaterializationFullHook func()

	activitySubscribers    map[uint64]func(apitypes.ActivityStatus)
	nextActivitySubscriber uint64

	runtimeMu     sync.Mutex
	activeRuntime *activeLibraryRuntime
	transportMu   sync.RWMutex
	transport     SyncTransport

	jobs              *JobsService
	catalogEvents     *CatalogEventsService
	pinEvents         *PinEventsService
	library           *LibraryService
	ingest            *IngestService
	catalog           *CatalogService
	cache             *CacheService
	pin               *PinService
	transcode         *TranscodeService
	artwork           *ArtworkService
	playlist          *PlaylistService
	playback          *PlaybackService
	invite            *InviteService
	transportService  *TransportService
	transcodeActivity map[string]apitypes.TranscodeActivityStatus
}

func Open(ctx context.Context, cfg Config) (*App, error) {
	resolved, err := ResolveConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved.BlobRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}

	storage, err := OpenDBService(resolved.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	openOK := false
	defer func() {
		if !openOK {
			_ = storage.Close()
		}
	}()
	db := storage.DB()

	app := &App{
		cfg:                 resolved,
		db:                  db,
		storage:             storage,
		blobs:               NewBlobStoreService(resolved.BlobRoot),
		activity:            newActivityStatus(),
		jobs:                NewJobsService(),
		catalogEvents:       NewCatalogEventsService(),
		pinEvents:           NewPinEventsService(),
		activitySubscribers: make(map[uint64]func(apitypes.ActivityStatus)),
		tagReader: func() TagReader {
			if resolved.TagReader != nil {
				return resolved.TagReader
			}
			return NewTagReader()
		}(),
	}
	app.identity = newIdentityMembershipService(app)
	app.operator = newOperatorService(app)
	app.sync = newSyncService(app)
	app.checkpoint = newCheckpointService(app)
	app.scanner = newScannerService(app)
	app.library = &LibraryService{app: app}
	app.ingest = &IngestService{app: app}
	app.catalog = &CatalogService{app: app}
	app.cache = &CacheService{app: app}
	app.pin = newPinService(app)
	app.transcode = newTranscodeService(app)
	app.artwork = newArtworkService(app)
	app.playlist = &PlaylistService{app: app}
	app.playback = newPlaybackService(app)
	app.invite = &InviteService{app: app}
	app.transportService = newTransportService(app)

	if _, err := app.ensureCurrentDevice(ctx); err != nil {
		return nil, fmt.Errorf("ensure current device: %w", err)
	}
	if err := app.runPathPrivacyMigration(ctx); err != nil {
		return nil, fmt.Errorf("run path privacy migration: %w", err)
	}
	if err := app.runContextIdentityMigration(ctx); err != nil {
		return nil, fmt.Errorf("run context identity migration: %w", err)
	}
	if err := app.runCatalogMaterializationMigration(ctx); err != nil {
		return nil, fmt.Errorf("run catalog materialization migration: %w", err)
	}
	if err := runPinStorageMigration(app.db); err != nil {
		return nil, fmt.Errorf("run pin storage migration: %w", err)
	}
	if err := app.syncActiveRuntimeServices(ctx); err != nil {
		return nil, fmt.Errorf("configure active runtime services: %w", err)
	}

	openOK = true
	return app, nil
}

func (a *App) Close() error {
	if a == nil || a.storage == nil {
		return nil
	}
	a.clearActiveLibraryRuntime()
	return a.storage.Close()
}

func (a *App) BlobRoot() string {
	if a == nil {
		return ""
	}
	return a.cfg.BlobRoot
}

func (a *App) Jobs() *JobsService {
	if a == nil {
		return nil
	}
	return a.jobs
}

func (a *App) SubscribeJobSnapshots(listener func(JobSnapshot)) func() {
	if a == nil || a.jobs == nil {
		return func() {}
	}
	return a.jobs.Subscribe(listener)
}

func (a *App) SubscribeCatalogChanges(listener func(apitypes.CatalogChangeEvent)) func() {
	if a == nil || a.catalogEvents == nil {
		return func() {}
	}
	return a.catalogEvents.Subscribe(listener)
}

func (a *App) SubscribePinChanges(listener func(apitypes.PinChangeEvent)) func() {
	if a == nil || a.pinEvents == nil {
		return func() {}
	}
	return a.pinEvents.Subscribe(listener)
}

func (a *App) emitCatalogChange(event apitypes.CatalogChangeEvent) {
	if a == nil || a.catalogEvents == nil {
		return
	}
	a.catalogEvents.Emit(event)
}

func (a *App) emitPinChange(event apitypes.PinChangeEvent) {
	if a == nil || a.pinEvents == nil {
		return
	}
	a.pinEvents.Emit(event)
}

func (a *App) ListJobs(_ context.Context, libraryID string) ([]JobSnapshot, error) {
	if a == nil {
		return nil, nil
	}
	return a.jobs.List(libraryID), nil
}

func (a *App) GetJob(_ context.Context, jobID string) (JobSnapshot, bool, error) {
	if a == nil {
		return JobSnapshot{}, false, nil
	}
	job, ok := a.jobs.Get(jobID)
	return job, ok, nil
}

func (a *App) Config() Config {
	if a == nil {
		return Config{}
	}
	return a.cfg
}

func (a *App) LibraryRuntime() LibraryRuntime {
	if a == nil {
		return nil
	}
	return newLibraryRuntimeAdapter(a.library, a.ingest)
}

func (a *App) NetworkRuntime() NetworkRuntime {
	if a == nil {
		return nil
	}
	return newNetworkRuntimeAdapter(a.operator, a.sync, a.checkpoint)
}

func (a *App) CatalogRuntime() CatalogRuntime {
	if a == nil {
		return nil
	}
	return newCatalogRuntimeAdapter(a.catalog, a.playlist)
}

func (a *App) PinRuntime() PinRuntime {
	if a == nil {
		return nil
	}
	return a.pin
}

func (a *App) InviteRuntime() InviteRuntime {
	if a == nil {
		return nil
	}
	return a.invite
}

func (a *App) CacheRuntime() CacheRuntime {
	if a == nil {
		return nil
	}
	return a.cache
}

func (a *App) PlaybackRuntime() PlaybackRuntime {
	if a == nil {
		return nil
	}
	return a.playback
}

func (a *App) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return a.operator.EnsureLocalContext(ctx)
}

func (a *App) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return a.operator.Inspect(ctx)
}

func (a *App) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return a.operator.InspectLibraryOplog(ctx, libraryID)
}

func (a *App) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	return a.operator.ActivityStatus(ctx)
}

func (a *App) NetworkStatus() apitypes.NetworkStatus {
	return a.operator.NetworkStatus()
}

func (a *App) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return a.operator.CheckpointStatus(ctx)
}

func (a *App) SetSyncTransport(transport SyncTransport) {
	a.sync.SetSyncTransport(transport)
}

func (a *App) SyncNow(ctx context.Context) error {
	return a.sync.SyncNow(ctx)
}

func (a *App) StartSyncNow(ctx context.Context) (JobSnapshot, error) {
	return a.sync.StartSyncNow(ctx)
}

func (a *App) ConnectPeer(ctx context.Context, peerAddr string) error {
	return a.sync.ConnectPeer(ctx, peerAddr)
}

func (a *App) StartConnectPeer(ctx context.Context, peerAddr string) (JobSnapshot, error) {
	return a.sync.StartConnectPeer(ctx, peerAddr)
}

func (a *App) catchupAllPeers(ctx context.Context, local apitypes.LocalContext, reason apitypes.NetworkSyncReason, job *JobTracker, failIfNoPeers bool) error {
	return a.sync.catchupAllPeers(ctx, local, reason, job, failIfNoPeers)
}

func (a *App) syncPeerCatchup(ctx context.Context, local apitypes.LocalContext, peer SyncPeer, reason apitypes.NetworkSyncReason, job *JobTracker) (int, error) {
	return a.sync.syncPeerCatchup(ctx, local, peer, reason, job)
}

func (a *App) buildSyncRequest(ctx context.Context, libraryID, deviceID, peerID string, maxOps int) (SyncRequest, error) {
	return a.sync.buildSyncRequest(ctx, libraryID, deviceID, peerID, maxOps)
}

func (a *App) buildSyncResponse(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	return a.sync.buildSyncResponse(ctx, req)
}

func (a *App) buildCheckpointFetchResponse(ctx context.Context, req CheckpointFetchRequest) (CheckpointFetchResponse, error) {
	return a.sync.buildCheckpointFetchResponse(ctx, req)
}

func (a *App) buildLibraryChangedResponse(ctx context.Context, libraryID, deviceID, peerID string) (LibraryChangedResponse, error) {
	return a.sync.buildLibraryChangedResponse(ctx, libraryID, deviceID, peerID)
}

func (a *App) installCheckpointRecord(ctx context.Context, localDeviceID string, record checkpointTransferRecord) (int, error) {
	return a.sync.installCheckpointRecord(ctx, localDeviceID, record)
}

func (a *App) applyRemoteOps(ctx context.Context, libraryID string, ops []checkpointOplogEntry) (int, error) {
	return a.sync.applyRemoteOps(ctx, libraryID, ops)
}

func (a *App) ensureLocalPeerContext(ctx context.Context, local apitypes.LocalContext) (apitypes.LocalContext, error) {
	return a.sync.ensureLocalPeerContext(ctx, local)
}

func (a *App) isLibraryMember(ctx context.Context, libraryID, deviceID string) bool {
	return a.sync.isLibraryMember(ctx, libraryID, deviceID)
}

func (a *App) listDeviceClocks(ctx context.Context, libraryID string) ([]DeviceClock, error) {
	return a.sync.listDeviceClocks(ctx, libraryID)
}

func (a *App) loadCheckpointTransferRecord(ctx context.Context, libraryID, checkpointID string, publishedOnly bool) (checkpointTransferRecord, bool, error) {
	return a.sync.loadCheckpointTransferRecord(ctx, libraryID, checkpointID, publishedOnly)
}

func (a *App) PublishCheckpoint(ctx context.Context) (apitypes.LibraryCheckpointManifest, error) {
	return a.checkpoint.PublishCheckpoint(ctx)
}

func (a *App) StartPublishCheckpoint(ctx context.Context) (JobSnapshot, error) {
	return a.checkpoint.StartPublishCheckpoint(ctx)
}

func (a *App) CompactCheckpoint(ctx context.Context, force bool) (apitypes.CheckpointCompactionResult, error) {
	return a.checkpoint.CompactCheckpoint(ctx, force)
}

func (a *App) StartCompactCheckpoint(ctx context.Context, force bool) (JobSnapshot, error) {
	return a.checkpoint.StartCompactCheckpoint(ctx, force)
}

func (a *App) backgroundCheckpointMaintenance(ctx context.Context, libraryID string) error {
	return a.checkpoint.backgroundCheckpointMaintenance(ctx, libraryID)
}

func (a *App) loadMembershipCert(ctx context.Context, libraryID, deviceID string) (MembershipCert, bool, error) {
	return a.identity.loadMembershipCert(ctx, libraryID, deviceID)
}

func (a *App) loadAdmissionAuthorityChain(ctx context.Context, libraryID string) ([]AdmissionAuthority, error) {
	return a.identity.loadAdmissionAuthorityChain(ctx, libraryID)
}

func (a *App) membershipCertRevoked(ctx context.Context, libraryID, deviceID string, serial int64) (bool, error) {
	return a.identity.membershipCertRevoked(ctx, libraryID, deviceID, serial)
}

func (a *App) transportIdentityPeerID() (string, error) {
	return a.identity.transportIdentityPeerID()
}

func (a *App) ensureLocalTransportMembershipAuth(ctx context.Context, local apitypes.LocalContext, transportPeerID string) (transportPeerAuth, error) {
	return a.identity.ensureLocalTransportMembershipAuth(ctx, local, transportPeerID)
}

func (a *App) verifyTransportPeerAuth(ctx context.Context, libraryID, claimedDeviceID, claimedPeerID, actualPeerID string, auth transportPeerAuth) (membershipCertEnvelope, error) {
	return a.identity.verifyTransportPeerAuth(ctx, libraryID, claimedDeviceID, claimedPeerID, actualPeerID, auth)
}

func (a *App) localMembershipRecoverySecret(ctx context.Context, libraryID, deviceID string) (string, bool, error) {
	return a.identity.localMembershipRecoverySecret(ctx, libraryID, deviceID)
}

func (a *App) buildMembershipRefreshResponse(ctx context.Context, req MembershipRefreshRequest) (MembershipRefreshResponse, error) {
	return a.identity.buildMembershipRefreshResponse(ctx, req)
}

func (a *App) handleInviteJoinStart(ctx context.Context, libraryID, localPeerID, actualPeerID string, req inviteJoinStartRequest) (inviteJoinStartResponse, error) {
	return a.invite.handleInviteJoinStart(ctx, libraryID, localPeerID, actualPeerID, req)
}

func (a *App) handleInviteJoinStatus(ctx context.Context, libraryID, localPeerID, actualPeerID string, req inviteJoinStatusRequest) (inviteJoinStatusResponse, error) {
	return a.invite.handleInviteJoinStatus(ctx, libraryID, localPeerID, actualPeerID, req)
}

func (a *App) handleInviteJoinCancel(ctx context.Context, libraryID, actualPeerID string, req inviteJoinCancelRequest) (inviteJoinCancelResponse, error) {
	return a.invite.handleInviteJoinCancel(ctx, libraryID, actualPeerID, req)
}

func (a *App) syncActiveScanWatcher(ctx context.Context) error {
	return a.scanner.syncActiveScanWatcher(ctx)
}

func (a *App) logf(format string, args ...any) {
	if a == nil || a.cfg.Logger == nil {
		return
	}
	a.cfg.Logger.Printf(format, args...)
}

func (a *App) syncActiveRuntimeServices(ctx context.Context) error {
	if a == nil {
		return nil
	}
	local, runtime, ok, err := a.syncActiveLibraryRuntimeState(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if a.transportService != nil {
		if err := a.transportService.syncRuntime(ctx, local, runtime); err != nil {
			return err
		}
	}
	if a.scanner != nil {
		if err := a.scanner.syncRuntime(ctx, local, runtime); err != nil {
			return err
		}
	}
	if a.pin != nil {
		a.pin.scheduleAllPinScopeRefresh(ctx, local, pinnedScopeDebounceWait)
	}
	return nil
}

func (a *App) hasTransportOverride() bool {
	if a == nil || a.transportService == nil {
		return false
	}
	return a.transportService.hasTransportOverride()
}

func (a *App) activeSyncTransport() SyncTransport {
	if a == nil || a.transportService == nil {
		return nil
	}
	return a.transportService.activeSyncTransport()
}

func (a *App) transportRunning() bool {
	if a == nil || a.transportService == nil {
		return false
	}
	return a.transportService.transportRunning()
}

func (a *App) updateDevicePeerID(ctx context.Context, libraryID, deviceID, peerID, deviceName string) error {
	if a == nil || a.transportService == nil {
		return nil
	}
	return a.transportService.updateDevicePeerID(ctx, libraryID, deviceID, peerID, deviceName)
}

func (a *App) touchDevicePeerID(ctx context.Context, deviceID, peerID, deviceName string) error {
	if a == nil || a.transportService == nil {
		return nil
	}
	return a.transportService.touchDevicePeerID(ctx, deviceID, peerID, deviceName)
}

func (a *App) markDevicePresenceOffline(ctx context.Context, libraryID, peerID string) error {
	if a == nil || a.transportService == nil {
		return nil
	}
	return a.transportService.markDevicePresenceOffline(ctx, libraryID, peerID)
}

func (a *App) memberDeviceIDForPeer(ctx context.Context, libraryID, peerID string) (string, bool, error) {
	if a == nil || a.transportService == nil {
		return "", false, nil
	}
	return a.transportService.memberDeviceIDForPeer(ctx, libraryID, peerID)
}

func (a *App) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return a.library.ListLibraries(ctx)
}

func (a *App) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return a.library.ActiveLibrary(ctx)
}

func (a *App) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return a.library.CreateLibrary(ctx, name)
}

func (a *App) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return a.library.SelectLibrary(ctx, libraryID)
}

func (a *App) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return a.library.RenameLibrary(ctx, libraryID, name)
}

func (a *App) LeaveLibrary(ctx context.Context, libraryID string) error {
	return a.library.LeaveLibrary(ctx, libraryID)
}

func (a *App) DeleteLibrary(ctx context.Context, libraryID string) error {
	return a.library.DeleteLibrary(ctx, libraryID)
}

func (a *App) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return a.library.ListLibraryMembers(ctx)
}

func (a *App) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return a.library.UpdateLibraryMemberRole(ctx, deviceID, role)
}

func (a *App) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return a.library.RemoveLibraryMember(ctx, deviceID)
}

func (a *App) SetScanRoots(ctx context.Context, roots []string) error {
	return a.ingest.SetScanRoots(ctx, roots)
}

func (a *App) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return a.ingest.AddScanRoots(ctx, roots)
}

func (a *App) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return a.ingest.RemoveScanRoots(ctx, roots)
}

func (a *App) ScanRoots(ctx context.Context) ([]string, error) {
	return a.ingest.ScanRoots(ctx)
}

func (a *App) RepairLibrary(ctx context.Context) (apitypes.ScanStats, error) {
	return a.ingest.RepairLibrary(ctx)
}

func (a *App) StartRepairLibrary(ctx context.Context) (JobSnapshot, error) {
	return a.ingest.StartRepairLibrary(ctx)
}

func (a *App) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return a.catalog.ListArtists(ctx, req)
}

func (a *App) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return a.catalog.GetArtist(ctx, artistID)
}

func (a *App) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return a.catalog.ListArtistAlbums(ctx, req)
}

func (a *App) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return a.catalog.ListAlbums(ctx, req)
}

func (a *App) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return a.catalog.GetAlbum(ctx, albumID)
}

func (a *App) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return a.catalog.ListRecordings(ctx, req)
}

func (a *App) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return a.catalog.GetRecording(ctx, recordingID)
}

func (a *App) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return a.catalog.ListRecordingVariants(ctx, req)
}

func (a *App) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return a.catalog.ListAlbumVariants(ctx, req)
}

func (a *App) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return a.catalog.SetPreferredRecordingVariant(ctx, recordingID, variantRecordingID)
}

func (a *App) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return a.catalog.SetPreferredAlbumVariant(ctx, albumID, variantAlbumID)
}

func (a *App) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return a.catalog.ListAlbumTracks(ctx, req)
}

func (a *App) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return a.catalog.ListPlaylists(ctx, req)
}

func (a *App) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return a.catalog.GetPlaylistSummary(ctx, playlistID)
}

func (a *App) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return a.catalog.ListPlaylistTracks(ctx, req)
}

func (a *App) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return a.catalog.ListLikedRecordings(ctx, req)
}

func (a *App) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return a.playlist.CreatePlaylist(ctx, name, kind)
}

func (a *App) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return a.cache.GetCacheOverview(ctx)
}

func (a *App) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return a.cache.ListCacheEntries(ctx, req)
}

func (a *App) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return a.cache.CleanupCache(ctx, req)
}

func (a *App) StartPin(ctx context.Context, req apitypes.PinIntentRequest) (JobSnapshot, error) {
	return a.pin.StartPin(ctx, req)
}

func (a *App) Unpin(ctx context.Context, req apitypes.PinIntentRequest) error {
	return a.pin.Unpin(ctx, req)
}

func (a *App) ListPinStates(ctx context.Context, req apitypes.PinStateListRequest) ([]apitypes.PinState, error) {
	return a.pin.ListPinStates(ctx, req)
}

func (a *App) GetPinState(ctx context.Context, req apitypes.PinStateRequest) (apitypes.PinState, error) {
	return a.pin.GetPinState(ctx, req)
}

func (a *App) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return a.playlist.RenamePlaylist(ctx, playlistID, name)
}

func (a *App) DeletePlaylist(ctx context.Context, playlistID string) error {
	return a.playlist.DeletePlaylist(ctx, playlistID)
}

func (a *App) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return a.playlist.AddPlaylistItem(ctx, req)
}

func (a *App) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return a.playlist.MovePlaylistItem(ctx, req)
}

func (a *App) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return a.playlist.RemovePlaylistItem(ctx, playlistID, itemID)
}

func (a *App) GetPlaylistCover(ctx context.Context, playlistID string) (apitypes.PlaylistCoverRecord, bool, error) {
	return a.playlist.GetPlaylistCover(ctx, playlistID)
}

func (a *App) SetPlaylistCover(ctx context.Context, req apitypes.PlaylistCoverUploadRequest) (apitypes.PlaylistCoverRecord, error) {
	return a.playlist.SetPlaylistCover(ctx, req)
}

func (a *App) ClearPlaylistCover(ctx context.Context, playlistID string) error {
	return a.playlist.ClearPlaylistCover(ctx, playlistID)
}

func (a *App) LikeRecording(ctx context.Context, recordingID string) error {
	return a.playlist.LikeRecording(ctx, recordingID)
}

func (a *App) UnlikeRecording(ctx context.Context, recordingID string) error {
	return a.playlist.UnlikeRecording(ctx, recordingID)
}

func (a *App) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return a.playlist.IsRecordingLiked(ctx, recordingID)
}

func (a *App) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (a *App) EnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (bool, error) {
	return a.playback.EnsureRecordingEncoding(ctx, recordingID, preferredProfile)
}

func (a *App) StartEnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (JobSnapshot, error) {
	return a.playback.StartEnsureRecordingEncoding(ctx, recordingID, preferredProfile)
}

func (a *App) EnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (apitypes.EnsureEncodingBatchResult, error) {
	return a.playback.EnsureAlbumEncodings(ctx, albumID, preferredProfile)
}

func (a *App) StartEnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (JobSnapshot, error) {
	return a.playback.StartEnsureAlbumEncodings(ctx, albumID, preferredProfile)
}

func (a *App) EnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (apitypes.EnsureEncodingBatchResult, error) {
	return a.playback.EnsurePlaylistEncodings(ctx, playlistID, preferredProfile)
}

func (a *App) StartEnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (JobSnapshot, error) {
	return a.playback.StartEnsurePlaylistEncodings(ctx, playlistID, preferredProfile)
}

func (a *App) EnsurePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return a.playback.EnsurePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (a *App) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.PreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (a *App) PreparePlaybackTarget(ctx context.Context, target playbackcore.PlaybackTargetRef, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.PreparePlaybackTarget(ctx, target, preferredProfile, purpose)
}

func (a *App) StartPreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (JobSnapshot, error) {
	return a.playback.StartPreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (a *App) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.GetPlaybackPreparation(ctx, recordingID, preferredProfile)
}

func (a *App) GetPlaybackTargetPreparation(ctx context.Context, target playbackcore.PlaybackTargetRef, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.GetPlaybackTargetPreparation(ctx, target, preferredProfile)
}

func (a *App) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return a.playback.ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (a *App) ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	return a.playback.ResolveArtworkRef(ctx, artwork)
}

func (a *App) ResolveAlbumArtwork(ctx context.Context, albumID, variant string) (apitypes.RecordingArtworkResult, error) {
	return a.playback.ResolveAlbumArtwork(ctx, albumID, variant)
}

func (a *App) ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
	return a.playback.ResolveRecordingArtwork(ctx, recordingID, variant)
}

func (a *App) EnsurePlaybackAlbum(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return a.playback.EnsurePlaybackAlbum(ctx, albumID, preferredProfile)
}

func (a *App) EnsurePlaybackPlaylist(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return a.playback.EnsurePlaybackPlaylist(ctx, playlistID, preferredProfile)
}

func (a *App) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return a.playback.ListRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (a *App) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return a.playback.GetRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (a *App) GetPlaybackTargetAvailability(ctx context.Context, target playbackcore.PlaybackTargetRef, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return a.playback.GetPlaybackTargetAvailability(ctx, target, preferredProfile)
}

func (a *App) ListRecordingPlaybackAvailability(ctx context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error) {
	return a.playback.ListRecordingPlaybackAvailability(ctx, req)
}

func (a *App) ListPlaybackTargetAvailability(ctx context.Context, req playbackcore.TargetAvailabilityRequest) ([]playbackcore.TargetAvailability, error) {
	return a.playback.ListPlaybackTargetAvailability(ctx, req)
}

func (a *App) ListAlbumAvailabilitySummaries(ctx context.Context, req apitypes.AlbumAvailabilitySummaryListRequest) ([]apitypes.AlbumAvailabilitySummaryItem, error) {
	return a.playback.ListAlbumAvailabilitySummaries(ctx, req)
}

func (a *App) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return a.playback.GetRecordingAvailabilityOverview(ctx, recordingID, preferredProfile)
}

func (a *App) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return a.playback.GetAlbumAvailabilityOverview(ctx, albumID, preferredProfile)
}

func (a *App) CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	return a.invite.CreateInviteCode(ctx, req)
}

func (a *App) ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error) {
	return a.invite.ListIssuedInvites(ctx, status)
}

func (a *App) RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error {
	return a.invite.RevokeIssuedInvite(ctx, inviteID, reason)
}

func (a *App) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	return a.invite.StartJoinFromInvite(ctx, req)
}

func (a *App) GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error) {
	return a.invite.GetJoinSession(ctx, sessionID)
}

func (a *App) FinalizeJoinSession(ctx context.Context, sessionID string) (apitypes.JoinLibraryResult, error) {
	return a.invite.FinalizeJoinSession(ctx, sessionID)
}

func (a *App) StartFinalizeJoinSession(ctx context.Context, sessionID string) (JobSnapshot, error) {
	return a.invite.StartFinalizeJoinSession(ctx, sessionID)
}

func (a *App) CancelJoinSession(ctx context.Context, sessionID string) error {
	return a.invite.CancelJoinSession(ctx, sessionID)
}

func (a *App) ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error) {
	return a.invite.ListJoinRequests(ctx, status)
}

func (a *App) ApproveJoinRequest(ctx context.Context, requestID, role string) error {
	return a.invite.ApproveJoinRequest(ctx, requestID, role)
}

func (a *App) RejectJoinRequest(ctx context.Context, requestID, reason string) error {
	return a.invite.RejectJoinRequest(ctx, requestID, reason)
}
