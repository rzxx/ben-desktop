import { Link } from "@tanstack/react-router";
import type { ArtistListItem } from "@/lib/api/models";
import { artistLetter, formatCount } from "@/lib/format";

export function ArtistGridTile({ artist }: { artist: ArtistListItem }) {
  return (
    <Link
      className="group block rounded-xl border border-white/6 bg-white/[0.035] p-4 text-left transition hover:border-white/14 hover:bg-white/[0.055]"
      params={{ artistId: artist.ArtistID }}
      to="/artists/$artistId"
    >
      <div className="bg-theme-100 text-theme-900 mb-4 flex h-20 w-20 items-center justify-center rounded-full text-2xl font-semibold shadow-[0_10px_30px_rgba(0,0,0,0.14)]">
        {artistLetter(artist.Name)}
      </div>
      <p className="text-theme-100 line-clamp-1 text-sm font-medium">
        {artist.Name}
      </p>
      <p className="text-theme-500 mt-1 line-clamp-2 text-xs leading-5">
        {formatCount(artist.AlbumCount, "album")} •{" "}
        {formatCount(artist.TrackCount, "track")}
      </p>
    </Link>
  );
}
