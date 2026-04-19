import { beforeEach, describe, expect, test } from "vitest";
import { Types } from "@/lib/api/models";
import { useCatalogStore } from "./store";

function makePageInfo(source: Partial<Types.PageInfo> = {}) {
  return Types.PageInfo.createFrom({
    Limit: 120,
    Offset: 0,
    Returned: 1,
    Total: 1,
    HasMore: false,
    NextOffset: 0,
    ...source,
  });
}

function makeRecording(source: Partial<Types.RecordingListItem> = {}) {
  return Types.RecordingListItem.createFrom({
    LibraryRecordingID: "lib-rec-1",
    PreferredVariantRecordingID: "variant-rec-1",
    TrackClusterID: "lib-rec-1",
    RecordingID: "rec-1",
    AlbumID: "album-1",
    Title: "Track 1",
    DurationMS: 1_000,
    Artists: ["Artist 1"],
    VariantCount: 1,
    HasVariants: false,
    ...source,
  });
}

function makePlaylistTrack(source: Partial<Types.PlaylistTrackItem> = {}) {
  return Types.PlaylistTrackItem.createFrom({
    ItemID: "item-1",
    LibraryRecordingID: "lib-playlist-1",
    RecordingID: "rec-playlist-1",
    AlbumID: "album-playlist-1",
    Title: "Playlist Track 1",
    DurationMS: 1_000,
    Artists: ["Playlist Artist 1"],
    AddedAt: new Date("2026-01-01T00:00:00.000Z"),
    ...source,
  });
}

beforeEach(() => {
  useCatalogStore.setState({
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
  });
});

describe("catalog track lookup indexes", () => {
  test("rebuilds indexes from loaded non-stale pages and prunes invalidated queries", () => {
    useCatalogStore.getState().setValueQueryPage(
      "tracks",
      [makeRecording()],
      makePageInfo(),
      0,
      (item) => item.RecordingID,
    );
    useCatalogStore.getState().setValueQueryPage(
      "playlistTracks:playlist-1",
      [makePlaylistTrack()],
      makePageInfo(),
      0,
      (item) => item.ItemID,
    );

    let state = useCatalogStore.getState();
    expect(state.trackItemsByRecordingId["rec-1"]?.Title).toBe("Track 1");
    expect(state.trackItemsByLibraryRecordingId["lib-rec-1"]?.Title).toBe(
      "Track 1",
    );
    expect(state.playlistTrackItemsByItemId["item-1"]?.Title).toBe(
      "Playlist Track 1",
    );

    useCatalogStore
      .getState()
      .invalidateValueQuery("playlistTracks:playlist-1", { dropAfterOffset: 0 });

    state = useCatalogStore.getState();
    expect(state.trackItemsByRecordingId["rec-1"]?.Title).toBe("Track 1");
    expect(state.playlistTrackItemsByItemId["item-1"]).toBeUndefined();

    useCatalogStore
      .getState()
      .invalidateValueQuery("tracks", { dropAfterOffset: 0 });

    state = useCatalogStore.getState();
    expect(state.trackItemsByRecordingId["rec-1"]).toBeUndefined();
    expect(state.trackItemsByLibraryRecordingId["lib-rec-1"]).toBeUndefined();
  });

  test("drops lookup entries from replaced front pages and removed later pages", () => {
    useCatalogStore.getState().setValueQueryPage(
      "tracks",
      [
        makeRecording({
          LibraryRecordingID: "lib-rec-1",
          PreferredVariantRecordingID: "variant-rec-1",
          TrackClusterID: "lib-rec-1",
          RecordingID: "rec-1",
          Title: "Track 1",
        }),
      ],
      makePageInfo({
        Total: 2,
        HasMore: true,
        NextOffset: 120,
      }),
      0,
      (item) => item.RecordingID,
    );
    useCatalogStore.getState().setValueQueryPage(
      "tracks",
      [
        makeRecording({
          LibraryRecordingID: "lib-rec-2",
          PreferredVariantRecordingID: "variant-rec-2",
          TrackClusterID: "lib-rec-2",
          RecordingID: "rec-2",
          Title: "Track 2",
        }),
      ],
      makePageInfo({
        Offset: 120,
        Total: 2,
      }),
      120,
      (item) => item.RecordingID,
    );

    let state = useCatalogStore.getState();
    expect(state.trackItemsByRecordingId["rec-1"]?.Title).toBe("Track 1");
    expect(state.trackItemsByRecordingId["rec-2"]?.Title).toBe("Track 2");

    useCatalogStore.getState().setValueQueryPage(
      "tracks",
      [
        makeRecording({
          LibraryRecordingID: "lib-rec-3",
          PreferredVariantRecordingID: "variant-rec-3",
          TrackClusterID: "lib-rec-3",
          RecordingID: "rec-3",
          Title: "Track 3",
        }),
      ],
      makePageInfo({
        Total: 1,
      }),
      0,
      (item) => item.RecordingID,
    );

    state = useCatalogStore.getState();
    expect(state.trackItemsByRecordingId["rec-1"]).toBeUndefined();
    expect(state.trackItemsByRecordingId["rec-2"]).toBeUndefined();
    expect(state.trackItemsByRecordingId["rec-3"]?.Title).toBe("Track 3");
  });
});
