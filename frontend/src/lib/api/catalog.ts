import { Dialogs } from "@wailsio/runtime";
import * as CatalogFacade from "../../../bindings/ben/desktop/catalogfacade";
import { DEFAULT_PAGE_SIZE, TRACK_PAGE_SIZE, Types } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export function listAlbumsPage(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return traceWailsCall("catalog", "list_albums", { offset, limit }, () =>
    CatalogFacade.ListAlbums(
      new Types.AlbumListRequest({
        Limit: limit,
        Offset: offset,
      }),
    ),
  );
}

export function getAlbum(albumId: string) {
  return traceWailsCall("catalog", "get_album", { albumId }, () =>
    CatalogFacade.GetAlbum(albumId),
  );
}

export function listAlbumVariants(albumId: string) {
  return traceWailsCall("catalog", "list_album_variants", { albumId }, () =>
    CatalogFacade.ListAlbumVariants(
      new Types.AlbumVariantListRequest({
        AlbumID: albumId,
        Limit: DEFAULT_PAGE_SIZE,
        Offset: 0,
      }),
    ),
  );
}

export function listAlbumTracksPage(
  albumId: string,
  offset = 0,
  limit = TRACK_PAGE_SIZE,
) {
  return traceWailsCall(
    "catalog",
    "list_album_tracks",
    { albumId, offset, limit },
    () =>
      CatalogFacade.ListAlbumTracks(
        new Types.AlbumTrackListRequest({
          AlbumID: albumId,
          Limit: limit,
          Offset: offset,
        }),
      ),
  );
}

export function listArtistsPage(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return traceWailsCall("catalog", "list_artists", { offset, limit }, () =>
    CatalogFacade.ListArtists(
      new Types.ArtistListRequest({
        Limit: limit,
        Offset: offset,
      }),
    ),
  );
}

export function getArtist(artistId: string) {
  return traceWailsCall("catalog", "get_artist", { artistId }, () =>
    CatalogFacade.GetArtist(artistId),
  );
}

export function listArtistAlbumsPage(
  artistId: string,
  offset = 0,
  limit = DEFAULT_PAGE_SIZE,
) {
  return traceWailsCall(
    "catalog",
    "list_artist_albums",
    { artistId, offset, limit },
    () =>
      CatalogFacade.ListArtistAlbums(
        new Types.ArtistAlbumListRequest({
          ArtistID: artistId,
          Limit: limit,
          Offset: offset,
        }),
      ),
  );
}

export function listTracksPage(offset = 0, limit = TRACK_PAGE_SIZE) {
  return traceWailsCall("catalog", "list_recordings", { offset, limit }, () =>
    CatalogFacade.ListRecordings(
      new Types.RecordingListRequest({
        Limit: limit,
        Offset: offset,
      }),
    ),
  );
}

export function listPlaylistsPage(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return traceWailsCall("catalog", "list_playlists", { offset, limit }, () =>
    CatalogFacade.ListPlaylists(
      new Types.PlaylistListRequest({
        Limit: limit,
        Offset: offset,
      }),
    ),
  );
}

export function getPlaylistSummary(playlistId: string) {
  return traceWailsCall("catalog", "get_playlist_summary", { playlistId }, () =>
    CatalogFacade.GetPlaylistSummary(playlistId),
  );
}

export async function getPlaylistCover(playlistId: string) {
  const [record, found] = await traceWailsCall(
    "catalog",
    "get_playlist_cover",
    { playlistId },
    () => CatalogFacade.GetPlaylistCover(playlistId),
  );
  return { found, record };
}

export function listPlaylistTracksPage(
  playlistId: string,
  offset = 0,
  limit = TRACK_PAGE_SIZE,
) {
  return traceWailsCall(
    "catalog",
    "list_playlist_tracks",
    { playlistId, offset, limit },
    () =>
      CatalogFacade.ListPlaylistTracks(
        new Types.PlaylistTrackListRequest({
          PlaylistID: playlistId,
          Limit: limit,
          Offset: offset,
        }),
      ),
  );
}

