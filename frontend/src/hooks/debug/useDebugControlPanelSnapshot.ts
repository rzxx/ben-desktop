import { useSyncExternalStore } from "react";
import {
  getDebugControlPanelSnapshot,
  subscribeDebugControlPanel,
} from "@/lib/debug/controlPanel";

export function useDebugControlPanelSnapshot() {
  return useSyncExternalStore(
    subscribeDebugControlPanel,
    getDebugControlPanelSnapshot,
    getDebugControlPanelSnapshot,
  );
}
