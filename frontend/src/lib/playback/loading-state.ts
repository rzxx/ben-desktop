import type { PlaybackPreparationStatus } from "@/lib/api";

export function playbackLoadingLabel(
  status?: PlaybackPreparationStatus | null,
) {
  switch (status?.phase) {
    case "preparing_fetch":
      return "Downloading";
    case "preparing_transcode":
      return "Transcoding";
    case "ready":
      return "Switching";
    case "failed":
      return "Failed";
    default:
      return "Loading";
  }
}

export function playbackLoadingDescription(
  status?: PlaybackPreparationStatus | null,
) {
  switch (status?.phase) {
    case "preparing_fetch":
      return "Fetching audio from another device.";
    case "preparing_transcode":
      return "Preparing a playable file.";
    case "ready":
      return "Sending the track into the player.";
    case "failed":
      return "Playback preparation failed.";
    default:
      return "Waiting for playback to become ready.";
  }
}

