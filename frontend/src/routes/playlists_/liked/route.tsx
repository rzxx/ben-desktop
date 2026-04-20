import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { LikedPlaylistPage } from "./page";

export const Route = createFileRoute("/playlists_/liked")({
  component: LikedPlaylistPage,
  loader: async ({ context }) => {
    await withActiveLibraryRoute(() =>
      Promise.all([
        context.catalog.ensureLikedRoute(),
        context.catalog.ensurePlaylistsRoute(),
      ]).then(() => undefined),
    );
  },
  staleTime: 0,
});
