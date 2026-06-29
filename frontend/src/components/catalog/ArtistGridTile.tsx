import { Link } from "@tanstack/react-router";
import type { ArtistListItem } from "@/lib/api/models";
import { artistLetter, formatCount } from "@/lib/format";

export function ArtistGridTile({ artist }: { artist: ArtistListItem }) {
  return (
    <Link
      className="group border-theme-300/70 shadow-theme-900/6 hover:border-theme-400/70 hover:bg-theme-50 block rounded-xl border bg-white/78 p-4 text-left shadow-sm transition dark:border-white/6 dark:bg-white/[0.035] dark:shadow-none dark:hover:border-white/14 dark:hover:bg-white/5.5"
      params={{ artistId: artist.ArtistID }}
      to="/artists/$artistId"
    >
      <div className="text-theme-900 border-theme-300/75 shadow-theme-900/8 dark:text-theme-100 mb-4 flex h-20 w-20 items-center justify-center rounded-full border bg-white/82 text-2xl font-semibold shadow-sm dark:border-white/10 dark:bg-white/[0.06] dark:shadow-none">
        {artistLetter(artist.Name)}
      </div>
      <p className="text-theme-900 dark:text-theme-100 line-clamp-1 text-sm font-medium">
        {artist.Name}
      </p>
      <p className="text-theme-500 mt-1 line-clamp-2 text-xs leading-5">
        {formatCount(artist.AlbumCount, "album")} •{" "}
        {formatCount(artist.TrackCount, "track")}
      </p>
    </Link>
  );
}
