import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { OfflinePlaylistPage } from "./page";

export const Route = createFileRoute("/playlists_/offline")({
  component: OfflinePlaylistPage,
  loader: async ({ context }) => {
    await withActiveLibraryRoute(() =>
      Promise.all([
        context.catalog.ensureOfflineRoute(),
        context.catalog.ensurePlaylistsRoute(),
      ]).then(() => undefined),
    );
  },
  staleTime: 0,
});
