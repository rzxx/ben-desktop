import type {
  AlbumListItem,
  AlbumTrackItem,
  PlaylistTrackItem,
} from "@/lib/api";
import {
  getAlbum,
  getArtist,
  getPlaylistSummary,
  listAlbumVariants,
} from "@/lib/api";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import {
  ensureAlbumTracksPage,
  ensureArtistAlbumsPage,
  ensurePlaylistTracksPage,
} from "@/lib/catalog/loader-paged";
import {
  dedupeRequest,
  describeError,
  type EnsureOptions,
} from "@/lib/catalog/loader-shared";

async function ensureAlbumDetail(albumId: string, options: EnsureOptions = {}) {
  const { force = false } = options;
  const record = getDetailRecord(
    useCatalogStore.getState().albumDetails,
    albumId,
  );
  if (record.inFlight || (!force && record.data)) {
    return;
  }

  return dedupeRequest(`album:${albumId}:detail`, async () => {
    const state = useCatalogStore.getState();
    state.markDetailLoading("album", albumId, {
      refreshing: record.data !== null,
    });
    try {
      state.setAlbumDetail(albumId, await getAlbum(albumId));
    } catch (error) {
      state.markDetailError("album", albumId, describeError(error));
      throw error;
    }
  });
}

async function ensureArtistDetail(
  artistId: string,
  options: EnsureOptions = {},
) {
  const { force = false } = options;
  const record = getDetailRecord(
    useCatalogStore.getState().artistDetails,
    artistId,
  );
  if (record.inFlight || (!force && record.data)) {
    return;
  }

  return dedupeRequest(`artist:${artistId}:detail`, async () => {
    const state = useCatalogStore.getState();
    state.markDetailLoading("artist", artistId, {
      refreshing: record.data !== null,
    });
    try {
      state.setArtistDetail(artistId, await getArtist(artistId));
    } catch (error) {
      state.markDetailError("artist", artistId, describeError(error));
      throw error;
    }
  });
}

async function ensurePlaylistSummary(
  playlistId: string,
  options: EnsureOptions = {},
) {
  const { force = false } = options;
  const record = getDetailRecord(
    useCatalogStore.getState().playlistSummaries,
    playlistId,
  );
  if (record.inFlight || (!force && record.data)) {
    return;
  }

  return dedupeRequest(`playlist:${playlistId}:summary`, async () => {
    const state = useCatalogStore.getState();
    state.markDetailLoading("playlistSummary", playlistId, {
      refreshing: record.data !== null,
    });
    try {
      state.setPlaylistSummary(
        playlistId,
        await getPlaylistSummary(playlistId),
      );
    } catch (error) {
      state.markDetailError(
        "playlistSummary",
        playlistId,
        describeError(error),
      );
      throw error;
    }
  });
}

async function ensureAlbumVariants(
  albumId: string,
  options: EnsureOptions = {},
) {
  const { force = false } = options;
  const record = getDetailRecord(
    useCatalogStore.getState().albumVariants,
    albumId,
  );
  if (record.inFlight || (!force && record.data)) {
    return;
  }

  return dedupeRequest(`album:${albumId}:variants`, async () => {
    const state = useCatalogStore.getState();
    state.markDetailLoading("albumVariants", albumId, {
      refreshing: record.data !== null,
    });
    try {
      const variants = await listAlbumVariants(albumId);
      state.setAlbumVariants(albumId, variants.Items);
    } catch (error) {
      state.markDetailError("albumVariants", albumId, describeError(error));
      throw error;
    }
  });
}

export function ensureAlbumRoute(albumId: string) {
  const detail = getDetailRecord(
    useCatalogStore.getState().albumDetails,
    albumId,
  );
  const query = getValueQuery<AlbumTrackItem>(
    useCatalogStore.getState(),
    `albumTracks:${albumId}`,
  );
  const variants = getDetailRecord(
    useCatalogStore.getState().albumVariants,
    albumId,
  );

  if (detail.data && query.items.length > 0 && variants.data) {
    void Promise.all([
      ensureAlbumDetail(albumId, { force: true }),
      ensureAlbumVariants(albumId, { force: true }),
      ensureAlbumTracksPage(albumId, 0, { force: true }),
    ]);
    return Promise.resolve();
  }

  return Promise.all([
    ensureAlbumDetail(albumId),
    ensureAlbumVariants(albumId),
    ensureAlbumTracksPage(albumId, 0),
  ]).then(() => undefined);
}

export function ensureArtistRoute(artistId: string) {
  const detail = getDetailRecord(
    useCatalogStore.getState().artistDetails,
    artistId,
  );
  const query = getValueQuery<AlbumListItem>(
    useCatalogStore.getState(),
    `artistAlbums:${artistId}`,
  );

  if (detail.data && query.items.length > 0) {
    void Promise.all([
      ensureArtistDetail(artistId, { force: true }),
      ensureArtistAlbumsPage(artistId, 0, { force: true }),
    ]);
    return Promise.resolve();
  }

  return Promise.all([
    ensureArtistDetail(artistId),
    ensureArtistAlbumsPage(artistId, 0),
  ]).then(() => undefined);
}

export function ensurePlaylistRoute(playlistId: string) {
  const detail = getDetailRecord(
    useCatalogStore.getState().playlistSummaries,
    playlistId,
  );
  const query = getValueQuery<PlaylistTrackItem>(
    useCatalogStore.getState(),
    `playlistTracks:${playlistId}`,
  );

  if (detail.data && query.items.length > 0) {
    void Promise.all([
      ensurePlaylistSummary(playlistId, { force: true }),
      ensurePlaylistTracksPage(playlistId, 0, { force: true }),
    ]);
    return Promise.resolve();
  }

  return Promise.all([
    ensurePlaylistSummary(playlistId),
    ensurePlaylistTracksPage(playlistId, 0),
  ]).then(() => undefined);
}


