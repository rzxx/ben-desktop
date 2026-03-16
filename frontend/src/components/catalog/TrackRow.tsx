import { Play, Plus } from "lucide-react";
import { IconButton } from "@/components/ui/Button";
import {
  availabilityLabel,
  formatDuration,
  isCatalogTrackActionable,
} from "@/lib/format";

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
    <div className="flex h-full items-center gap-4 rounded-md border border-transparent px-3 hover:border-zinc-800 hover:bg-zinc-900">
      <div className="flex w-16 shrink-0 justify-center text-xs uppercase tracking-wide text-zinc-500">
        {indexLabel}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-zinc-100">{title}</div>
        <div className="truncate text-xs text-zinc-400">{subtitle}</div>
      </div>
      <div className="hidden w-28 shrink-0 justify-center text-xs text-zinc-500 xl:flex">
        {availabilityLabel(availabilityState)}
      </div>
      <div className="w-16 shrink-0 text-right text-xs text-zinc-500 tabular-nums">
        {formatDuration(durationMs)}
      </div>
      <div className="flex shrink-0 gap-2">
        <IconButton
          disabled={!actionsEnabled}
          label="Play track"
          onClick={onPlay}
        >
          <Play className="h-4 w-4" />
        </IconButton>
        <IconButton
          disabled={!actionsEnabled}
          label="Queue track"
          onClick={onQueue}
        >
          <Plus className="h-4 w-4" />
        </IconButton>
      </div>
    </div>
  );
}


