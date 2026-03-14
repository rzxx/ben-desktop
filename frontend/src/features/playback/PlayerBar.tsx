import {
  Pause,
  Play,
  Repeat,
  Repeat1,
  SkipBack,
  SkipForward,
  Volume2,
  Waves,
} from "lucide-react";
import { ArtworkTile } from "../../shared/ui/ArtworkTile";
import { formatDuration } from "../../shared/lib/format";
import { useRecordingArtworkUrl } from "../../shared/lib/use-recording-artwork-url";
import {
  playbackLoadingLabel,
  playbackLoadingDescription,
} from "./loading-state";
import { usePlaybackStore } from "./store";

function nextRepeatMode(mode: string) {
  switch (mode) {
    case "off":
      return "all";
    case "all":
      return "one";
    default:
      return "off";
  }
}

export function PlayerBar() {
  const snapshot = usePlaybackStore((state) => state.snapshot);
  const error = usePlaybackStore((state) => state.error);
  const togglePlayback = usePlaybackStore((state) => state.togglePlayback);
  const next = usePlaybackStore((state) => state.next);
  const previous = usePlaybackStore((state) => state.previous);
  const seekTo = usePlaybackStore((state) => state.seekTo);
  const setVolume = usePlaybackStore((state) => state.setVolume);
  const setShuffle = usePlaybackStore((state) => state.setShuffle);
  const setRepeatMode = usePlaybackStore((state) => state.setRepeatMode);

  const currentItem = snapshot?.currentItem ?? null;
  const loadingItem = snapshot?.loadingItem ?? null;
  const visibleItem = currentItem ?? loadingItem;
  const artworkUrl = useRecordingArtworkUrl(visibleItem?.artworkRef);
  const currentTitle = visibleItem?.title ?? "Nothing selected";
  const currentSubtitle =
    visibleItem?.subtitle ??
    (loadingItem
      ? playbackLoadingDescription(snapshot?.loadingPreparation?.status)
      : "Pick something from the library");
  const durationMs = currentItem
    ? (snapshot?.durationMs ?? currentItem.durationMs ?? 0)
    : (loadingItem?.durationMs ?? 0);
  const positionMs = currentItem ? (snapshot?.positionMs ?? 0) : 0;
  const isPlaying = snapshot?.status === "playing";
  const isLoadingOnly = !currentItem && Boolean(loadingItem);
  const volume = snapshot?.volume ?? 80;
  const repeatMode = snapshot?.repeatMode ?? "off";
  const shuffle = snapshot?.shuffle ?? false;
  const hasCurrent = Boolean(currentItem);
  const hasUpcoming = (snapshot?.upcomingEntries?.length ?? 0) > 0;
  const canGoNext =
    hasCurrent && (repeatMode === "one" ? hasCurrent : hasUpcoming);
  const canResume =
    !isLoadingOnly && (hasCurrent || (snapshot?.queueLength ?? 0) > 0);
  const statusLabel = currentItem
    ? (snapshot?.status ?? "idle")
    : loadingItem
      ? playbackLoadingLabel(snapshot?.loadingPreparation?.status)
      : "idle";

  return (
    <footer className="player-bar fixed inset-x-0 bottom-0 z-40 border-t border-white/10 bg-[linear-gradient(180deg,rgba(10,12,19,0.85),rgba(6,8,14,0.98))] px-5 py-4 backdrop-blur-2xl">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,1.35fr)_minmax(260px,0.9fr)]">
        <div className="flex min-w-0 items-center gap-4">
          <ArtworkTile
            alt={currentTitle}
            className="h-18 w-18 shrink-0"
            rounded="soft"
            src={artworkUrl}
            title={currentTitle}
          />
          <div className="min-w-0">
            <p className="truncate text-[0.7rem] tracking-[0.3em] text-white/40 uppercase">
              {statusLabel}
            </p>
            <h2 className="truncate text-lg font-semibold text-white">
              {currentTitle}
            </h2>
            <p className="truncate text-sm text-white/55">{currentSubtitle}</p>
            {isLoadingOnly && (
              <p className="truncate text-xs text-white/38">
                Requested track is still preparing.
              </p>
            )}
            {error && (
              <p className="truncate text-xs text-amber-300">{error}</p>
            )}
          </div>
        </div>

        <div className="flex min-w-0 flex-col justify-center gap-3">
          <div className="flex items-center justify-center gap-3">
            <button
              className={`player-bar__toggle ${shuffle ? "is-active" : ""}`}
              onClick={() => {
                void setShuffle(!shuffle);
              }}
              type="button"
            >
              <Waves className="h-4 w-4" />
            </button>
            <button
              className="player-bar__transport"
              disabled={!hasCurrent}
              onClick={() => {
                void previous();
              }}
              type="button"
            >
              <SkipBack className="h-5 w-5" />
            </button>
            <button
              className="player-bar__play"
              disabled={!canResume}
              onClick={() => {
                void togglePlayback();
              }}
              type="button"
            >
              {isPlaying ? (
                <Pause className="h-6 w-6" />
              ) : (
                <Play className="ml-0.5 h-6 w-6" />
              )}
            </button>
            <button
              className="player-bar__transport"
              disabled={!canGoNext}
              onClick={() => {
                void next();
              }}
              type="button"
            >
              <SkipForward className="h-5 w-5" />
            </button>
            <button
              className={`player-bar__toggle ${repeatMode !== "off" ? "is-active" : ""}`}
              onClick={() => {
                void setRepeatMode(nextRepeatMode(repeatMode));
              }}
              type="button"
            >
              {repeatMode === "one" ? (
                <Repeat1 className="h-4 w-4" />
              ) : (
                <Repeat className="h-4 w-4" />
              )}
            </button>
          </div>
          <div className="flex items-center gap-3 text-xs text-white/45">
            <span className="w-12 text-right tabular-nums">
              {formatDuration(positionMs)}
            </span>
            <input
              className="player-slider flex-1"
              disabled={!hasCurrent}
              max={Math.max(durationMs, 1)}
              min={0}
              onChange={(event) => {
                void seekTo(Number(event.target.value));
              }}
              type="range"
              value={Math.min(positionMs, Math.max(durationMs, 1))}
            />
            <span className="w-12 tabular-nums">
              {formatDuration(durationMs)}
            </span>
          </div>
        </div>

        <div className="flex items-center justify-end gap-3">
          <div className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/5 px-3 py-2">
            <Volume2 className="h-4 w-4 text-white/60" />
            <input
              className="player-slider w-28"
              max={100}
              min={0}
              onChange={(event) => {
                void setVolume(Number(event.target.value));
              }}
              type="range"
              value={volume}
            />
          </div>
          <div className="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-xs tracking-[0.25em] text-white/45 uppercase">
            Queue {snapshot?.queueLength ?? 0}
          </div>
        </div>
      </div>
    </footer>
  );
}
