import { TrackListRow } from "@/components/catalog/TrackListRow";
import { isTrackListRowActive } from "@/components/catalog/ManagedTrackListRow.helpers";
import { Types, type PinState } from "@/lib/api/models";
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
  pinState,
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
  pinState?: PinState | null;
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
      state.transport?.currentEntry?.item ??
      state.transport?.loadingEntry?.item ??
      null,
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
      pinState={pinState}
      removeLabel={removeLabel}
      recording={
        likeState.hasIdentity
          ? {
              libraryRecordingId,
              pinRecordingId:
                mode === "album"
                  ? recordingId
                  : (libraryRecordingId ?? recordingId),
              pinSubjectKind:
                mode === "album"
                  ? Types.PinSubjectKind.PinSubjectRecordingVariant
                  : Types.PinSubjectKind.PinSubjectRecordingCluster,
              recordingId,
            }
          : undefined
      }
      subtitle={subtitle}
      title={title}
    />
  );
}
