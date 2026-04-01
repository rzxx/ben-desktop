import type { LikedRecordingItem } from "@/lib/api/models";

export function likedPlaybackRecordingId(track: LikedRecordingItem) {
  return track.LibraryRecordingID || track.RecordingID;
}
