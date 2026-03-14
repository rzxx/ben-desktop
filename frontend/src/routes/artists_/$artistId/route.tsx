import { createFileRoute } from "@tanstack/react-router";
import { ArtistDetailPage } from "./page";

export const Route = createFileRoute("/artists_/$artistId")({
  component: ArtistDetailPage,
  loader: ({ context, params }) =>
    context.catalog.ensureArtistRoute(params.artistId),
  staleTime: 0,
});
