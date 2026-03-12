import * as CatalogFacade from "../../../bindings/ben/desktop/catalogfacade";
import * as PlaybackFacade from "../../../bindings/ben/desktop/playbackfacade";
import * as Types from "../../../bindings/ben/core/api/types/models";
import * as PlaybackModels from "../../../bindings/ben/desktop/internal/playback/models";

export { PlaybackModels, Types };

export type AlbumListItem = Types.AlbumListItem;
export type AlbumVariantItem = Types.AlbumVariantItem;
export type AlbumTrackItem = Types.AlbumTrackItem;
export type ArtworkRef = Types.ArtworkRef;
export type ArtistListItem = Types.ArtistListItem;
export type LikedRecordingItem = Types.LikedRecordingItem;
export type PageInfo = Types.PageInfo;
export type PlaylistListItem = Types.PlaylistListItem;
export type PlaylistTrackItem = Types.PlaylistTrackItem;
export type RecordingListItem = Types.RecordingListItem;
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
