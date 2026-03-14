import { createFileRoute } from "@tanstack/react-router";
import { ArtistsPage } from "./page";

export const Route = createFileRoute("/artists")({
  component: ArtistsPage,
  loader: ({ context }) => context.catalog.ensureArtistsRoute(),
  staleTime: 0,
});
