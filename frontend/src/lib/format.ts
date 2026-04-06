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
    case "PENDING":
      return "Pending";
    case "PLAYABLE:LOCAL_FILE":
      return "Local";
    case "PLAYABLE:CACHED_OPT":
      return "Cached";
    case "PLAYABLE:REMOTE_OPT":
    case "WAITING:PROVIDER_TRANSCODE":
      return "Online";
    case "UNAVAILABLE:PROVIDER_OFFLINE":
      return "Offline";
    case "UNAVAILABLE:NO_PATH":
      return "Unavailable";
    default:
      return "Pending";
  }
}

export function aggregateAvailabilityLabel(
  availability?: {
    State?: string | null;
    AvailableNowTrackCount?: number | null;
    LocalTrackCount?: number | null;
    LocalSourceTrackCount?: number | null;
    PinnedTrackCount?: number | null;
    CachedTrackCount?: number | null;
    HasRemote?: boolean | null;
    RemoteTrackCount?: number | null;
    AvailableTrackCount?: number | null;
    OfflineTrackCount?: number | null;
    UnavailableTrackCount?: number | null;
    TrackCount?: number | null;
  } | null,
) {
  if (!availability) {
    return availabilityLabel();
  }
  switch (availability.State) {
    case "LOCAL":
      return "Local";
    case "PINNED":
      return "Pinned";
    case "CACHED":
      return "Cached";
    case "AVAILABLE":
      return "Available";
    case "PARTIAL":
      return `${Math.max(0, Math.trunc(availability.AvailableNowTrackCount ?? availability.AvailableTrackCount ?? 0))} available`;
    case "OFFLINE":
      return "Offline";
    case "UNAVAILABLE":
      return "Unavailable";
    default:
      break;
  }
  if ((availability?.LocalTrackCount ?? 0) > 0) {
    return availabilityLabel("PLAYABLE:LOCAL_FILE");
  }
  if ((availability?.CachedTrackCount ?? 0) > 0) {
    return availabilityLabel("PLAYABLE:CACHED_OPT");
  }
  if ((availability?.AvailableTrackCount ?? 0) > 0) {
    return availabilityLabel("PLAYABLE:REMOTE_OPT");
  }
  if ((availability?.RemoteTrackCount ?? 0) > 0 || availability?.HasRemote) {
    return availabilityLabel("UNAVAILABLE:PROVIDER_OFFLINE");
  }
  if ((availability?.UnavailableTrackCount ?? 0) > 0) {
    return availabilityLabel("UNAVAILABLE:NO_PATH");
  }
  return availabilityLabel("PENDING");
}

export function isAggregateAvailabilityPlayable(
  availability?: {
    State?: string | null;
    AvailableNowTrackCount?: number | null;
    TrackCount?: number | null;
  } | null,
) {
  if (!availability) {
    return true;
  }

  const availableNowTrackCount = Math.max(
    0,
    Math.trunc(availability.AvailableNowTrackCount ?? 0),
  );
  if (availableNowTrackCount > 0) {
    return true;
  }

  const trackCount = Math.max(0, Math.trunc(availability.TrackCount ?? 0));
  switch (availability.State) {
    case "LOCAL":
    case "PINNED":
    case "CACHED":
    case "AVAILABLE":
      return true;
    case "OFFLINE":
    case "UNAVAILABLE":
      return false;
    default:
      return trackCount > 0;
  }
}

export function isAlbumUnavailableInCatalog(
  availability?: {
    State?: string | null;
  } | null,
) {
  switch (availability?.State) {
    case "OFFLINE":
    case "UNAVAILABLE":
      return true;
    default:
      return false;
  }
}

export function isTrackCollectionPlayable({
  trackCount,
  fullyLoaded = false,
  hasPlayableLoadedTrack = false,
}: {
  trackCount?: number | null;
  fullyLoaded?: boolean;
  hasPlayableLoadedTrack?: boolean;
}) {
  const total = Math.max(0, Math.trunc(trackCount ?? 0));
  if (total === 0) {
    return false;
  }
  if (!fullyLoaded) {
    return true;
  }
  return hasPlayableLoadedTrack;
}

export function isCatalogTrackActionable(state?: string) {
  switch (state) {
    case "PLAYABLE:LOCAL_FILE":
    case "PLAYABLE:CACHED_OPT":
    case "PLAYABLE:REMOTE_OPT":
    case "WAITING:PROVIDER_TRANSCODE":
      return true;
    default:
      return false;
  }
}

export function availabilityTone(
  state?:
    | string
    | {
        State?: string | null;
        AvailableNowTrackCount?: number | null;
        AvailableTrackCount?: number | null;
        CachedTrackCount?: number | null;
        HasRemote?: boolean | null;
        LocalTrackCount?: number | null;
        OfflineTrackCount?: number | null;
        RemoteTrackCount?: number | null;
        UnavailableTrackCount?: number | null;
      }
    | null,
): "default" | "success" | "warning" | "danger" {
  const resolvedState =
    typeof state === "string"
      ? state
      : (state?.State ?? aggregateAvailabilityLabel(state).toUpperCase());

  switch (resolvedState) {
    case "LOCAL":
    case "PINNED":
    case "CACHED":
      return "success";
    case "AVAILABLE":
    case "PARTIAL":
    case "ONLINE":
    case "WAITING:PROVIDER_TRANSCODE":
    case "PLAYABLE:REMOTE_OPT":
      return "default";
    case "OFFLINE":
    case "UNAVAILABLE:PROVIDER_OFFLINE":
      return "warning";
    default:
      return "danger";
  }
}

export function pinStateLabel(
  pinState?: {
    Pinned?: boolean | null;
    Direct?: boolean | null;
    Covered?: boolean | null;
    Pending?: boolean | null;
  } | null,
) {
  if (!pinState?.Pinned) {
    return "";
  }
  if (pinState.Direct) {
    return pinState.Pending ? "Pinned • syncing" : "Pinned";
  }
  if (pinState.Covered) {
    return pinState.Pending ? "Covered • syncing" : "Covered";
  }
  return pinState.Pending ? "Pinned • syncing" : "Pinned";
}
