import { useState } from "react";
import { AddToPlaylistDialog } from "@/components/catalog/PlaylistDialogs";
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
  const [addOpen, setAddOpen] = useState(false);
  const likeState = useRecordingLikeState({
    initialLiked,
    libraryRecordingId,
    recordingId,
  });

  return (
    <>
      <TrackListRow
        availabilityState={availabilityState}
        durationMs={durationMs}
        indexLabel={indexLabel}
        isLiked={likeState.liked}
        likeBusy={likeState.inFlight}
        mode={mode}
        onAddToPlaylist={
          likeState.hasIdentity
            ? () => {
                setAddOpen(true);
              }
            : undefined
        }
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
        subtitle={subtitle}
        title={title}
      />
      <AddToPlaylistDialog
        onClose={() => {
          setAddOpen(false);
        }}
        open={addOpen}
        recording={{
          libraryRecordingId,
          recordingId,
        }}
        title={title}
      />
    </>
  );
}
