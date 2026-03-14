import { createFileRoute } from "@tanstack/react-router";
import { AlbumDetailPage } from "./page";

export const Route = createFileRoute("/albums_/$albumId")({
  component: AlbumDetailPage,
  loader: ({ context, params }) =>
    context.catalog.ensureAlbumRoute(params.albumId),
  staleTime: 0,
});
