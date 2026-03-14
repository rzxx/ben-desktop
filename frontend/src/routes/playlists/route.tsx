import { createFileRoute } from "@tanstack/react-router";
import { PlaylistsPage } from "./page";

export const Route = createFileRoute("/playlists")({
  component: PlaylistsPage,
  loader: ({ context }) => context.catalog.ensurePlaylistsRoute(),
  staleTime: 0,
});
