import { LoaderCircle } from "lucide-react";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { useRecordingArtworkUrl } from "@/hooks/media/useRecordingArtworkUrl";
import { usePlaybackStore } from "@/stores/playback/store";
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
      className={`bg-theme-900/75 border-theme-500/15 rounded-2xl border p-4 shadow-xl shadow-black/25 backdrop-blur-xl ${className}`.trim()}
    >
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <h2 className="text-theme-100 text-lg font-semibold">
            Preparing playback
          </h2>
        </div>
        <div className="text-theme-300 inline-flex items-center gap-2 py-1.5 text-xs">
          <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
          {playbackLoadingLabel(status)}
        </div>
      </div>

      <div className="flex items-center gap-3">
        <ArtworkTile
          alt={item.title}
          className="h-16 w-16 shrink-0 rounded-md"
          src={artworkUrl}
          title={item.title}
        />
        <div className="min-w-0 flex-1">
          <p className="text-theme-100 truncate text-sm font-semibold">
            {item.title}
          </p>
          <p className="text-theme-500 truncate text-sm">{item.subtitle}</p>
          <p className="text-theme-300 mt-1 text-xs">
            {playbackLoadingDescription(status)}
          </p>
        </div>
      </div>

      {/* <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-white/8">
        <div className="bg-accent-100 h-full w-1/3 animate-pulse rounded-full" />
      </div> */}
    </section>
  );
}
