import { ListMusic, Trash2 } from "lucide-react";
import { Button, IconButton } from "@/components/ui/Button";
import { usePlaybackStore } from "@/stores/playback/store";
import { formatDuration } from "@/lib/format";

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
  const hasVisibleEntries = queuedEntries.length > 0 || listEntries.length > 0;

  return (
    <aside className="flex h-full min-h-0 flex-col rounded-lg border border-zinc-800 bg-zinc-950 px-4 py-4">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <p className="text-xs tracking-wide text-zinc-500 uppercase">Queue</p>
          <h2 className="text-lg font-semibold text-zinc-100">Queue</h2>
        </div>
        <Button
          className="no-drag"
          onClick={() => {
            void clearQueue();
          }}
        >
          Clear
        </Button>
      </div>

      <div className="mb-4 rounded-md border border-zinc-800 bg-zinc-900 p-4">
        <p className="mb-2 text-xs tracking-wide text-zinc-500 uppercase">
          Now playing
        </p>
        {currentEntry ? (
          <button
            className="no-drag flex w-full flex-col rounded-md border border-zinc-800 bg-zinc-950 p-3 text-left transition hover:border-zinc-700 hover:bg-zinc-900"
            onClick={() => {
              void selectEntry(currentEntry.entryId);
            }}
            type="button"
          >
            <span className="truncate text-sm font-medium text-zinc-100">
              {currentEntry.item.title}
            </span>
            <span className="truncate text-xs text-zinc-400">
              {currentEntry.item.subtitle}
            </span>
          </button>
        ) : (
          <p className="text-sm text-zinc-400">Player is empty.</p>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
        {!hasVisibleEntries ? (
          <div className="flex h-full flex-col items-center justify-center rounded-lg border border-dashed border-zinc-800 bg-zinc-950 px-6 text-center">
            <ListMusic className="mb-3 h-7 w-7 text-zinc-500" />
            <p className="text-sm text-zinc-400">
              Tracks you queue or leave in the current context will show here.
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {queuedEntries.length > 0 && (
              <>
                <p className="px-2 text-xs tracking-wide text-zinc-500 uppercase">
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
                <p className="px-2 pt-2 text-xs tracking-wide text-zinc-500 uppercase">
                  Context order
                </p>
                {contextEntries.map((entry, index) => (
                  <QueueEntryRow
                    entry={entry}
                    isActive={entry.entryId === activeEntryId}
                    key={entry.entryId}
                    label={
                      entry.entryId === activeEntryId
                        ? "Current"
                        : `#${index + 1}`
                    }
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
  entry: NonNullable<
    ReturnType<typeof usePlaybackStore.getState>["snapshot"]
  >["queuedEntries"][number];
  isActive: boolean;
  label: string;
  onSelect: () => void;
  onRemove?: () => void;
};

function QueueEntryRow(props: QueueEntryRowProps) {
  return (
    <div
      className={[
        "group flex items-center gap-3 rounded-md border p-3",
        props.isActive
          ? "border-zinc-600 bg-zinc-900"
          : "border-zinc-800 bg-zinc-950",
      ].join(" ")}
    >
      <button
        className="no-drag flex min-w-0 flex-1 flex-col text-left"
        onClick={props.onSelect}
        type="button"
      >
        <span
          className={`text-xs tracking-wide uppercase ${props.isActive ? "text-zinc-300" : "text-zinc-500"}`}
        >
          {props.label}
        </span>
        <span
          className={`truncate text-sm font-medium ${props.isActive ? "text-zinc-100" : "text-zinc-200"}`}
        >
          {props.entry.item.title}
        </span>
        <span className="truncate text-xs text-zinc-400">
          {props.entry.item.subtitle} •{" "}
          {formatDuration(props.entry.item.durationMs)}
        </span>
      </button>
      {props.onRemove && (
        <IconButton
          className="no-drag border-red-500/30 bg-red-500/10 text-red-100 hover:border-red-400/40 hover:bg-red-500/15"
          label="Remove queued entry"
          onClick={props.onRemove}
        >
          <Trash2 className="h-4 w-4" />
        </IconButton>
      )}
    </div>
  );
}
