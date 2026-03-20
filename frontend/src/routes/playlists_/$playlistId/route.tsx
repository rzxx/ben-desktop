import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { PlaylistDetailPage } from "./page";

export const Route = createFileRoute("/playlists_/$playlistId")({
  component: PlaylistDetailPage,
  loader: async ({ context, params }) => {
    await withActiveLibraryRoute(() =>
      context.catalog.ensurePlaylistRoute(params.playlistId),
    );
  },
  staleTime: 0,
});
