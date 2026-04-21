import { describe, expect, test } from "vitest";
import type { OfflineRecordingItem } from "@/lib/api/models";
import { offlinePlaybackRecordingId } from "./-playback-id";

describe("offlinePlaybackRecordingId", () => {
  test("prefers library recording id for offline playback actions", () => {
    const track: OfflineRecordingItem = {
      LibraryRecordingID: "library-rec-1",
      RecordingID: "variant-rec-1",
      AlbumID: "album-1",
      Title: "Track 1",
      DurationMS: 1000,
      Artists: [],
      OfflineSince: new Date(),
      HasLocalSource: true,
      HasLocalCached: false,
    };

    expect(offlinePlaybackRecordingId(track)).toBe("library-rec-1");
  });

  test("falls back to variant recording id when library id is missing", () => {
    const track: OfflineRecordingItem = {
      LibraryRecordingID: "",
      RecordingID: "variant-rec-2",
      AlbumID: "album-2",
      Title: "Track 2",
      DurationMS: 1000,
      Artists: [],
      OfflineSince: new Date(),
      HasLocalSource: false,
      HasLocalCached: true,
    };

    expect(offlinePlaybackRecordingId(track)).toBe("variant-rec-2");
  });
});
