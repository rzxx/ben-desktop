import { Outlet } from "@tanstack/react-router";
import { AppShell } from "./AppShell";

export function RootNotFoundView() {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="text-theme-700 border-theme-300 shadow-theme-900/8 rounded-md border bg-white px-6 py-5 text-sm shadow-sm dark:border-zinc-800 dark:bg-zinc-950 dark:text-zinc-300 dark:shadow-none">
        Route not found.
      </div>
    </div>
  );
}

export function RootRouteView() {
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}
