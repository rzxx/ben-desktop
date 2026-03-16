import { Outlet } from "@tanstack/react-router";
import { AppShell } from "./AppShell";

export function RootNotFoundView() {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="rounded-md border border-zinc-800 bg-zinc-950 px-6 py-5 text-sm text-zinc-300">
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
