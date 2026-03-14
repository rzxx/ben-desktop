import { createFileRoute } from "@tanstack/react-router";
import { AlbumsPage } from "./page";

export const Route = createFileRoute("/albums")({
  component: AlbumsPage,
  loader: ({ context }) => context.catalog.ensureAlbumsRoute(),
  staleTime: 0,
});
