import { LoaderCircle } from "lucide-react";
import { ArtworkTile } from "../../shared/ui/ArtworkTile";
import { useRecordingArtworkUrl } from "../../shared/lib/use-recording-artwork-url";
import { usePlaybackStore } from "./store";
import {
  playbackLoadingDescription,
  playbackLoadingLabel,
} from "./loading-state";

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
      className={`playback-loading-panel rounded-[1.8rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.08),rgba(255,255,255,0.03))] p-4 shadow-[0_24px_70px_rgba(0,0,0,0.35)] backdrop-blur-xl ${className}`.trim()}
    >
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
            Loading track
          </p>
          <h2 className="text-lg font-semibold text-white">
            Preparing playback
          </h2>
        </div>
        <div className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/6 px-3 py-1.5 text-xs tracking-[0.18em] text-white/55 uppercase">
          <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
          {playbackLoadingLabel(status)}
        </div>
      </div>

      <div className="flex items-center gap-3 rounded-[1.35rem] border border-white/10 bg-black/15 p-3">
        <ArtworkTile
          alt={item.title}
          className="h-16 w-16 shrink-0"
          rounded="soft"
          src={artworkUrl}
          title={item.title}
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-white/92">
            {item.title}
          </p>
          <p className="truncate text-sm text-white/52">{item.subtitle}</p>
          <p className="mt-1 text-xs text-white/42">
            {playbackLoadingDescription(status)}
          </p>
        </div>
      </div>

      <div className="playback-loading-panel__progress mt-4" />
    </section>
  );
}
