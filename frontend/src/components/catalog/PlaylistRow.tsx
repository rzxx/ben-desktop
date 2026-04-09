import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Pencil, Play, Plus, Trash2 } from "lucide-react";
import type { PinState, PlaylistListItem } from "@/lib/api/models";
import {
  ConfirmPlaylistDeleteDialog,
  PlaylistNameDialog,
} from "@/components/catalog/PlaylistDialogs";
import {
  formatCount,
  formatRelativeDate,
  isTrackCollectionPlayable,
  pinStateLabel,
} from "@/lib/format";
import { deletePlaylist, renamePlaylist } from "@/lib/api/catalog";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { IconButton } from "@/components/ui/Button";
import { usePlaybackStore } from "@/stores/playback/store";

export function PlaylistRow({
  pinState,
  playlist,
}: {
  pinState?: PinState | null;
  playlist: PlaylistListItem;
}) {
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const artworkUrl = useThumbnailUrl(playlist.Thumb);
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const isLiked = playlist.Kind === "liked";
  const canPlayPlaylist = isTrackCollectionPlayable({
    trackCount: playlist.ItemCount,
  });

  const details = (
    <>
      <p className="text-theme-500 text-[11px] tracking-[0.28em] uppercase">
        {isLiked ? "Reserved playlist" : "Playlist"}
      </p>
      <h2 className="text-theme-900 dark:text-theme-100 mt-1 truncate text-base font-medium">
        {playlist.Name}
      </h2>
      <p className="text-theme-500 mt-1 truncate text-xs">
        {formatCount(playlist.ItemCount, "track")} • Updated{" "}
        {formatRelativeDate(playlist.UpdatedAt)}
        {pinStateLabel(pinState) ? ` • ${pinStateLabel(pinState)}` : ""}
      </p>
    </>
  );

  return (
    <div className="group border-theme-300/70 shadow-theme-900/6 hover:border-theme-400/70 hover:bg-theme-50 flex items-center gap-4 rounded-xl border bg-white/78 px-4 py-3 shadow-sm transition dark:border-white/6 dark:bg-white/[0.035] dark:shadow-none dark:hover:border-white/12 dark:hover:bg-white/[0.05]">
      {isLiked ? (
        <Link
          className="flex min-w-0 flex-1 items-center gap-4"
          to="/playlists/liked"
        >
          <ArtworkTile
            alt={playlist.Name}
            className="h-18 w-18 shrink-0"
            src={artworkUrl}
            title={playlist.Name}
          />
          <div className="min-w-0">{details}</div>
        </Link>
      ) : (
        <Link
          className="flex min-w-0 flex-1 items-center gap-4"
          params={{ playlistId: playlist.PlaylistID }}
          to="/playlists/$playlistId"
        >
          <ArtworkTile
            alt={playlist.Name}
            className="h-18 w-18 shrink-0"
            src={artworkUrl}
            title={playlist.Name}
          />
          <div className="min-w-0">{details}</div>
        </Link>
      )}

      <div className="flex shrink-0 gap-2">
        <IconButton
          disabled={!canPlayPlaylist}
          label={isLiked ? "Play liked songs" : "Play playlist"}
          onClick={() => {
            if (isLiked) {
              void playLiked();
              return;
            }
            void playPlaylist(playlist.PlaylistID);
          }}
        >
          <Play className="h-4 w-4" />
        </IconButton>
        {!isLiked ? (
          <IconButton
            disabled={!canPlayPlaylist}
            label="Queue playlist"
            onClick={() => {
              void queuePlaylist(playlist.PlaylistID);
            }}
          >
            <Plus className="h-4 w-4" />
          </IconButton>
        ) : null}
        {!isLiked ? (
          <IconButton
            label="Rename playlist"
            onClick={() => {
              setRenameOpen(true);
            }}
          >
            <Pencil className="h-4 w-4" />
          </IconButton>
        ) : null}
        {!isLiked ? (
          <IconButton
            label="Delete playlist"
            onClick={() => {
              setDeleteOpen(true);
            }}
          >
            <Trash2 className="h-4 w-4" />
          </IconButton>
        ) : null}
      </div>

      <PlaylistNameDialog
        confirmLabel="Save name"
        description="Rename this playlist."
        initialValue={playlist.Name}
        onClose={() => {
          setRenameOpen(false);
        }}
        onConfirm={async (name) => {
          await renamePlaylist(playlist.PlaylistID, name);
        }}
        open={renameOpen}
        title="Rename playlist"
      />
      <ConfirmPlaylistDeleteDialog
        description={`Delete "${playlist.Name}" and remove its custom cover and track order.`}
        onClose={() => {
          setDeleteOpen(false);
        }}
        onConfirm={async () => {
          await deletePlaylist(playlist.PlaylistID);
        }}
        open={deleteOpen}
        title="Delete playlist?"
      />
    </div>
  );
}
