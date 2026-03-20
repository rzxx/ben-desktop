import { createFileRoute } from "@tanstack/react-router";
import { redirectToStartupRoute } from "@/app/router/library-routing";

export const Route = createFileRoute("/")({
  beforeLoad: () => redirectToStartupRoute(),
});
