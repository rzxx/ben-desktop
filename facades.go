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

func (f facadeBase) pin() desktopcore.PinRuntime {
	if f.host == nil {
		return nil
	}
	return f.host.PinRuntime()
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
	ctx, span := startFacadeSpan(ctx, "library", "list_libraries", nil)
	defer span.End()
	result, err := s.library().ListLibraries(ctx)
	return finishFacadeSpan(span, result, err, map[string]any{"count": len(result)})
}

func (s *LibraryFacade) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	ctx, span := startFacadeSpan(ctx, "library", "active_library", nil)
	defer span.End()
	result, found, err := s.library().ActiveLibrary(ctx)
	if err != nil {
		span.RecordError(err)
	}
	span.SetOutput(apitypes.TraceSummary{Summary: "wails response", Fields: map[string]any{"found": found, "library_id": result.LibraryID}})
	return result, found, err
}

func (s *LibraryFacade) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	ctx, span := startFacadeSpan(ctx, "library", "create_library", map[string]any{"name": name})
	defer span.End()
	result, err := s.library().CreateLibrary(ctx, name)
	return finishFacadeSpan(span, result, err, map[string]any{"library_id": result.LibraryID})
}

func (s *LibraryFacade) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	ctx, span := startFacadeSpan(ctx, "library", "select_library", map[string]any{"library_id": libraryID})
	defer span.End()
	result, err := s.library().SelectLibrary(ctx, libraryID)
	return finishFacadeSpan(span, result, err, map[string]any{"library_id": result.LibraryID})
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

func (s *LibraryFacade) StartRepairLibrary(ctx context.Context) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "library", "start_repair_library", nil)
	defer span.End()
	result, err := s.library().StartRepairLibrary(ctx)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

type NetworkFacade struct {
	facadeBase
}

func NewNetworkFacade(host *coreHost) *NetworkFacade {
	return &NetworkFacade{facadeBase: facadeBase{host: host}}
}

func (s *NetworkFacade) ServiceName() string { return "NetworkFacade" }

func (s *NetworkFacade) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	ctx, span := startFacadeSpan(ctx, "network", "ensure_local_context", nil)
	defer span.End()
	result, err := s.network().EnsureLocalContext(ctx)
	return finishFacadeSpan(span, result, err, map[string]any{"library_id": result.LibraryID, "device_id": result.DeviceID, "peer_id": result.PeerID})
}

func (s *NetworkFacade) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return s.network().Inspect(ctx)
}

func (s *NetworkFacade) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return s.network().InspectLibraryOplog(ctx, libraryID)
}

func (s *NetworkFacade) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	ctx, span := startFacadeSpan(ctx, "network", "activity_status", nil)
	defer span.End()
	result, err := s.network().ActivityStatus(ctx)
	return finishFacadeSpan(span, result, err, map[string]any{"scan_phase": result.Scan.Phase})
}

func (s *NetworkFacade) NetworkStatus() apitypes.NetworkStatus {
	_, span := startFacadeSpan(context.Background(), "network", "network_status", nil)
	defer span.End()
	result := s.network().NetworkStatus()
	span.SetOutput(apitypes.TraceSummary{Summary: "wails response", Fields: map[string]any{"library_id": result.LibraryID, "running": result.Running, "mode": result.Mode}})
	return result
}

func (s *NetworkFacade) StartSyncNow(ctx context.Context) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "network", "start_sync_now", nil)
	defer span.End()
	result, err := s.network().StartSyncNow(ctx)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

func (s *NetworkFacade) StartConnectPeer(ctx context.Context, peerAddr string) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "network", "start_connect_peer", map[string]any{"peer_addr": peerAddr})
	defer span.End()
	result, err := s.network().StartConnectPeer(ctx, peerAddr)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
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
	ctx, span := startFacadeSpan(ctx, "catalog", "list_albums", map[string]any{"offset": req.Offset, "limit": req.Limit})
	defer span.End()
	result, err := s.catalog().ListAlbums(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"returned": len(result.Items), "total": result.Page.Total})
}

func (s *CatalogFacade) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	ctx, span := startFacadeSpan(ctx, "catalog", "get_album", map[string]any{"album_id": albumID})
	defer span.End()
	result, err := s.catalog().GetAlbum(ctx, albumID)
	return finishFacadeSpan(span, result, err, map[string]any{"album_id": result.AlbumID})
}

func (s *CatalogFacade) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	ctx, span := startFacadeSpan(ctx, "catalog", "list_recordings", map[string]any{"offset": req.Offset, "limit": req.Limit})
	defer span.End()
	result, err := s.catalog().ListRecordings(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"returned": len(result.Items), "total": result.Page.Total})
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
	ctx, span := startFacadeSpan(ctx, "catalog", "list_album_tracks", map[string]any{"album_id": req.AlbumID, "offset": req.Offset, "limit": req.Limit})
	defer span.End()
	result, err := s.catalog().ListAlbumTracks(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"returned": len(result.Items), "total": result.Page.Total})
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
	ctx, span := startFacadeSpan(ctx, "catalog", "list_liked_recordings", map[string]any{"offset": req.Offset, "limit": req.Limit})
	defer span.End()
	result, err := s.catalog().ListLikedRecordings(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"returned": len(result.Items), "total": result.Page.Total})
}

