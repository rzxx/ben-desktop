import { useSyncExternalStore } from "react";
import { Events, Window } from "@wailsio/runtime";

type Listener = () => void;

const listeners = new Set<Listener>();

let isMaximised = false;
let refreshInFlight: Promise<void> | null = null;
let unsubscribeWindowEvents: (() => void) | null = null;

function emitChange() {
  listeners.forEach((listener) => {
    listener();
  });
}

function setWindowMaximised(nextValue: boolean) {
  if (isMaximised === nextValue) {
    return;
  }

  isMaximised = nextValue;
  emitChange();
}

function ensureWindowEventSubscription() {
  if (unsubscribeWindowEvents) {
    return;
  }

  const offMaximise = Events.On("common:WindowMaximise", () => {
    setWindowMaximised(true);
  });
  const offUnMaximise = Events.On("common:WindowUnMaximise", () => {
    setWindowMaximised(false);
  });
  const offRestore = Events.On("common:WindowRestore", () => {
    void refreshWindowMaximisedState();
  });
  const offUnMinimise = Events.On("common:WindowUnMinimise", () => {
    void refreshWindowMaximisedState();
  });
  const offRuntimeReady = Events.On("common:WindowRuntimeReady", () => {
    void refreshWindowMaximisedState();
  });

  unsubscribeWindowEvents = () => {
    offMaximise();
    offUnMaximise();
    offRestore();
    offUnMinimise();
    offRuntimeReady();
    unsubscribeWindowEvents = null;
  };
}

function subscribe(listener: Listener) {
  listeners.add(listener);
  ensureWindowEventSubscription();
  void refreshWindowMaximisedState();

  return () => {
    listeners.delete(listener);

    if (listeners.size === 0) {
      unsubscribeWindowEvents?.();
    }
  };
}

function getSnapshot() {
  return isMaximised;
}

export function useWindowMaximised() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

export async function refreshWindowMaximisedState() {
  if (refreshInFlight) {
    return refreshInFlight;
  }

  refreshInFlight = Window.IsMaximised()
    .then((nextValue) => {
      setWindowMaximised(nextValue);
    })
    .catch(() => undefined)
    .finally(() => {
      refreshInFlight = null;
    });

  return refreshInFlight;
}

export async function toggleWindowMaximised() {
  try {
    await Window.ToggleMaximise();
  } finally {
    await refreshWindowMaximisedState();
  }
}
