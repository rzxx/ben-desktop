import type { ReactNode } from "react";
import { Slider } from "@base-ui/react/slider";
import { Link } from "@tanstack/react-router";
import {
  Heart,
  Pause,
  Play,
  Repeat,
  Repeat1,
  Shuffle,
  SkipBack,
  SkipForward,
  Volume2,
} from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { useRecordingLikeState } from "@/hooks/catalog/useRecordingLikeState";
import { useRecordingArtworkUrl } from "@/hooks/media/useRecordingArtworkUrl";
import { usePlayerProgress } from "@/hooks/playback/usePlayerProgress";
import { usePlayerVolume } from "@/hooks/playback/usePlayerVolume";
import type { SessionSnapshot } from "@/lib/api/models";
import { formatDuration } from "@/lib/format";
import { playbackLoadingDescription } from "@/lib/playback/loading-state";
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

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

export function PlayerBar() {
  return (
    <footer className="border-theme-500/15 dark:border-theme-500/15 dark:bg-theme-900/75 bg-theme-100-desat/90 rounded-2xl border px-6 py-4 shadow-xl shadow-black/20 backdrop-blur-xl backdrop-saturate-150 lg:px-8 dark:shadow-black/25">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:gap-4">
        <PlayerBarMeta />
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <PlayerBarControls />
          <PlayerBarProgress />
        </div>
        <PlayerBarVolume />
      </div>
    </footer>
  );
}

function PlayerBarMeta() {
  const {
    currentAlbumId,
    currentLibraryRecordingId,
    currentRecordingId,
    currentSubtitle,
    currentTitle,
    error,
    isLoadingOnly,
  } = usePlaybackStore(
    useShallow((state) => {
      const transport = state.transport;
      const currentItem = transport?.currentEntry?.item ?? null;
      const loadingItem = transport?.loadingEntry?.item ?? null;
      const visibleItem = currentItem ?? loadingItem;

      return {
        currentAlbumId: visibleItem?.albumId ?? "",
        currentLibraryRecordingId: visibleItem?.libraryRecordingId ?? "",
        currentRecordingId:
          visibleItem?.recordingId ?? visibleItem?.artworkRef ?? "",
        currentSubtitle:
          visibleItem?.subtitle ??
          (loadingItem
            ? playbackLoadingDescription(transport?.loadingPreparation?.status)
            : "Pick something from the library"),
        currentTitle: visibleItem?.title ?? "Nothing selected",
        error: state.error,
        isLoadingOnly: !currentItem && Boolean(loadingItem),
      };
    }),
  );
  const artworkUrl = useRecordingArtworkUrl(currentRecordingId || undefined);
  const likeState = useRecordingLikeState({
    libraryRecordingId: currentLibraryRecordingId || undefined,
    recordingId: currentRecordingId || undefined,
  });

  return (
    <div className="flex min-w-0 items-center gap-3 lg:w-72 lg:shrink-0">
      <ArtworkTile
        alt={currentTitle}
        className="border-theme-300/70 h-12 w-12 shrink-0 rounded-md dark:border-black/10"
        rounded="soft"
        src={artworkUrl}
        title={currentTitle}
      />
      <div className="min-w-0">
        {currentAlbumId ? (
          <Link
            className="text-theme-900 hover:text-theme-700 dark:text-theme-100 dark:hover:text-theme-50 block truncate text-left text-sm font-medium transition-colors"
            params={{ albumId: currentAlbumId }}
            to="/albums/$albumId"
          >
            {currentTitle}
          </Link>
        ) : (
          <h2 className="text-theme-900 dark:text-theme-100 truncate text-sm font-medium">
            {currentTitle}
          </h2>
        )}
        <p className="text-theme-500 truncate text-xs">{currentSubtitle}</p>
        {isLoadingOnly && (
          <p className="text-theme-500 truncate text-[11px]">
            Requested track is still preparing.
          </p>
        )}
        {error && (
          <p className="truncate text-[11px] text-red-600 dark:text-red-300">
            {error}
          </p>
        )}
      </div>
      {likeState.hasIdentity ? (
        <PlayerIconButton
          active={likeState.liked}
          className="shrink-0"
          disabled={likeState.inFlight}
          label={likeState.liked ? "Unlike current song" : "Like current song"}
          onClick={() => {
            void likeState.toggleLike().catch(() => {});
          }}
        >
          <Heart
            className="h-4 w-4"
            fill={likeState.liked ? "currentColor" : "none"}
          />
        </PlayerIconButton>
      ) : null}
    </div>
  );
}

