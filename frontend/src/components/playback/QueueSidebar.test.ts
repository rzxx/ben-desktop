import { describe, expect, test } from "vitest";
import { PlaybackModels } from "@/lib/api/models";
import {
  buildQueueRows,
  queueEntrySecondaryText,
} from "./QueueSidebar.helpers";

function makeEntry(source: Partial<PlaybackModels.SessionEntry> = {}) {
  return new PlaybackModels.SessionEntry({
    entryId: "entry-1",
    origin: PlaybackModels.EntryOrigin.EntryOriginQueued,
    item: new PlaybackModels.SessionItem({
      recordingId: "rec-1",
      title: "Track 1",
      subtitle: "Artist 1",
      durationMs: 1_000,
    }),
    ...source,
  });
}

describe("QueueSidebar queue row state", () => {
  test("marks unavailable rows as non-actionable and appends availability text", () => {
    const rows = buildQueueRows(
      [makeEntry()],
      null,
      {},
      {},
      {},
      {},
      {
        "rec-1": {
          data: {
            State: "UNAVAILABLE:NO_PATH",
          },
        },
      },
    );

    expect(rows).toHaveLength(2);
    expect(rows[1]).toMatchObject({
      type: "entry",
      actionable: false,
      secondaryText: "Artist 1 • Unavailable",
    });
  });

  test("prefers playback snapshot entry availability over catalog fallback", () => {
    const rows = buildQueueRows(
      [makeEntry()],
      null,
      {
        "entry-1": {
          State: "PLAYABLE:CACHED_OPT",
        },
      },
      {},
      {},
      {},
      {
        "rec-1": {
          data: {
            State: "UNAVAILABLE:NO_PATH",
          },
        },
      },
    );

    expect(rows[1]).toMatchObject({
      type: "entry",
      actionable: true,
      secondaryText: "Artist 1",
    });
  });

  test("keeps actionable subtitle unchanged", () => {
    const entry = makeEntry();

    expect(queueEntrySecondaryText(entry, "PLAYABLE:REMOTE_OPT")).toBe(
      "Artist 1",
    );
  });

  test("prefers cached catalog metadata when queue entry metadata is incomplete", () => {
    const rows = buildQueueRows(
      [
        makeEntry({
          item: new PlaybackModels.SessionItem({
            recordingId: "rec-1",
            title: "",
            subtitle: "",
            durationMs: 1_000,
          }),
        }),
      ],
      null,
      {},
      {
        "rec-1": {
          RecordingID: "rec-1",
          LibraryRecordingID: "lib-rec-1",
          Title: "Cached Track",
          Artists: ["Cached Artist"],
          DurationMS: 1_000,
        } as never,
      },
      {},
      {},
      {
        "rec-1": {
          data: {
            State: "PLAYABLE:LOCAL_FILE",
          },
        },
      },
    );

    expect(rows[1]).toMatchObject({
      type: "entry",
      title: "Cached Track",
      secondaryText: "Cached Artist",
    });
  });

  test("keeps queue entry title when cached metadata disagrees", () => {
    const rows = buildQueueRows(
      [makeEntry()],
      null,
      {},
      {
        "rec-1": {
          RecordingID: "rec-1",
          LibraryRecordingID: "lib-rec-1",
          Title: "Stale Cached Title",
          Artists: ["Cached Artist"],
          DurationMS: 1_000,
        } as never,
      },
      {},
      {},
      {
        "rec-1": {
          data: {
            State: "PLAYABLE:LOCAL_FILE",
          },
        },
      },
    );

    expect(rows[1]).toMatchObject({
      type: "entry",
      title: "Track 1",
      secondaryText: "Artist 1",
    });
  });

  test("renders context markers when earlier and later items are hidden", () => {
    const rows = buildQueueRows(
      [],
      {
        title: "Tracks",
        entries: [makeEntry()],
        hasBefore: true,
        hasAfter: true,
      },
      {},
      {},
      {},
      {},
      {
        "rec-1": {
          data: {
            State: "PLAYABLE:LOCAL_FILE",
          },
        },
      },
    );

    expect(rows).toMatchObject([
      { type: "section", title: "Tracks" },
      { type: "marker", title: "Earlier in context" },
      { type: "entry", title: "Track 1" },
      { type: "marker", title: "More in context" },
    ]);
  });
});
