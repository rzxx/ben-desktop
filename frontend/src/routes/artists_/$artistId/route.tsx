import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { ArtistDetailPage } from "./page";

export const Route = createFileRoute("/artists_/$artistId")({
  component: ArtistDetailPage,
  loader: async ({ context, params }) => {
    await withActiveLibraryRoute(() =>
      context.catalog.ensureArtistRoute(params.artistId),
    );
  },
  staleTime: 0,
});
