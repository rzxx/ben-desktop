import { Outlet } from "@tanstack/react-router";
import { WindowShell } from "../../app/shell/WindowShell";

export function RootNotFoundView() {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="rounded-[1.4rem] border border-white/8 bg-black/15 px-6 py-5 text-sm text-white/60">
        Route not found.
      </div>
    </div>
  );
}

export function RootRouteView() {
  return (
    <WindowShell>
      <Outlet />
    </WindowShell>
  );
}
