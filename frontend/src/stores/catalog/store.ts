import { create as createDraftState, type Draft } from "mutative";
import { create } from "zustand";
import {
  createDetailRecord,
  createIdQueryRecord,
  createQueryPageRecord,
  createValueQueryRecord,
  dropPagesAfterOffset,
  pageHeadChanged,
  pageHeadChangedIds,
  rebuildIdQueryRecord,
  rebuildValueQueryRecord,
} from "./records";
import {
  getAlbumAvailabilityRecord,
  getDetailContainer,
  getDetailRecord,
  getIdQuery,
  getTrackAvailabilityRecord,
  getValueQuery,
} from "./selectors";
import type { CatalogStore, CatalogStoreState } from "./types";
import type {
  CatalogTrackLookupItem,
  CatalogValueQueryItem,
  QueryPageRecord,
} from "./types";

function createCatalogState(): CatalogStoreState {
  return {
    albumsById: {},
    albumAvailabilityByAlbumId: {},
    albumDetails: {},
    albumVariants: {},
    artistDetails: {},
    artistsById: {},
    idQueries: {},
    playlistSummaries: {},
    playlistTrackItemsByItemId: {},
    playlistsById: {},
    trackItemsByLibraryRecordingId: {},
    trackItemsByRecordingId: {},
    trackAvailabilityByRecordingId: {},
    valueQueries: {},
  };
}

function isCatalogTrackLookupItem(
  item: CatalogValueQueryItem,
): item is CatalogTrackLookupItem {
  return "RecordingID" in item && "Title" in item;
}

function indexCatalogTrackItem(
  indexes: {
    playlistTrackItemsByItemId: CatalogStoreState["playlistTrackItemsByItemId"];
    trackItemsByLibraryRecordingId: CatalogStoreState["trackItemsByLibraryRecordingId"];
    trackItemsByRecordingId: CatalogStoreState["trackItemsByRecordingId"];
  },
  item: CatalogTrackLookupItem,
) {
  if (item.RecordingID) {
    indexes.trackItemsByRecordingId[item.RecordingID] = item;
  }
  if ("LibraryRecordingID" in item && item.LibraryRecordingID) {
    indexes.trackItemsByLibraryRecordingId[item.LibraryRecordingID] = item;
  }
  if ("ItemID" in item && item.ItemID) {
    indexes.playlistTrackItemsByItemId[item.ItemID] = item;
  }
}

function rebuildCatalogTrackIndexes(draft: Draft<CatalogStore>) {
  const indexes = {
    playlistTrackItemsByItemId:
      {} as CatalogStoreState["playlistTrackItemsByItemId"],
    trackItemsByLibraryRecordingId:
      {} as CatalogStoreState["trackItemsByLibraryRecordingId"],
    trackItemsByRecordingId:
      {} as CatalogStoreState["trackItemsByRecordingId"],
  };

  for (const record of Object.values(draft.valueQueries)) {
    for (const page of Object.values(
      record.pages as Record<string, QueryPageRecord<CatalogValueQueryItem>>,
    )) {
      if (page.fetchedAt === null || page.stale) {
        continue;
      }
      for (const item of page.items) {
        if (isCatalogTrackLookupItem(item)) {
          indexCatalogTrackItem(indexes, item);
        }
      }
    }
  }

  draft.playlistTrackItemsByItemId = indexes.playlistTrackItemsByItemId;
  draft.trackItemsByLibraryRecordingId =
    indexes.trackItemsByLibraryRecordingId;
  draft.trackItemsByRecordingId = indexes.trackItemsByRecordingId;
}

function createCatalogDraftSetter(
  set: (recipe: (state: CatalogStore) => CatalogStore) => void,
) {
  return (recipe: (draft: Draft<CatalogStore>) => void) => {
    set((state) => createDraftState(state, recipe));
  };
}

