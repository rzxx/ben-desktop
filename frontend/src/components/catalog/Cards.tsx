import { Link } from "@tanstack/react-router";
import { Play, Plus } from "lucide-react";
import { IconButton } from "@/components/ui/Button";
import type {
  AlbumListItem,
  ArtistListItem,
  PlaylistListItem,
} from "@/lib/api/models";
import {
  aggregateAvailabilityLabel,
  artistLetter,
  formatCount,
  formatRelativeDate,
  joinArtists,
} from "@/lib/format";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { usePlaybackStore } from "@/stores/playback/store";

export function AlbumCard({ album }: { album: AlbumListItem }) {
  const artworkUrl = useThumbnailUrl(album.Thumb);

  return (
    <Link
      className="block rounded-lg border border-zinc-800 bg-zinc-950 p-3 transition hover:border-zinc-700 hover:bg-zinc-900"
      params={{ albumId: album.AlbumID }}
      to="/albums/$albumId"
    >
      <ArtworkTile alt={album.Title} src={artworkUrl} title={album.Title} />
      <div className="mt-4">
        <h2 className="truncate text-base font-semibold text-zinc-100">
          {album.Title}
        </h2>
        <p className="mt-1 truncate text-sm text-zinc-400">
          {joinArtists(album.Artists)}
        </p>
        <div className="mt-3 flex items-center justify-between text-xs tracking-wide text-zinc-500 uppercase">
          <span>{formatCount(album.TrackCount, "track")}</span>
          <span>{aggregateAvailabilityLabel(album.Availability)}</span>
        </div>
      </div>
    </Link>
  );
}

export function ArtistCard({ artist }: { artist: ArtistListItem }) {
  return (
    <Link
      className="flex h-full flex-col rounded-lg border border-zinc-800 bg-zinc-950 p-4 transition hover:border-zinc-700 hover:bg-zinc-900"
      params={{ artistId: artist.ArtistID }}
      to="/artists/$artistId"
    >
      <div className="mb-5 flex h-28 w-28 items-center justify-center rounded-full border border-zinc-800 bg-zinc-900 text-5xl font-semibold text-zinc-100">
        {artistLetter(artist.Name)}
      </div>
      <h2 className="truncate text-lg font-semibold text-zinc-100">
        {artist.Name}
      </h2>
      <p className="mt-2 text-sm text-zinc-400">
        {formatCount(artist.AlbumCount, "album")} •{" "}
        {formatCount(artist.TrackCount, "track")}
      </p>
    </Link>
  );
}

export function PlaylistCard({ playlist }: { playlist: PlaylistListItem }) {
  const artworkUrl = useThumbnailUrl(playlist.Thumb);
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const isLiked = playlist.Kind === "liked";

  return (
    <div className="flex items-center gap-4 rounded-lg border border-zinc-800 bg-zinc-950 px-4 py-3">
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
          <div className="min-w-0">
            <p className="text-xs tracking-wide text-zinc-500 uppercase">
              Reserved
            </p>
            <h2 className="truncate text-base font-semibold text-zinc-100">
              {playlist.Name}
            </h2>
            <p className="truncate text-sm text-zinc-400">
              {formatCount(playlist.ItemCount, "track")} • Updated{" "}
              {formatRelativeDate(playlist.UpdatedAt)}
            </p>
          </div>
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
          <div className="min-w-0">
            <p className="text-xs tracking-wide text-zinc-500 uppercase">
              Playlist
            </p>
            <h2 className="truncate text-base font-semibold text-zinc-100">
              {playlist.Name}
            </h2>
            <p className="truncate text-sm text-zinc-400">
              {formatCount(playlist.ItemCount, "track")} • Updated{" "}
              {formatRelativeDate(playlist.UpdatedAt)}
            </p>
          </div>
        </Link>
      )}

      <div className="flex gap-2">
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
        {!isLiked && (
          <IconButton
            label="Queue playlist"
            onClick={() => {
              void queuePlaylist(playlist.PlaylistID);
            }}
          >
            <Plus className="h-4 w-4" />
          </IconButton>
        )}
      </div>
    </div>
  );
}
