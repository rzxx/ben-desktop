import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { ArtistsPage } from "./page";

export const Route = createFileRoute("/artists")({
  component: ArtistsPage,
  loader: async ({ context }) => {
    await withActiveLibraryRoute(() => context.catalog.ensureArtistsRoute());
  },
  staleTime: 0,
});