export function listLikedRecordingsPage(offset = 0, limit = TRACK_PAGE_SIZE) {
  return traceWailsCall(
    "catalog",
    "list_liked_recordings",
    { offset, limit },
    () =>
      CatalogFacade.ListLikedRecordings(
        new Types.LikedRecordingListRequest({
          Limit: limit,
          Offset: offset,
        }),
      ),
  );
}

export function listOfflineRecordingsPage(offset = 0, limit = TRACK_PAGE_SIZE) {
  return traceWailsCall(
    "catalog",
    "list_offline_recordings",
    { offset, limit },
    () =>
      CatalogFacade.ListOfflineRecordings(
        new Types.OfflineRecordingListRequest({
          Limit: limit,
          Offset: offset,
        }),
      ),
  );
}

export function createPlaylist(name: string, kind = "normal") {
  return traceWailsCall("catalog", "create_playlist", { name, kind }, () =>
    CatalogFacade.CreatePlaylist(name, kind),
  );
}

export function renamePlaylist(playlistId: string, name: string) {
  return traceWailsCall(
    "catalog",
    "rename_playlist",
    { playlistId, name },
    () => CatalogFacade.RenamePlaylist(playlistId, name),
  );
}

export function deletePlaylist(playlistId: string) {
  return traceWailsCall("catalog", "delete_playlist", { playlistId }, () =>
    CatalogFacade.DeletePlaylist(playlistId),
  );
}

export function addPlaylistItem(input: {
  playlistId: string;
  libraryRecordingId?: string;
  recordingId?: string;
  afterItemId?: string;
  beforeItemId?: string;
}) {
  return traceWailsCall(
    "catalog",
    "add_playlist_item",
    { playlistId: input.playlistId, recordingId: input.recordingId ?? "" },
    () =>
      CatalogFacade.AddPlaylistItem(
        new Types.PlaylistAddItemRequest({
          PlaylistID: input.playlistId,
          LibraryRecordingID: input.libraryRecordingId ?? "",
          RecordingID: input.recordingId ?? "",
          AfterItemID: input.afterItemId ?? "",
          BeforeItemID: input.beforeItemId ?? "",
        }),
      ),
  );
}

export function removePlaylistItem(playlistId: string, itemId: string) {
  return traceWailsCall(
    "catalog",
    "remove_playlist_item",
    { playlistId, itemId },
    () => CatalogFacade.RemovePlaylistItem(playlistId, itemId),
  );
}

export async function pickPlaylistCoverSourcePath() {
  const selected = await Dialogs.OpenFile({
    AllowsMultipleSelection: false,
    ButtonText: "Choose cover",
    CanChooseDirectories: false,
    CanChooseFiles: true,
    Filters: [
      {
        DisplayName: "Images",
        Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.avif;*.gif",
      },
    ],
    Message: "Choose an image file to use as a playlist cover.",
    Title: "Choose playlist cover",
  });

  return typeof selected === "string" ? selected.trim() : "";
}

export function setPlaylistCover(playlistId: string, sourcePath: string) {
  return traceWailsCall("catalog", "set_playlist_cover", { playlistId }, () =>
    CatalogFacade.SetPlaylistCover(
      new Types.PlaylistCoverUploadRequest({
        PlaylistID: playlistId,
        SourcePath: sourcePath,
      }),
    ),
  );
}

export function clearPlaylistCover(playlistId: string) {
  return traceWailsCall("catalog", "clear_playlist_cover", { playlistId }, () =>
    CatalogFacade.ClearPlaylistCover(playlistId),
  );
}

export function likeRecording(recordingId: string) {
  return traceWailsCall("catalog", "like_recording", { recordingId }, () =>
    CatalogFacade.LikeRecording(recordingId),
  );
}

export function unlikeRecording(recordingId: string) {
  return traceWailsCall("catalog", "unlike_recording", { recordingId }, () =>
    CatalogFacade.UnlikeRecording(recordingId),
  );
}

export function isRecordingLiked(recordingId: string) {
  return traceWailsCall("catalog", "is_recording_liked", { recordingId }, () =>
    CatalogFacade.IsRecordingLiked(recordingId),
  );
}

export function subscribeCatalogEvents() {
  return CatalogFacade.SubscribeCatalogEvents();
}
