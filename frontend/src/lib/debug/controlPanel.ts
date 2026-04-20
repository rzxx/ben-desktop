const DEBUG_CONTROL_PANEL_KEY = "ben.desktop.debugControlPanel";

type DebugControlPanelWindow = {
  hide: () => void;
  isVisible: () => boolean;
  show: () => void;
  toggle: () => void;
};

const listeners = new Set<() => void>();

let visible = loadInitialVisibility();
let snapshotCache = {
  visible,
};

function loadInitialVisibility() {
  if (typeof window === "undefined") {
    return false;
  }
  const storage = window.localStorage;
  if (typeof storage?.getItem !== "function") {
    return false;
  }
  return storage.getItem(DEBUG_CONTROL_PANEL_KEY) === "1";
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
    storage.setItem(DEBUG_CONTROL_PANEL_KEY, "1");
  } else {
    storage.removeItem(DEBUG_CONTROL_PANEL_KEY);
  }
}

function notify() {
  for (const listener of listeners) {
    listener();
  }
}

function refreshSnapshotCache() {
  snapshotCache = {
    visible,
  };
}

export function setDebugControlPanelVisible(nextVisible: boolean) {
  if (visible === nextVisible) {
    return;
  }
  visible = nextVisible;
  persistVisibility(nextVisible);
  refreshSnapshotCache();
  notify();
}

export function getDebugControlPanelSnapshot() {
  return snapshotCache;
}

export function subscribeDebugControlPanel(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function installDebugControlWindow() {
  if (typeof window === "undefined") {
    return;
  }
  const debugWindow = window as Window & {
    __benDebug?: DebugControlPanelWindow;
  };
  debugWindow.__benDebug = {
    hide: () => setDebugControlPanelVisible(false),
    isVisible: () => visible,
    show: () => setDebugControlPanelVisible(true),
    toggle: () => setDebugControlPanelVisible(!visible),
  };
}
