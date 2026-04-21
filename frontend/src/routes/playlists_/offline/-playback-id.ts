import type { OfflineRecordingItem } from "@/lib/api/models";

export function offlinePlaybackRecordingId(track: OfflineRecordingItem) {
  return track.LibraryRecordingID || track.RecordingID;
}
