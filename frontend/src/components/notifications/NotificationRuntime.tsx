import { useEffect, useMemo, useRef, useState } from "react";
import {
  hasNotificationToastBeenShown,
  notificationDescription,
  notificationHeading,
  notificationTimeout,
  recordNotificationToastShown,
  shouldToastNotification,
} from "@/lib/notifications";
import { toast } from "@/components/ui/toast";
import { useNotificationsStore } from "@/stores/notifications/store";

const historyKey = "ben.notifications.toast-history";

function phaseToToastType(phase: string) {
  switch (phase) {
    case "success":
      return "success";
    case "error":
      return "error";
    case "queued":
    case "running":
      return "loading";
    default:
      return "info";
  }
}

function readHistory(): Record<string, number> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const raw = window.sessionStorage.getItem(historyKey);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed as Record<string, unknown>).flatMap(
        ([id, value]) =>
          typeof value === "number" && Number.isFinite(value) && value > 0
            ? [[id, Math.trunc(value)]]
            : [],
      ),
    );
  } catch {
    return {};
  }
}

function writeHistory(history: Record<string, number>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.sessionStorage.setItem(historyKey, JSON.stringify(history));
  } catch {
    // Ignore storage failures; toast delivery still works for the current session.
  }
}

export function NotificationRuntime() {
  const notifications = useNotificationsStore((state) => state.notifications);
  const preferences = useNotificationsStore((state) => state.preferences);
  const [dismissedAt, setDismissedAt] = useState<Record<string, number>>({});
  const activeRef = useRef<Map<string, string>>(new Map());
  const shownAtRef = useRef<Record<string, number>>(readHistory());

  const visibleNotifications = useMemo(
    () =>
      notifications.filter((notification) => {
        if (!shouldToastNotification(notification, preferences.verbosity)) {
          return false;
        }
        const dismissedAtValue = dismissedAt[notification.id] ?? 0;
        return (
          dismissedAtValue === 0 ||
          new Date(notification.updatedAt).getTime() > dismissedAtValue
        );
      }),
    [dismissedAt, notifications, preferences.verbosity],
  );

  useEffect(() => {
    const visibleIds = new Set(visibleNotifications.map((item) => item.id));
    let nextShownAt = shownAtRef.current;
    let shownAtChanged = false;

    for (const notification of visibleNotifications) {
      const id = notification.id;
      const options = {
        id,
        title: notificationHeading(notification),
        description: notificationDescription(notification),
        timeout: notificationTimeout(notification),
        priority:
          notification.phase === "error" ? ("high" as const) : ("low" as const),
        type: phaseToToastType(notification.phase),
        onRemove: () => {
          if (activeRef.current.has(id)) {
            setDismissedAt((current) => ({ ...current, [id]: Date.now() }));
            activeRef.current.delete(id);
          }
        },
      } as const;
      const renderKey = JSON.stringify([
        options.title,
        options.description,
        options.timeout,
        options.priority,
        options.type,
      ]);

      const markShown = () => {
        const updatedHistory = recordNotificationToastShown(
          nextShownAt,
          notification,
        );
        if (updatedHistory !== nextShownAt) {
          nextShownAt = updatedHistory;
          shownAtChanged = true;
        }
      };

      if (activeRef.current.has(id)) {
        if (activeRef.current.get(id) !== renderKey) {
          toast.update(id, options);
          activeRef.current.set(id, renderKey);
        }
      } else if (!hasNotificationToastBeenShown(notification, nextShownAt)) {
        toast(options);
        activeRef.current.set(id, renderKey);
      } else {
        continue;
      }
      markShown();
    }

    for (const notification of notifications) {
      if (visibleIds.has(notification.id)) {
        continue;
      }
      if (activeRef.current.has(notification.id)) {
        toast.dismiss(notification.id);
        activeRef.current.delete(notification.id);
      }
    }

    if (shownAtChanged) {
      shownAtRef.current = nextShownAt;
      writeHistory(nextShownAt);
    }
  }, [notifications, visibleNotifications]);

  return null;
}
