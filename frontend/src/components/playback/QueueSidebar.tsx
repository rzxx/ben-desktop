import { useEffect, useMemo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ListMusic, X } from "lucide-react";
import type { PlaybackModels } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { buildQueueRows } from "@/components/playback/QueueSidebar.helpers";
import { ensureTrackAvailability } from "@/lib/catalog/loader-availability";
import { useCatalogStore } from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";

export function QueueSidebar() {
  const queue = usePlaybackStore((state) => state.queue);
  const transport = usePlaybackStore((state) => state.transport);
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const trackItemsByRecordingId = useCatalogStore(
    (state) => state.trackItemsByRecordingId,
  );
  const trackItemsByLibraryRecordingId = useCatalogStore(
    (state) => state.trackItemsByLibraryRecordingId,
  );
  const playlistTrackItemsByItemId = useCatalogStore(
    (state) => state.playlistTrackItemsByItemId,
  );
  const selectEntry = usePlaybackStore((state) => state.selectEntry);
  const removeQueuedEntry = usePlaybackStore(
    (state) => state.removeQueuedEntry,
  );
  const clearQueue = usePlaybackStore((state) => state.clearQueue);

  const activeEntryId =
    transport?.currentEntry?.entryId ?? transport?.loadingEntry?.entryId ?? "";
  const missingRecordingIds = useMemo(() => {
    const playbackAvailabilityByEntryId = queue?.entryAvailability ?? {};
    const missing = new Set<string>();
    for (const entry of queue?.userQueue ?? []) {
      if (
        !playbackAvailabilityByEntryId[entry.entryId]?.State &&
        entry.item.recordingId
      ) {
        missing.add(entry.item.recordingId);
      }
    }
    for (const entry of queue?.contextQueue?.entries ?? []) {
      if (
        !playbackAvailabilityByEntryId[entry.entryId]?.State &&
        entry.item.recordingId
      ) {
        missing.add(entry.item.recordingId);
      }
    }
    return Array.from(missing);
  }, [
    queue?.contextQueue?.entries,
    queue?.entryAvailability,
    queue?.userQueue,
  ]);

  useEffect(() => {
    if (missingRecordingIds.length === 0) {
      return;
    }
    void ensureTrackAvailability(missingRecordingIds);
  }, [missingRecordingIds]);

  const rows = useMemo(() => {
    return buildQueueRows(
      queue?.userQueue ?? [],
      queue?.contextQueue,
      queue?.entryAvailability ?? {},
      trackItemsByRecordingId,
      trackItemsByLibraryRecordingId,
      playlistTrackItemsByItemId,
      trackAvailabilityByRecordingId,
    );
  }, [
    queue?.contextQueue,
    queue?.entryAvailability,
    queue?.userQueue,
    playlistTrackItemsByItemId,
    trackAvailabilityByRecordingId,
    trackItemsByLibraryRecordingId,
    trackItemsByRecordingId,
  ]);
  const parentRef = useRef<HTMLDivElement | null>(null);
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    getItemKey: (index) => rows[index]?.id ?? index,
    estimateSize: (index) =>
      rows[index]?.type === "entry"
        ? 52
        : rows[index]?.type === "section"
          ? 20
          : 28,
    gap: 8,
    overscan: 8,
  });
  const hasVisibleEntries = rows.length > 0;

  return (
    <aside className="border-theme-300/15 flex h-full min-h-0 flex-col border-l px-4 pt-6 pb-4 dark:border-white/5">
      <div className="mb-4 flex items-center justify-between gap-3 px-1">
        <div>
          <h2 className="text-theme-900 dark:text-theme-100 mt-1 text-xl font-semibold tracking-[-0.03em]">
            Queue
          </h2>
        </div>
        <Button
          className="wails-no-drag"
          onClick={() => {
            void clearQueue();
          }}
        >
          Clear
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        {!hasVisibleEntries ? (
          <div className="border-theme-300/70 flex h-full flex-col items-center justify-center rounded-xl border border-dashed bg-white/50 px-6 text-center dark:border-white/8 dark:bg-transparent">
            <ListMusic className="text-theme-500 mb-3 h-7 w-7" />
            <p className="text-theme-500 text-sm">
              Tracks you queue or leave in the current context will show here.
            </p>
          </div>
        ) : (
          <div
            className="ben-scrollbar h-full overflow-y-auto pr-1"
            ref={parentRef}
            style={{
              scrollPaddingBottom: "var(--shell-queue-scroll-clearance, 0px)",
            }}
          >
            <div
              className="relative w-full"
              style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
            >
              {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                const row = rows[virtualRow.index];
                if (!row) {
                  return null;
                }
                return (
                  <div
                    className="absolute top-0 left-0 w-full"
                    key={virtualRow.key}
                    style={{
                      height: `${virtualRow.size}px`,
                      transform: `translateY(${virtualRow.start}px)`,
                    }}
                  >
                    {row.type === "section" || row.type === "marker" ? (
                      <QueueSectionTitle title={row.title} />
                    ) : (
                      <QueueEntryRow
                        actionable={row.actionable}
                        entry={row.entry}
                        isActive={row.entry.entryId === activeEntryId}
                        selectable={
                          row.actionable && row.entry.entryId !== activeEntryId
                        }
                        secondaryText={row.secondaryText}
                        title={row.title}
                        onRemove={
                          row.entry.origin === "queued"
                            ? () => {
                                void removeQueuedEntry(row.entry.entryId);
                              }
                            : undefined
                        }
                        onSelect={
                          row.actionable && row.entry.entryId !== activeEntryId
                            ? () => {
                                void selectEntry(row.entry.entryId);
                              }
                            : undefined
                        }
                      />
                    )}
                  </div>
                );
              })}
            </div>
            <div
              aria-hidden="true"
              style={{ height: "var(--shell-queue-scroll-clearance, 0px)" }}
            />
          </div>
        )}
      </div>
    </aside>
  );
}

function QueueSectionTitle({ title }: { title: string }) {
  return (
    <p className="text-theme-500 px-2 text-[11px] tracking-[0.28em] uppercase">
      {title}
    </p>
  );
}

type QueueEntryRowProps = {
  entry: PlaybackModels.SessionEntry;
  isActive: boolean;
  actionable: boolean;
  selectable: boolean;
  title: string;
  secondaryText: string;
  onSelect?: () => void;
  onRemove?: () => void;
};

function QueueEntryRow(props: QueueEntryRowProps) {
  return (
    <div
      className={[
        "group flex min-w-0 items-center gap-2 rounded-md p-2 transition-colors",
        props.isActive || !props.selectable
          ? ""
          : "hover:bg-theme-100 dark:hover:bg-theme-800",
        props.actionable ? "" : "opacity-40",
      ].join(" ")}
    >
      <button
        className="wails-no-drag min-w-0 flex-1 text-left disabled:pointer-events-none"
        disabled={!props.selectable}
        onClick={props.onSelect}
        type="button"
      >
        <p
          className={`truncate text-sm font-medium ${
            props.isActive
              ? "text-accent-700 dark:text-accent-300"
              : "text-theme-900 dark:text-theme-100"
          }`}
        >
          {props.title}
        </p>
        <p className="text-theme-400 truncate text-xs">{props.secondaryText}</p>
      </button>
      {props.onRemove && (
        <button
          aria-label="Remove queued entry"
          className="wails-no-drag group-hover:text-theme-500 hover:text-accent-700 dark:group-hover:text-theme-400 dark:hover:text-accent-200 rounded p-1 transition-colors not-group-hover:hidden"
          onClick={props.onRemove}
          type="button"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}
