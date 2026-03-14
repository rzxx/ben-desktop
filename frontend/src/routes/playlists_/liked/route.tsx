import { createFileRoute } from "@tanstack/react-router";
import { LikedPlaylistPage } from "./page";

export const Route = createFileRoute("/playlists_/liked")({
  component: LikedPlaylistPage,
  loader: ({ context }) => context.catalog.ensureLikedRoute(),
  staleTime: 0,
});
