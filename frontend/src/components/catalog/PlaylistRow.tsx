import { Link } from "@tanstack/react-router";
import { Play, Plus } from "lucide-react";
import type { PlaylistListItem } from "@/lib/api/models";
import { formatCount, formatRelativeDate } from "@/lib/format";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { IconButton } from "@/components/ui/Button";
import { usePlaybackStore } from "@/stores/playback/store";

export function PlaylistRow({ playlist }: { playlist: PlaylistListItem }) {
  const artworkUrl = useThumbnailUrl(playlist.Thumb);
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const isLiked = playlist.Kind === "liked";

  const details = (
    <>
      <p className="text-theme-500 text-[11px] tracking-[0.28em] uppercase">
        {isLiked ? "Reserved playlist" : "Playlist"}
      </p>
      <h2 className="text-theme-100 mt-1 truncate text-base font-medium">
        {playlist.Name}
      </h2>
      <p className="text-theme-500 mt-1 truncate text-xs">
        {formatCount(playlist.ItemCount, "track")} • Updated{" "}
        {formatRelativeDate(playlist.UpdatedAt)}
      </p>
    </>
  );

  return (
    <div className="group flex items-center gap-4 rounded-xl border border-white/6 bg-white/[0.035] px-4 py-3 transition hover:border-white/12 hover:bg-white/[0.05]">
      {isLiked ? (
        <Link className="flex min-w-0 flex-1 items-center gap-4" to="/playlists/liked">
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
            label="Queue playlist"
            onClick={() => {
              void queuePlaylist(playlist.PlaylistID);
            }}
          >
            <Plus className="h-4 w-4" />
          </IconButton>
        ) : null}
      </div>
    </div>
  );
}
