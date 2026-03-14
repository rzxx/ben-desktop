import { Play, Plus } from "lucide-react";
import {
  availabilityLabel,
  formatDuration,
  isCatalogTrackActionable,
} from "../../../shared/lib/format";

export function TrackRow({
  availabilityState,
  durationMs,
  indexLabel,
  onPlay,
  onQueue,
  subtitle,
  title,
}: {
  availabilityState?: string;
  durationMs: number;
  indexLabel: string;
  onPlay: () => void;
  onQueue: () => void;
  subtitle: string;
  title: string;
}) {
  const actionsEnabled = isCatalogTrackActionable(availabilityState);

  return (
    <div className="track-row flex h-full items-center gap-4 rounded-[1.1rem] px-3">
      <div className="flex w-16 shrink-0 justify-center text-xs tracking-[0.25em] text-white/30 uppercase">
        {indexLabel}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-white">{title}</div>
        <div className="truncate text-xs text-white/45">{subtitle}</div>
      </div>
      <div className="hidden w-28 shrink-0 justify-center text-xs text-white/38 xl:flex">
        {availabilityLabel(availabilityState)}
      </div>
      <div className="w-16 shrink-0 text-right text-xs text-white/38 tabular-nums">
        {formatDuration(durationMs)}
      </div>
      <div className="flex shrink-0 gap-2">
        <button
          className={`row-action ${actionsEnabled ? "" : "cursor-not-allowed opacity-45"}`}
          disabled={!actionsEnabled}
          onClick={onPlay}
          type="button"
        >
          <Play className="h-4 w-4" />
        </button>
        <button
          className={`row-action ${actionsEnabled ? "" : "cursor-not-allowed opacity-45"}`}
          disabled={!actionsEnabled}
          onClick={onQueue}
          type="button"
        >
          <Plus className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
