import type { PlaylistListItem } from "@/lib/api/models";

export function reservedPlaylistAlias(kind: string) {
  switch (kind) {
    case "liked":
      return "liked";
    case "offline":
      return "offline";
    default:
      return null;
  }
}

export function playlistEntityKey(
  playlist: Pick<PlaylistListItem, "Kind" | "PlaylistID">,
) {
  return reservedPlaylistAlias(playlist.Kind) ?? playlist.PlaylistID;
}

export function reservedPlaylistRoute(kind: string) {
  switch (kind) {
    case "liked":
      return "/playlists/liked";
    case "offline":
      return "/playlists/offline";
    default:
      return null;
  }
}

export function isReservedPlaylistId(playlistId: string) {
  return playlistId.startsWith("liked-") || playlistId.startsWith("offline-");
}
