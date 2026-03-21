import { Events } from "@wailsio/runtime";
import { create } from "zustand";
import type {
  NotificationPreferences,
  NotificationSnapshot,
} from "@/lib/api/models";
import { Types } from "@/lib/api/models";
import {
  getNotificationPreferences,
  listNotifications,
  setNotificationVerbosity,
  subscribeNotificationEvents,
} from "@/lib/api/notifications";
import {
  applyNotificationSnapshotBatch,
  type NotificationFilter,
  sortNotifications,
} from "@/lib/notifications";

const queuedNotifications = new Map<string, NotificationSnapshot>();
let scheduledFlush: number | null = null;

function scheduleNotificationFlush() {
  if (scheduledFlush !== null) {
    return;
  }
  scheduledFlush = window.requestAnimationFrame(() => {
    scheduledFlush = null;
    flushQueuedNotifications();
  });
}

function flushQueuedNotifications() {
  if (queuedNotifications.size === 0) {
    return;
  }
  const batch = Array.from(queuedNotifications.values());
  queuedNotifications.clear();
  useNotificationsStore.getState().applyNotificationBatch(batch);
}

function cancelNotificationFlush() {
  if (scheduledFlush !== null) {
    window.cancelAnimationFrame(scheduledFlush);
    scheduledFlush = null;
  }
  queuedNotifications.clear();
}

type NotificationsStore = {
  started: boolean;
  notifications: NotificationSnapshot[];
  preferences: NotificationPreferences;
  centerOpen: boolean;
  centerFilter: NotificationFilter;
  stopListening?: () => void;
  bootstrap: () => Promise<void>;
  teardown: () => void;
  setNotifications: (notifications: NotificationSnapshot[]) => void;
  applyNotificationBatch: (notifications: NotificationSnapshot[]) => void;
  setCenterOpen: (open: boolean) => void;
  toggleCenter: () => void;
  setCenterFilter: (filter: NotificationFilter) => void;
  setVerbosity: (verbosity: string) => Promise<void>;
};

const defaultPreferences = new Types.NotificationPreferences({
  verbosity: Types.NotificationVerbosity.NotificationVerbosityUserActivity,
});

export const useNotificationsStore = create<NotificationsStore>((set, get) => ({
  started: false,
  notifications: [],
  preferences: defaultPreferences,
  centerOpen: false,
  centerFilter: "all",
  stopListening: undefined,

  setNotifications: (notifications) => {
    set({
      notifications: sortNotifications(notifications),
    });
  },

  applyNotificationBatch: (notifications) => {
    set((current) => ({
      notifications: applyNotificationSnapshotBatch(
        current.notifications,
        notifications,
      ),
    }));
  },

  bootstrap: async () => {
    if (get().started) {
      return;
    }

    const [notifications, preferences, eventName] = await Promise.all([
      listNotifications(),
      getNotificationPreferences().catch(() => defaultPreferences),
      subscribeNotificationEvents(),
    ]);

    const stopListening = Events.On(eventName, (event) => {
      const notification = Types.NotificationSnapshot.createFrom(event.data);
      queuedNotifications.set(notification.id, notification);
      scheduleNotificationFlush();
    });

    set({
      started: true,
      notifications: sortNotifications(notifications),
      preferences,
      stopListening,
    });
  },

  teardown: () => {
    get().stopListening?.();
    cancelNotificationFlush();
    set({
      started: false,
      stopListening: undefined,
    });
  },

  setCenterOpen: (open) => {
    set({ centerOpen: open });
  },

  toggleCenter: () => {
    set((current) => ({ centerOpen: !current.centerOpen }));
  },

  setCenterFilter: (filter) => {
    set({ centerFilter: filter });
  },

  setVerbosity: async (verbosity) => {
    const preferences = await setNotificationVerbosity(verbosity);
    set({ preferences });
  },
}));
