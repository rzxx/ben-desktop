import type {
  NotificationAudience,
  NotificationPhase,
  NotificationSnapshot,
  NotificationVerbosity,
} from "@/lib/api/models";

export type NotificationFilter = "all" | "user" | "system";

export const notificationToastHistoryStorageKey =
  "ben.notifications.toast-history";

const notificationToastHistoryLimit = 200;

export function upsertNotificationSnapshots(
  current: NotificationSnapshot[],
  next: NotificationSnapshot,
) {
  return applyNotificationSnapshotBatch(current, [next]);
}

export function applyNotificationSnapshotBatch(
  current: NotificationSnapshot[],
  incoming: NotificationSnapshot[],
) {
  if (incoming.length === 0) {
    return current;
  }

  const notificationsById = new Map(
    current.map((notification) => [notification.id, notification] as const),
  );
  let changed = false;

  for (const next of incoming) {
    const previous = notificationsById.get(next.id);
    if (previous && notificationsEqual(previous, next)) {
      continue;
    }
    notificationsById.set(next.id, next);
    changed = true;
  }

  if (!changed) {
    return current;
  }

  return sortNotifications(Array.from(notificationsById.values()));
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

export function isNotificationActive(
  phase?: NotificationPhase | string | null,
) {
  return phase === "queued" || phase === "running";
}

export function shouldToastNotification(
  notification: NotificationSnapshot,
  verbosity?: NotificationVerbosity | string | null,
) {
  if (
    notification.phase === "success" &&
    (notification.kind === "playback-loading" ||
      notification.kind === "playback-preload")
  ) {
    return false;
  }
  if (notification.phase === "error") {
    return true;
  }
  switch (verbosity) {
    case "important":
      return (
        notification.audience === "user" &&
        notification.importance === "important"
      );
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
    case "playback-skip":
      return "Playback queue";
    case "playback-preload":
      return "Playback preload";
    case "scan-activity":
      return "Scan activity";
    case "artwork-activity":
      return "Artwork activity";
    case "sync-activity":
      return "Background sync";
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

export function notificationTimestamp(value?: Date | string | null) {
  if (!value) {
    return 0;
  }
  const date = value instanceof Date ? value : new Date(value);
  const timestamp = date.getTime();
  return Number.isFinite(timestamp) ? timestamp : 0;
}

export function notificationVersion(notification: NotificationSnapshot) {
  return Math.max(
    notificationTimestamp(notification.updatedAt),
    notificationTimestamp(notification.createdAt),
  );
}

export function hasNotificationToastBeenShown(
  notification: NotificationSnapshot,
  history: Record<string, number>,
) {
  const version = notificationVersion(notification);
  if (version === 0) {
    return false;
  }
  return (history[notification.id] ?? 0) >= version;
}

export function recordNotificationToastShown(
  history: Record<string, number>,
  notification: NotificationSnapshot,
) {
  const version = notificationVersion(notification);
  if (version === 0 || (history[notification.id] ?? 0) >= version) {
    return history;
  }
  return trimNotificationToastHistory({
    ...history,
    [notification.id]: version,
  });
}

export function readNotificationToastHistory(raw?: string | null) {
  if (!raw) {
    return {};
  }

  try {
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }

    const history = Object.fromEntries(
      Object.entries(parsed).flatMap(([id, value]) =>
        typeof value === "number" && Number.isFinite(value) && value > 0
          ? [[id, Math.trunc(value)]]
          : [],
      ),
    );

    return trimNotificationToastHistory(history);
  } catch {
    return {};
  }
}

export function serializeNotificationToastHistory(
  history: Record<string, number>,
) {
  return JSON.stringify(trimNotificationToastHistory(history));
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

function notificationsEqual(
  left: NotificationSnapshot,
  right: NotificationSnapshot,
) {
  return (
    left.id === right.id &&
    left.kind === right.kind &&
    left.libraryId === right.libraryId &&
    left.audience === right.audience &&
    left.importance === right.importance &&
    left.phase === right.phase &&
    left.message === right.message &&
    left.error === right.error &&
    left.progress === right.progress &&
    left.sticky === right.sticky &&
    String(left.createdAt ?? "") === String(right.createdAt ?? "") &&
    String(left.updatedAt ?? "") === String(right.updatedAt ?? "") &&
    String(left.finishedAt ?? "") === String(right.finishedAt ?? "") &&
    notificationSubjectsEqual(left.subject, right.subject)
  );
}

function notificationSubjectsEqual(
  left?: NotificationSnapshot["subject"] | null,
  right?: NotificationSnapshot["subject"] | null,
) {
  if (!left && !right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.recordingId === right.recordingId &&
    left.title === right.title &&
    left.subtitle === right.subtitle &&
    left.artworkRef === right.artworkRef
  );
}

function trimNotificationToastHistory(history: Record<string, number>) {
  const entries = Object.entries(history).filter(
    ([id, value]) => id && Number.isFinite(value) && value > 0,
  );

  if (entries.length <= notificationToastHistoryLimit) {
    return Object.fromEntries(entries);
  }

  return Object.fromEntries(
    entries
      .sort((left, right) => right[1] - left[1])
      .slice(0, notificationToastHistoryLimit),
  );
}
