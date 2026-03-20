import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { PlaylistsPage } from "./page";

export const Route = createFileRoute("/playlists")({
  component: PlaylistsPage,
  loader: async ({ context }) => {
    await withActiveLibraryRoute(() => context.catalog.ensurePlaylistsRoute());
  },
  staleTime: 0,
});
