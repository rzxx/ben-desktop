import { createHashHistory, createRouter } from "@tanstack/react-router";
import { catalogLoaderClient } from "../../features/library/catalog-loader-client";
import { routeTree } from "../../routeTree.gen";
import { PendingRouteView } from "./PendingRouteView";

export const router = createRouter({
  context: {
    catalog: catalogLoaderClient,
  },
  defaultPendingComponent: PendingRouteView,
  history: createHashHistory(),
  routeTree,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
