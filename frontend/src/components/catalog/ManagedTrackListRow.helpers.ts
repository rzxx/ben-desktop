import type { SessionItem } from "@/lib/api/models";

export function isTrackListRowActive(
  playbackItem: SessionItem | null | undefined,
  {
    libraryRecordingId,
    recordingId,
  }: {
    libraryRecordingId?: string;
    recordingId?: string;
  },
) {
  if (!playbackItem) {
    return false;
  }

  if (
    libraryRecordingId &&
    playbackItem.libraryRecordingId &&
    playbackItem.libraryRecordingId === libraryRecordingId
  ) {
    return true;
  }

  return Boolean(recordingId && playbackItem.recordingId === recordingId);
}
