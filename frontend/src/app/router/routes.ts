import type { PlaylistListItem } from "../../shared/lib/desktop";

export const routes = {
  albums: "/albums",
  album: (albumId: string) => `/albums/${albumId}`,
  artists: "/artists",
  artist: (artistId: string) => `/artists/${artistId}`,
  operations: "/operations",
  tracks: "/tracks",
  playlists: "/playlists",
  playlist: (playlistId: string) => `/playlists/${playlistId}`,
  liked: "/playlists/liked",
};

export function playlistRoute(item: PlaylistListItem) {
  return item.Kind === "liked" ? routes.liked : routes.playlist(item.PlaylistID);
}
