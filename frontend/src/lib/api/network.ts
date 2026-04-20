import * as NetworkFacade from "../../../bindings/ben/desktop/networkfacade";

export function getLocalContext() {
  return NetworkFacade.EnsureLocalContext();
}

export function getActivityStatus() {
  return NetworkFacade.ActivityStatus();
}

export function getNetworkStatus() {
  return NetworkFacade.NetworkStatus();
}

export function getNetworkDebugDump() {
  return NetworkFacade.GetNetworkDebugDump();
}

export function getNetworkTraceEnabled() {
  return NetworkFacade.GetNetworkTraceEnabled();
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

export function getNetworkDebugTrace() {
  return NetworkFacade.GetNetworkDebugTrace();
}

export function clearNetworkDebugTrace() {
  return NetworkFacade.ClearNetworkDebugTrace();
}

export function setNetworkTraceEnabled(enabled: boolean) {
  return NetworkFacade.SetNetworkTraceEnabled(enabled);
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
