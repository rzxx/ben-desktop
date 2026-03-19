import {
  listAlbumAvailabilitySummaries,
  listRecordingPlaybackAvailability,
} from "@/lib/api/playback";
import {
  getAlbumAvailabilityRecord,
  getTrackAvailabilityRecord,
  useCatalogStore,
} from "@/stores/catalog/store";
import {
  compactIds,
  describeError,
  type EnsureOptions,
  loadChunkedBulk,
} from "./loader-shared";

const CATALOG_PLAYBACK_PROFILE = "desktop";
const TRACK_AVAILABILITY_CHUNK_SIZE = 40;
const ALBUM_AVAILABILITY_CHUNK_SIZE = 12;

export function ensureTrackAvailability(
  recordingIds: string[],
  options: EnsureOptions = {},
) {
  const ids = compactIds(recordingIds);
  if (ids.length === 0) {
    return Promise.resolve([]);
  }

  const { force = false } = options;
  const pending = ids.filter((recordingId) => {
    const record = getTrackAvailabilityRecord(
      useCatalogStore.getState(),
      recordingId,
    );
    if (record.inFlight) {
      return false;
    }
    if (!force && record.data !== null && !record.stale) {
      return false;
    }
    return true;
  });
  if (pending.length === 0) {
    return Promise.resolve([]);
  }

  const state = useCatalogStore.getState();
  state.markTrackAvailabilityLoading(pending, {
    refreshing: pending.some(
      (recordingId) =>
        getTrackAvailabilityRecord(useCatalogStore.getState(), recordingId)
          .data !== null,
    ),
  });

  return loadChunkedBulk({
    ids: pending,
    chunkSize: TRACK_AVAILABILITY_CHUNK_SIZE,
    requestKeyPrefix: "trackAvailability",
    loadChunk: (chunk) =>
      listRecordingPlaybackAvailability(chunk, CATALOG_PLAYBACK_PROFILE),
    onChunkLoaded: (items) => {
      useCatalogStore.getState().setTrackAvailability(items);
    },
  }).catch((error) => {
    useCatalogStore
      .getState()
      .markTrackAvailabilityError(pending, describeError(error));
    throw error;
  });
}

export function ensureAlbumAvailability(
  albumIds: string[],
  options: EnsureOptions = {},
) {
  const ids = compactIds(albumIds);
  if (ids.length === 0) {
    return Promise.resolve([]);
  }

  const { force = false } = options;
  const pending = ids.filter((albumId) => {
    const record = getAlbumAvailabilityRecord(
      useCatalogStore.getState(),
      albumId,
    );
    if (record.inFlight) {
      return false;
    }
    if (!force && record.data !== null && !record.stale) {
      return false;
    }
    return true;
  });
  if (pending.length === 0) {
    return Promise.resolve([]);
  }

  const state = useCatalogStore.getState();
  state.markAlbumAvailabilityLoading(pending, {
    refreshing: pending.some(
      (albumId) =>
        getAlbumAvailabilityRecord(useCatalogStore.getState(), albumId).data !==
        null,
    ),
  });

  return loadChunkedBulk({
    ids: pending,
    chunkSize: ALBUM_AVAILABILITY_CHUNK_SIZE,
    requestKeyPrefix: "albumAvailability",
    loadChunk: (chunk) =>
      listAlbumAvailabilitySummaries(chunk, CATALOG_PLAYBACK_PROFILE),
    onChunkLoaded: (items) => {
      useCatalogStore.getState().setAlbumAvailability(items);
    },
  }).catch((error) => {
    useCatalogStore
      .getState()
      .markAlbumAvailabilityError(pending, describeError(error));
    throw error;
  });
}
