import type { AlbumListItem, ArtworkRef } from "@/lib/api/models";
import { joinArtists } from "@/lib/format";

export const EMPTY_THUMB: ArtworkRef = {
  BlobID: "",
  Bytes: 0,
  FileExt: "",
  Height: 0,
  MIME: "",
  Variant: "",
  Width: 0,
};

export function buildAlbumSubtitle(album: AlbumListItem) {
  const artists = joinArtists(album.Artists);
  const year = album.Year ? ` • ${album.Year}` : "";
  return `${artists}${year}`;
}

