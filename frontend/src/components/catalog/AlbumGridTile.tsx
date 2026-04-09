import { Link } from "@tanstack/react-router";
import type { AlbumListItem, PinState } from "@/lib/api/models";
import {
  formatCount,
  isAlbumUnavailableInCatalog,
  joinArtists,
  pinStateLabel,
} from "@/lib/format";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { ArtworkTile } from "@/components/ui/ArtworkTile";

export function AlbumGridTile({
  album,
  availabilityState,
  availabilityLabel,
  pinState,
  state,
}: {
  album: AlbumListItem;
  availabilityState?: string | null;
  availabilityLabel: string;
  pinState?: PinState | null;
  state?: Record<string, unknown>;
}) {
  const artworkUrl = useThumbnailUrl(album.Thumb);
  const unavailable = isAlbumUnavailableInCatalog({
    State: availabilityState,
  });

  return (
    <Link
      className={[
        "group block text-left transition-opacity",
        unavailable ? "opacity-40" : "",
      ].join(" ")}
      params={{ albumId: album.AlbumID }}
      state={state}
      to="/albums/$albumId"
    >
      <ArtworkTile
        alt={`${album.Title} cover`}
        className={[
          "border-theme-300/70 mb-2 w-full rounded-lg transition-[filter] dark:border-black/10",
          unavailable ? "grayscale" : "",
        ].join(" ")}
        src={artworkUrl}
        title={album.Title}
      />
      <p className="text-theme-900 dark:text-theme-100 line-clamp-1 text-base font-medium">
        {album.Title}
      </p>
      <p className="text-theme-500 line-clamp-1 text-xs">
        {joinArtists(album.Artists)}
      </p>
      <p className="text-theme-500 mt-1 line-clamp-1 text-xs">
        {formatCount(album.TrackCount, "track")} • {availabilityLabel}
        {pinStateLabel(pinState) ? ` • ${pinStateLabel(pinState)}` : ""}
      </p>
    </Link>
  );
}
