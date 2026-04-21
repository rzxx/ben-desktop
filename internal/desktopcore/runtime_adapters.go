package desktopcore

import (
	"context"

	apitypes "ben/desktop/api/types"
)

type libraryRuntimeAdapter struct {
	library *LibraryService
	ingest  *IngestService
}

func newLibraryRuntimeAdapter(library *LibraryService, ingest *IngestService) LibraryRuntime {
	if library == nil || ingest == nil {
		return nil
	}
	return &libraryRuntimeAdapter{
		library: library,
		ingest:  ingest,
	}
}

func (r *libraryRuntimeAdapter) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return r.library.ListLibraries(ctx)
}

func (r *libraryRuntimeAdapter) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return r.library.ActiveLibrary(ctx)
}

func (r *libraryRuntimeAdapter) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return r.library.CreateLibrary(ctx, name)
}

func (r *libraryRuntimeAdapter) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return r.library.SelectLibrary(ctx, libraryID)
}

func (r *libraryRuntimeAdapter) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return r.library.RenameLibrary(ctx, libraryID, name)
}

func (r *libraryRuntimeAdapter) LeaveLibrary(ctx context.Context, libraryID string) error {
	return r.library.LeaveLibrary(ctx, libraryID)
}

func (r *libraryRuntimeAdapter) DeleteLibrary(ctx context.Context, libraryID string) error {
	return r.library.DeleteLibrary(ctx, libraryID)
}

func (r *libraryRuntimeAdapter) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return r.library.ListLibraryMembers(ctx)
}

func (r *libraryRuntimeAdapter) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return r.library.UpdateLibraryMemberRole(ctx, deviceID, role)
}

func (r *libraryRuntimeAdapter) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return r.library.RemoveLibraryMember(ctx, deviceID)
}

func (r *libraryRuntimeAdapter) SetScanRoots(ctx context.Context, roots []string) error {
	return r.ingest.SetScanRoots(ctx, roots)
}

func (r *libraryRuntimeAdapter) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return r.ingest.AddScanRoots(ctx, roots)
}

func (r *libraryRuntimeAdapter) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return r.ingest.RemoveScanRoots(ctx, roots)
}

func (r *libraryRuntimeAdapter) ScanRoots(ctx context.Context) ([]string, error) {
	return r.ingest.ScanRoots(ctx)
}

func (r *libraryRuntimeAdapter) StartRepairLibrary(ctx context.Context) (JobSnapshot, error) {
	return r.ingest.StartRepairLibrary(ctx)
}

type networkRuntimeAdapter struct {
	operator   *OperatorService
	sync       *SyncService
	checkpoint *CheckpointService
}

func newNetworkRuntimeAdapter(operator *OperatorService, sync *SyncService, checkpoint *CheckpointService) NetworkRuntime {
	if operator == nil || sync == nil || checkpoint == nil {
		return nil
	}
	return &networkRuntimeAdapter{
		operator:   operator,
		sync:       sync,
		checkpoint: checkpoint,
	}
}

func (r *networkRuntimeAdapter) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	return r.operator.EnsureLocalContext(ctx)
}

func (r *networkRuntimeAdapter) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	return r.operator.Inspect(ctx)
}

func (r *networkRuntimeAdapter) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	return r.operator.InspectLibraryOplog(ctx, libraryID)
}

func (r *networkRuntimeAdapter) ActivityStatus(ctx context.Context) (apitypes.ActivityStatus, error) {
	return r.operator.ActivityStatus(ctx)
}

func (r *networkRuntimeAdapter) NetworkStatus() apitypes.NetworkStatus {
	return r.operator.NetworkStatus()
}

func (r *networkRuntimeAdapter) StartSyncNow(ctx context.Context) (JobSnapshot, error) {
	return r.sync.StartSyncNow(ctx)
}

func (r *networkRuntimeAdapter) StartConnectPeer(ctx context.Context, peerAddr string) (JobSnapshot, error) {
	return r.sync.StartConnectPeer(ctx, peerAddr)
}

func (r *networkRuntimeAdapter) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return r.operator.CheckpointStatus(ctx)
}

func (r *networkRuntimeAdapter) StartPublishCheckpoint(ctx context.Context) (JobSnapshot, error) {
	return r.checkpoint.StartPublishCheckpoint(ctx)
}

func (r *networkRuntimeAdapter) StartCompactCheckpoint(ctx context.Context, force bool) (JobSnapshot, error) {
	return r.checkpoint.StartCompactCheckpoint(ctx, force)
}

type catalogRuntimeAdapter struct {
	catalog  *CatalogService
	playlist *PlaylistService
}

func newCatalogRuntimeAdapter(catalog *CatalogService, playlist *PlaylistService) CatalogRuntime {
	if catalog == nil || playlist == nil {
		return nil
	}
	return &catalogRuntimeAdapter{
		catalog:  catalog,
		playlist: playlist,
	}
}

func (r *catalogRuntimeAdapter) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return r.catalog.ListArtists(ctx, req)
}

func (r *catalogRuntimeAdapter) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return r.catalog.GetArtist(ctx, artistID)
}

func (r *catalogRuntimeAdapter) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return r.catalog.ListArtistAlbums(ctx, req)
}

