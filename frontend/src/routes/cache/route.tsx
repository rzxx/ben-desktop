import { createFileRoute } from "@tanstack/react-router";
import { CachePage } from "./page";

export const Route = createFileRoute("/cache")({
  component: CachePage,
});
