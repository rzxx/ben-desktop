import {
  listAlbumsPage,
  listArtistsPage,
  listPlaylistsPage,
} from "../../shared/lib/desktop";
import { getIdQuery, getValueQuery, useCatalogStore } from "./catalog-store";
import {
  ensureAlbumRoute,
  ensureArtistRoute,
  ensurePlaylistRoute,
} from "./catalog-loader-details";
import { QUERY_KEYS } from "./catalog-loader-keys";
import {
  ensureAlbumTracksPage,
  ensureArtistAlbumsPage,
  ensureIdQueryPage,
  ensureLikedPage,
  ensurePlaylistTracksPage,
  ensureTracksPage,
} from "./catalog-loader-paged";
import type { EnsureOptions } from "./catalog-loader-shared";

export const catalogLoaderClient = {
  ensureAlbumsPage(offset = 0, options: EnsureOptions = {}) {
    return ensureIdQueryPage(
      QUERY_KEYS.albums,
      offset,
      options,
      listAlbumsPage,
      (album) => album.AlbumID,
      (albums) => useCatalogStore.getState().upsertAlbums(albums),
    );
  },

  ensureAlbumsRoute() {
    const record = getIdQuery(useCatalogStore.getState(), QUERY_KEYS.albums);
    if (record.ids.length > 0) {
      void this.ensureAlbumsPage(0, { force: true });
      return Promise.resolve();
    }
    return this.ensureAlbumsPage(0);
  },

  ensureArtistsPage(offset = 0, options: EnsureOptions = {}) {
    return ensureIdQueryPage(
      QUERY_KEYS.artists,
      offset,
      options,
      listArtistsPage,
      (artist) => artist.ArtistID,
      (artists) => useCatalogStore.getState().upsertArtists(artists),
    );
  },

  ensureArtistsRoute() {
    const record = getIdQuery(useCatalogStore.getState(), QUERY_KEYS.artists);
    if (record.ids.length > 0) {
      void this.ensureArtistsPage(0, { force: true });
      return Promise.resolve();
    }
    return this.ensureArtistsPage(0);
  },

  ensureTracksPage(offset = 0, options: EnsureOptions = {}) {
    return ensureTracksPage(offset, options);
  },

  ensureTracksRoute() {
    const record = getValueQuery(useCatalogStore.getState(), QUERY_KEYS.tracks);
    if (record.items.length > 0) {
      void this.ensureTracksPage(0, { force: true });
      return Promise.resolve();
    }
    return this.ensureTracksPage(0);
  },

  ensurePlaylistsPage(offset = 0, options: EnsureOptions = {}) {
    return ensureIdQueryPage(
      QUERY_KEYS.playlists,
      offset,
      options,
      listPlaylistsPage,
      (playlist) => (playlist.Kind === "liked" ? "liked" : playlist.PlaylistID),
      (playlists) => useCatalogStore.getState().upsertPlaylists(playlists),
    );
  },

  ensurePlaylistsRoute() {
    const record = getIdQuery(useCatalogStore.getState(), QUERY_KEYS.playlists);
    if (record.ids.length > 0) {
      void this.ensurePlaylistsPage(0, { force: true });
      return Promise.resolve();
    }
    return this.ensurePlaylistsPage(0);
  },

  ensureLikedPage(offset = 0, options: EnsureOptions = {}) {
    return ensureLikedPage(offset, options);
  },

  ensureLikedRoute() {
    const record = getValueQuery(useCatalogStore.getState(), QUERY_KEYS.liked);
    if (record.items.length > 0) {
      void this.ensureLikedPage(0, { force: true });
      return Promise.resolve();
    }
    return this.ensureLikedPage(0);
  },

  ensureAlbumTracksPage(
    albumId: string,
    offset = 0,
    options: EnsureOptions = {},
  ) {
    return ensureAlbumTracksPage(albumId, offset, options);
  },

  ensureAlbumRoute(albumId: string) {
    return ensureAlbumRoute(albumId);
  },

  ensureArtistAlbumsPage(
    artistId: string,
    offset = 0,
    options: EnsureOptions = {},
  ) {
    return ensureArtistAlbumsPage(artistId, offset, options);
  },

  ensureArtistRoute(artistId: string) {
    return ensureArtistRoute(artistId);
  },

  ensurePlaylistTracksPage(
    playlistId: string,
    offset = 0,
    options: EnsureOptions = {},
  ) {
    return ensurePlaylistTracksPage(playlistId, offset, options);
  },

  ensurePlaylistRoute(playlistId: string) {
    return ensurePlaylistRoute(playlistId);
  },

  refetchAlbums() {
    return this.ensureAlbumsPage(0, { force: true });
  },

  refetchArtists() {
    return this.ensureArtistsPage(0, { force: true });
  },

  refetchTracks() {
    return this.ensureTracksPage(0, { force: true });
  },

  refetchPlaylists() {
    return this.ensurePlaylistsPage(0, { force: true });
  },

  refetchLiked() {
    return this.ensureLikedPage(0, { force: true });
  },

  refetchAlbum(albumId: string) {
    return this.ensureAlbumRoute(albumId);
  },

  refetchArtist(artistId: string) {
    return this.ensureArtistRoute(artistId);
  },

  refetchPlaylist(playlistId: string) {
    return this.ensurePlaylistRoute(playlistId);
  },
};

export type CatalogLoaderClient = typeof catalogLoaderClient;