function PlayerBarControls() {
  const {
    hasCurrent,
    isLoadingOnly,
    isPlaying,
    queueLength,
    repeatMode,
    shuffle,
  } = usePlaybackStore(
    useShallow((state) => {
      const transport = state.transport;
      const queue = state.queue;
      const hasCurrent = Boolean(transport?.currentEntry?.item);
      return {
        hasCurrent,
        isLoadingOnly:
          !transport?.currentEntry?.item &&
          Boolean(transport?.loadingEntry?.item),
        isPlaying: transport?.status === "playing",
        queueLength: queue?.queueLength ?? 0,
        repeatMode: transport?.repeatMode ?? "off",
        shuffle: transport?.shuffle ?? false,
      };
    }),
  );
  const next = usePlaybackStore((state) => state.next);
  const previous = usePlaybackStore((state) => state.previous);
  const setRepeatMode = usePlaybackStore((state) => state.setRepeatMode);
  const setShuffle = usePlaybackStore((state) => state.setShuffle);
  const togglePlayback = usePlaybackStore((state) => state.togglePlayback);

  const hasUpcoming = queueLength > (hasCurrent ? 1 : 0);
  const canGoNext =
    hasCurrent && (repeatMode === "one" ? hasCurrent : hasUpcoming);
  const canResume = !isLoadingOnly && (hasCurrent || queueLength > 0);

  return (
    <div className="flex items-center justify-center gap-2">
      <PlayerIconButton
        active={shuffle}
        label="Toggle shuffle"
        onClick={() => {
          void setShuffle(!shuffle);
        }}
      >
        <Shuffle className="h-4 w-4" />
      </PlayerIconButton>
      <PlayerIconButton
        disabled={!hasCurrent}
        label="Previous track"
        onClick={() => {
          void previous();
        }}
      >
        <SkipBack className="h-4 w-4 fill-current" />
      </PlayerIconButton>
      <button
        className="bg-accent-700 text-accent-50 hover:bg-accent-600 dark:bg-accent-200 dark:text-accent-950 dark:hover:bg-accent-50 inline-flex h-11 w-11 items-center justify-center rounded-full bg-linear-to-b from-white/15 to-black/15 shadow-md shadow-black/25 transition hover:scale-105 active:scale-95 disabled:cursor-default disabled:opacity-50 dark:from-white/21 dark:to-black/21"
        disabled={!canResume}
        onClick={() => {
          void togglePlayback();
        }}
        type="button"
      >
        {isPlaying ? (
          <Pause fill="currentColor" size={18} />
        ) : (
          <Play fill="currentColor" size={18} />
        )}
      </button>
      <PlayerIconButton
        disabled={!canGoNext}
        label="Next track"
        onClick={() => {
          void next();
        }}
      >
        <SkipForward className="h-4 w-4 fill-current" />
      </PlayerIconButton>
      <PlayerIconButton
        active={repeatMode !== "off"}
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
      </PlayerIconButton>
    </div>
  );
}

function PlayerBarProgress() {
  const {
    currentEntryId,
    durationMs,
    hasCurrent,
    isPlaying,
    positionMs,
    positionCapturedAtMs,
  } = usePlaybackStore(
    useShallow((state) => {
      const transport = state.transport;
      const currentItem = transport?.currentEntry?.item ?? null;
      const loadingItem = transport?.loadingEntry?.item ?? null;

      return {
        currentEntryId: transport?.currentEntry?.entryId ?? "",
        durationMs: currentItem
          ? (transport?.durationMs ?? currentItem.durationMs ?? 0)
          : (loadingItem?.durationMs ?? 0),
        hasCurrent: Boolean(currentItem),
        isPlaying: transport?.status === "playing",
        positionMs: currentItem ? (transport?.positionMs ?? 0) : 0,
        positionCapturedAtMs: currentItem
          ? (transport?.positionCapturedAtMs ?? 0)
          : 0,
      };
    }),
  );
  const seekTo = usePlaybackStore((state) => state.seekTo);

  return (
    <PlayerBarProgressStateful
      currentEntryId={currentEntryId}
      key={hasCurrent ? currentEntryId || "current" : "idle"}
      durationMs={durationMs}
      hasCurrent={hasCurrent}
      isPlaying={isPlaying}
      positionMs={positionMs}
      positionCapturedAtMs={positionCapturedAtMs}
      seekTo={seekTo}
    />
  );
}

