import * as AppUpdateFacade from "../../../bindings/ben/desktop/appupdatefacade";
import type * as Types from "../../../bindings/ben/desktop/api/types/models";

export type AppUpdateStatus = Types.AppUpdateStatus;
export type AppUpdateCheckResult = Types.AppUpdateCheckResult;

export function getAppUpdateStatus() {
  return AppUpdateFacade.GetStatus();
}

export function checkForUpdates() {
  return AppUpdateFacade.CheckForUpdates();
}
