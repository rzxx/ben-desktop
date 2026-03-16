import { Link } from "@tanstack/react-router";
import type { AlbumListItem } from "@/lib/api/models";
import {
  aggregateAvailabilityLabel,
  formatCount,
  joinArtists,
} from "@/lib/format";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { ArtworkTile } from "@/components/ui/ArtworkTile";

export function AlbumGridTile({ album }: { album: AlbumListItem }) {
  const artworkUrl = useThumbnailUrl(album.Thumb);

  return (
    <Link
      className="group block text-left"
      params={{ albumId: album.AlbumID }}
      to="/albums/$albumId"
    >
      <ArtworkTile
        alt={`${album.Title} cover`}
        className="mb-2 w-full rounded-lg border-black/10"
        src={artworkUrl}
        title={album.Title}
      />
      <p className="text-theme-100 line-clamp-1 text-base font-medium">
        {album.Title}
      </p>
      <p className="text-theme-500 line-clamp-1 text-xs">
        {joinArtists(album.Artists)}
      </p>
      <p className="text-theme-500 mt-1 line-clamp-1 text-xs">
        {formatCount(album.TrackCount, "track")} •{" "}
        {aggregateAvailabilityLabel(album.Availability)}
      </p>
    </Link>
  );
}
