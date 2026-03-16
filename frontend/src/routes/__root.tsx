import { createRootRouteWithContext } from "@tanstack/react-router";
import type { AppRouterContext } from "../app/router/context";
import { RootNotFoundView, RootRouteView } from "@/app/shell/RootRouteView";

export const Route = createRootRouteWithContext<AppRouterContext>()({
  component: RootRouteView,
  notFoundComponent: RootNotFoundView,
});
