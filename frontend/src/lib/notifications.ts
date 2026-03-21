import type {
  NotificationAudience,
  NotificationPhase,
  NotificationSnapshot,
  NotificationVerbosity,
} from "@/lib/api/models";

export type NotificationFilter = "all" | "user" | "system";

export function upsertNotificationSnapshots(
  current: NotificationSnapshot[],
  next: NotificationSnapshot,
) {
  return sortNotifications([
    next,
    ...current.filter((notification) => notification.id !== next.id),
  ]);
}

export function sortNotifications(notifications: NotificationSnapshot[]) {
  return [...notifications].sort((left, right) => {
    const updatedDiff =
      new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime();
    if (updatedDiff !== 0) {
      return updatedDiff;
    }
    const createdDiff =
      new Date(right.createdAt).getTime() - new Date(left.createdAt).getTime();
    if (createdDiff !== 0) {
      return createdDiff;
    }
    return left.id.localeCompare(right.id);
  });
}

export function isNotificationActive(phase?: NotificationPhase | string | null) {
  return phase === "queued" || phase === "running";
}

export function shouldToastNotification(
  notification: NotificationSnapshot,
  verbosity?: NotificationVerbosity | string | null,
) {
  if (notification.phase === "error") {
    return true;
  }
  switch (verbosity) {
    case "important":
      return notification.audience === "user" && notification.importance === "important";
    case "everything":
      return true;
    default:
      return notification.audience === "user";
  }
}

export function matchesNotificationFilter(
  notification: NotificationSnapshot,
  filter: NotificationFilter,
) {
  if (filter === "all") {
    return true;
  }
  return notification.audience === filter;
}

export function notificationHeading(notification: NotificationSnapshot) {
  const subjectTitle = notification.subject?.title?.trim() ?? "";
  if (subjectTitle) {
    return subjectTitle;
  }
  switch (notification.kind) {
    case "playback-loading":
      return "Preparing playback";
    case "scan-activity":
      return "Scan activity";
    case "artwork-activity":
      return "Artwork activity";
    case "transcode-activity":
      return "Transcode activity";
    case "scan-library":
      return "Library scan";
    case "scan-root":
      return "Root scan";
    case "sync-now":
      return "Manual sync";
    case "connect-peer":
      return "Connect peer";
    case "publish-checkpoint":
      return "Publish checkpoint";
    case "compact-checkpoint":
      return "Compact checkpoint";
    case "install-checkpoint":
      return "Checkpoint install";
    case "ensure-recording-encoding":
      return "Track encoding";
    case "ensure-album-encodings":
      return "Album encodings";
    case "ensure-playlist-encodings":
      return "Playlist encodings";
    case "join-session":
      return "Join session";
    case "finalize-join-session":
      return "Finalize join";
    default:
      return notification.kind || "Notification";
  }
}

export function notificationDescription(notification: NotificationSnapshot) {
  const detail =
    notification.error?.trim() || notification.message?.trim() || "No details";
  const subtitle = notification.subject?.subtitle?.trim() ?? "";
  if (subtitle && subtitle !== detail) {
    return `${subtitle}. ${detail}`;
  }
  return detail;
}

export function notificationMetaLine(notification: NotificationSnapshot) {
  const parts = [
    audienceLabel(notification.audience),
    importanceLabel(notification.importance),
  ].filter(Boolean);
  return parts.join(" • ");
}

export function phaseLabel(phase?: NotificationPhase | string | null) {
  switch (phase) {
    case "queued":
      return "Queued";
    case "running":
      return "Running";
    case "success":
      return "Done";
    case "error":
      return "Failed";
    default:
      return "Active";
  }
}

export function phaseTone(phase?: NotificationPhase | string | null) {
  switch (phase) {
    case "success":
      return "border-emerald-400/25 bg-emerald-400/12 text-emerald-100";
    case "error":
      return "border-rose-400/25 bg-rose-400/12 text-rose-100";
    case "queued":
      return "border-amber-300/20 bg-amber-300/12 text-amber-100";
    default:
      return "border-sky-400/20 bg-sky-400/12 text-sky-100";
  }
}

export function notificationTimeout(notification: NotificationSnapshot) {
  if (notification.phase === "error") {
    return 0;
  }
  if (notification.phase === "success") {
    return 3200;
  }
  return 0;
}

export function relativeNotificationTime(value?: Date | string | null) {
  if (!value) {
    return "";
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const diff = Date.now() - date.getTime();
  if (diff < 60_000) {
    return "just now";
  }
  if (diff < 3_600_000) {
    return `${Math.max(1, Math.round(diff / 60_000))}m ago`;
  }
  if (diff < 86_400_000) {
    return `${Math.max(1, Math.round(diff / 3_600_000))}h ago`;
  }
  return date.toLocaleString();
}

function audienceLabel(audience?: NotificationAudience | string | null) {
  switch (audience) {
    case "user":
      return "User";
    case "system":
      return "System";
    default:
      return "";
  }
}

function importanceLabel(importance?: string | null) {
  switch (importance) {
    case "important":
      return "Important";
    case "normal":
      return "Activity";
    case "debug":
      return "Debug";
    default:
      return "";
  }
}
