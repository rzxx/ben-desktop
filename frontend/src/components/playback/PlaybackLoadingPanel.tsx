import { LoaderCircle } from "lucide-react";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { useRecordingArtworkUrl } from "@/lib/media/useRecordingArtworkUrl";
import { usePlaybackStore } from "@/stores/playback/usePlaybackStore";
import {
  playbackLoadingDescription,
  playbackLoadingLabel,
} from "@/lib/playback/loading-state";

type PlaybackLoadingPanelProps = {
  className?: string;
};

export function PlaybackLoadingPanel({
  className = "",
}: PlaybackLoadingPanelProps) {
  const snapshot = usePlaybackStore((state) => state.snapshot);
  const item = snapshot?.loadingItem ?? null;
  const status = snapshot?.loadingPreparation?.status ?? null;
  const artworkUrl = useRecordingArtworkUrl(item?.artworkRef);

  if (!item) {
    return null;
  }

  return (
    <section
      className={`rounded-lg border border-zinc-800 bg-zinc-950 p-4 ${className}`.trim()}
    >
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-wide text-zinc-500">
            Loading track
          </p>
          <h2 className="text-lg font-semibold text-zinc-100">
            Preparing playback
          </h2>
        </div>
        <div className="inline-flex items-center gap-2 rounded-full border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs uppercase tracking-wide text-zinc-300">
          <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
          {playbackLoadingLabel(status)}
        </div>
      </div>

      <div className="flex items-center gap-3 rounded-md border border-zinc-800 bg-zinc-900 p-3">
        <ArtworkTile
          alt={item.title}
          className="h-16 w-16 shrink-0"
          rounded="soft"
          src={artworkUrl}
          title={item.title}
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-zinc-100">
            {item.title}
          </p>
          <p className="truncate text-sm text-zinc-400">{item.subtitle}</p>
          <p className="mt-1 text-xs text-zinc-500">
            {playbackLoadingDescription(status)}
          </p>
        </div>
      </div>

      <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-zinc-800">
        <div className="h-full w-1/3 animate-pulse rounded-full bg-zinc-200" />
      </div>
    </section>
  );
}


