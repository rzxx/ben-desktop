import { useMemo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ListMusic, X } from "lucide-react";
import type { PlaybackModels } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { usePlaybackStore } from "@/stores/playback/store";

export function QueueSidebar() {
  const snapshot = usePlaybackStore((state) => state.snapshot);
  const selectEntry = usePlaybackStore((state) => state.selectEntry);
  const removeQueuedEntry = usePlaybackStore(
    (state) => state.removeQueuedEntry,
  );
  const clearQueue = usePlaybackStore((state) => state.clearQueue);

  const activeEntryId =
    snapshot?.currentEntry?.entryId ?? snapshot?.loadingEntry?.entryId ?? "";
  const rows = useMemo(() => {
    const queuedEntries = snapshot?.queuedEntries ?? [];
    const contextEntries = snapshot?.context?.entries ?? [];

    return buildQueueRows(queuedEntries, contextEntries);
  }, [snapshot?.context?.entries, snapshot?.queuedEntries]);
  const parentRef = useRef<HTMLDivElement | null>(null);
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    getItemKey: (index) => rows[index]?.id ?? index,
    estimateSize: (index) => (rows[index]?.type === "section" ? 20 : 52),
    gap: 8,
    overscan: 8,
  });
  const hasVisibleEntries = rows.length > 0;

  return (
    <aside className="flex h-full min-h-0 flex-col border-l border-white/5 px-4 pt-6 pb-4">
      <div className="mb-4 flex items-center justify-between gap-3 px-1">
        <div>
          <h2 className="text-theme-100 mt-1 text-xl font-semibold tracking-[-0.03em]">
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
          <div className="flex h-full flex-col items-center justify-center rounded-xl border border-dashed border-white/8 px-6 text-center">
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
                    {row.type === "section" ? (
                      <QueueSectionTitle title={row.title} />
                    ) : (
                      <QueueEntryRow
                        entry={row.entry}
                        isActive={row.entry.entryId === activeEntryId}
                        onRemove={
                          row.entry.origin === "queued"
                            ? () => {
                                void removeQueuedEntry(row.entry.entryId);
                              }
                            : undefined
                        }
                        onSelect={() => {
                          void selectEntry(row.entry.entryId);
                        }}
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

type QueueRow =
  | { type: "section"; id: string; title: string }
  | { type: "entry"; id: string; entry: PlaybackModels.SessionEntry };

function buildQueueRows(
  queuedEntries: PlaybackModels.SessionEntry[],
  listEntries: PlaybackModels.SessionEntry[],
): QueueRow[] {
  const rows: QueueRow[] = [];

  if (queuedEntries.length > 0) {
    rows.push({
      type: "section",
      id: "section-queued",
      title: "Queued",
    });
    queuedEntries.forEach((entry) => {
      rows.push({
        type: "entry",
        id: `queued-${entry.entryId}`,
        entry,
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
      rows.push({
        type: "entry",
        id: `context-${entry.entryId}`,
        entry,
      });
    });
  }

  return rows;
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
  onSelect: () => void;
  onRemove?: () => void;
};

function QueueEntryRow(props: QueueEntryRowProps) {
  return (
    <div
      className={[
        "group flex min-w-0 items-center gap-2 rounded-md p-2 transition-colors",
        props.isActive ? "" : "hover:bg-theme-800",
      ].join(" ")}
    >
      <button
        className="wails-no-drag min-w-0 flex-1 text-left"
        onClick={props.onSelect}
        type="button"
      >
        <p
          className={`truncate text-sm font-medium ${
            props.isActive ? "text-accent-300" : "text-theme-100"
          }`}
        >
          {props.entry.item.title}
        </p>
        <p className="text-theme-400 truncate text-xs">
          {props.entry.item.subtitle}
        </p>
      </button>
      {props.onRemove && (
        <button
          aria-label="Remove queued entry"
          className="wails-no-drag group-hover:text-theme-400 hover:text-accent-200 rounded p-1 transition-colors not-group-hover:hidden"
          onClick={props.onRemove}
          type="button"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}
