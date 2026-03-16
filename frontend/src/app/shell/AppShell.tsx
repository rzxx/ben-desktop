import type { PropsWithChildren } from "react";
import { useEffect, useState } from "react";
import { Window, Events } from "@wailsio/runtime";
import {
  Activity,
  Album,
  Disc3,
  FolderCog,
  HardDrive,
  KeyRound,
  LibraryBig,
  Minus,
  Music4,
  Square,
  UsersRound,
  X,
} from "lucide-react";
import { Link, useLocation } from "@tanstack/react-router";
import { PlaybackLoadingPanel } from "@/components/playback/PlaybackLoadingPanel";
import { PlayerBar } from "@/components/playback/PlayerBar";
import { QueueSidebar } from "@/components/playback/QueueSidebar";
import { IconButton } from "@/components/ui/Button";
import { usePlaybackStore } from "@/stores/playback/store";

const navItems = [
  { href: "/libraries", icon: FolderCog, label: "Libraries" },
  { href: "/albums", icon: Disc3, label: "Albums" },
  { href: "/artists", icon: UsersRound, label: "Artists" },
  { href: "/sharing", icon: KeyRound, label: "Sharing" },
  { href: "/cache", icon: HardDrive, label: "Cache" },
  { href: "/operations", icon: Activity, label: "Operations" },
  { href: "/tracks", icon: Music4, label: "Tracks" },
  { href: "/playlists", icon: LibraryBig, label: "Playlists" },
];

function TitleBar() {
  const [isMaximised, setIsMaximised] = useState(false);

  useEffect(() => {
    void Window.IsMaximised().then(setIsMaximised);

    const offMaximise = Events.On("common:WindowMaximise", () => {
      setIsMaximised(true);
    });
    const offRestore = Events.On("common:WindowRestore", () => {
      setIsMaximised(false);
    });
    const offUnMaximise = Events.On("common:WindowUnMaximise", () => {
      setIsMaximised(false);
    });

    return () => {
      offMaximise();
      offRestore();
      offUnMaximise();
    };
  }, []);

  return (
    <header
      className="fixed inset-x-0 top-0 z-50 flex h-14 items-center justify-between border-b border-zinc-800 bg-zinc-950 px-4"
      style={{ ["--wails-draggable" as string]: "drag" }}
    >
      <div className="flex min-w-0 items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-md border border-zinc-800 bg-zinc-900 text-zinc-100">
          <Album className="h-4 w-4" />
        </div>
        <div className="min-w-0">
          <p className="truncate text-[11px] uppercase tracking-wide text-zinc-500">
            Desktop package
          </p>
          <p className="truncate text-sm font-medium text-zinc-100">ben</p>
        </div>
      </div>

      <div className="no-drag flex items-center gap-2">
        <IconButton
          className="no-drag"
          label="Minimise window"
          onClick={() => {
            void Window.Minimise();
          }}
        >
          <Minus className="h-4 w-4" />
        </IconButton>
        <IconButton
          className="no-drag"
          label={isMaximised ? "Restore window" : "Maximise window"}
          onClick={() => {
            void Window.ToggleMaximise();
          }}
        >
          <Square className="h-3.5 w-3.5" />
        </IconButton>
        <IconButton
          className="no-drag border-red-500/30 bg-red-500/10 text-red-100 hover:border-red-400/40 hover:bg-red-500/15"
          label="Close window"
          onClick={() => {
            void Window.Close();
          }}
        >
          <X className="h-4 w-4" />
        </IconButton>
      </div>
    </header>
  );
}

function NavigationSidebar() {
  const location = useLocation({
    select: (current) => current.pathname,
  });

  return (
    <aside className="flex h-full flex-col gap-4 rounded-lg border border-zinc-800 bg-zinc-950 p-4">
      <div>
        <p className="text-xs uppercase tracking-wide text-zinc-500">Browse</p>
        <h2 className="mt-2 text-lg font-semibold text-zinc-100">Library</h2>
      </div>
      <nav className="space-y-2">
        {navItems.map((item) => {
          const Icon = item.icon;
          const active =
            location === item.href || location.startsWith(`${item.href}/`);

          return (
            <Link
              className={[
                "flex items-center gap-3 rounded-md border px-3 py-2 text-sm transition",
                active
                  ? "border-zinc-600 bg-zinc-800 text-zinc-50"
                  : "border-transparent text-zinc-400 hover:border-zinc-800 hover:bg-zinc-900 hover:text-zinc-100",
              ].join(" ")}
              key={item.href}
              to={item.href}
            >
              <Icon className="h-4 w-4" />
              <span>{item.label}</span>
            </Link>
          );
        })}
      </nav>
      <div className="mt-auto rounded-md border border-zinc-800 bg-zinc-900 p-3 text-sm text-zinc-400">
        Wails runtime shell with route content and playback controls.
      </div>
    </aside>
  );
}

export function AppShell({ children }: PropsWithChildren) {
  const bootstrap = usePlaybackStore((state) => state.bootstrap);
  const teardown = usePlaybackStore((state) => state.teardown);

  useEffect(() => {
    void bootstrap();
    return () => {
      teardown();
    };
  }, [bootstrap, teardown]);

  return (
    <div className="h-screen overflow-hidden bg-zinc-950 text-zinc-100">
      <TitleBar />
      <div className="pointer-events-none fixed inset-x-0 top-16 z-40 flex justify-end px-4">
        <PlaybackLoadingPanel className="pointer-events-auto w-full max-w-md" />
      </div>
      <div className="absolute inset-x-0 top-14 bottom-24 grid grid-cols-[220px_minmax(0,1fr)_320px] gap-4 px-4 py-4 max-xl:grid-cols-[220px_minmax(0,1fr)] max-lg:grid-cols-1 max-lg:overflow-y-auto">
        <NavigationSidebar />
        <main className="min-h-0 overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950 p-4">
          {children}
        </main>
        <div className="hidden min-h-0 xl:block">
          <QueueSidebar />
        </div>
      </div>
      <PlayerBar />
    </div>
  );
}
