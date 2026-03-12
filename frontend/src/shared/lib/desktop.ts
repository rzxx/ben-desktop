import * as CatalogFacade from "../../../bindings/ben/desktop/catalogfacade";
import * as CacheFacade from "../../../bindings/ben/desktop/cachefacade";
import * as InviteFacade from "../../../bindings/ben/desktop/invitefacade";
import * as JobsFacade from "../../../bindings/ben/desktop/jobsfacade";
import * as LibraryFacade from "../../../bindings/ben/desktop/libraryfacade";
import * as NetworkFacade from "../../../bindings/ben/desktop/networkfacade";
import * as PlaybackFacade from "../../../bindings/ben/desktop/playbackfacade";
import * as Types from "../../../bindings/ben/core/api/types/models";
import * as DesktopCoreModels from "../../../bindings/ben/desktop/internal/desktopcore/models";
import * as PlaybackModels from "../../../bindings/ben/desktop/internal/playback/models";

export { DesktopCoreModels, PlaybackModels, Types };

export type AlbumListItem = Types.AlbumListItem;
export type AlbumVariantItem = Types.AlbumVariantItem;
export type AlbumTrackItem = Types.AlbumTrackItem;
export type ActivityStatus = Types.ActivityStatus;
export type ArtworkRef = Types.ArtworkRef;
export type ArtistListItem = Types.ArtistListItem;
export type CacheEntryItem = Types.CacheEntryItem;
export type CacheOverview = Types.CacheOverview;
export type LibraryCheckpointStatus = Types.LibraryCheckpointStatus;
export type LibraryMemberStatus = Types.LibraryMemberStatus;
export type LibrarySummary = Types.LibrarySummary;
export type InviteCodeResult = Types.InviteCodeResult;
export type InviteJoinRequestRecord = Types.InviteJoinRequestRecord;
export type IssuedInviteRecord = Types.IssuedInviteRecord;
export type JoinLibraryResult = Types.JoinLibraryResult;
export type JoinSession = Types.JoinSession;
export type LikedRecordingItem = Types.LikedRecordingItem;
export type LocalContext = Types.LocalContext;
export type PageInfo = Types.PageInfo;
export type PlaybackPreparationStatus = Types.PlaybackPreparationStatus;
export type PlaylistListItem = Types.PlaylistListItem;
export type PlaylistTrackItem = Types.PlaylistTrackItem;
export type RecordingListItem = Types.RecordingListItem;
export type JobPhase = DesktopCoreModels.JobPhase;
export type JobSnapshot = DesktopCoreModels.JobSnapshot;
export type SessionEntry = PlaybackModels.SessionEntry;
export type SessionItem = PlaybackModels.SessionItem;
export type SessionSnapshot = PlaybackModels.SessionSnapshot;

export const DEFAULT_PAGE_SIZE = 60;
export const TRACK_PAGE_SIZE = 120;

