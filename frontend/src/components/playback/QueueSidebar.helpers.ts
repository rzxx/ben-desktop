import type { PlaybackModels } from "@/lib/api/models";
import { availabilityLabel, isCatalogTrackActionable } from "@/lib/format";
import type { CatalogTrackLookupItem } from "@/stores/catalog/types";

type QueueContextState = {
  title?: string;
  entries?: PlaybackModels.SessionEntry[];
  hasBefore?: boolean;
  hasAfter?: boolean;
};

export type QueueRow =
  | { type: "section"; id: string; title: string }
  | { type: "marker"; id: string; title: string }
  | {
      type: "entry";
      id: string;
      entry: PlaybackModels.SessionEntry;
      actionable: boolean;
      title: string;
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
  contextQueue: QueueContextState | null | undefined,
  entryAvailabilityByEntryId: Record<string, AvailabilityState>,
  catalogTrackItemsByRecordingId: Record<string, CatalogTrackLookupItem>,
  catalogTrackItemsByLibraryRecordingId: Record<string, CatalogTrackLookupItem>,
  playlistTrackItemsByItemId: Record<string, CatalogTrackLookupItem>,
  trackAvailabilityByRecordingId: Record<
    string,
    { data?: { State?: string | null } | null }
  >,
): QueueRow[] {
  const rows: QueueRow[] = [];
  const contextQueueEntries = contextQueue?.entries ?? [];
  const pushEntryRow = (
    idPrefix: string,
    entry: PlaybackModels.SessionEntry,
  ) => {
    if (!entry.entryId) {
      return;
    }
    const availabilityState = availabilityStateForEntry(
      entry,
      entryAvailabilityByEntryId,
      trackAvailabilityByRecordingId,
    );
    const cachedTrack = cachedTrackForEntry(
      entry,
      catalogTrackItemsByRecordingId,
      catalogTrackItemsByLibraryRecordingId,
      playlistTrackItemsByItemId,
    );
    rows.push({
      type: "entry",
      id: `${idPrefix}-${entry.entryId}`,
      entry,
      actionable: isCatalogTrackActionable(availabilityState ?? undefined),
      title: entry.item.title || cachedTrack?.Title || "",
      secondaryText: queueEntrySecondaryText(
        entry,
        availabilityState,
        cachedTrack,
      ),
    });
  };

  if (userQueueEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-user-queue",
      title: "User Queue",
    });
    userQueueEntries.forEach((entry) => {
      pushEntryRow("user", entry);
    });
  }

  if (contextQueueEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-context-queue",
      title: contextQueue?.title || "Context Queue",
    });
    if (contextQueue?.hasBefore) {
      rows.push({
        type: "marker",
        id: "context-earlier",
        title: "Earlier in context",
      });
    }
    contextQueueEntries.forEach((entry) => {
      pushEntryRow("context", entry);
    });
    if (contextQueue?.hasAfter) {
      rows.push({
        type: "marker",
        id: "context-more",
        title: "More in context",
      });
    }
  }

  return rows;
}

export function queueEntrySecondaryText(
  entry: PlaybackModels.SessionEntry,
  availabilityState?: string | null,
  cachedTrack?: CatalogTrackLookupItem | null,
) {
  const subtitle = entry.item.subtitle || cachedTrackSubtitle(cachedTrack);
  if (isCatalogTrackActionable(availabilityState ?? undefined)) {
    return subtitle;
  }
  return subtitle
    ? `${subtitle} • ${availabilityLabel(availabilityState ?? undefined)}`
    : availabilityLabel(availabilityState ?? undefined);
}

function cachedTrackForEntry(
  entry: PlaybackModels.SessionEntry,
  catalogTrackItemsByRecordingId: Record<string, CatalogTrackLookupItem>,
  catalogTrackItemsByLibraryRecordingId: Record<string, CatalogTrackLookupItem>,
  playlistTrackItemsByItemId: Record<string, CatalogTrackLookupItem>,
) {
  if (entry.item.sourceItemId) {
    const playlistTrack = playlistTrackItemsByItemId[entry.item.sourceItemId];
    if (playlistTrack) {
      return playlistTrack;
    }
  }
  if (entry.item.libraryRecordingId) {
    const libraryTrack =
      catalogTrackItemsByLibraryRecordingId[entry.item.libraryRecordingId];
    if (libraryTrack) {
      return libraryTrack;
    }
  }
  if (entry.item.recordingId) {
    return catalogTrackItemsByRecordingId[entry.item.recordingId] ?? null;
  }
  return null;
}

function cachedTrackSubtitle(cachedTrack?: CatalogTrackLookupItem | null) {
  if (!cachedTrack || !("Artists" in cachedTrack)) {
    return "";
  }
  return cachedTrack.Artists?.join(", ") ?? "";
}