func (s *CatalogFacade) ListOfflineRecordings(ctx context.Context, req apitypes.OfflineRecordingListRequest) (apitypes.Page[apitypes.OfflineRecordingItem], error) {
	ctx, span := startFacadeSpan(ctx, "catalog", "list_offline_recordings", map[string]any{"offset": req.Offset, "limit": req.Limit})
	defer span.End()
	result, err := s.catalog().ListOfflineRecordings(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"returned": len(result.Items), "total": result.Page.Total})
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

func (s *CatalogFacade) GetPlaylistCover(ctx context.Context, playlistID string) (apitypes.PlaylistCoverRecord, bool, error) {
	return s.catalog().GetPlaylistCover(ctx, playlistID)
}

func (s *CatalogFacade) SetPlaylistCover(ctx context.Context, req apitypes.PlaylistCoverUploadRequest) (apitypes.PlaylistCoverRecord, error) {
	return s.catalog().SetPlaylistCover(ctx, req)
}

func (s *CatalogFacade) ClearPlaylistCover(ctx context.Context, playlistID string) error {
	return s.catalog().ClearPlaylistCover(ctx, playlistID)
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

func (s *InviteFacade) CreateInvite(ctx context.Context, req apitypes.InviteCreateRequest) (apitypes.InviteRecord, error) {
	ctx, span := startFacadeSpan(ctx, "invite", "create_invite", map[string]any{"role": req.Role, "uses": req.Uses})
	defer span.End()
	result, err := s.invite().CreateInvite(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"invite_id": result.InviteID, "library_id": result.LibraryID})
}

func (s *InviteFacade) ListActiveInvites(ctx context.Context) ([]apitypes.InviteRecord, error) {
	return s.invite().ListActiveInvites(ctx)
}

func (s *InviteFacade) DeleteInvite(ctx context.Context, inviteID string) error {
	return s.invite().DeleteInvite(ctx, inviteID)
}

func (s *InviteFacade) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	ctx, span := startFacadeSpan(ctx, "invite", "start_join_from_invite", map[string]any{"has_invite": req.InviteCode != ""})
	defer span.End()
	result, err := s.invite().StartJoinFromInvite(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"session_id": result.SessionID, "request_id": result.RequestID})
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

type PinFacade struct {
	facadeBase

	mu            sync.Mutex
	stopListening func()
}

func NewPinFacade(host *coreHost) *PinFacade {
	return &PinFacade{facadeBase: facadeBase{host: host}}
}

func (s *PinFacade) ServiceName() string { return "PinFacade" }

func (s *PinFacade) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
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

	stopListening := s.pin().SubscribePinChanges(func(event apitypes.PinChangeEvent) {
		app.Event.Emit(desktopcore.EventPinChanged, event)
	})

	s.mu.Lock()
	s.stopListening = stopListening
	s.mu.Unlock()
	return nil
}

func (s *PinFacade) ServiceShutdown() error {
	s.mu.Lock()
	stopListening := s.stopListening
	s.stopListening = nil
	s.mu.Unlock()
	if stopListening != nil {
		stopListening()
	}
	return nil
}

func (s *PinFacade) StartPin(ctx context.Context, req apitypes.PinIntentRequest) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "pin", "start_pin", map[string]any{"profile": req.Profile, "subject_kind": req.Subject.Kind})
	defer span.End()
	result, err := s.pin().StartPin(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

func (s *PinFacade) Unpin(ctx context.Context, req apitypes.PinIntentRequest) error {
	ctx, span := startFacadeSpan(ctx, "pin", "unpin", map[string]any{"profile": req.Profile, "subject_kind": req.Subject.Kind})
	defer span.End()
	err := s.pin().Unpin(ctx, req)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func (s *PinFacade) ListPinStates(ctx context.Context, req apitypes.PinStateListRequest) ([]apitypes.PinState, error) {
	return s.pin().ListPinStates(ctx, req)
}

func (s *PinFacade) GetPinState(ctx context.Context, req apitypes.PinStateRequest) (apitypes.PinState, error) {
	return s.pin().GetPinState(ctx, req)
}

func (s *PinFacade) SubscribePinEvents() string {
	return desktopcore.EventPinChanged
}

type CacheFacade struct {
	facadeBase
}

func NewCacheFacade(host *coreHost) *CacheFacade {
	return &CacheFacade{facadeBase: facadeBase{host: host}}
}

func (s *CacheFacade) ServiceName() string { return "CacheFacade" }

func (s *CacheFacade) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	ctx, span := startFacadeSpan(ctx, "cache", "get_cache_overview", nil)
	defer span.End()
	result, err := s.cache().GetCacheOverview(ctx)
	return finishFacadeSpan(span, result, err, map[string]any{"used_bytes": result.UsedBytes, "entries": result.EntryCount})
}

func (s *CacheFacade) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	ctx, span := startFacadeSpan(ctx, "cache", "list_cache_entries", map[string]any{"offset": req.Offset, "limit": req.Limit})
	defer span.End()
	result, err := s.cache().ListCacheEntries(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"returned": len(result.Items), "total": result.Page.Total})
}

