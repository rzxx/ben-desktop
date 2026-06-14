import * as NetworkFacade from "../../../bindings/ben/desktop/networkfacade";
import { traceWailsCall } from "@/lib/observability/trace";

export function getLocalContext() {
  return traceWailsCall("network", "ensure_local_context", undefined, () =>
    NetworkFacade.EnsureLocalContext(),
  );
}

export function getActivityStatus() {
  return traceWailsCall("network", "activity_status", undefined, () =>
    NetworkFacade.ActivityStatus(),
  );
}

export function getNetworkStatus() {
  return traceWailsCall("network", "network_status", undefined, () =>
    NetworkFacade.NetworkStatus(),
  );
}

export function getCheckpointStatus() {
  return traceWailsCall("network", "checkpoint_status", undefined, () =>
    NetworkFacade.CheckpointStatus(),
  );
}

export function getInspectSummary() {
  return traceWailsCall("network", "inspect", undefined, () =>
    NetworkFacade.Inspect(),
  );
}

export function getLibraryOplogDiagnostics(libraryId = "") {
  return traceWailsCall("network", "inspect_library_oplog", { libraryId }, () =>
    NetworkFacade.InspectLibraryOplog(libraryId),
  );
}

export function startConnectPeer(peerAddr: string) {
  return traceWailsCall("network", "start_connect_peer", { peerAddr }, () =>
    NetworkFacade.StartConnectPeer(peerAddr),
  );
}

export function startPublishCheckpoint() {
  return traceWailsCall("network", "start_publish_checkpoint", undefined, () =>
    NetworkFacade.StartPublishCheckpoint(),
  );
}

export function startCompactCheckpoint(force = false) {
  return traceWailsCall("network", "start_compact_checkpoint", { force }, () =>
    NetworkFacade.StartCompactCheckpoint(force),
  );
}

export function startSyncNow() {
  return traceWailsCall("network", "start_sync_now", undefined, () =>
    NetworkFacade.StartSyncNow(),
  );
}
