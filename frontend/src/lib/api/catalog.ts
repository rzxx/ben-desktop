import { Dialogs } from "@wailsio/runtime";
import * as CatalogFacade from "../../../bindings/ben/desktop/catalogfacade";
import { DEFAULT_PAGE_SIZE, TRACK_PAGE_SIZE, Types } from "./models";

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

export async function getPlaylistCover(playlistId: string) {
  const [record, found] = await CatalogFacade.GetPlaylistCover(playlistId);
  return { found, record };
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

export function createPlaylist(name: string, kind = "normal") {
  return CatalogFacade.CreatePlaylist(name, kind);
}

export function renamePlaylist(playlistId: string, name: string) {
  return CatalogFacade.RenamePlaylist(playlistId, name);
}

export function deletePlaylist(playlistId: string) {
  return CatalogFacade.DeletePlaylist(playlistId);
}

export function addPlaylistItem(input: {
  playlistId: string;
  libraryRecordingId?: string;
  recordingId?: string;
  afterItemId?: string;
  beforeItemId?: string;
}) {
  return CatalogFacade.AddPlaylistItem(
    new Types.PlaylistAddItemRequest({
      PlaylistID: input.playlistId,
      LibraryRecordingID: input.libraryRecordingId ?? "",
      RecordingID: input.recordingId ?? "",
      AfterItemID: input.afterItemId ?? "",
      BeforeItemID: input.beforeItemId ?? "",
    }),
  );
}

export function removePlaylistItem(playlistId: string, itemId: string) {
  return CatalogFacade.RemovePlaylistItem(playlistId, itemId);
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
  return CatalogFacade.SetPlaylistCover(
    new Types.PlaylistCoverUploadRequest({
      PlaylistID: playlistId,
      SourcePath: sourcePath,
    }),
  );
}

export function clearPlaylistCover(playlistId: string) {
  return CatalogFacade.ClearPlaylistCover(playlistId);
}

export function likeRecording(recordingId: string) {
  return CatalogFacade.LikeRecording(recordingId);
}

export function unlikeRecording(recordingId: string) {
  return CatalogFacade.UnlikeRecording(recordingId);
}

export function isRecordingLiked(recordingId: string) {
  return CatalogFacade.IsRecordingLiked(recordingId);
}

export function subscribeCatalogEvents() {
  return CatalogFacade.SubscribeCatalogEvents();
}
