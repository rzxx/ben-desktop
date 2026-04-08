import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { Types } from "@/lib/api/models";

let notificationEventListener: ((event: { data: unknown }) => void) | undefined;

vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return {
    ...actual,
    Events: {
      ...actual.Events,
      On: vi.fn(
        (_eventName: string, callback: (event: { data: unknown }) => void) => {
          notificationEventListener = callback;
          return () => {
            notificationEventListener = undefined;
          };
        },
      ),
    },
  };
});

vi.mock("@/lib/api/notifications", () => ({
  listNotifications: vi.fn(async () => []),
  getNotificationPreferences: vi.fn(async () => ({
    verbosity: "user_activity",
  })),
  setNotificationVerbosity: vi.fn(async (verbosity: string) => ({ verbosity })),
  subscribeNotificationEvents: vi.fn(async () => "notifications:snapshot"),
}));

import { useNotificationsStore } from "./store";

function makeNotification(source: Partial<Types.NotificationSnapshot> = {}) {
  return new Types.NotificationSnapshot({
    id: "scan-1",
    kind: "repair-library",
    audience: Types.NotificationAudience.NotificationAudienceUser,
    importance: Types.NotificationImportance.NotificationImportanceNormal,
    phase: Types.NotificationPhase.NotificationPhaseRunning,
    createdAt: "2026-03-21T10:00:00.000Z",
    updatedAt: "2026-03-21T10:00:00.000Z",
    ...source,
  });
}

describe("notifications store batching", () => {
  beforeEach(() => {
    notificationEventListener = undefined;
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      return window.setTimeout(() => callback(performance.now()), 0);
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation((handle) => {
      window.clearTimeout(handle);
    });
  });

  afterEach(() => {
    useNotificationsStore.getState().teardown();
    useNotificationsStore.setState({
      started: false,
      notifications: [],
      preferences: new Types.NotificationPreferences({
        verbosity:
          Types.NotificationVerbosity.NotificationVerbosityUserActivity,
      }),
      centerOpen: false,
      centerFilter: "all",
      stopListening: undefined,
    });
    vi.restoreAllMocks();
  });

  test("collapses repeated notification events for one id into a single frame flush", async () => {
    await useNotificationsStore.getState().bootstrap();

    let notificationUpdates = 0;
    const unsubscribe = useNotificationsStore.subscribe((state, previous) => {
      if (state.notifications !== previous.notifications) {
        notificationUpdates += 1;
      }
    });

    notificationEventListener?.({
      data: makeNotification({
        id: "scan-1",
        message: "Scanning tracks 1/100",
        updatedAt: "2026-03-21T10:00:01.000Z",
      }),
    });
    notificationEventListener?.({
      data: makeNotification({
        id: "scan-1",
        message: "Scanning tracks 2/100",
        updatedAt: "2026-03-21T10:00:02.000Z",
      }),
    });

    await new Promise((resolve) => window.setTimeout(resolve, 10));

    const notifications = useNotificationsStore.getState().notifications;
    unsubscribe();

    expect(notificationUpdates).toBe(1);
    expect(notifications).toHaveLength(1);
    expect(notifications[0].message).toBe("Scanning tracks 2/100");
  });
});
