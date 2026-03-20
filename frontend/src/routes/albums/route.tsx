import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { AlbumsPage } from "./page";

export const Route = createFileRoute("/albums")({
  component: AlbumsPage,
  loader: async ({ context }) => {
    await withActiveLibraryRoute(() => context.catalog.ensureAlbumsRoute());
  },
  staleTime: 0,
});
