import { describe, expect, test } from "vitest";
import type { LikedRecordingItem } from "@/lib/api/models";
import { likedPlaybackRecordingId } from "./-playback-id";

describe("likedPlaybackRecordingId", () => {
  test("prefers library recording id for liked playback actions", () => {
    const track: LikedRecordingItem = {
      LibraryRecordingID: "library-rec-1",
      RecordingID: "variant-rec-1",
      AlbumID: "album-1",
      Title: "Track 1",
      DurationMS: 1000,
      Artists: [],
      AddedAt: new Date(),
    };

    expect(likedPlaybackRecordingId(track)).toBe("library-rec-1");
  });

  test("falls back to variant recording id when library id is missing", () => {
    const track: LikedRecordingItem = {
      LibraryRecordingID: "",
      RecordingID: "variant-rec-2",
      AlbumID: "album-2",
      Title: "Track 2",
      DurationMS: 1000,
      Artists: [],
      AddedAt: new Date(),
    };

    expect(likedPlaybackRecordingId(track)).toBe("variant-rec-2");
  });
});
