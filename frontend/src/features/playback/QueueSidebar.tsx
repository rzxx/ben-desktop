import { ListMusic, Trash2 } from "lucide-react";
import { usePlaybackStore } from "./store";
import { formatDuration } from "../../shared/lib/format";

export function QueueSidebar() {
  const snapshot = usePlaybackStore((state) => state.snapshot);
  const selectEntry = usePlaybackStore((state) => state.selectEntry);
  const removeQueuedEntry = usePlaybackStore(
    (state) => state.removeQueuedEntry,
  );
  const clearQueue = usePlaybackStore((state) => state.clearQueue);

  const currentEntry = snapshot?.currentEntry ?? null;
  const queuedEntries = snapshot?.queuedEntries ?? [];
  const contextEntries = snapshot?.context?.entries;
  const listEntries = contextEntries?.length
    ? contextEntries
    : (snapshot?.upcomingEntries ?? []);
  const activeEntryId =
    snapshot?.loadingEntry?.entryId ?? currentEntry?.entryId ?? "";
  const hasVisibleEntries =
    queuedEntries.length > 0 || listEntries.length > 0;

  return (
    <aside className="queue-sidebar flex h-full min-h-0 flex-col rounded-[1.8rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] px-4 py-4 shadow-[0_20px_60px_rgba(0,0,0,0.25)]">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
            Queue
          </p>
          <h2 className="text-lg font-semibold text-white">Queue</h2>
        </div>
        <button
          className="queue-sidebar__clear no-drag"
          onClick={() => {
            void clearQueue();
          }}
          type="button"
        >
          Clear
        </button>
      </div>

      <div className="mb-4 rounded-[1.3rem] border border-white/10 bg-black/15 p-4">
        <p className="mb-2 text-xs tracking-[0.3em] text-white/35 uppercase">
          Now playing
        </p>
        {currentEntry ? (
          <button
            className="queue-entry no-drag w-full text-left"
            onClick={() => {
              void selectEntry(currentEntry.entryId);
            }}
            type="button"
          >
            <span className="queue-entry__title">
              {currentEntry.item.title}
            </span>
            <span className="queue-entry__meta">
              {currentEntry.item.subtitle}
            </span>
          </button>
        ) : (
          <p className="text-sm text-white/45">Player is empty.</p>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
        {!hasVisibleEntries ? (
          <div className="flex h-full flex-col items-center justify-center rounded-[1.5rem] border border-dashed border-white/10 bg-black/10 px-6 text-center">
            <ListMusic className="mb-3 h-7 w-7 text-white/25" />
            <p className="text-sm text-white/55">
              Tracks you queue or leave in the current context will show here.
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {queuedEntries.length > 0 && (
              <>
                <p className="px-2 text-[0.68rem] tracking-[0.3em] text-white/32 uppercase">
                  Play next
                </p>
                {queuedEntries.map((entry, index) => (
                  <QueueEntryRow
                    entry={entry}
                    isActive={entry.entryId === activeEntryId}
                    key={entry.entryId}
                    label={index === 0 ? "Next" : `Q${index + 1}`}
                    onRemove={
                      entry.origin === "queued"
                        ? () => {
                            void removeQueuedEntry(entry.entryId);
                          }
                        : undefined
                    }
                    onSelect={() => {
                      void selectEntry(entry.entryId);
                    }}
                  />
                ))}
              </>
            )}

            {contextEntries?.length ? (
              <>
                <p className="px-2 pt-2 text-[0.68rem] tracking-[0.3em] text-white/32 uppercase">
                  Context order
                </p>
                {contextEntries.map((entry, index) => (
                  <QueueEntryRow
                    entry={entry}
                    isActive={entry.entryId === activeEntryId}
                    key={entry.entryId}
                    label={entry.entryId === activeEntryId ? "Current" : `#${index + 1}`}
                    onSelect={() => {
                      void selectEntry(entry.entryId);
                    }}
                  />
                ))}
              </>
            ) : (
              listEntries.map((entry, index) => (
                <QueueEntryRow
                  entry={entry}
                  isActive={entry.entryId === activeEntryId}
                  key={entry.entryId}
                  label={index === 0 ? "Next" : `#${index + 1}`}
                  onRemove={
                    entry.origin === "queued"
                      ? () => {
                          void removeQueuedEntry(entry.entryId);
                        }
                      : undefined
                  }
                  onSelect={() => {
                    void selectEntry(entry.entryId);
                  }}
                />
              ))
            )}
          </div>
        )}
      </div>
    </aside>
  );
}

type QueueEntryRowProps = {
  entry: NonNullable<ReturnType<typeof usePlaybackStore.getState>["snapshot"]>["queuedEntries"][number];
  isActive: boolean;
  label: string;
  onSelect: () => void;
  onRemove?: () => void;
};

function QueueEntryRow(props: QueueEntryRowProps) {
  return (
    <div
      className={`queue-entry group ${props.isActive ? "border-white/18 bg-white/8" : ""}`}
    >
      <button
        className="no-drag flex min-w-0 flex-1 flex-col text-left"
        onClick={props.onSelect}
        type="button"
      >
        <span
          className={`text-xs tracking-[0.2em] uppercase ${props.isActive ? "text-amber-200/85" : "text-white/30"}`}
        >
          {props.label}
        </span>
        <span
          className={`truncate text-sm font-medium ${props.isActive ? "text-white" : "text-white/88"}`}
        >
          {props.entry.item.title}
        </span>
        <span className="truncate text-xs text-white/45">
          {props.entry.item.subtitle} • {formatDuration(props.entry.item.durationMs)}
        </span>
      </button>
      {props.onRemove && (
        <button
          className="queue-entry__remove no-drag"
          onClick={props.onRemove}
          type="button"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      )}
    </div>
  );
}
