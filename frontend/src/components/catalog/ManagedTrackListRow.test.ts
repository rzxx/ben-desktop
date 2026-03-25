import { describe, expect, test } from "vitest";
import { PlaybackModels } from "@/lib/api/models";
import { isTrackListRowActive } from "./ManagedTrackListRow";

describe("isTrackListRowActive", () => {
  test("matches the visible playback item by library recording id", () => {
    const playbackItem = new PlaybackModels.SessionItem({
      libraryRecordingId: "lib-rec-1",
      recordingId: "rec-1",
      title: "Track 1",
      subtitle: "Artist 1",
      durationMs: 1_000,
    });

    expect(
      isTrackListRowActive(playbackItem, {
        libraryRecordingId: "lib-rec-1",
        recordingId: "rec-2",
      }),
    ).toBe(true);
  });

  test("falls back to recording id when there is no library recording id match", () => {
    const playbackItem = new PlaybackModels.SessionItem({
      recordingId: "rec-1",
      title: "Track 1",
      subtitle: "Artist 1",
      durationMs: 1_000,
    });

    expect(
      isTrackListRowActive(playbackItem, {
        recordingId: "rec-1",
      }),
    ).toBe(true);
  });

  test("returns false when the row does not match the visible playback item", () => {
    const playbackItem = new PlaybackModels.SessionItem({
      libraryRecordingId: "lib-rec-1",
      recordingId: "rec-1",
      title: "Track 1",
      subtitle: "Artist 1",
      durationMs: 1_000,
    });

    expect(
      isTrackListRowActive(playbackItem, {
        libraryRecordingId: "lib-rec-2",
        recordingId: "rec-2",
      }),
    ).toBe(false);
  });
});
