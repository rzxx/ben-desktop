import { create as createDraftState, type Draft } from "mutative";
import { create } from "zustand";
import {
  appendUniqueNumber,
  createDetailRecord,
  createIdQueryRecord,
  createValueQueryRecord,
  mergeAppendUnique,
  mergeFrontUnique,
  mergeItemsByKey,
  removeNumber,
} from "./catalog-store-records";
import {
  getDetailContainer,
  getDetailRecord,
  getIdQuery,
  getValueQuery,
} from "./catalog-store-selectors";
import type { CatalogStore, CatalogStoreState } from "./catalog-store-types";

function createCatalogState(): CatalogStoreState {
  return {
    albumsById: {},
    albumDetails: {},
    albumVariants: {},
    artistDetails: {},
    artistsById: {},
    idQueries: {},
    playlistSummaries: {},
    playlistsById: {},
    valueQueries: {},
  };
}

function createCatalogDraftSetter(
  set: (recipe: (state: CatalogStore) => CatalogStore) => void,
) {
  return (recipe: (draft: Draft<CatalogStore>) => void) => {
    set((state) => createDraftState(state, recipe));
  };
}

export { getDetailRecord, getIdQuery, getValueQuery };
export type {
  CatalogStore,
  CatalogStoreActions,
  CatalogStoreState,
  DetailKind,
  DetailRecord,
  IdQueryRecord,
  QueryStatus,
  ValueQueryRecord,
} from "./catalog-store-types";