func (s *CacheFacade) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	ctx, span := startFacadeSpan(ctx, "cache", "cleanup_cache", nil)
	defer span.End()
	result, err := s.cache().CleanupCache(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"deleted_bytes": result.DeletedBytes})
}

type PlaybackFacade struct {
	facadeBase
}

func NewPlaybackFacade(host *coreHost) *PlaybackFacade {
	return &PlaybackFacade{facadeBase: facadeBase{host: host}}
}

func (s *PlaybackFacade) ServiceName() string { return "PlaybackFacade" }

func (s *PlaybackFacade) StartEnsureRecordingEncoding(ctx context.Context, recordingID, preferredProfile string) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "start_ensure_recording_encoding", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().StartEnsureRecordingEncoding(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

func (s *PlaybackFacade) StartEnsureAlbumEncodings(ctx context.Context, albumID, preferredProfile string) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "start_ensure_album_encodings", map[string]any{"album_id": albumID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().StartEnsureAlbumEncodings(ctx, albumID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

func (s *PlaybackFacade) StartEnsurePlaylistEncodings(ctx context.Context, playlistID, preferredProfile string) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "start_ensure_playlist_encodings", map[string]any{"playlist_id": playlistID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().StartEnsurePlaylistEncodings(ctx, playlistID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

func (s *PlaybackFacade) EnsurePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "ensure_playback_recording", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().EnsurePlaybackRecording(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"source_kind": result.SourceKind, "encoding_id": result.EncodingID})
}

func (s *PlaybackFacade) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "inspect_playback_recording", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().InspectPlaybackRecording(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"phase": result.Phase, "reason": result.Reason})
}

func (s *PlaybackFacade) StartPreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (desktopcore.JobSnapshot, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "start_prepare_playback_recording", map[string]any{"recording_id": recordingID, "profile": preferredProfile, "purpose": purpose})
	defer span.End()
	result, err := s.playback().StartPreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
	return finishFacadeSpan(span, result, err, map[string]any{"job_id": result.JobID})
}

func (s *PlaybackFacade) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "get_playback_preparation", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().GetPlaybackPreparation(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"phase": result.Phase, "reason": result.Reason})
}

func (s *PlaybackFacade) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "resolve_playback_recording", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"source_kind": result.SourceKind, "recording_id": result.RecordingID})
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

func (s *PlaybackFacade) EnsurePlaybackAlbum(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "ensure_playback_album", map[string]any{"album_id": albumID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().EnsurePlaybackAlbum(ctx, albumID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"tracks": result.Tracks, "bytes": result.TotalBytes})
}

func (s *PlaybackFacade) EnsurePlaybackPlaylist(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "ensure_playback_playlist", map[string]any{"playlist_id": playlistID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().EnsurePlaybackPlaylist(ctx, playlistID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"tracks": result.Tracks, "bytes": result.TotalBytes})
}

func (s *PlaybackFacade) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "list_recording_availability", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().ListRecordingAvailability(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"count": len(result)})
}

func (s *PlaybackFacade) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "get_recording_availability", map[string]any{"recording_id": recordingID, "profile": preferredProfile})
	defer span.End()
	result, err := s.playback().GetRecordingAvailability(ctx, recordingID, preferredProfile)
	return finishFacadeSpan(span, result, err, map[string]any{"state": result.State})
}

func (s *PlaybackFacade) ListRecordingPlaybackAvailability(ctx context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "list_recording_playback_availability", map[string]any{"count": len(req.RecordingIDs), "profile": req.PreferredProfile})
	defer span.End()
	result, err := s.playback().ListRecordingPlaybackAvailability(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"count": len(result)})
}

func (s *PlaybackFacade) ListAlbumAvailabilitySummaries(ctx context.Context, req apitypes.AlbumAvailabilitySummaryListRequest) ([]apitypes.AlbumAvailabilitySummaryItem, error) {
	ctx, span := startFacadeSpan(ctx, "playback", "list_album_availability_summaries", map[string]any{"count": len(req.AlbumIDs), "profile": req.PreferredProfile})
	defer span.End()
	result, err := s.playback().ListAlbumAvailabilitySummaries(ctx, req)
	return finishFacadeSpan(span, result, err, map[string]any{"count": len(result)})
}

func (s *PlaybackFacade) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return s.playback().GetRecordingAvailabilityOverview(ctx, recordingID, preferredProfile)
}

func (s *PlaybackFacade) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return s.playback().GetAlbumAvailabilityOverview(ctx, albumID, preferredProfile)
}
