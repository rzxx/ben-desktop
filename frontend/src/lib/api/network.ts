import * as NetworkFacade from "../../../bindings/ben/desktop/networkfacade";

export function getLocalContext() {
  return NetworkFacade.EnsureLocalContext();
}

export function getActivityStatus() {
  return NetworkFacade.ActivityStatus();
}

export function getCheckpointStatus() {
  return NetworkFacade.CheckpointStatus();
}

export function getInspectSummary() {
  return NetworkFacade.Inspect();
}

export function getLibraryOplogDiagnostics(libraryId = "") {
  return NetworkFacade.InspectLibraryOplog(libraryId);
}

export function startConnectPeer(peerAddr: string) {
  return NetworkFacade.StartConnectPeer(peerAddr);
}

export function startPublishCheckpoint() {
  return NetworkFacade.StartPublishCheckpoint();
}

export function startCompactCheckpoint(force = false) {
  return NetworkFacade.StartCompactCheckpoint(force);
}

export function startSyncNow() {
  return NetworkFacade.StartSyncNow();
}
