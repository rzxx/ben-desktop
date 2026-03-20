import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { AlbumDetailPage } from "./page";

export const Route = createFileRoute("/albums_/$albumId")({
  component: AlbumDetailPage,
  loader: async ({ context, params }) => {
    await withActiveLibraryRoute(() =>
      context.catalog.ensureAlbumRoute(params.albumId),
    );
  },
  staleTime: 0,
});