export function listAlbumsPage(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return CatalogFacade.ListAlbums(
    new Types.AlbumListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function getAlbum(albumId: string) {
  return CatalogFacade.GetAlbum(albumId);
}

export function listAlbumVariants(albumId: string) {
  return CatalogFacade.ListAlbumVariants(
    new Types.AlbumVariantListRequest({
      AlbumID: albumId,
      Limit: DEFAULT_PAGE_SIZE,
      Offset: 0,
    }),
  );
}

export function listAlbumTracksPage(
  albumId: string,
  offset = 0,
  limit = TRACK_PAGE_SIZE,
) {
  return CatalogFacade.ListAlbumTracks(
    new Types.AlbumTrackListRequest({
      AlbumID: albumId,
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function listArtistsPage(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return CatalogFacade.ListArtists(
    new Types.ArtistListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function getArtist(artistId: string) {
  return CatalogFacade.GetArtist(artistId);
}

export function listArtistAlbumsPage(
  artistId: string,
  offset = 0,
  limit = DEFAULT_PAGE_SIZE,
) {
  return CatalogFacade.ListArtistAlbums(
    new Types.ArtistAlbumListRequest({
      ArtistID: artistId,
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function listTracksPage(offset = 0, limit = TRACK_PAGE_SIZE) {
  return CatalogFacade.ListRecordings(
    new Types.RecordingListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function listPlaylistsPage(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return CatalogFacade.ListPlaylists(
    new Types.PlaylistListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function getPlaylistSummary(playlistId: string) {
  return CatalogFacade.GetPlaylistSummary(playlistId);
}

export function listPlaylistTracksPage(
  playlistId: string,
  offset = 0,
  limit = TRACK_PAGE_SIZE,
) {
  return CatalogFacade.ListPlaylistTracks(
    new Types.PlaylistTrackListRequest({
      PlaylistID: playlistId,
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function listLikedRecordingsPage(offset = 0, limit = TRACK_PAGE_SIZE) {
  return CatalogFacade.ListLikedRecordings(
    new Types.LikedRecordingListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function resolveThumbnailURL(thumb: ArtworkRef) {
  return PlaybackFacade.ResolveThumbnailURL(thumb);
}

export function resolveRecordingArtworkURL(
  recordingId: string,
  variant = "320_webp",
) {
  return PlaybackFacade.ResolveRecordingArtworkURL(recordingId, variant);
}

export async function getActiveLibrary() {
  const [library, found] = await LibraryFacade.ActiveLibrary();
  return { library, found };
}

export function listLibraries() {
  return LibraryFacade.ListLibraries();
}

export function createLibrary(name: string) {
  return LibraryFacade.CreateLibrary(name);
}

export function selectLibrary(libraryId: string) {
  return LibraryFacade.SelectLibrary(libraryId);
}

export function renameLibrary(libraryId: string, name: string) {
  return LibraryFacade.RenameLibrary(libraryId, name);
}

export function leaveLibrary(libraryId: string) {
  return LibraryFacade.LeaveLibrary(libraryId);
}

export function deleteLibrary(libraryId: string) {
  return LibraryFacade.DeleteLibrary(libraryId);
}

export function listLibraryMembers() {
  return LibraryFacade.ListLibraryMembers();
}

export function updateLibraryMemberRole(deviceId: string, role: string) {
  return LibraryFacade.UpdateLibraryMemberRole(deviceId, role);
}

export function removeLibraryMember(deviceId: string) {
  return LibraryFacade.RemoveLibraryMember(deviceId);
}

export function getLocalContext() {
  return NetworkFacade.EnsureLocalContext();
}

export function getScanRoots() {
  return LibraryFacade.ScanRoots();
}

export function getActivityStatus() {
  return NetworkFacade.ActivityStatus();
}

export function getCheckpointStatus() {
  return NetworkFacade.CheckpointStatus();
}

export function connectPeer(peerAddr: string) {
  return NetworkFacade.ConnectPeer(peerAddr);
}

export function startLibraryRescan() {
  return LibraryFacade.StartRescanNow();
}

export function startRootRescan(root: string) {
  return LibraryFacade.StartRescanRoot(root);
}

export function startPublishCheckpoint() {
  return NetworkFacade.StartPublishCheckpoint();
}

export function startCompactCheckpoint(force = false) {
  return NetworkFacade.StartCompactCheckpoint(force);
}

export function startSyncNow() {
  return NetworkFacade.StartSyncNow();
}

export function createInviteCode(req: Types.InviteCodeRequest) {
  return InviteFacade.CreateInviteCode(req);
}

export function listIssuedInvites(status = "") {
  return InviteFacade.ListIssuedInvites(status);
}

export function revokeIssuedInvite(inviteId: string, reason: string) {
  return InviteFacade.RevokeIssuedInvite(inviteId, reason);
}

export function startJoinFromInvite(req: Types.JoinFromInviteInput) {
  return InviteFacade.StartJoinFromInvite(req);
}

export function getJoinSession(sessionId: string) {
  return InviteFacade.GetJoinSession(sessionId);
}

export function finalizeJoinSession(sessionId: string) {
  return InviteFacade.FinalizeJoinSession(sessionId);
}

export function cancelJoinSession(sessionId: string) {
  return InviteFacade.CancelJoinSession(sessionId);
}

export function listJoinRequests(status = "") {
  return InviteFacade.ListJoinRequests(status);
}

export function approveJoinRequest(requestId: string, role: string) {
  return InviteFacade.ApproveJoinRequest(requestId, role);
}

export function rejectJoinRequest(requestId: string, reason: string) {
  return InviteFacade.RejectJoinRequest(requestId, reason);
}

export function getCacheOverview() {
  return CacheFacade.GetCacheOverview();
}

export function listCacheEntries(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return CacheFacade.ListCacheEntries(
    new Types.CacheEntryListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function cleanupCache(req: Types.CacheCleanupRequest) {
  return CacheFacade.CleanupCache(req);
}

export function startPreparePlaybackRecording(
  recordingId: string,
  preferredProfile = "",
  purpose = Types.PlaybackPreparationPurpose.PlaybackPreparationPlayNow,
) {
  return PlaybackFacade.StartPreparePlaybackRecording(
    recordingId,
    preferredProfile,
    purpose,
  );
}

export function listJobs(libraryId = "") {
  return JobsFacade.ListJobs(libraryId);
}

export async function getJob(jobId: string) {
  const [job, found] = await JobsFacade.GetJob(jobId);
  return { job, found };
}

export function subscribeJobEvents() {
  return JobsFacade.SubscribeJobEvents();
}
