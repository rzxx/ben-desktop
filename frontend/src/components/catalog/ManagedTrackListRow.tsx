import { TrackListRow } from "@/components/catalog/TrackListRow";
import { useRecordingLikeState } from "@/hooks/catalog/useRecordingLikeState";

export function ManagedTrackListRow({
  availabilityState,
  durationMs,
  indexLabel,
  initialLiked,
  libraryRecordingId,
  mode = "list",
  onPlay,
  onQueue,
  onRemove,
  recordingId,
  removeLabel,
  subtitle,
  title,
}: {
  availabilityState?: string;
  durationMs: number;
  indexLabel: string;
  initialLiked?: boolean;
  libraryRecordingId?: string;
  mode?: "album" | "list";
  onPlay: () => void;
  onQueue: () => void;
  onRemove?: () => void;
  recordingId?: string;
  removeLabel?: string;
  subtitle: string;
  title: string;
}) {
  const likeState = useRecordingLikeState({
    initialLiked,
    libraryRecordingId,
    recordingId,
  });

  return (
    <TrackListRow
      availabilityState={availabilityState}
      durationMs={durationMs}
      indexLabel={indexLabel}
      isLiked={likeState.liked}
      likeBusy={likeState.inFlight}
      mode={mode}
      onPlay={onPlay}
      onQueue={onQueue}
      onRemove={onRemove}
      onToggleLike={
        likeState.hasIdentity
          ? () => {
              void likeState.toggleLike().catch(() => {});
            }
          : undefined
      }
      removeLabel={removeLabel}
      recording={
        likeState.hasIdentity
          ? {
              libraryRecordingId,
              recordingId,
            }
          : undefined
      }
      subtitle={subtitle}
      title={title}
    />
  );
}
