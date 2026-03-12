package desktopcore

import (
	"context"
	"fmt"

	apitypes "ben/core/api/types"
)

type UnavailableCore struct {
	err error
}

func NewUnavailableCore(err error) *UnavailableCore {
	if err == nil {
		err = fmt.Errorf("desktop core is not available")
	}
	return &UnavailableCore{err: err}
}

func (c *UnavailableCore) Close() error { return nil }

func (c *UnavailableCore) ListArtists(context.Context, apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return apitypes.Page[apitypes.ArtistListItem]{}, c.err
}

func (c *UnavailableCore) GetArtist(context.Context, string) (apitypes.ArtistListItem, error) {
	return apitypes.ArtistListItem{}, c.err
}

func (c *UnavailableCore) ListArtistAlbums(context.Context, apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return apitypes.Page[apitypes.AlbumListItem]{}, c.err
}

func (c *UnavailableCore) ListAlbums(context.Context, apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return apitypes.Page[apitypes.AlbumListItem]{}, c.err
}

func (c *UnavailableCore) GetAlbum(context.Context, string) (apitypes.AlbumListItem, error) {
	return apitypes.AlbumListItem{}, c.err
}

func (c *UnavailableCore) ListRecordings(context.Context, apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return apitypes.Page[apitypes.RecordingListItem]{}, c.err
}

func (c *UnavailableCore) GetRecording(context.Context, string) (apitypes.RecordingListItem, error) {
	return apitypes.RecordingListItem{}, c.err
}

func (c *UnavailableCore) ListRecordingVariants(context.Context, apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return apitypes.Page[apitypes.RecordingVariantItem]{}, c.err
}

func (c *UnavailableCore) ListAlbumVariants(context.Context, apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return apitypes.Page[apitypes.AlbumVariantItem]{}, c.err
}

func (c *UnavailableCore) SetPreferredRecordingVariant(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) SetPreferredAlbumVariant(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) ListAlbumTracks(context.Context, apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return apitypes.Page[apitypes.AlbumTrackItem]{}, c.err
}

func (c *UnavailableCore) ListPlaylists(context.Context, apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return apitypes.Page[apitypes.PlaylistListItem]{}, c.err
}

func (c *UnavailableCore) GetPlaylistSummary(context.Context, string) (apitypes.PlaylistListItem, error) {
	return apitypes.PlaylistListItem{}, c.err
}

func (c *UnavailableCore) ListPlaylistTracks(context.Context, apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return apitypes.Page[apitypes.PlaylistTrackItem]{}, c.err
}

func (c *UnavailableCore) ListLikedRecordings(context.Context, apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return apitypes.Page[apitypes.LikedRecordingItem]{}, c.err
}

func (c *UnavailableCore) CreatePlaylist(context.Context, string, string) (apitypes.PlaylistRecord, error) {
	return apitypes.PlaylistRecord{}, c.err
}

func (c *UnavailableCore) GetCacheOverview(context.Context) (apitypes.CacheOverview, error) {
	return apitypes.CacheOverview{}, c.err
}

func (c *UnavailableCore) ListCacheEntries(context.Context, apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return apitypes.Page[apitypes.CacheEntryItem]{}, c.err
}

func (c *UnavailableCore) CleanupCache(context.Context, apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return apitypes.CacheCleanupResult{}, c.err
}

func (c *UnavailableCore) RenamePlaylist(context.Context, string, string) (apitypes.PlaylistRecord, error) {
	return apitypes.PlaylistRecord{}, c.err
}

func (c *UnavailableCore) DeletePlaylist(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) AddPlaylistItem(context.Context, apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return apitypes.PlaylistItemRecord{}, c.err
}

func (c *UnavailableCore) MovePlaylistItem(context.Context, apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return apitypes.PlaylistItemRecord{}, c.err
}

func (c *UnavailableCore) RemovePlaylistItem(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) LikeRecording(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) UnlikeRecording(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) IsRecordingLiked(context.Context, string) (bool, error) {
	return false, c.err
}

func (c *UnavailableCore) CreateInviteCode(context.Context, apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	return apitypes.InviteCodeResult{}, c.err
}

func (c *UnavailableCore) ListIssuedInvites(context.Context, string) ([]apitypes.IssuedInviteRecord, error) {
	return nil, c.err
}

func (c *UnavailableCore) RevokeIssuedInvite(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) StartJoinFromInvite(context.Context, apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	return apitypes.JoinSession{}, c.err
}

func (c *UnavailableCore) GetJoinSession(context.Context, string) (apitypes.JoinSession, error) {
	return apitypes.JoinSession{}, c.err
}

