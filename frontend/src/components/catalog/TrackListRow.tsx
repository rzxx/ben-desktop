import { FolderPlus, Heart, ListPlus, Play, Trash2 } from "lucide-react";
import {
  availabilityLabel,
  formatDuration,
  isCatalogTrackActionable,
} from "@/lib/format";

export function TrackListRow({
  availabilityState,
  durationMs,
  indexLabel,
  isLiked = false,
  likeBusy = false,
  mode = "list",
  onAddToPlaylist,
  onRemove,
  onToggleLike,
  onPlay,
  onQueue,
  removeLabel = "Remove track",
  subtitle,
  title,
}: {
  availabilityState?: string;
  durationMs: number;
  indexLabel: string;
  isLiked?: boolean;
  likeBusy?: boolean;
  mode?: "album" | "list";
  onAddToPlaylist?: () => void;
  onRemove?: () => void;
  onToggleLike?: () => void;
  onPlay: () => void;
  onQueue: () => void;
  removeLabel?: string;
  subtitle: string;
  title: string;
}) {
  const actionable = isCatalogTrackActionable(availabilityState);
  const secondaryText = actionable
    ? subtitle
    : `${subtitle} • ${availabilityLabel(availabilityState)}`;

  if (mode === "album") {
    return (
      <div className="group flex items-center rounded-2xl px-2 py-1">
        <button
          className="hover:bg-theme-800 flex min-w-0 flex-1 items-center rounded-2xl px-2 py-3 text-left transition-colors disabled:pointer-events-none disabled:opacity-40"
          disabled={!actionable}
          onClick={onPlay}
          type="button"
        >
          <span className="text-theme-500 w-10 shrink-0 text-xs tabular-nums">
            {indexLabel}
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-theme-100 group-hover:text-theme-50 truncate font-medium">
              {title}
            </p>
            <p className="text-theme-500 truncate text-xs">{secondaryText}</p>
          </div>
          <span className="text-theme-300 ml-auto w-14 shrink-0 pl-1 text-right text-xs tabular-nums">
            {formatDuration(durationMs)}
          </span>
        </button>

        <button
          aria-label={`Queue ${title}`}
          className="text-theme-500 hover:text-theme-200 ml-1 rounded p-2 transition-colors disabled:pointer-events-none disabled:opacity-40"
          disabled={!actionable}
          onClick={onQueue}
          title={actionable ? "Queue track" : `${title} unavailable`}
          type="button"
        >
          <ListPlus className="h-4 w-4" />
        </button>
        {onAddToPlaylist ? (
          <button
            aria-label={`Add ${title} to playlist`}
            className="text-theme-500 hover:text-theme-200 ml-1 rounded p-2 transition-colors"
            onClick={onAddToPlaylist}
            title="Add to playlist"
            type="button"
          >
            <FolderPlus className="h-4 w-4" />
          </button>
        ) : null}
        {onToggleLike ? (
          <button
            aria-label={isLiked ? `Unlike ${title}` : `Like ${title}`}
            className="text-theme-500 hover:text-theme-200 ml-1 rounded p-2 transition-colors disabled:opacity-50"
            disabled={likeBusy}
            onClick={onToggleLike}
            title={isLiked ? "Unlike track" : "Like track"}
            type="button"
          >
            <Heart
              className="h-4 w-4"
              fill={isLiked ? "currentColor" : "none"}
            />
          </button>
        ) : null}
        {onRemove ? (
          <button
            aria-label={removeLabel}
            className="text-theme-500 ml-1 rounded p-2 transition-colors hover:text-red-200"
            onClick={onRemove}
            title={removeLabel}
            type="button"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        ) : null}
      </div>
    );
  }

  return (
    <div
      className={[
        "flex items-center gap-3 rounded-md px-3 py-2",
        actionable ? "" : "opacity-33",
      ].join(" ")}
    >
      <div className="min-w-0 flex-1">
        <p className="text-theme-100 truncate text-sm font-medium">{title}</p>
        <p className="text-theme-500 truncate text-xs">{secondaryText}</p>
      </div>

      <p className="text-theme-500 w-14 shrink-0 text-right text-xs tabular-nums">
        {formatDuration(durationMs)}
      </p>

      <button
        aria-label={`Queue ${title}`}
        className="text-theme-500 hover:text-theme-100 rounded p-2 transition-colors disabled:pointer-events-none disabled:opacity-40"
        disabled={!actionable}
        onClick={onQueue}
        title={actionable ? "Queue track" : `${title} unavailable`}
        type="button"
      >
        <ListPlus className="h-4 w-4" />
      </button>

      {onAddToPlaylist ? (
        <button
          aria-label={`Add ${title} to playlist`}
          className="text-theme-500 hover:text-theme-100 rounded p-2 transition-colors"
          onClick={onAddToPlaylist}
          title="Add to playlist"
          type="button"
        >
          <FolderPlus className="h-4 w-4" />
        </button>
      ) : null}

      {onToggleLike ? (
        <button
          aria-label={isLiked ? `Unlike ${title}` : `Like ${title}`}
          className="text-theme-500 hover:text-theme-100 rounded p-2 transition-colors disabled:opacity-50"
          disabled={likeBusy}
          onClick={onToggleLike}
          title={isLiked ? "Unlike track" : "Like track"}
          type="button"
        >
          <Heart className="h-4 w-4" fill={isLiked ? "currentColor" : "none"} />
        </button>
      ) : null}

      <button
        aria-label={`Play ${title}`}
        className="text-theme-500 hover:text-theme-100 rounded p-2 transition-colors disabled:pointer-events-none disabled:opacity-40"
        disabled={!actionable}
        onClick={onPlay}
        title={actionable ? "Play track" : `${title} unavailable`}
        type="button"
      >
        <Play className="h-4 w-4" />
      </button>

      {onRemove ? (
        <button
          aria-label={removeLabel}
          className="text-theme-500 rounded p-2 transition-colors hover:text-red-200"
          onClick={onRemove}
          title={removeLabel}
          type="button"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      ) : null}
    </div>
  );
}
