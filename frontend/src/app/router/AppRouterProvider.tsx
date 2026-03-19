import { RouterProvider } from "@tanstack/react-router";
import { CatalogEventsRuntime } from "./CatalogEventsRuntime";
import { router } from "./router-instance";

export function AppRouterProvider() {
  return (
    <>
      <CatalogEventsRuntime />
      <RouterProvider router={router} />
    </>
  );
}
