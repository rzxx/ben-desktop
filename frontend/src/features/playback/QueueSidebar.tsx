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
  const upcomingEntries = snapshot?.upcomingEntries ?? [];

  return (
    <aside className="queue-sidebar flex h-full min-h-0 flex-col rounded-[1.8rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] px-4 py-4 shadow-[0_20px_60px_rgba(0,0,0,0.25)]">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
            Queue
          </p>
          <h2 className="text-lg font-semibold text-white">Up next</h2>
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
        {upcomingEntries.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center rounded-[1.5rem] border border-dashed border-white/10 bg-black/10 px-6 text-center">
            <ListMusic className="mb-3 h-7 w-7 text-white/25" />
            <p className="text-sm text-white/55">
              Tracks you queue or leave in the current context will show here.
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {upcomingEntries.map((entry, index) => (
              <div className="queue-entry group" key={entry.entryId}>
                <button
                  className="no-drag flex min-w-0 flex-1 flex-col text-left"
                  onClick={() => {
                    void selectEntry(entry.entryId);
                  }}
                  type="button"
                >
                  <span className="text-xs tracking-[0.2em] text-white/30 uppercase">
                    {index === 0 ? "Next" : `#${index + 1}`}
                  </span>
                  <span className="truncate text-sm font-medium text-white/88">
                    {entry.item.title}
                  </span>
                  <span className="truncate text-xs text-white/45">
                    {entry.item.subtitle} •{" "}
                    {formatDuration(entry.item.durationMs)}
                  </span>
                </button>
                {entry.origin === "queued" && (
                  <button
                    className="queue-entry__remove no-drag"
                    onClick={() => {
                      void removeQueuedEntry(entry.entryId);
                    }}
                    type="button"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </aside>
  );
}
