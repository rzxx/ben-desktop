import * as NotificationsFacade from "../../../bindings/ben/desktop/notificationsfacade";
import { Types } from "./models";

export function listNotifications() {
  return NotificationsFacade.ListNotifications();
}

export function subscribeNotificationEvents() {
  return NotificationsFacade.SubscribeNotificationEvents();
}

export function getNotificationPreferences() {
  return NotificationsFacade.GetNotificationPreferences();
}

export function setNotificationVerbosity(verbosity: string) {
  return NotificationsFacade.SetNotificationVerbosity(
    verbosity as Types.NotificationVerbosity,
  );
}
