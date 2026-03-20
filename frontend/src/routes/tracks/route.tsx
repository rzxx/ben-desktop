import { createFileRoute } from "@tanstack/react-router";
import { withActiveLibraryRoute } from "@/app/router/library-routing";
import { TracksPage } from "./page";

export const Route = createFileRoute("/tracks")({
  component: TracksPage,
  loader: async ({ context }) => {
    await withActiveLibraryRoute(() => context.catalog.ensureTracksRoute());
  },
  staleTime: 0,
});
