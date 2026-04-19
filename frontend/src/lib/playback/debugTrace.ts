import {
  clearPlaybackDebugTrace as clearBackendPlaybackDebugTrace,
  getPlaybackTraceEnabled as getBackendPlaybackTraceEnabled,
  getPlaybackDebugDump,
} from "@/lib/api/playback";

const PLAYBACK_DEBUG_PANEL_KEY = "ben.desktop.playbackDebugPanel";
const PLAYBACK_DEBUG_TRACE_LIMIT = 240;

export type PlaybackFrontendTraceEntry = {
  timestampMs: number;
  kind: string;
  message?: string;
  seekRequestId?: string;
  currentEntryId?: string;
  status?: string;
  positionMs?: number;
  positionCapturedAtMs?: number;
  shownPositionMs?: number;
  draftPositionMs?: number | null;
  pendingSeekMs?: number | null;
  pendingSeekCapturedAtMs?: number | null;
  isDragging?: boolean;
};

type PlaybackDebugLiveState = {
  currentEntryId: string;
  status: string;
  transportPositionMs: number;
  transportCapturedAtMs: number;
  shownPositionMs: number;
  draftPositionMs: number | null;
  pendingSeekMs: number | null;
  pendingSeekCapturedAtMs: number | null;
  pendingSeekRequestId: string;
  isDragging: boolean;
};

type PlaybackDebugSnapshot = {
  enabled: boolean;
  visible: boolean;
  trace: PlaybackFrontendTraceEntry[];
  liveState: PlaybackDebugLiveState;
};

type PlaybackDebugWindow = {
  clear: () => Promise<void>;
  copy: () => Promise<string>;
  dump: () => Promise<string>;
  hide: () => void;
  isEnabled: () => boolean;
  show: () => void;
  state: () => PlaybackDebugSnapshot;
  toggle: () => void;
};

const listeners = new Set<() => void>();

let trace: PlaybackFrontendTraceEntry[] = [];
let enabled = false;
let visible = false;
let lastValueChangeLog: PlaybackFrontendTraceEntry | null = null;
let seekRequestSequence = 0;
let liveState: PlaybackDebugLiveState = {
  currentEntryId: "",
  status: "",
  transportPositionMs: 0,
  transportCapturedAtMs: 0,
  shownPositionMs: 0,
  draftPositionMs: null,
  pendingSeekMs: null,
  pendingSeekCapturedAtMs: null,
  pendingSeekRequestId: "",
  isDragging: false,
};
let snapshotCache: PlaybackDebugSnapshot = {
  enabled,
  visible,
  trace,
  liveState,
};

function loadInitialVisibility() {
  if (typeof window === "undefined") {
    return false;
  }
  const storage = window.localStorage;
  if (typeof storage?.getItem !== "function") {
    return false;
  }
  return storage.getItem(PLAYBACK_DEBUG_PANEL_KEY) === "1";
}

visible = loadInitialVisibility();

function notify() {
  for (const listener of listeners) {
    listener();
  }
}

function persistVisibility(nextVisible: boolean) {
  if (typeof window === "undefined") {
    return;
  }
  const storage = window.localStorage;
  if (
    storage == null ||
    typeof storage.getItem !== "function" ||
    typeof storage.setItem !== "function" ||
    typeof storage.removeItem !== "function"
  ) {
    return;
  }
  if (nextVisible) {
    storage.setItem(PLAYBACK_DEBUG_PANEL_KEY, "1");
  } else {
    storage.removeItem(PLAYBACK_DEBUG_PANEL_KEY);
  }
}

function getSnapshot(): PlaybackDebugSnapshot {
  return snapshotCache;
}

function refreshSnapshotCache() {
  snapshotCache = {
    enabled,
    visible,
    trace,
    liveState,
  };
}

function appendTrace(entry: PlaybackFrontendTraceEntry) {
  trace = [...trace, entry].slice(-PLAYBACK_DEBUG_TRACE_LIMIT);
}

function areLiveStatesEqual(
  left: PlaybackDebugLiveState,
  right: PlaybackDebugLiveState,
) {
  return (
    left.currentEntryId === right.currentEntryId &&
    left.status === right.status &&
    left.transportPositionMs === right.transportPositionMs &&
    left.transportCapturedAtMs === right.transportCapturedAtMs &&
    left.shownPositionMs === right.shownPositionMs &&
    left.draftPositionMs === right.draftPositionMs &&
    left.pendingSeekMs === right.pendingSeekMs &&
    left.pendingSeekCapturedAtMs === right.pendingSeekCapturedAtMs &&
    left.pendingSeekRequestId === right.pendingSeekRequestId &&
    left.isDragging === right.isDragging
  );
}

