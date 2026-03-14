import { createFileRoute } from "@tanstack/react-router";
import { PlaylistDetailPage } from "./page";

export const Route = createFileRoute("/playlists_/$playlistId")({
  component: PlaylistDetailPage,
  loader: ({ context, params }) =>
    context.catalog.ensurePlaylistRoute(params.playlistId),
  staleTime: 0,
});
