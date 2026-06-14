import * as NotificationsFacade from "../../../bindings/ben/desktop/notificationsfacade";
import { Types } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export function listNotifications() {
  return traceWailsCall("notifications", "list_notifications", undefined, () =>
    NotificationsFacade.ListNotifications(),
  );
}

export function subscribeNotificationEvents() {
  return NotificationsFacade.SubscribeNotificationEvents();
}

export function getNotificationPreferences() {
  return traceWailsCall(
    "notifications",
    "get_notification_preferences",
    undefined,
    () => NotificationsFacade.GetNotificationPreferences(),
  );
}

export function setNotificationVerbosity(verbosity: string) {
  return traceWailsCall(
    "notifications",
    "set_notification_verbosity",
    { verbosity },
    () =>
      NotificationsFacade.SetNotificationVerbosity(
        verbosity as Types.NotificationVerbosity,
      ),
  );
}
