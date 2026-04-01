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

type AvailabilityState =
  | {
      State?: string | null;
    }
  | null
  | undefined;

function availabilityStateForEntry(
  entry: PlaybackModels.SessionEntry,
  entryAvailabilityByEntryId: Record<string, AvailabilityState>,
  trackAvailabilityByRecordingId: Record<
    string,
    { data?: { State?: string | null } | null }
  >,
) {
  return (
    entryAvailabilityByEntryId[entry.entryId]?.State ??
    trackAvailabilityByRecordingId[entry.item.recordingId]?.data?.State
  );
}

export function buildQueueRows(
  userQueueEntries: PlaybackModels.SessionEntry[],
  contextQueueEntries: PlaybackModels.SessionEntry[],
  entryAvailabilityByEntryId: Record<string, AvailabilityState>,
  trackAvailabilityByRecordingId: Record<
    string,
    { data?: { State?: string | null } | null }
  >,
): QueueRow[] {
  const rows: QueueRow[] = [];

  if (userQueueEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-user-queue",
      title: "User Queue",
    });
    userQueueEntries.forEach((entry) => {
      const availabilityState = availabilityStateForEntry(
        entry,
        entryAvailabilityByEntryId,
        trackAvailabilityByRecordingId,
      );
      rows.push({
        type: "entry",
        id: `user-${entry.entryId}`,
        entry,
        actionable: isCatalogTrackActionable(availabilityState ?? undefined),
        secondaryText: queueEntrySecondaryText(entry, availabilityState),
      });
    });
  }

  if (contextQueueEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-context-queue",
      title: "Context Queue",
    });
    contextQueueEntries.forEach((entry) => {
      const availabilityState = availabilityStateForEntry(
        entry,
        entryAvailabilityByEntryId,
        trackAvailabilityByRecordingId,
      );
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
