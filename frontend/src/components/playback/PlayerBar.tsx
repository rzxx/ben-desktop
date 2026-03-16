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
import { Badge } from "@/components/ui/Badge";
import { IconButton } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { formatDuration } from "@/lib/format";
import { useRecordingArtworkUrl } from "@/hooks/media/useRecordingArtworkUrl";
import {
  playbackLoadingLabel,
  playbackLoadingDescription,
} from "@/lib/playback/loading-state";
import { usePlaybackStore } from "@/stores/playback/store";

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
    <footer className="fixed inset-x-0 bottom-0 z-40 border-t border-zinc-800 bg-zinc-950 px-4 py-3">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,1.35fr)_minmax(260px,0.9fr)]">
        <div className="flex min-w-0 items-center gap-4">
          <ArtworkTile
            alt={currentTitle}
            className="h-16 w-16 shrink-0"
            rounded="soft"
            src={artworkUrl}
            title={currentTitle}
          />
          <div className="min-w-0">
            <p className="truncate text-xs uppercase tracking-wide text-zinc-500">
              {statusLabel}
            </p>
            <h2 className="truncate text-lg font-semibold text-zinc-100">
              {currentTitle}
            </h2>
            <p className="truncate text-sm text-zinc-400">{currentSubtitle}</p>
            {isLoadingOnly && (
              <p className="truncate text-xs text-zinc-500">
                Requested track is still preparing.
              </p>
            )}
            {error && (
              <p className="truncate text-xs text-red-300">{error}</p>
            )}
          </div>
        </div>

        <div className="flex min-w-0 flex-col justify-center gap-3">
          <div className="flex items-center justify-center gap-3">
            <IconButton
              className={shuffle ? "border-zinc-500 bg-zinc-800 text-zinc-50" : ""}
              label="Toggle shuffle"
              onClick={() => {
                void setShuffle(!shuffle);
              }}
            >
              <Waves className="h-4 w-4" />
            </IconButton>
            <IconButton
              disabled={!hasCurrent}
              label="Previous track"
              onClick={() => {
                void previous();
              }}
            >
              <SkipBack className="h-5 w-5" />
            </IconButton>
            <button
              className="inline-flex h-11 w-11 items-center justify-center rounded-full border border-zinc-600 bg-zinc-100 text-zinc-950 transition hover:bg-white disabled:cursor-default disabled:opacity-50"
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
            <IconButton
              disabled={!canGoNext}
              label="Next track"
              onClick={() => {
                void next();
              }}
            >
              <SkipForward className="h-5 w-5" />
            </IconButton>
            <IconButton
              className={repeatMode !== "off" ? "border-zinc-500 bg-zinc-800 text-zinc-50" : ""}
              label="Toggle repeat mode"
              onClick={() => {
                void setRepeatMode(nextRepeatMode(repeatMode));
              }}
            >
              {repeatMode === "one" ? (
                <Repeat1 className="h-4 w-4" />
              ) : (
                <Repeat className="h-4 w-4" />
              )}
            </IconButton>
          </div>
          <div className="flex items-center gap-3 text-xs text-zinc-400">
            <span className="w-12 text-right tabular-nums">
              {formatDuration(positionMs)}
            </span>
            <input
              className="flex-1 accent-zinc-100"
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
          <div className="inline-flex items-center gap-2 rounded-md border border-zinc-800 bg-zinc-900 px-3 py-2">
            <Volume2 className="h-4 w-4 text-zinc-400" />
            <input
              className="w-28 accent-zinc-100"
              max={100}
              min={0}
              onChange={(event) => {
                void setVolume(Number(event.target.value));
              }}
              type="range"
              value={volume}
            />
          </div>
          <Badge>Queue {snapshot?.queueLength ?? 0}</Badge>
        </div>
      </div>
    </footer>
  );
}


