import { resolveRecordingArtworkURL } from "@/lib/api/playback";
import { useResolvedUrl } from "./useResolvedUrl";

function recordingArtworkKey(recordingId?: string | null) {
  const trimmed = recordingId?.trim() ?? "";
  if (!trimmed) {
    return "";
  }
  return `recording-artwork:${trimmed}:96_jpeg`;
}

export function useRecordingArtworkUrl(recordingId?: string | null) {
  const trimmed = recordingId?.trim() ?? "";
  return useResolvedUrl(
    recordingArtworkKey(trimmed),
    trimmed ? () => resolveRecordingArtworkURL(trimmed, "96_jpeg") : undefined,
  );
}
