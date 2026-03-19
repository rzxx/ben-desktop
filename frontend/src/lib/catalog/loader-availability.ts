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
  dedupeRequest,
  describeError,
  type EnsureOptions,
} from "./loader-shared";

const CATALOG_PLAYBACK_PROFILE = "desktop";
const ALBUM_AVAILABILITY_CHUNK_SIZE = 12;

function compactIds(values: string[]) {
  return Array.from(
    new Set(values.map((value) => value.trim()).filter(Boolean)),
  );
}

function chunkIds(values: string[], size: number) {
  const chunks: string[][] = [];
  for (let index = 0; index < values.length; index += size) {
    chunks.push(values.slice(index, index + size));
  }
  return chunks;
}

function waitForNextPaint() {
  return new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
}

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

  const requestKey = `trackAvailability:${pending.slice().sort().join(",")}`;
  return dedupeRequest(requestKey, async () => {
    const state = useCatalogStore.getState();
    state.markTrackAvailabilityLoading(pending, {
      refreshing: pending.some(
        (recordingId) =>
          getTrackAvailabilityRecord(useCatalogStore.getState(), recordingId)
            .data !== null,
      ),
    });

    try {
      const items = await listRecordingPlaybackAvailability(
        pending,
        CATALOG_PLAYBACK_PROFILE,
      );
      useCatalogStore.getState().setTrackAvailability(items);
      return items;
    } catch (error) {
      useCatalogStore
        .getState()
        .markTrackAvailabilityError(pending, describeError(error));
      throw error;
    }
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

  const chunks = chunkIds(pending, ALBUM_AVAILABILITY_CHUNK_SIZE);
  const results: Awaited<ReturnType<typeof listAlbumAvailabilitySummaries>> = [];

  return (async () => {
    try {
      for (const [index, chunk] of chunks.entries()) {
        const requestKey = `albumAvailability:${chunk.slice().sort().join(",")}`;
        const items = await dedupeRequest(requestKey, () =>
          listAlbumAvailabilitySummaries(chunk, CATALOG_PLAYBACK_PROFILE),
        );
        useCatalogStore.getState().setAlbumAvailability(items);
        results.push(...items);
        if (index < chunks.length - 1) {
          await waitForNextPaint();
        }
      }
      return results;
    } catch (error) {
      useCatalogStore
        .getState()
        .markAlbumAvailabilityError(pending, describeError(error));
      throw error;
    }
  })();
}