function shouldSkipTrace(entry: PlaybackFrontendTraceEntry) {
  if (entry.kind !== "slider:valueChange") {
    return false;
  }
  if (lastValueChangeLog == null) {
    return false;
  }
  if (entry.currentEntryId !== lastValueChangeLog.currentEntryId) {
    return false;
  }
  if (entry.timestampMs - lastValueChangeLog.timestampMs >= 120) {
    return false;
  }
  return (
    Math.abs((entry.positionMs ?? 0) - (lastValueChangeLog.positionMs ?? 0)) <
    250
  );
}

export function nextPlaybackDebugSeekRequestId() {
  seekRequestSequence += 1;
  return `seek-${seekRequestSequence}`;
}

export function recordPlaybackDebugTrace(
  entry: Omit<PlaybackFrontendTraceEntry, "timestampMs">,
) {
  if (!enabled) {
    return;
  }
  const nextEntry: PlaybackFrontendTraceEntry = {
    timestampMs: Math.round(performance.now()),
    ...entry,
  };
  if (shouldSkipTrace(nextEntry)) {
    return;
  }
  if (nextEntry.kind === "slider:valueChange") {
    lastValueChangeLog = nextEntry;
  }
  appendTrace(nextEntry);
  refreshSnapshotCache();
  notify();
}

export function updatePlaybackDebugLiveState(
  nextPartial: Partial<PlaybackDebugLiveState>,
) {
  if (!enabled) {
    return;
  }
  const nextState: PlaybackDebugLiveState = {
    ...liveState,
    ...nextPartial,
  };
  if (areLiveStatesEqual(liveState, nextState)) {
    return;
  }
  liveState = nextState;
  refreshSnapshotCache();
  notify();
}

export function setPlaybackDebugPanelVisible(nextVisible: boolean) {
  if (visible === nextVisible) {
    return;
  }
  visible = nextVisible;
  persistVisibility(nextVisible);
  refreshSnapshotCache();
  notify();
}

function resetPlaybackDebugState() {
  trace = [];
  lastValueChangeLog = null;
  liveState = {
    currentEntryId: "",
    status: "",
    transportPositionMs: 0,
    transportCapturedAtMs: 0,
    shownPositionMs: 0,
    draftPositionMs: null,
    pendingSeekMs: null,
    pendingSeekCapturedAtMs: null,
    pendingSeekRequestId: "",
    isDragging: false,
  };
}

function applyPlaybackTraceEnabled(nextEnabled: boolean) {
  if (enabled === nextEnabled) {
    return;
  }
  enabled = nextEnabled;
  if (!nextEnabled) {
    resetPlaybackDebugState();
    visible = false;
    persistVisibility(false);
  }
  refreshSnapshotCache();
  notify();
}

export async function syncPlaybackTraceEnabled() {
  try {
    applyPlaybackTraceEnabled(await getBackendPlaybackTraceEnabled());
  } catch {
    applyPlaybackTraceEnabled(false);
  }
}

export function isPlaybackTraceEnabled() {
  return enabled;
}

export async function clearPlaybackDebugState() {
  const hadState =
    trace.length > 0 ||
    lastValueChangeLog != null ||
    liveState.currentEntryId !== "" ||
    liveState.status !== "" ||
    liveState.transportPositionMs !== 0 ||
    liveState.transportCapturedAtMs !== 0 ||
    liveState.shownPositionMs !== 0 ||
    liveState.draftPositionMs != null ||
    liveState.pendingSeekMs != null ||
    liveState.pendingSeekCapturedAtMs != null ||
    liveState.pendingSeekRequestId !== "" ||
    liveState.isDragging;
  resetPlaybackDebugState();
  await clearBackendPlaybackDebugTrace();
  if (hadState) {
    refreshSnapshotCache();
    notify();
  }
}

export async function buildPlaybackDebugDump() {
  const backendRaw = await getPlaybackDebugDump();
  let backend: unknown;
  try {
    backend = JSON.parse(backendRaw);
  } catch {
    backend = {
      parseError: true,
      raw: backendRaw,
    };
  }

  return {
    generatedAtIso: new Date().toISOString(),
    frontend: {
      enabled,
      liveState,
      trace,
    },
    backend,
  };
}

export async function copyPlaybackDebugDump() {
  const payload = await buildPlaybackDebugDump();
  const text = JSON.stringify(payload, null, 2);
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
  }
  return text;
}

export function installPlaybackDebugWindow() {
  if (typeof window === "undefined") {
    return;
  }
  const debugWindow = window as Window & {
    __benPlaybackDebug?: PlaybackDebugWindow;
  };
  debugWindow.__benPlaybackDebug = {
    clear: clearPlaybackDebugState,
    copy: copyPlaybackDebugDump,
    dump: async () => JSON.stringify(await buildPlaybackDebugDump(), null, 2),
    hide: () => setPlaybackDebugPanelVisible(false),
    isEnabled: () => enabled,
    show: () => setPlaybackDebugPanelVisible(true),
    state: getSnapshot,
    toggle: () => setPlaybackDebugPanelVisible(!visible),
  };
}

export function subscribePlaybackDebug(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getPlaybackDebugSnapshot() {
  return getSnapshot();
}
