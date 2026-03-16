import { createHashHistory, createRouter } from "@tanstack/react-router";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { routeTree } from "../../routeTree.gen";
import { RouterPendingView } from "./RouterPendingView";

export const router = createRouter({
  context: {
    catalog: catalogLoaderClient,
  },
  defaultPendingComponent: RouterPendingView,
  history: createHashHistory(),
  routeTree,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

