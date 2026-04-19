import { useSyncExternalStore } from "react";
import {
  getPlaybackDebugSnapshot,
  subscribePlaybackDebug,
} from "@/lib/playback/debugTrace";

export function usePlaybackDebugSnapshot() {
  return useSyncExternalStore(
    subscribePlaybackDebug,
    getPlaybackDebugSnapshot,
    getPlaybackDebugSnapshot,
  );
}
