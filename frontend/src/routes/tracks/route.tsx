import { createFileRoute } from "@tanstack/react-router";
import { TracksPage } from "./page";

export const Route = createFileRoute("/tracks")({
  component: TracksPage,
  loader: ({ context }) => context.catalog.ensureTracksRoute(),
  staleTime: 0,
});
