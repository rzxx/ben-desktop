import { createFileRoute } from "@tanstack/react-router";
import { SharingPage } from "./page";

export const Route = createFileRoute("/sharing")({
  component: SharingPage,
});
