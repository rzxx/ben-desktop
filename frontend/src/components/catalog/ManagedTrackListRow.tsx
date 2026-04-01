import { TrackListRow } from "@/components/catalog/TrackListRow";
import { isTrackListRowActive } from "@/components/catalog/ManagedTrackListRow.helpers";
import { useRecordingLikeState } from "@/hooks/catalog/useRecordingLikeState";
import { usePlaybackStore } from "@/stores/playback/store";

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
  pinned = false,
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
  pinned?: boolean;
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
  const playbackItem = usePlaybackStore(
    (state) =>
      state.snapshot?.currentItem ?? state.snapshot?.loadingItem ?? null,
  );
  const isActive = isTrackListRowActive(playbackItem, {
    libraryRecordingId,
    recordingId,
  });

  return (
    <TrackListRow
      availabilityState={availabilityState}
      durationMs={durationMs}
      indexLabel={indexLabel}
      isActive={isActive}
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
      pinned={pinned}
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