func (r *catalogRuntimeAdapter) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return r.catalog.ListAlbums(ctx, req)
}

func (r *catalogRuntimeAdapter) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return r.catalog.GetAlbum(ctx, albumID)
}

func (r *catalogRuntimeAdapter) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return r.catalog.ListRecordings(ctx, req)
}

func (r *catalogRuntimeAdapter) ListRecordingsCursor(ctx context.Context, req apitypes.RecordingCursorRequest) (apitypes.CursorPage[apitypes.RecordingListItem], error) {
	return r.catalog.ListRecordingsCursor(ctx, req)
}

func (r *catalogRuntimeAdapter) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return r.catalog.GetRecording(ctx, recordingID)
}

func (r *catalogRuntimeAdapter) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return r.catalog.ListRecordingVariants(ctx, req)
}

func (r *catalogRuntimeAdapter) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return r.catalog.ListAlbumVariants(ctx, req)
}

func (r *catalogRuntimeAdapter) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return r.catalog.SetPreferredRecordingVariant(ctx, recordingID, variantRecordingID)
}

func (r *catalogRuntimeAdapter) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return r.catalog.SetPreferredAlbumVariant(ctx, albumID, variantAlbumID)
}

func (r *catalogRuntimeAdapter) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return r.catalog.ListAlbumTracks(ctx, req)
}

func (r *catalogRuntimeAdapter) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return r.catalog.ListPlaylists(ctx, req)
}

func (r *catalogRuntimeAdapter) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return r.catalog.GetPlaylistSummary(ctx, playlistID)
}

func (r *catalogRuntimeAdapter) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return r.catalog.ListPlaylistTracks(ctx, req)
}

func (r *catalogRuntimeAdapter) ListPlaylistTracksCursor(ctx context.Context, req apitypes.PlaylistTrackCursorRequest) (apitypes.CursorPage[apitypes.PlaylistTrackItem], error) {
	return r.catalog.ListPlaylistTracksCursor(ctx, req)
}

func (r *catalogRuntimeAdapter) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return r.catalog.ListLikedRecordings(ctx, req)
}

func (r *catalogRuntimeAdapter) ListLikedRecordingsCursor(ctx context.Context, req apitypes.LikedRecordingCursorRequest) (apitypes.CursorPage[apitypes.LikedRecordingItem], error) {
	return r.catalog.ListLikedRecordingsCursor(ctx, req)
}

func (r *catalogRuntimeAdapter) ListOfflineRecordings(ctx context.Context, req apitypes.OfflineRecordingListRequest) (apitypes.Page[apitypes.OfflineRecordingItem], error) {
	return r.catalog.ListOfflineRecordings(ctx, req)
}

func (r *catalogRuntimeAdapter) ListOfflineRecordingsCursor(ctx context.Context, req apitypes.OfflineRecordingCursorRequest) (apitypes.CursorPage[apitypes.OfflineRecordingItem], error) {
	return r.catalog.ListOfflineRecordingsCursor(ctx, req)
}

func (r *catalogRuntimeAdapter) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return r.playlist.CreatePlaylist(ctx, name, kind)
}

func (r *catalogRuntimeAdapter) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return r.playlist.RenamePlaylist(ctx, playlistID, name)
}

func (r *catalogRuntimeAdapter) DeletePlaylist(ctx context.Context, playlistID string) error {
	return r.playlist.DeletePlaylist(ctx, playlistID)
}

func (r *catalogRuntimeAdapter) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return r.playlist.AddPlaylistItem(ctx, req)
}

func (r *catalogRuntimeAdapter) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return r.playlist.MovePlaylistItem(ctx, req)
}

func (r *catalogRuntimeAdapter) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return r.playlist.RemovePlaylistItem(ctx, playlistID, itemID)
}

func (r *catalogRuntimeAdapter) GetPlaylistCover(ctx context.Context, playlistID string) (apitypes.PlaylistCoverRecord, bool, error) {
	return r.playlist.GetPlaylistCover(ctx, playlistID)
}

func (r *catalogRuntimeAdapter) SetPlaylistCover(ctx context.Context, req apitypes.PlaylistCoverUploadRequest) (apitypes.PlaylistCoverRecord, error) {
	return r.playlist.SetPlaylistCover(ctx, req)
}

func (r *catalogRuntimeAdapter) ClearPlaylistCover(ctx context.Context, playlistID string) error {
	return r.playlist.ClearPlaylistCover(ctx, playlistID)
}

func (r *catalogRuntimeAdapter) LikeRecording(ctx context.Context, recordingID string) error {
	return r.playlist.LikeRecording(ctx, recordingID)
}

func (r *catalogRuntimeAdapter) UnlikeRecording(ctx context.Context, recordingID string) error {
	return r.playlist.UnlikeRecording(ctx, recordingID)
}

func (r *catalogRuntimeAdapter) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return r.playlist.IsRecordingLiked(ctx, recordingID)
}

func (r *catalogRuntimeAdapter) SubscribeCatalogChanges(listener func(apitypes.CatalogChangeEvent)) func() {
	return r.catalog.SubscribeCatalogChanges(listener)
}
