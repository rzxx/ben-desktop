export function formatDuration(durationMs?: number | null) {
  if (!durationMs || durationMs <= 0) {
    return "--:--";
  }
  const totalSeconds = Math.floor(durationMs / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  }
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

export function formatCount(
  value: number,
  singular: string,
  plural = `${singular}s`,
) {
  const rounded = Math.max(0, Math.trunc(value));
  return `${rounded} ${rounded === 1 ? singular : plural}`;
}

export function formatBytes(value?: number | null) {
  const bytes = Math.max(0, Math.trunc(value ?? 0));
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  const units = ["KB", "MB", "GB", "TB"];
  let size = bytes / 1024;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[unitIndex]}`;
}

export function formatRelativeDate(value?: Date | string | null) {
  if (!value) {
    return "No activity";
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "No activity";
  }
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}

export function formatDateTime(value?: Date | string | null) {
  if (!value) {
    return "No activity";
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "No activity";
  }
  return new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    month: "short",
  }).format(date);
}

export function artistLetter(name: string) {
  const match = name.trim().match(/[A-Za-z0-9]/);
  return match ? match[0]!.toUpperCase() : "#";
}

export function joinArtists(values: string[]) {
  return values.filter(Boolean).join(", ");
}

export function availabilityLabel(state?: string) {
  switch (state) {
    case "LOCAL":
      return "Local";
    case "CACHED":
      return "Cached";
    case "PROVIDER_ONLINE":
      return "Online";
    case "PROVIDER_OFFLINE":
      return "Offline";
    default:
      return "Unavailable";
  }
}

export function aggregateAvailabilityLabel(availability?: {
  LocalTrackCount?: number | null;
  CachedTrackCount?: number | null;
  ProviderOnlineTrackCount?: number | null;
  ProviderOfflineTrackCount?: number | null;
}) {
  if ((availability?.LocalTrackCount ?? 0) > 0) {
    return availabilityLabel("LOCAL");
  }
  if ((availability?.CachedTrackCount ?? 0) > 0) {
    return availabilityLabel("CACHED");
  }
  if ((availability?.ProviderOnlineTrackCount ?? 0) > 0) {
    return availabilityLabel("PROVIDER_ONLINE");
  }
  if ((availability?.ProviderOfflineTrackCount ?? 0) > 0) {
    return availabilityLabel("PROVIDER_OFFLINE");
  }
  return availabilityLabel();
}

export function isCatalogTrackActionable(state?: string) {
  switch (state) {
    case "LOCAL":
    case "CACHED":
    case "PROVIDER_ONLINE":
      return true;
    default:
      return false;
  }
}

export function availabilityTone(
  state?: string | { LocalTrackCount?: number | null; CachedTrackCount?: number | null; ProviderOnlineTrackCount?: number | null; ProviderOfflineTrackCount?: number | null },
): "default" | "success" | "warning" | "danger" {
  const resolvedState =
    typeof state === "string" ? state : aggregateAvailabilityLabel(state).toUpperCase();

  switch (resolvedState) {
    case "LOCAL":
    case "CACHED":
      return "success";
    case "ONLINE":
    case "PROVIDER_ONLINE":
      return "default";
    case "OFFLINE":
    case "PROVIDER_OFFLINE":
      return "warning";
    default:
      return "danger";
  }
}
