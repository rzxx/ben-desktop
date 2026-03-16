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
      className={`rounded-2xl border border-white/10 bg-theme-100-desat/74 p-4 shadow-[0_18px_45px_rgba(0,0,0,0.28)] backdrop-blur-xl ${className}`.trim()}
    >
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <p className="text-theme-500 text-xs tracking-wide uppercase">
            Loading track
          </p>
          <h2 className="text-theme-100 text-lg font-semibold">
            Preparing playback
          </h2>
        </div>
        <div className="text-theme-300 inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.06] px-3 py-1.5 text-xs tracking-wide uppercase">
          <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
          {playbackLoadingLabel(status)}
        </div>
      </div>

      <div className="flex items-center gap-3 rounded-xl border border-white/10 bg-white/[0.055] p-3">
        <ArtworkTile
          alt={item.title}
          className="h-16 w-16 shrink-0"
          rounded="soft"
          src={artworkUrl}
          title={item.title}
        />
        <div className="min-w-0 flex-1">
          <p className="text-theme-100 truncate text-sm font-semibold">
            {item.title}
          </p>
          <p className="text-theme-500 truncate text-sm">{item.subtitle}</p>
          <p className="text-theme-500 mt-1 text-xs">
            {playbackLoadingDescription(status)}
          </p>
        </div>
      </div>

      <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-white/8">
        <div className="bg-theme-100 h-full w-1/3 animate-pulse rounded-full" />
      </div>
    </section>
  );
}
