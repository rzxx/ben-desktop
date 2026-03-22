import {
  BellRing,
  Bug,
  CircleAlert,
  LoaderCircle,
  UserRound,
} from "lucide-react";
import type { NotificationSnapshot } from "@/lib/api/models";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { useRecordingArtworkUrl } from "@/hooks/media/useRecordingArtworkUrl";
import {
  notificationDescription,
  notificationHeading,
  notificationMetaLine,
  phaseLabel,
  phaseTone,
  relativeNotificationTime,
} from "@/lib/notifications";

type NotificationCardProps = {
  notification: NotificationSnapshot;
  compact?: boolean;
  muted?: boolean;
  className?: string;
};

export function NotificationCard({
  notification,
  compact = false,
  muted = false,
  className = "",
}: NotificationCardProps) {
  const artworkUrl = useRecordingArtworkUrl(
    notification.subject?.artworkRef || notification.subject?.recordingId,
  );
  const title = notificationHeading(notification);
  const meta = notificationMetaLine(notification);
  const description = notificationDescription(notification);
  const progress = Math.round((notification.progress ?? 0) * 100);

  return (
    <article
      className={[
        "rounded-[1.2rem] border border-white/8 bg-black/10",
        compact ? "p-3" : "p-4",
        muted ? "opacity-70" : "",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="flex items-start gap-3">
        <div className="shrink-0">
          {notification.subject?.recordingId ? (
            <ArtworkTile
              alt={title}
              className={
                compact ? "h-12 w-12 rounded-xl" : "h-14 w-14 rounded-2xl"
              }
              src={artworkUrl}
              title={title}
            />
          ) : (
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
              <NotificationGlyph notification={notification} />
            </div>
          )}
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold text-white">
                {title}
              </p>
              {notification.subject?.subtitle ? (
                <p className="mt-1 truncate text-xs text-white/45">
                  {notification.subject.subtitle}
                </p>
              ) : null}
            </div>
            <span
              className={`rounded-full border px-2 py-1 text-[0.62rem] tracking-[0.18em] uppercase ${phaseTone(notification.phase)}`}
            >
              {phaseLabel(notification.phase)}
            </span>
          </div>

          <p className="mt-2 text-sm leading-5 text-white/70">{description}</p>

          {(notification.phase === "queued" ||
            notification.phase === "running") && (
            <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-white/8">
              <div
                className="h-full rounded-full bg-[linear-gradient(90deg,rgba(251,146,60,0.92),rgba(14,165,233,0.8))] transition-[width] duration-300"
                style={{ width: `${Math.max(8, progress)}%` }}
              />
            </div>
          )}

          <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-[0.68rem] tracking-[0.18em] text-white/35 uppercase">
            <span>{meta || "Core"}</span>
            <span>{relativeNotificationTime(notification.updatedAt)}</span>
          </div>
        </div>
      </div>
    </article>
  );
}

function NotificationGlyph({
  notification,
}: {
  notification: NotificationSnapshot;
}) {
  if (notification.phase === "error") {
    return <CircleAlert className="h-5 w-5" />;
  }
  if (notification.audience === "user") {
    return <UserRound className="h-5 w-5" />;
  }
  if (notification.importance === "debug") {
    return <Bug className="h-5 w-5" />;
  }
  if (notification.phase === "running" || notification.phase === "queued") {
    return <LoaderCircle className="h-5 w-5 animate-spin" />;
  }
  return <BellRing className="h-5 w-5" />;
}