export const useCatalogStore = create<CatalogStore>((set) => {
  const setDraft = createCatalogDraftSetter(set);

  return {
    ...createCatalogState(),

    upsertAlbums: (albums) => {
      setDraft((draft) => {
        for (const album of albums) {
          draft.albumsById[album.AlbumID] = album;
        }
      });
    },

    upsertArtists: (artists) => {
      setDraft((draft) => {
        for (const artist of artists) {
          draft.artistsById[artist.ArtistID] = artist;
        }
      });
    },

    upsertPlaylists: (playlists) => {
      setDraft((draft) => {
        for (const playlist of playlists) {
          const key = playlist.Kind === "liked" ? "liked" : playlist.PlaylistID;
          draft.playlistsById[key] = playlist;
        }
      });
    },

    markIdQueryLoading: (key, offset, options) => {
      setDraft((draft) => {
        const record = draft.idQueries[key] ?? createIdQueryRecord();
        draft.idQueries[key] = record;
        appendUniqueNumber(record.inFlightOffsets, offset);
        record.error = "";
        record.isRefreshing =
          options?.refreshing ??
          (record.ids.length > 0 || record.loadedOffsets.length > 0);
        record.status =
          record.ids.length > 0 || record.loadedOffsets.length > 0
            ? "success"
            : "loading";
      });
    },

    setIdQueryPage: (
      key,
      ids,
      pageInfo,
      offset,
      mode = "append",
      fetchedAt,
    ) => {
      setDraft((draft) => {
        const record = draft.idQueries[key] ?? createIdQueryRecord();
        draft.idQueries[key] = record;
        record.ids =
          mode === "replace-front"
            ? mergeFrontUnique(record.ids, ids)
            : mergeAppendUnique(record.ids, ids);
        record.pageInfo = pageInfo;
        record.status = "success";
        record.error = "";
        record.isRefreshing = false;
        record.lastFetchedAt = fetchedAt ?? Date.now();
        appendUniqueNumber(record.loadedOffsets, offset);
        removeNumber(record.inFlightOffsets, offset);
      });
    },

    markIdQueryError: (key, message, offset) => {
      setDraft((draft) => {
        const record = draft.idQueries[key] ?? createIdQueryRecord();
        draft.idQueries[key] = record;
        record.error = message;
        record.isRefreshing = false;
        record.status = record.ids.length > 0 ? "success" : "error";
        removeNumber(record.inFlightOffsets, offset);
      });
    },

    removeIdQueryInFlight: (key, offset) => {
      setDraft((draft) => {
        const record = draft.idQueries[key];
        if (!record) {
          return;
        }
        removeNumber(record.inFlightOffsets, offset);
        if (record.inFlightOffsets.length === 0) {
          record.isRefreshing = false;
        }
      });
    },

    markValueQueryLoading: (key, offset, options) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key] ?? createValueQueryRecord();
        draft.valueQueries[key] = record;
        appendUniqueNumber(record.inFlightOffsets, offset);
        record.error = "";
        record.isRefreshing =
          options?.refreshing ??
          (record.items.length > 0 || record.loadedOffsets.length > 0);
        record.status =
          record.items.length > 0 || record.loadedOffsets.length > 0
            ? "success"
            : "loading";
      });
    },

    setValueQueryPage: (
      key,
      items,
      pageInfo,
      offset,
      getItemKey,
      mode = "append",
      fetchedAt,
    ) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key] ?? createValueQueryRecord();
        draft.valueQueries[key] = record;
        record.items = mergeItemsByKey(
          record.items as typeof items,
          items,
          getItemKey,
          mode,
        ) as typeof record.items;
        record.pageInfo = pageInfo;
        record.status = "success";
        record.error = "";
        record.isRefreshing = false;
        record.lastFetchedAt = fetchedAt ?? Date.now();
        appendUniqueNumber(record.loadedOffsets, offset);
        removeNumber(record.inFlightOffsets, offset);
      });
    },

    markValueQueryError: (key, message, offset) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key] ?? createValueQueryRecord();
        draft.valueQueries[key] = record;
        record.error = message;
        record.isRefreshing = false;
        record.status = record.items.length > 0 ? "success" : "error";
        removeNumber(record.inFlightOffsets, offset);
      });
    },

    removeValueQueryInFlight: (key, offset) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key];
        if (!record) {
          return;
        }
        removeNumber(record.inFlightOffsets, offset);
        if (record.inFlightOffsets.length === 0) {
          record.isRefreshing = false;
        }
      });
    },

    markDetailLoading: (kind, id, options) => {
      setDraft((draft) => {
        const container = getDetailContainer(draft, kind);
        const record = container[id] ?? createDetailRecord();
        container[id] = record;
        record.error = "";
        record.inFlight = true;
        record.isRefreshing = options?.refreshing ?? record.data !== null;
        record.status = record.data !== null ? "success" : "loading";
      });
    },

    markDetailError: (kind, id, message) => {
      setDraft((draft) => {
        const container = getDetailContainer(draft, kind);
        const record = container[id] ?? createDetailRecord();
        container[id] = record;
        record.error = message;
        record.inFlight = false;
        record.isRefreshing = false;
        record.status = record.data !== null ? "success" : "error";
      });
    },

    setAlbumDetail: (albumId, album, fetchedAt) => {
      setDraft((draft) => {
        draft.albumsById[album.AlbumID] = album;
        draft.albumDetails[albumId] = {
          data: album,
          error: "",
          inFlight: false,
          isRefreshing: false,
          lastFetchedAt: fetchedAt ?? Date.now(),
          status: "success",
        };
      });
    },

    setArtistDetail: (artistId, artist, fetchedAt) => {
      setDraft((draft) => {
        draft.artistsById[artist.ArtistID] = artist;
        draft.artistDetails[artistId] = {
          data: artist,
          error: "",
          inFlight: false,
          isRefreshing: false,
          lastFetchedAt: fetchedAt ?? Date.now(),
          status: "success",
        };
      });
    },

    setPlaylistSummary: (playlistId, playlist, fetchedAt) => {
      setDraft((draft) => {
        const key = playlist.Kind === "liked" ? "liked" : playlist.PlaylistID;
        draft.playlistsById[key] = playlist;
        draft.playlistSummaries[playlistId] = {
          data: playlist,
          error: "",
          inFlight: false,
          isRefreshing: false,
          lastFetchedAt: fetchedAt ?? Date.now(),
          status: "success",
        };
      });
    },

    setAlbumVariants: (albumId, variants, fetchedAt) => {
      setDraft((draft) => {
        draft.albumVariants[albumId] = {
          data: variants,
          error: "",
          inFlight: false,
          isRefreshing: false,
          lastFetchedAt: fetchedAt ?? Date.now(),
          status: "success",
        };
      });
    },
  };
});
