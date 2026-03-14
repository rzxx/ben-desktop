import { useEffect, useState } from "react";
import { Window, Events } from "@wailsio/runtime";
import {
  Activity,
  Album,
  ChevronsUpDown,
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
import { Link, useLocation } from "wouter";
import { AppRouter } from "../router/AppRouter";
import { routes } from "../router/routes";
import { QueueSidebar } from "../../features/playback/QueueSidebar";
import { PlayerBar } from "../../features/playback/PlayerBar";
import { usePlaybackStore } from "../../features/playback/store";

const navItems = [
  { href: routes.libraries, label: "Libraries", icon: FolderCog },
  { href: routes.albums, label: "Albums", icon: Disc3 },
  { href: routes.artists, label: "Artists", icon: UsersRound },
  { href: routes.sharing, label: "Sharing", icon: KeyRound },
  { href: routes.cache, label: "Cache", icon: HardDrive },
  { href: routes.operations, label: "Operations", icon: Activity },
  { href: routes.tracks, label: "Tracks", icon: Music4 },
  { href: routes.playlists, label: "Playlists", icon: LibraryBig },
];

function TitleBar() {
  const [isMaximised, setIsMaximised] = useState(false);

  useEffect(() => {
    void Window.IsMaximised().then(setIsMaximised);

    const offMaximise = Events.On("common:WindowMaximise", () =>
      setIsMaximised(true),
    );
    const offRestore = Events.On("common:WindowRestore", () =>
      setIsMaximised(false),
    );
    const offUnMaximise = Events.On("common:WindowUnMaximise", () =>
      setIsMaximised(false),
    );
    return () => {
      offMaximise();
      offRestore();
      offUnMaximise();
    };
  }, []);

  return (
    <header
      className="titlebar fixed inset-x-0 top-0 z-50 flex h-14 items-center justify-between border-b border-white/8 bg-[linear-gradient(180deg,rgba(10,14,24,0.92),rgba(10,14,24,0.72))] px-4 backdrop-blur-xl"
      style={{ ["--wails-draggable" as string]: "drag" }}
    >
      <div className="titlebar__brand flex min-w-0 items-center gap-3">
        <div className="titlebar__glyph flex h-9 w-9 items-center justify-center rounded-2xl border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.6),transparent_60%),rgba(255,255,255,0.05)]">
          <Album className="h-4 w-4 text-white" />
        </div>
        <div className="min-w-0">
          <p className="truncate text-[0.68rem] tracking-[0.34em] text-white/35 uppercase">
            Desktop package
          </p>
          <p className="truncate text-sm font-semibold text-white/90">ben</p>
        </div>
      </div>

      <div className="titlebar__status hidden items-center gap-2 rounded-full border border-white/8 bg-white/5 px-3 py-1.5 text-xs text-white/45 lg:flex">
        <ChevronsUpDown className="h-3.5 w-3.5" />
        Frameless Wails 3 shell
      </div>

      <div className="titlebar__controls no-drag flex items-center gap-2">
        <button
          className="window-control"
          onClick={() => {
            void Window.Minimise();
          }}
          type="button"
        >
          <Minus className="h-4 w-4" />
        </button>
        <button
          className="window-control"
          onClick={() => {
            void Window.ToggleMaximise();
          }}
          type="button"
        >
          {isMaximised ? (
            <ChevronsUpDown className="h-4 w-4" />
          ) : (
            <Square className="h-3.5 w-3.5" />
          )}
        </button>
        <button
          className="window-control is-danger"
          onClick={() => {
            void Window.Close();
          }}
          type="button"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </header>
  );
}

function NavigationSidebar() {
  const [location] = useLocation();

  return (
    <aside className="navigation-sidebar flex h-full flex-col rounded-[1.8rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.06),rgba(255,255,255,0.02))] p-4 shadow-[0_20px_60px_rgba(0,0,0,0.25)]">
      <div className="mb-6">
        <p className="text-[0.7rem] tracking-[0.35em] text-white/35 uppercase">
          Browse
        </p>
        <h2 className="mt-2 text-xl font-semibold text-white">Library</h2>
        <p className="mt-2 text-sm text-white/48">
          Browse the catalog, inspect core operations, and keep playback in the
          same shell.
        </p>
      </div>
      <nav className="space-y-2">
        {navItems.map((item) => {
          const Icon = item.icon;
          const active =
            location === item.href || location.startsWith(`${item.href}/`);
          return (
            <Link
              className={`nav-link ${active ? "is-active" : ""}`}
              href={item.href}
              key={item.href}
            >
              <Icon className="h-4 w-4" />
              <span>{item.label}</span>
            </Link>
          );
        })}
      </nav>
      <div className="mt-auto rounded-[1.45rem] border border-white/10 bg-black/15 p-4">
        <p className="text-[0.68rem] tracking-[0.3em] text-white/35 uppercase">
          Host notes
        </p>
        <p className="mt-2 text-sm text-white/52">
          Sharing, cache, and operations now talk to the desktop-core facades
          directly.
        </p>
      </div>
    </aside>
  );
}

export function WindowShell() {
  const bootstrap = usePlaybackStore((state) => state.bootstrap);
  const teardown = usePlaybackStore((state) => state.teardown);

  useEffect(() => {
    void bootstrap();
    return () => {
      teardown();
    };
  }, [bootstrap, teardown]);

  return (
    <div className="app-shell h-screen overflow-hidden bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.18),transparent_28%),radial-gradient(circle_at_top_right,rgba(34,197,94,0.12),transparent_24%),linear-gradient(180deg,#090b12_0%,#101522_100%)] text-white">
      <TitleBar />
      <div className="shell-grid absolute inset-x-0 top-14 bottom-28 grid grid-cols-[220px_minmax(0,1fr)_320px] gap-4 px-5 py-4 max-xl:grid-cols-[220px_minmax(0,1fr)] max-lg:grid-cols-1 max-lg:overflow-y-auto">
        <NavigationSidebar />
        <main className="route-panel min-h-0 overflow-hidden rounded-[1.9rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.015))] p-4 shadow-[0_28px_70px_rgba(0,0,0,0.28)]">
          <AppRouter />
        </main>
        <div className="max-xl:hidden">
          <QueueSidebar />
        </div>
      </div>
      <PlayerBar />
    </div>
  );
}
