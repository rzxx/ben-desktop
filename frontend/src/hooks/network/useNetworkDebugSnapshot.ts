import { useSyncExternalStore } from "react";
import {
  getNetworkDebugSnapshot,
  subscribeNetworkDebug,
} from "@/lib/network/debugTrace";

export function useNetworkDebugSnapshot() {
  return useSyncExternalStore(
    subscribeNetworkDebug,
    getNetworkDebugSnapshot,
    getNetworkDebugSnapshot,
  );
}
