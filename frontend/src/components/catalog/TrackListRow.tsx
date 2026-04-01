import { Heart, Trash2 } from "lucide-react";
import { type MouseEvent, type ReactNode } from "react";
import {
  TrackRowContextMenu,
  type TrackRowRecordingIdentity,
} from "@/components/catalog/TrackRowContextMenu";
import {
  availabilityLabel,
  formatDuration,
  isCatalogTrackActionable,
} from "@/lib/format";

function stopClickPropagation(event: MouseEvent<HTMLButtonElement>) {
  event.stopPropagation();
}

function InlineActionButton({
  children,
  disabled,
  label,
  onClick,
  title,
  tone = "default",
}: {
  children: ReactNode;
  disabled?: boolean;
  label: string;
  onClick: () => void;
  title: string;
  tone?: "danger" | "default";
}) {
  return (
    <button
      aria-label={label}
      className={[
        "rounded-full p-2 transition disabled:pointer-events-none disabled:opacity-45",
        tone === "danger"
          ? "text-theme-500 hover:text-red-200"
          : "text-theme-500 hover:text-theme-100",
      ].join(" ")}
      disabled={disabled}
      onClick={(event) => {
        stopClickPropagation(event);
        onClick();
      }}
      title={title}
      type="button"
    >
      {children}
    </button>
  );
}

export function TrackListRow({
  availabilityState,
  durationMs,
  indexLabel,
  isActive = false,
  isLiked = false,
  likeBusy = false,
  mode = "list",
  onPlay,
  onQueue,
  onRemove,
  onToggleLike,
  recording,
  removeLabel = "Remove track",
  subtitle,
  title,
}: {
  availabilityState?: string;
  durationMs: number;
  indexLabel: string;
  isActive?: boolean;
  isLiked?: boolean;
  likeBusy?: boolean;
  mode?: "album" | "list";
  onPlay: () => void;
  onQueue: () => void;
  onRemove?: () => void;
  onToggleLike?: () => void;
  recording?: TrackRowRecordingIdentity;
  removeLabel?: string;
  subtitle: string;
  title: string;
}) {
  const actionable = isCatalogTrackActionable(availabilityState);
  const pendingAvailability =
    !actionable && (!availabilityState || availabilityState === "PENDING");
  const secondaryText =
    actionable || pendingAvailability
      ? subtitle
      : `${subtitle} • ${availabilityLabel(availabilityState)}`;
  const compact = mode === "list";

  return (
    <TrackRowContextMenu
      actionable={actionable}
      onQueue={onQueue}
      recording={recording}
      title={title}
    >
      {({ open }) => (
        <div
          className={[
            "group flex items-center gap-2 transition-colors",
            compact ? "rounded-xl px-1 py-0.5" : "rounded-2xl px-1.5 py-1",
            actionable ? "" : "opacity-50",
            pendingAvailability ? "animate-pulse" : "",
          ].join(" ")}
        >
          <button
            className={[
              "flex min-w-0 flex-1 items-center text-left transition-colors disabled:pointer-events-none",
              compact ? "rounded-xl px-3 py-2.5" : "rounded-2xl px-3 py-3",
              open ? "bg-theme-800" : "hover:bg-theme-800",
            ].join(" ")}
            disabled={!actionable}
            onClick={onPlay}
            title={
              actionable
                ? `Play ${title}`
                : pendingAvailability
                  ? `${title} pending availability`
                  : `${title} unavailable`
            }
            type="button"
          >
            <span
              className={[
                "text-theme-500 shrink-0 tabular-nums",
                compact ? "w-8 text-[11px]" : "w-10 text-xs",
              ].join(" ")}
            >
              {indexLabel}
            </span>

            <div className="min-w-0 flex-1">
              <p
                className={[
                  "truncate font-medium",
                  compact ? "text-sm" : "",
                  isActive ? "text-accent-300" : "text-theme-100",
                  isActive
                    ? ""
                    : compact
                      ? "group-hover:text-white"
                      : "group-hover:text-theme-50",
                ].join(" ")}
              >
                {title}
              </p>
              <p
                className={[
                  "text-theme-500 truncate",
                  compact ? "text-[11px]" : "text-xs",
                ].join(" ")}
              >
                {secondaryText}
              </p>
            </div>

            <span
              className={[
                "text-theme-300 ml-3 shrink-0 pl-1 text-right tabular-nums",
                compact ? "w-12 text-[11px]" : "w-14 text-xs",
              ].join(" ")}
            >
              {formatDuration(durationMs)}
            </span>
          </button>

          {onToggleLike ? (
            <InlineActionButton
              disabled={likeBusy}
              label={isLiked ? `Unlike ${title}` : `Like ${title}`}
              onClick={onToggleLike}
              title={isLiked ? "Unlike track" : "Like track"}
            >
              <Heart
                className="h-4 w-4"
                fill={isLiked ? "currentColor" : "none"}
              />
            </InlineActionButton>
          ) : null}

          {onRemove ? (
            <InlineActionButton
              label={removeLabel}
              onClick={onRemove}
              title={removeLabel}
              tone="danger"
            >
              <Trash2 className="h-4 w-4" />
            </InlineActionButton>
          ) : null}
        </div>
      )}
    </TrackRowContextMenu>
  );
}
