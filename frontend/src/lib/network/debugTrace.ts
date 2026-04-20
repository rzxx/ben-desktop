import {
  clearNetworkDebugTrace as clearBackendNetworkDebugTrace,
  getNetworkDebugDump as getBackendNetworkDebugDump,
  getNetworkDebugTrace,
  getNetworkStatus,
  getNetworkTraceEnabled as getBackendNetworkTraceEnabled,
  setNetworkTraceEnabled as setBackendNetworkTraceEnabled,
} from "@/lib/api/network";
import { Types } from "@/lib/api/models";

const NETWORK_DEBUG_TRACE_LIMIT = 240;

type NetworkDebugSnapshot = {
  enabled: boolean;
  status: Types.NetworkStatus | null;
  trace: Types.NetworkDebugTraceEntry[];
  error: string;
};

type NetworkDebugWindow = {
  clear: () => Promise<void>;
  copy: () => Promise<string>;
  dump: () => Promise<string>;
  isEnabled: () => boolean;
  refresh: () => Promise<void>;
  setEnabled: (enabled: boolean) => Promise<void>;
  state: () => NetworkDebugSnapshot;
};

const listeners = new Set<() => void>();

let enabled = false;
let status: Types.NetworkStatus | null = null;
let trace: Types.NetworkDebugTraceEntry[] = [];
let error = "";
let pollHandle: number | null = null;
let snapshotCache: NetworkDebugSnapshot = {
  enabled,
  status,
  trace,
  error,
};

function notify() {
  for (const listener of listeners) {
    listener();
  }
}

function refreshSnapshotCache() {
  snapshotCache = {
    enabled,
    status,
    trace,
    error,
  };
}

function resetNetworkDebugState() {
  status = null;
  trace = [];
  error = "";
}

function stopPolling() {
  if (pollHandle == null) {
    return;
  }
  window.clearInterval(pollHandle);
  pollHandle = null;
}

function startPolling() {
  if (typeof window === "undefined" || pollHandle != null || !enabled) {
    return;
  }
  pollHandle = window.setInterval(() => {
    void refreshNetworkDebugState();
  }, 2000);
}

function applyNetworkTraceEnabled(nextEnabled: boolean) {
  if (enabled === nextEnabled) {
    return;
  }
  enabled = nextEnabled;
  if (!nextEnabled) {
    stopPolling();
    resetNetworkDebugState();
  } else {
    startPolling();
    void refreshNetworkDebugState();
  }
  refreshSnapshotCache();
  notify();
}

export async function syncNetworkTraceEnabled() {
  try {
    applyNetworkTraceEnabled(await getBackendNetworkTraceEnabled());
  } catch {
    applyNetworkTraceEnabled(false);
  }
}

export function isNetworkTraceEnabled() {
  return enabled;
}

export async function setNetworkTraceEnabled(enabled: boolean) {
  await setBackendNetworkTraceEnabled(enabled);
  applyNetworkTraceEnabled(enabled);
}

export async function refreshNetworkDebugState() {
  if (!enabled) {
    return;
  }
  try {
    const [nextStatus, nextTrace] = await Promise.all([
      getNetworkStatus(),
      getNetworkDebugTrace(),
    ]);
    status = nextStatus;
    trace = nextTrace.slice(-NETWORK_DEBUG_TRACE_LIMIT);
    error = "";
  } catch (nextError) {
    error =
      nextError instanceof Error
        ? nextError.message
        : "Failed to refresh network trace";
  }
  refreshSnapshotCache();
  notify();
}

export async function clearNetworkDebugState() {
  resetNetworkDebugState();
  await clearBackendNetworkDebugTrace();
  refreshSnapshotCache();
  notify();
}

export async function buildNetworkDebugDump() {
  const backendRaw = await getBackendNetworkDebugDump();
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
      status,
      trace,
      error,
    },
    backend,
  };
}

export async function copyNetworkDebugDump() {
  const payload = await buildNetworkDebugDump();
  const text = JSON.stringify(payload, null, 2);
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
  }
  return text;
}

export function installNetworkDebugWindow() {
  if (typeof window === "undefined") {
    return;
  }
  const debugWindow = window as Window & {
    __benNetworkDebug?: NetworkDebugWindow;
  };
  debugWindow.__benNetworkDebug = {
    clear: clearNetworkDebugState,
    copy: copyNetworkDebugDump,
    dump: async () => JSON.stringify(await buildNetworkDebugDump(), null, 2),
    isEnabled: () => enabled,
    refresh: refreshNetworkDebugState,
    setEnabled: setNetworkTraceEnabled,
    state: () => snapshotCache,
  };
}

export function subscribeNetworkDebug(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getNetworkDebugSnapshot() {
  return snapshotCache;
}
