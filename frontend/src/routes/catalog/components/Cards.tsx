import { Link } from "@tanstack/react-router";
import { Play, Plus } from "lucide-react";
import type {
  AlbumListItem,
  ArtistListItem,
  PlaylistListItem,
} from "../../../shared/lib/desktop";
import {
  aggregateAvailabilityLabel,
  artistLetter,
  formatCount,
  formatRelativeDate,
  joinArtists,
} from "../../../shared/lib/format";
import { useThumbnailUrl } from "../../../shared/lib/use-thumbnail-url";
import { ArtworkTile } from "../../../shared/ui/ArtworkTile";
import { usePlaybackStore } from "../../../features/playback/store";

export function AlbumCard({ album }: { album: AlbumListItem }) {
  const artworkUrl = useThumbnailUrl(album.Thumb);

  return (
    <Link
      className="album-card group block rounded-[1.6rem] border border-white/8 bg-black/10 p-3 transition duration-200 hover:-translate-y-1 hover:border-white/18 hover:bg-white/8"
      params={{ albumId: album.AlbumID }}
      to="/albums/$albumId"
    >
      <ArtworkTile alt={album.Title} src={artworkUrl} title={album.Title} />
      <div className="mt-4">
        <h2 className="truncate text-base font-semibold text-white">
          {album.Title}
        </h2>
        <p className="mt-1 truncate text-sm text-white/50">
          {joinArtists(album.Artists)}
        </p>
        <div className="mt-3 flex items-center justify-between text-xs tracking-[0.2em] text-white/35 uppercase">
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
      className="artist-card group flex h-full flex-col rounded-[1.6rem] border border-white/8 bg-black/10 p-4 transition duration-200 hover:-translate-y-1 hover:border-white/18 hover:bg-white/8"
      params={{ artistId: artist.ArtistID }}
      to="/artists/$artistId"
    >
      <div className="mb-5 flex h-28 w-28 items-center justify-center rounded-full border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.35),transparent_60%),rgba(255,255,255,0.05)] text-5xl font-semibold text-white/85">
        {artistLetter(artist.Name)}
      </div>
      <h2 className="truncate text-lg font-semibold text-white">
        {artist.Name}
      </h2>
      <p className="mt-2 text-sm text-white/50">
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
    <div className="playlist-row flex items-center gap-4 rounded-[1.35rem] border border-white/8 bg-black/10 px-4 py-3">
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
            <p className="text-[0.7rem] tracking-[0.28em] text-white/35 uppercase">
              Reserved
            </p>
            <h2 className="truncate text-base font-semibold text-white">
              {playlist.Name}
            </h2>
            <p className="truncate text-sm text-white/50">
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
            <p className="text-[0.7rem] tracking-[0.28em] text-white/35 uppercase">
              Playlist
            </p>
            <h2 className="truncate text-base font-semibold text-white">
              {playlist.Name}
            </h2>
            <p className="truncate text-sm text-white/50">
              {formatCount(playlist.ItemCount, "track")} • Updated{" "}
              {formatRelativeDate(playlist.UpdatedAt)}
            </p>
          </div>
        </Link>
      )}

      <div className="flex gap-2">
        <button
          className="row-action"
          onClick={() => {
            if (isLiked) {
              void playLiked();
              return;
            }
            void playPlaylist(playlist.PlaylistID);
          }}
          type="button"
        >
          <Play className="h-4 w-4" />
        </button>
        {!isLiked && (
          <button
            className="row-action"
            onClick={() => {
              void queuePlaylist(playlist.PlaylistID);
            }}
            type="button"
          >
            <Plus className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
}
