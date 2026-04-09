import { Toast } from "@base-ui/react/toast";
import { X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import type { NotificationSnapshot } from "@/lib/api/models";
import { NotificationCard } from "@/components/notifications/NotificationCard";
import {
  hasNotificationToastBeenShown,
  isNotificationActive,
  notificationDescription,
  notificationHeading,
  notificationToastHistoryStorageKey,
  notificationTimeout,
  recordNotificationToastShown,
  readNotificationToastHistory,
  serializeNotificationToastHistory,
  shouldToastNotification,
} from "@/lib/notifications";
import { useNotificationsStore } from "@/stores/notifications/store";

type ToastData = {
  notification: NotificationSnapshot;
};

const notificationToastManager = Toast.createToastManager<ToastData>();

export function NotificationRuntime() {
  const notifications = useNotificationsStore((state) => state.notifications);
  const preferences = useNotificationsStore((state) => state.preferences);
  const [dismissedAt, setDismissedAt] = useState<Record<string, number>>({});
  const [shownAt, setShownAt] = useState<Record<string, number>>(
    loadNotificationToastHistory,
  );
  const activeToastIdsRef = useRef<Set<string>>(new Set());
  const activeToastNotificationsRef = useRef<Map<string, NotificationSnapshot>>(
    new Map(),
  );
  const suppressDismissRef = useRef<Set<string>>(new Set());
  const shownAtRef = useRef<Record<string, number>>(shownAt);

  useEffect(() => {
    shownAtRef.current = shownAt;
    persistNotificationToastHistory(shownAt);
  }, [shownAt]);

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
      const payload = {
        id: notification.id,
        title: notificationHeading(notification),
        description: notificationDescription(notification),
        timeout: notificationTimeout(notification),
        priority: notification.phase === "error" ? "high" : "low",
        type: notification.phase,
        data: { notification },
        onRemove: () => {
          const currentNotification = activeToastNotificationsRef.current.get(
            notification.id,
          );
          const isCurrentInstance =
            currentNotification !== undefined &&
            String(currentNotification.updatedAt ?? "") ===
              String(notification.updatedAt ?? "") &&
            currentNotification.phase === notification.phase;

          if (suppressDismissRef.current.has(notification.id)) {
            suppressDismissRef.current.delete(notification.id);
          } else if (isCurrentInstance) {
            setDismissedAt((current) => ({
              ...current,
              [notification.id]: Date.now(),
            }));
          }
          if (isCurrentInstance) {
            activeToastIdsRef.current.delete(notification.id);
            activeToastNotificationsRef.current.delete(notification.id);
          }
        },
      } as const;

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

      if (activeToastIdsRef.current.has(notification.id)) {
        const previous = activeToastNotificationsRef.current.get(
          notification.id,
        );
        const becameTerminal =
          previous !== undefined &&
          isNotificationActive(previous.phase) &&
          !isNotificationActive(notification.phase);
        if (becameTerminal) {
          suppressDismissRef.current.add(notification.id);
          notificationToastManager.close(notification.id);
          notificationToastManager.add(payload);
        } else {
          notificationToastManager.update(notification.id, payload);
        }
        markShown();
      } else {
        if (hasNotificationToastBeenShown(notification, nextShownAt)) {
          continue;
        }
        notificationToastManager.add(payload);
        activeToastIdsRef.current.add(notification.id);
        markShown();
      }
      activeToastNotificationsRef.current.set(notification.id, notification);
    }

    for (const notification of notifications) {
      if (visibleIds.has(notification.id)) {
        continue;
      }
      notificationToastManager.close(notification.id);
      activeToastIdsRef.current.delete(notification.id);
      activeToastNotificationsRef.current.delete(notification.id);
    }

    if (shownAtChanged) {
      shownAtRef.current = nextShownAt;
      setShownAt(nextShownAt);
    }
  }, [notifications, visibleNotifications]);

  return (
    <Toast.Provider limit={4} toastManager={notificationToastManager}>
      <NotificationToastViewport />
    </Toast.Provider>
  );
}

function NotificationToastViewport() {
  const { toasts } = Toast.useToastManager<ToastData>();

  return (
    <Toast.Portal>
      <Toast.Viewport className="pointer-events-none fixed top-12 right-4 z-50 flex w-full max-w-md flex-col gap-3 max-lg:left-4 max-lg:max-w-none">
        {toasts.map((toast) => {
          if (!toast.data) {
            return null;
          }
          return (
            <Toast.Root
              className="pointer-events-auto transition-[opacity,transform,height,margin] duration-200 ease-out data-[ending-style]:-translate-x-4 data-[ending-style]:opacity-0 data-[limited]:pointer-events-none data-[limited]:-mt-[calc(var(--toast-height,0px)+0.75rem)] data-[limited]:opacity-0 data-[starting-style]:translate-y-2 data-[starting-style]:opacity-0"
              key={toast.id}
              swipeDirection={["right", "down"]}
              toast={toast}
            >
              <Toast.Content className="transition-[transform,opacity,filter] duration-200 ease-out data-[behind]:-translate-y-3 data-[behind]:scale-[0.97] data-[behind]:opacity-95 data-[expanded]:translate-y-0 data-[expanded]:scale-100 data-[expanded]:opacity-100">
                <div className="relative">
                  <Toast.Title className="sr-only">{toast.title}</Toast.Title>
                  <Toast.Description className="sr-only">
                    {toast.description}
                  </Toast.Description>
                  <NotificationCard
                    className="border-theme-300/75 shadow-theme-900/14 dark:border-theme-500/12 dark:bg-theme-900/82 bg-white/92 shadow-2xl backdrop-blur-xl dark:shadow-black/35"
                    compact
                    notification={toast.data.notification}
                  />
                  {!isNotificationActive(toast.data.notification.phase) && (
                    <div className="ring-theme-300/70 pointer-events-none absolute inset-0 rounded-[1.2rem] ring-1 dark:ring-white/6" />
                  )}
                  <Toast.Close
                    aria-label="Dismiss notification"
                    className="text-theme-600 border-theme-300/75 hover:border-theme-400/75 hover:text-theme-900 absolute top-3 right-3 inline-flex h-7 w-7 items-center justify-center rounded-full border bg-white/88 transition dark:border-white/10 dark:bg-black/20 dark:text-white/55 dark:hover:border-white/18 dark:hover:text-white"
                  >
                    <X className="h-3.5 w-3.5" />
                  </Toast.Close>
                </div>
              </Toast.Content>
            </Toast.Root>
          );
        })}
      </Toast.Viewport>
    </Toast.Portal>
  );
}

function loadNotificationToastHistory() {
  const storage = getNotificationToastHistoryStorage();
  return readNotificationToastHistory(
    storage?.getItem(notificationToastHistoryStorageKey),
  );
}

function persistNotificationToastHistory(history: Record<string, number>) {
  const storage = getNotificationToastHistoryStorage();
  if (!storage) {
    return;
  }

  try {
    storage.setItem(
      notificationToastHistoryStorageKey,
      serializeNotificationToastHistory(history),
    );
  } catch {
    // Ignore storage failures; toast delivery still works for the current session.
  }
}

function getNotificationToastHistoryStorage() {
  if (typeof window === "undefined") {
    return null;
  }

  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}
