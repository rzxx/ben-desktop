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
  type NotificationFilter,
  sortNotifications,
  upsertNotificationSnapshots,
} from "@/lib/notifications";

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
  upsertNotification: (notification: NotificationSnapshot) => void;
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

  upsertNotification: (notification) => {
    set((current) => ({
      notifications: upsertNotificationSnapshots(
        current.notifications,
        notification,
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
      get().upsertNotification(notification);
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