function PlayerBarProgressStateful({
  currentEntryId,
  durationMs,
  hasCurrent,
  isPlaying,
  positionMs,
  positionCapturedAtMs,
  seekTo,
}: {
  currentEntryId: string;
  durationMs: number;
  hasCurrent: boolean;
  isPlaying: boolean;
  positionMs: number;
  positionCapturedAtMs: number;
  seekTo: (
    positionMs: number,
    expectedEntryId?: string,
    debugRequestId?: string,
  ) => Promise<SessionSnapshot | null>;
}) {
  const {
    shownPositionMs,
    onInteractionStart,
    onValueChange,
    onValueCommitted,
  } = usePlayerProgress({
    currentEntryId,
    durationMs,
    hasCurrent,
    isPlaying,
    positionMs,
    positionCapturedAtMs,
    seekTo,
  });

  return (
    <div className="flex items-center justify-center gap-3">
      <span className="text-theme-500 -mt-0.5 w-10 shrink-0 text-right text-xs tabular-nums">
        {formatDuration(shownPositionMs)}
      </span>
      <PlayerSlider
        ariaLabel="Seek position"
        disabled={!hasCurrent}
        max={Math.max(durationMs, 1)}
        min={0}
        onInteractionStart={onInteractionStart}
        onValueChange={onValueChange}
        onValueCommitted={onValueCommitted}
        value={shownPositionMs}
      />
      <span className="text-theme-500 -mt-0.5 w-10 shrink-0 text-xs tabular-nums">
        {formatDuration(durationMs)}
      </span>
    </div>
  );
}

function PlayerBarVolume() {
  const volume = usePlaybackStore((state) => state.transport?.volume ?? 80);
  const setVolume = usePlaybackStore((state) => state.setVolume);
  const { displayedVolume, onValueChange, onValueCommitted } = usePlayerVolume({
    setVolume,
    volume,
  });

  return (
    <div className="flex items-center gap-2 lg:w-56 lg:shrink-0">
      <Volume2 className="text-theme-500 dark:text-theme-400 h-4 w-4" />
      <PlayerSlider
        ariaLabel="Volume"
        max={100}
        min={0}
        onValueChange={onValueChange}
        onValueCommitted={onValueCommitted}
        value={displayedVolume}
      />
    </div>
  );
}

function PlayerIconButton({
  active = false,
  children,
  className = "",
  disabled,
  label,
  onClick,
}: {
  active?: boolean;
  children: ReactNode;
  className?: string;
  disabled?: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      aria-label={label}
      className={[
        "rounded p-2 transition-colors disabled:cursor-default disabled:opacity-50",
        active
          ? "text-accent-800 hover:text-accent-600 dark:text-accent-200 dark:hover:text-accent-50"
          : "text-theme-700 hover:text-theme-600 dark:text-theme-300 dark:hover:text-theme-100",
        className,
      ].join(" ")}
      disabled={disabled}
      onClick={onClick}
      type="button"
    >
      {children}
    </button>
  );
}

function PlayerSlider({
  ariaLabel,
  disabled,
  max,
  min,
  onInteractionStart,
  onValueChange,
  onValueCommitted,
  step = 1,
  value,
}: {
  ariaLabel: string;
  disabled?: boolean;
  max: number;
  min: number;
  onInteractionStart?: () => void;
  onValueChange: (value: number) => void;
  onValueCommitted?: (value: number, details?: { reason?: string }) => void;
  step?: number;
  value: number;
}) {
  const safeMax = Math.max(max, min + 1);
  const clampedValue = clamp(value, min, safeMax);

  return (
    <Slider.Root
      className={[
        "flex min-w-0 flex-1 items-center",
        disabled ? "opacity-45" : "",
      ].join(" ")}
      disabled={disabled}
      max={safeMax}
      min={min}
      onValueChange={onValueChange}
      onValueCommitted={onValueCommitted}
      step={step}
      value={clampedValue}
    >
      <Slider.Control
        className="flex h-4 w-full items-center"
        onPointerDownCapture={onInteractionStart}
        onTouchStartCapture={onInteractionStart}
      >
        <Slider.Track className="bg-theme-200 relative h-1.5 w-full rounded-full dark:bg-black/50">
          <Slider.Indicator className="bg-theme-700 dark:bg-theme-300 absolute h-full rounded-full bg-linear-to-b from-white/10 to-black/8 dark:from-white/7 dark:to-black/7" />
          <Slider.Thumb
            className="focus-visible:outline-theme-700 border-theme-300/80 to-theme-100 shadow-theme-900/15 dark:bg-theme-100 dark:focus-visible:outline-theme-100 bg-theme-50 block h-4 w-4 rounded-full border bg-linear-to-b from-white shadow-md focus-visible:outline-2 focus-visible:outline-offset-2 dark:border-black/28 dark:from-white/15 dark:to-black/15 dark:shadow-black/24"
            getAriaLabel={() => ariaLabel}
          />
        </Slider.Track>
      </Slider.Control>
    </Slider.Root>
  );
}
