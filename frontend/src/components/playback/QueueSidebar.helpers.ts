import type { PlaybackModels } from "@/lib/api/models";
import { availabilityLabel, isCatalogTrackActionable } from "@/lib/format";

export type QueueRow =
  | { type: "section"; id: string; title: string }
  | {
      type: "entry";
      id: string;
      entry: PlaybackModels.SessionEntry;
      actionable: boolean;
      secondaryText: string;
    };

export function buildQueueRows(
  queuedEntries: PlaybackModels.SessionEntry[],
  listEntries: PlaybackModels.SessionEntry[],
  trackAvailabilityByRecordingId: Record<
    string,
    { data?: { State?: string | null } | null }
  >,
): QueueRow[] {
  const rows: QueueRow[] = [];

  if (queuedEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-queued",
      title: "Queued",
    });
    queuedEntries.forEach((entry) => {
      const availabilityState =
        trackAvailabilityByRecordingId[entry.item.recordingId]?.data?.State;
      rows.push({
        type: "entry",
        id: `queued-${entry.entryId}`,
        entry,
        actionable: isCatalogTrackActionable(availabilityState ?? undefined),
        secondaryText: queueEntrySecondaryText(entry, availabilityState),
      });
    });
  }

  if (listEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-context",
      title: "Context",
    });
    listEntries.forEach((entry) => {
      const availabilityState =
        trackAvailabilityByRecordingId[entry.item.recordingId]?.data?.State;
      rows.push({
        type: "entry",
        id: `context-${entry.entryId}`,
        entry,
        actionable: isCatalogTrackActionable(availabilityState ?? undefined),
        secondaryText: queueEntrySecondaryText(entry, availabilityState),
      });
    });
  }

  return rows;
}

export function queueEntrySecondaryText(
  entry: PlaybackModels.SessionEntry,
  availabilityState?: string | null,
) {
  const subtitle = entry.item.subtitle;
  if (isCatalogTrackActionable(availabilityState ?? undefined)) {
    return subtitle;
  }
  return subtitle
    ? `${subtitle} • ${availabilityLabel(availabilityState ?? undefined)}`
    : availabilityLabel(availabilityState ?? undefined);
}