export {
  getAlbumAvailabilityRecord,
  getDetailRecord,
  getIdQuery,
  getTrackAvailabilityRecord,
  getValueQuery,
};
export type {
  CatalogValueQueryItem,
  CatalogStore,
  CatalogStoreActions,
  CatalogStoreState,
  DetailKind,
  DetailRecord,
  IdQueryRecord,
  QueryStatus,
  ValueQueryRecord,
} from "./types";

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

    invalidateIdQuery: (key, options) => {
      setDraft((draft) => {
        const record = draft.idQueries[key];
        if (!record) {
          return;
        }
        if (options?.clear) {
          draft.idQueries[key] = createIdQueryRecord();
          return;
        }
        if (
          options?.dropAfterOffset !== undefined &&
          options.dropAfterOffset !== null
        ) {
          dropPagesAfterOffset(record.pages, options.dropAfterOffset);
        }
        for (const page of Object.values(record.pages)) {
          page.stale = true;
        }
        rebuildIdQueryRecord(record);
      });
    },

    invalidateValueQuery: (key, options) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key];
        if (!record) {
          return;
        }
        if (options?.clear) {
          draft.valueQueries[key] = createValueQueryRecord();
          rebuildCatalogTrackIndexes(draft);
          return;
        }
        if (
          options?.dropAfterOffset !== undefined &&
          options.dropAfterOffset !== null
        ) {
          dropPagesAfterOffset(record.pages, options.dropAfterOffset);
        }
        for (const page of Object.values(record.pages)) {
          page.stale = true;
        }
        rebuildValueQueryRecord(record);
        rebuildCatalogTrackIndexes(draft);
      });
    },

    markIdQueryLoading: (key, offset, options) => {
      setDraft((draft) => {
        const record = draft.idQueries[key] ?? createIdQueryRecord();
        draft.idQueries[key] = record;
        const page =
          record.pages[String(offset)] ?? createQueryPageRecord<string>();
        record.pages[String(offset)] = page;
        page.error = "";
        page.inFlight = true;
        page.stale = false;
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
      _mode = "append",
      fetchedAt,
    ) => {
      setDraft((draft) => {
        void _mode;
        const record = draft.idQueries[key] ?? createIdQueryRecord();
        draft.idQueries[key] = record;
        if (
          offset === 0 &&
          pageHeadChangedIds(record.pages["0"], ids, pageInfo.Total)
        ) {
          dropPagesAfterOffset(record.pages, 0);
        }
        const page =
          record.pages[String(offset)] ?? createQueryPageRecord<string>();
        record.pages[String(offset)] = page;
        page.error = "";
        page.fetchedAt = fetchedAt ?? Date.now();
        page.inFlight = false;
        page.items = [...ids];
        page.pageInfo = pageInfo;
        page.stale = false;
        rebuildIdQueryRecord(record);
      });
    },

    markIdQueryError: (key, message, offset) => {
      setDraft((draft) => {
        const record = draft.idQueries[key] ?? createIdQueryRecord();
        draft.idQueries[key] = record;
        const page =
          record.pages[String(offset)] ?? createQueryPageRecord<string>();
        record.pages[String(offset)] = page;
        page.error = message;
        page.inFlight = false;
        rebuildIdQueryRecord(record);
      });
    },

    removeIdQueryInFlight: (key, offset) => {
      setDraft((draft) => {
        const record = draft.idQueries[key];
        if (!record) {
          return;
        }
        const page = record.pages[String(offset)];
        if (!page) {
          return;
        }
        page.inFlight = false;
        rebuildIdQueryRecord(record);
      });
    },

    markValueQueryLoading: (key, offset, options) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key] ?? createValueQueryRecord();
        draft.valueQueries[key] = record;
        const page = record.pages[String(offset)] ?? createQueryPageRecord();
        record.pages[String(offset)] = page;
        page.error = "";
        page.inFlight = true;
        page.stale = false;
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
      _mode = "append",
      fetchedAt,
    ) => {
      setDraft((draft) => {
        void _mode;
        const record =
          (draft.valueQueries[key] as
            | (typeof draft.valueQueries)[string]
            | undefined) ?? createValueQueryRecord<CatalogValueQueryItem>();
        draft.valueQueries[key] = record;
        record.getItemKey = getItemKey as typeof record.getItemKey;
        const existingPage = record.pages["0"] as
          | QueryPageRecord<CatalogValueQueryItem>
          | undefined;
        if (
          offset === 0 &&
          pageHeadChanged(
            existingPage,
            items as CatalogValueQueryItem[],
            getItemKey as (item: CatalogValueQueryItem) => string,
            pageInfo.Total,
          )
        ) {
          dropPagesAfterOffset(record.pages, 0);
        }
        const page =
          (record.pages[String(offset)] as
            | QueryPageRecord<CatalogValueQueryItem>
            | undefined) ?? createQueryPageRecord<CatalogValueQueryItem>();
        record.pages[String(offset)] = page;
        page.error = "";
        page.fetchedAt = fetchedAt ?? Date.now();
        page.inFlight = false;
        page.items = [...(items as CatalogValueQueryItem[])];
        page.pageInfo = pageInfo;
        page.stale = false;
        rebuildValueQueryRecord(record);
        rebuildCatalogTrackIndexes(draft);
      });
    },

    markValueQueryError: (key, message, offset) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key] ?? createValueQueryRecord();
        draft.valueQueries[key] = record;
        const page = record.pages[String(offset)] ?? createQueryPageRecord();
        record.pages[String(offset)] = page;
        page.error = message;
        page.inFlight = false;
        rebuildValueQueryRecord(record);
      });
    },

    removeValueQueryInFlight: (key, offset) => {
      setDraft((draft) => {
        const record = draft.valueQueries[key];
        if (!record) {
          return;
        }
        const page = record.pages[String(offset)];
        if (!page) {
          return;
        }
        page.inFlight = false;
        rebuildValueQueryRecord(record);
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
        record.stale = false;
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

    invalidateDetail: (kind, id) => {
      setDraft((draft) => {
        const container = getDetailContainer(draft, kind);
        const record = container[id];
        if (!record) {
          return;
        }
        record.stale = true;
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
          stale: false,
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
          stale: false,
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
          stale: false,
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
          stale: false,
          status: "success",
        };
      });
    },

    markTrackAvailabilityLoading: (recordingIds, options) => {
      setDraft((draft) => {
        for (const recordingId of recordingIds) {
          const record =
            draft.trackAvailabilityByRecordingId[recordingId] ??
            createDetailRecord();
          draft.trackAvailabilityByRecordingId[recordingId] = record;
          record.error = "";
          record.inFlight = true;
          record.isRefreshing = options?.refreshing ?? record.data !== null;
          record.stale = false;
          record.status = record.data !== null ? "success" : "loading";
        }
      });
    },

    markTrackAvailabilityError: (recordingIds, message) => {
      setDraft((draft) => {
        for (const recordingId of recordingIds) {
          const record =
            draft.trackAvailabilityByRecordingId[recordingId] ??
            createDetailRecord();
          draft.trackAvailabilityByRecordingId[recordingId] = record;
          record.error = message;
          record.inFlight = false;
          record.isRefreshing = false;
          record.status = record.data !== null ? "success" : "error";
        }
      });
    },

    setTrackAvailability: (items, fetchedAt) => {
      setDraft((draft) => {
        const nextFetchedAt = fetchedAt ?? Date.now();
        for (const item of items) {
          draft.trackAvailabilityByRecordingId[item.RecordingID] = {
            data: item,
            error: "",
            inFlight: false,
            isRefreshing: false,
            lastFetchedAt: nextFetchedAt,
            stale: false,
            status: "success",
          };
        }
      });
    },

    invalidateTrackAvailability: (recordingIds) => {
      setDraft((draft) => {
        for (const recordingId of recordingIds) {
          const record = draft.trackAvailabilityByRecordingId[recordingId];
          if (!record) {
            continue;
          }
          record.stale = true;
        }
      });
    },

    markAlbumAvailabilityLoading: (albumIds, options) => {
      setDraft((draft) => {
        for (const albumId of albumIds) {
          const record =
            draft.albumAvailabilityByAlbumId[albumId] ?? createDetailRecord();
          draft.albumAvailabilityByAlbumId[albumId] = record;
          record.error = "";
          record.inFlight = true;
          record.isRefreshing = options?.refreshing ?? record.data !== null;
          record.stale = false;
          record.status = record.data !== null ? "success" : "loading";
        }
      });
    },

    markAlbumAvailabilityError: (albumIds, message) => {
      setDraft((draft) => {
        for (const albumId of albumIds) {
          const record =
            draft.albumAvailabilityByAlbumId[albumId] ?? createDetailRecord();
          draft.albumAvailabilityByAlbumId[albumId] = record;
          record.error = message;
          record.inFlight = false;
          record.isRefreshing = false;
          record.status = record.data !== null ? "success" : "error";
        }
      });
    },

    setAlbumAvailability: (items, fetchedAt) => {
      setDraft((draft) => {
        const nextFetchedAt = fetchedAt ?? Date.now();
        for (const item of items) {
          draft.albumAvailabilityByAlbumId[item.AlbumID] = {
            data: item.Availability,
            error: "",
            inFlight: false,
            isRefreshing: false,
            lastFetchedAt: nextFetchedAt,
            stale: false,
            status: "success",
          };
        }
      });
    },

    invalidateAlbumAvailability: (albumIds) => {
      setDraft((draft) => {
        for (const albumId of albumIds) {
          const record = draft.albumAvailabilityByAlbumId[albumId];
          if (!record) {
            continue;
          }
          record.stale = true;
        }
      });
    },
  };
});
