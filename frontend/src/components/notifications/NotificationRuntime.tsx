import { Toast } from "@base-ui/react/toast";
import { X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import type { NotificationSnapshot } from "@/lib/api/models";
import { NotificationCard } from "@/components/notifications/NotificationCard";
import {
  isNotificationActive,
  notificationDescription,
  notificationHeading,
  notificationTimeout,
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
  const activeToastIdsRef = useRef<Set<string>>(new Set());
  const activeToastNotificationsRef = useRef<Map<string, NotificationSnapshot>>(
    new Map(),
  );
  const suppressDismissRef = useRef<Set<string>>(new Set());

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
      } else {
        notificationToastManager.add(payload);
        activeToastIdsRef.current.add(notification.id);
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
  }, [notifications, visibleNotifications]);

  return (
    <Toast.Provider
      limit={4}
      timeout={0}
      toastManager={notificationToastManager}
    >
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
              className="pointer-events-auto"
              key={toast.id}
              swipeDirection={["right", "down"]}
              toast={toast}
            >
              <Toast.Content>
                <div className="relative">
                  <Toast.Title className="sr-only">{toast.title}</Toast.Title>
                  <Toast.Description className="sr-only">
                    {toast.description}
                  </Toast.Description>
                  <NotificationCard
                    className="bg-theme-900/82 border-theme-500/12 shadow-2xl shadow-black/35 backdrop-blur-xl"
                    compact
                    notification={toast.data.notification}
                  />
                  {!isNotificationActive(toast.data.notification.phase) && (
                    <div className="pointer-events-none absolute inset-0 rounded-[1.2rem] ring-1 ring-white/6" />
                  )}
                  <Toast.Close
                    aria-label="Dismiss notification"
                    className="absolute top-3 right-3 inline-flex h-7 w-7 items-center justify-center rounded-full border border-white/10 bg-black/20 text-white/55 transition hover:border-white/18 hover:text-white"
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
