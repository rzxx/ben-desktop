import type {
  AlbumListItem,
  AlbumTrackItem,
  LikedRecordingItem,
  PageInfo,
  PlaylistTrackItem,
  RecordingListItem,
} from "@/lib/api/models";
import {
  listAlbumTracksPage,
  listArtistAlbumsPage,
  listLikedRecordingsPage,
  listPlaylistTracksPage,
  listTracksPage,
} from "@/lib/api/catalog";
import {
  getIdQuery,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import {
  dedupeRequest,
  describeError,
  type EnsureOptions,
} from "@/lib/catalog/loader-shared";
import { QUERY_KEYS } from "@/lib/catalog/query-keys";

type EntityId = string;

function shouldSkipIdQuery(key: string, offset: number, force = false) {
  const record = getIdQuery(useCatalogStore.getState(), key);
  if (record.inFlightOffsets.includes(offset)) {
    return true;
  }
  if (!force && record.loadedOffsets.includes(offset)) {
    return true;
  }
  return false;
}

function shouldSkipValueQuery(key: string, offset: number, force = false) {
  const record = getValueQuery(useCatalogStore.getState(), key);
  if (record.inFlightOffsets.includes(offset)) {
    return true;
  }
  if (!force && record.loadedOffsets.includes(offset)) {
    return true;
  }
  return false;
}

export async function ensureIdQueryPage<TItem>(
  key: string,
  offset: number,
  options: EnsureOptions,
  fetchPage: (offset: number) => Promise<{ Items: TItem[]; Page: PageInfo }>,
  getId: (item: TItem) => EntityId,
  upsert: (items: TItem[]) => void,
) {
  const { force = false } = options;
  if (shouldSkipIdQuery(key, offset, force)) {
    return;
  }

  const requestKey = `${key}:${offset}`;
  return dedupeRequest(requestKey, async () => {
    const state = useCatalogStore.getState();
    const record = getIdQuery(state, key);
    state.markIdQueryLoading(key, offset, {
      refreshing: record.ids.length > 0,
    });

    try {
      const page = await fetchPage(offset);
      upsert(page.Items);
      useCatalogStore
        .getState()
        .setIdQueryPage(
          key,
          page.Items.map(getId),
          page.Page,
          offset,
          offset === 0 ? "replace-front" : "append",
        );
    } catch (error) {
      useCatalogStore
        .getState()
        .markIdQueryError(key, describeError(error), offset);
      throw error;
    }
  });
}

export async function ensureValueQueryPage<TItem>(
  key: string,
  offset: number,
  options: EnsureOptions,
  fetchPage: (offset: number) => Promise<{ Items: TItem[]; Page: PageInfo }>,
  getItemKey: (item: TItem) => string,
) {
  const { force = false } = options;
  if (shouldSkipValueQuery(key, offset, force)) {
    return;
  }

  const requestKey = `${key}:${offset}`;
  return dedupeRequest(requestKey, async () => {
    const state = useCatalogStore.getState();
    const record = getValueQuery<TItem>(state, key);
    state.markValueQueryLoading(key, offset, {
      refreshing: record.items.length > 0,
    });

    try {
      const page = await fetchPage(offset);
      useCatalogStore
        .getState()
        .setValueQueryPage(
          key,
          page.Items,
          page.Page,
          offset,
          getItemKey,
          offset === 0 ? "replace-front" : "append",
        );
    } catch (error) {
      useCatalogStore
        .getState()
        .markValueQueryError(key, describeError(error), offset);
      throw error;
    }
  });
}

export function ensureTracksPage(offset = 0, options: EnsureOptions = {}) {
  return ensureValueQueryPage(
    QUERY_KEYS.tracks,
    offset,
    options,
    listTracksPage,
    (track: RecordingListItem) => track.RecordingID,
  );
}

export function ensureLikedPage(offset = 0, options: EnsureOptions = {}) {
  return ensureValueQueryPage(
    QUERY_KEYS.liked,
    offset,
    options,
    listLikedRecordingsPage,
    (track: LikedRecordingItem) => track.RecordingID,
  );
}

export function ensureAlbumTracksPage(
  albumId: string,
  offset = 0,
  options: EnsureOptions = {},
) {
  return ensureValueQueryPage(
    `albumTracks:${albumId}`,
    offset,
    options,
    (nextOffset) => listAlbumTracksPage(albumId, nextOffset),
    (track: AlbumTrackItem) => track.RecordingID,
  );
}

export function ensureArtistAlbumsPage(
  artistId: string,
  offset = 0,
  options: EnsureOptions = {},
) {
  return ensureValueQueryPage(
    `artistAlbums:${artistId}`,
    offset,
    options,
    (nextOffset) => listArtistAlbumsPage(artistId, nextOffset),
    (album: AlbumListItem) => album.AlbumID,
  ).then(() => {
    const record = getValueQuery<AlbumListItem>(
      useCatalogStore.getState(),
      `artistAlbums:${artistId}`,
    );
    useCatalogStore.getState().upsertAlbums(record.items.filter(Boolean));
  });
}

export function ensurePlaylistTracksPage(
  playlistId: string,
  offset = 0,
  options: EnsureOptions = {},
) {
  return ensureValueQueryPage(
    `playlistTracks:${playlistId}`,
    offset,
    options,
    (nextOffset) => listPlaylistTracksPage(playlistId, nextOffset),
    (track: PlaylistTrackItem) => track.ItemID,
  );
}
