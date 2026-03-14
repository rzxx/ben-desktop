import { RouterProvider } from "@tanstack/react-router";
import { router } from "./router-instance";

export function AppRouterProvider() {
  return <RouterProvider router={router} />;
}
