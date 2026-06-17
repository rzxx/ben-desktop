import type { NotificationSnapshot } from "@/lib/api/models";
import {
  notificationDescription,
  notificationHeading,
} from "@/lib/notifications";

type NotificationItemProps = {
  muted?: boolean;
  notification: NotificationSnapshot;
};

export function NotificationItem({
  muted = false,
  notification,
}: NotificationItemProps) {
  return (
    <article
      className={[
        "border-theme-300/75 rounded-2xl border bg-white/75 p-3 dark:border-white/8 dark:bg-black/10",
        muted ? "opacity-60" : "",
      ].join(" ")}
    >
      <p className="text-theme-900 dark:text-theme-100 text-sm font-semibold">
        {notificationHeading(notification)}
      </p>
      <p className="text-theme-600 dark:text-theme-300 mt-0.5 text-xs">
        {notificationDescription(notification)}
      </p>
    </article>
  );
}
