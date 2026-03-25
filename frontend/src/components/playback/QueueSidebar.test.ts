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
    const rows = buildQueueRows([makeEntry()], [], {
      "rec-1": {
        data: {
          State: "UNAVAILABLE:NO_PATH",
        },
      },
    });

    expect(rows).toHaveLength(2);
    expect(rows[1]).toMatchObject({
      type: "entry",
      actionable: false,
      secondaryText: "Artist 1 • Unavailable",
    });
  });

  test("keeps actionable subtitle unchanged", () => {
    const entry = makeEntry();

    expect(queueEntrySecondaryText(entry, "PLAYABLE:REMOTE_OPT")).toBe(
      "Artist 1",
    );
  });
});
