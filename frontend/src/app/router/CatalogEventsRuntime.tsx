import { useEffect } from "react";
import { Events } from "@wailsio/runtime";
import { subscribeCatalogEvents } from "@/lib/api/catalog";
import { Types } from "@/lib/api/models";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  ensureAlbumAvailability,
  ensureTrackAvailability,
} from "@/lib/catalog/loader-availability";
import {
  getDetailRecord,
  getIdQuery,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { useRecordingLikesStore } from "@/stores/catalog/likes";

function hasLoadedIdQuery(key: string) {
  const record = getIdQuery(useCatalogStore.getState(), key);
  return record.ids.length > 0 || record.loadedOffsets.length > 0;
}

function hasLoadedValueQuery(key: string) {
  const record = getValueQuery(useCatalogStore.getState(), key);
  return record.items.length > 0 || record.loadedOffsets.length > 0;
}

function loadedTrackAvailabilityIDs() {
  return Object.entries(
    useCatalogStore.getState().trackAvailabilityByRecordingId,
  )
    .filter(([, record]) => record.data !== null)
    .map(([recordingID]) => recordingID);
}

function loadedAlbumAvailabilityIDs() {
  return Object.entries(useCatalogStore.getState().albumAvailabilityByAlbumId)
    .filter(([, record]) => record.data !== null)
    .map(([albumID]) => albumID);
}

function refetchDynamicValueQueries(
  prefix: string,
  refetch: (id: string) => void,
) {
  const keys = Object.keys(useCatalogStore.getState().valueQueries).filter(
    (key) => key.startsWith(prefix),
  );
  for (const key of keys) {
    useCatalogStore.getState().invalidateValueQuery(key, {
      dropAfterOffset: 0,
    });
    if (!hasLoadedValueQuery(key)) {
      continue;
    }
    const id = key.slice(prefix.length);
    void refetch(id);
  }
}

function handleBaseInvalidation(
  event: InstanceType<typeof Types.CatalogChangeEvent>,
) {
  const store = useCatalogStore.getState();

  switch (event.QueryKey) {
    case "albums":
      store.invalidateIdQuery("albums", { dropAfterOffset: 0 });
      if (hasLoadedIdQuery("albums")) {
        void catalogLoaderClient.refetchAlbums();
      }
      break;
    case "artists":
      store.invalidateIdQuery("artists", { dropAfterOffset: 0 });
      if (hasLoadedIdQuery("artists")) {
        void catalogLoaderClient.refetchArtists();
      }
      break;
    case "tracks":
      store.invalidateValueQuery("tracks", { dropAfterOffset: 0 });
      if (hasLoadedValueQuery("tracks")) {
        void catalogLoaderClient.refetchTracks();
      }
      break;
    case "playlists":
      store.invalidateIdQuery("playlists", { dropAfterOffset: 0 });
      if (hasLoadedIdQuery("playlists")) {
        void catalogLoaderClient.refetchPlaylists();
      }
      break;
    case "liked":
      store.invalidateValueQuery("liked", { dropAfterOffset: 0 });
      break;
    default:
      if (event.QueryKey.startsWith("albumTracks:")) {
        store.invalidateValueQuery(event.QueryKey, { dropAfterOffset: 0 });
        if (hasLoadedValueQuery(event.QueryKey)) {
          const albumId = event.QueryKey.slice("albumTracks:".length);
          void catalogLoaderClient.ensureAlbumTracksPage(albumId, 0, {
            force: true,
          });
        }
      } else if (event.QueryKey.startsWith("artistAlbums:")) {
        store.invalidateValueQuery(event.QueryKey, { dropAfterOffset: 0 });
        if (hasLoadedValueQuery(event.QueryKey)) {
          const artistId = event.QueryKey.slice("artistAlbums:".length);
          void catalogLoaderClient.ensureArtistAlbumsPage(artistId, 0, {
            force: true,
          });
        }
      } else if (event.QueryKey.startsWith("playlistTracks:")) {
        store.invalidateValueQuery(event.QueryKey, { dropAfterOffset: 0 });
        if (hasLoadedValueQuery(event.QueryKey)) {
          const playlistId = event.QueryKey.slice("playlistTracks:".length);
          void catalogLoaderClient.ensurePlaylistTracksPage(playlistId, 0, {
            force: true,
          });
        }
      }
      break;
  }

  if (
    event.InvalidateAll &&
    event.Entity === Types.CatalogChangeEntity.CatalogChangeEntityArtistAlbums
  ) {
    refetchDynamicValueQueries("artistAlbums:", (artistId) => {
      void catalogLoaderClient.ensureArtistAlbumsPage(artistId, 0, {
        force: true,
      });
    });
  }

  for (const albumId of event.AlbumIDs ?? []) {
    store.invalidateDetail("album", albumId);
    store.invalidateDetail("albumVariants", albumId);
    store.invalidateAlbumAvailability([albumId]);
    const albumDetail = getDetailRecord(
      useCatalogStore.getState().albumDetails,
      albumId,
    );
    const albumVariants = getDetailRecord(
      useCatalogStore.getState().albumVariants,
      albumId,
    );
    if (albumDetail.data !== null || albumVariants.data !== null) {
      void catalogLoaderClient.refetchAlbum(albumId);
    }
  }

  if (
    event.EntityID &&
    event.Entity === Types.CatalogChangeEntity.CatalogChangeEntityPlaylists
  ) {
    store.invalidateDetail("playlistSummary", event.EntityID);
    const detail = getDetailRecord(
      useCatalogStore.getState().playlistSummaries,
      event.EntityID,
    );
    if (detail.data !== null) {
      void catalogLoaderClient.refetchPlaylist(event.EntityID);
    }
  }

  if (event.RecordingIDs?.length) {
    store.invalidateTrackAvailability(event.RecordingIDs);
    useRecordingLikesStore
      .getState()
      .invalidateRecordingLikes(event.RecordingIDs);
    void ensureTrackAvailability(event.RecordingIDs, { force: true });
  }
  if (event.AlbumIDs?.length) {
    store.invalidateAlbumAvailability(event.AlbumIDs);
    void ensureAlbumAvailability(event.AlbumIDs, { force: true });
  }
}

function handleAvailabilityInvalidation(
  event: InstanceType<typeof Types.CatalogChangeEvent>,
) {
  const store = useCatalogStore.getState();
  const recordingIDs = event.InvalidateAll
    ? loadedTrackAvailabilityIDs()
    : (event.RecordingIDs ?? []);
  const albumIDs = event.InvalidateAll
    ? loadedAlbumAvailabilityIDs()
    : (event.AlbumIDs ?? []);
  if (
    event.InvalidateAll &&
    event.Entity === Types.CatalogChangeEntity.CatalogChangeEntityLiked
  ) {
    useRecordingLikesStore.getState().invalidateAllRecordingLikes();
  }

  if (recordingIDs.length) {
    store.invalidateTrackAvailability(recordingIDs);
    useRecordingLikesStore.getState().invalidateRecordingLikes(recordingIDs);
    void ensureTrackAvailability(recordingIDs, { force: true });
  }
  if (albumIDs.length) {
    store.invalidateAlbumAvailability(albumIDs);
    void ensureAlbumAvailability(albumIDs, { force: true });
  }
}

export function CatalogEventsRuntime() {
  useEffect(() => {
    let stopListening: (() => void) | undefined;

    void subscribeCatalogEvents().then((eventName) => {
      stopListening = Events.On(eventName, (event) => {
        const changeEvent = Types.CatalogChangeEvent.createFrom(event.data);
        switch (changeEvent.Kind) {
          case Types.CatalogChangeKind.CatalogChangeInvalidateBase:
            handleBaseInvalidation(changeEvent);
            break;
          case Types.CatalogChangeKind.CatalogChangeInvalidateAvailability:
            handleAvailabilityInvalidation(changeEvent);
            break;
          default:
            break;
        }
      });
    });

    return () => {
      stopListening?.();
    };
  }, []);

  return null;
}
