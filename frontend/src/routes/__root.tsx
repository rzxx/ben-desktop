import { createRootRouteWithContext } from "@tanstack/react-router";
import type { AppRouterContext } from "../app/router/context";
import { RootNotFoundView, RootRouteView } from "./components/RootRouteView";

export const Route = createRootRouteWithContext<AppRouterContext>()({
  component: RootRouteView,
  notFoundComponent: RootNotFoundView,
});
