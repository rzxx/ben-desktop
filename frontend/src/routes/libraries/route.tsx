import { createFileRoute } from "@tanstack/react-router";
import { LibrariesPage } from "./page";

export const Route = createFileRoute("/libraries")({
  component: LibrariesPage,
});