func (c *UnavailableCore) FinalizeJoinSession(context.Context, string) (apitypes.JoinLibraryResult, error) {
	return apitypes.JoinLibraryResult{}, c.err
}

func (c *UnavailableCore) CancelJoinSession(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) ListJoinRequests(context.Context, string) ([]apitypes.InviteJoinRequestRecord, error) {
	return nil, c.err
}

func (c *UnavailableCore) ApproveJoinRequest(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) RejectJoinRequest(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) InspectPlaybackRecording(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, c.err
}

func (c *UnavailableCore) PreparePlaybackRecording(context.Context, string, string, apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, c.err
}

func (c *UnavailableCore) GetPlaybackPreparation(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, c.err
}

func (c *UnavailableCore) ResolvePlaybackRecording(context.Context, string, string) (apitypes.PlaybackResolveResult, error) {
	return apitypes.PlaybackResolveResult{}, c.err
}

func (c *UnavailableCore) ResolveArtworkRef(context.Context, apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	return apitypes.ArtworkResolveResult{}, c.err
}

func (c *UnavailableCore) ResolveRecordingArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, c.err
}

func (c *UnavailableCore) PinRecordingOffline(context.Context, string, string) (apitypes.PlaybackRecordingResult, error) {
	return apitypes.PlaybackRecordingResult{}, c.err
}

func (c *UnavailableCore) UnpinRecordingOffline(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) PinAlbumOffline(context.Context, string, string) (apitypes.PlaybackBatchResult, error) {
	return apitypes.PlaybackBatchResult{}, c.err
}

func (c *UnavailableCore) UnpinAlbumOffline(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) PinPlaylistOffline(context.Context, string, string) (apitypes.PlaybackBatchResult, error) {
	return apitypes.PlaybackBatchResult{}, c.err
}

func (c *UnavailableCore) UnpinPlaylistOffline(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) PinLikedOffline(context.Context, string) (apitypes.PlaybackBatchResult, error) {
	return apitypes.PlaybackBatchResult{}, c.err
}

func (c *UnavailableCore) UnpinLikedOffline(context.Context) error {
	return c.err
}

func (c *UnavailableCore) ListRecordingAvailability(context.Context, string, string) ([]apitypes.RecordingAvailabilityItem, error) {
	return nil, c.err
}

func (c *UnavailableCore) GetRecordingAvailability(context.Context, string, string) (apitypes.RecordingPlaybackAvailability, error) {
	return apitypes.RecordingPlaybackAvailability{}, c.err
}

func (c *UnavailableCore) GetRecordingAvailabilityOverview(context.Context, string, string) (apitypes.RecordingAvailabilityOverview, error) {
	return apitypes.RecordingAvailabilityOverview{}, c.err
}

func (c *UnavailableCore) GetAlbumAvailabilityOverview(context.Context, string, string) (apitypes.AlbumAvailabilityOverview, error) {
	return apitypes.AlbumAvailabilityOverview{}, c.err
}

func (c *UnavailableCore) ListLibraries(context.Context) ([]apitypes.LibrarySummary, error) {
	return nil, c.err
}

func (c *UnavailableCore) ActiveLibrary(context.Context) (apitypes.LibrarySummary, bool, error) {
	return apitypes.LibrarySummary{}, false, c.err
}

func (c *UnavailableCore) CreateLibrary(context.Context, string) (apitypes.LibrarySummary, error) {
	return apitypes.LibrarySummary{}, c.err
}

func (c *UnavailableCore) SelectLibrary(context.Context, string) (apitypes.LibrarySummary, error) {
	return apitypes.LibrarySummary{}, c.err
}

func (c *UnavailableCore) RenameLibrary(context.Context, string, string) (apitypes.LibrarySummary, error) {
	return apitypes.LibrarySummary{}, c.err
}

func (c *UnavailableCore) LeaveLibrary(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) DeleteLibrary(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) ListLibraryMembers(context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return nil, c.err
}

func (c *UnavailableCore) UpdateLibraryMemberRole(context.Context, string, string) error {
	return c.err
}

func (c *UnavailableCore) RemoveLibraryMember(context.Context, string) error {
	return c.err
}

func (c *UnavailableCore) SetScanRoots(context.Context, []string) error {
	return c.err
}

func (c *UnavailableCore) AddScanRoots(context.Context, []string) ([]string, error) {
	return nil, c.err
}

func (c *UnavailableCore) RemoveScanRoots(context.Context, []string) ([]string, error) {
	return nil, c.err
}

func (c *UnavailableCore) ScanRoots(context.Context) ([]string, error) {
	return nil, c.err
}
