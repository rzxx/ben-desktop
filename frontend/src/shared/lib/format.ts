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

export function formatCount(value: number, singular: string, plural = `${singular}s`) {
  const rounded = Math.max(0, Math.trunc(value));
  return `${rounded} ${rounded === 1 ? singular : plural}`;
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
    case "PROVIDER_ONLINE":
      return "Online";
    case "PROVIDER_OFFLINE":
      return "Offline";
    default:
      return "Unavailable";
  }
}
