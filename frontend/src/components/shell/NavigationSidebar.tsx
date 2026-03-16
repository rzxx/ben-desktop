import { Link, useLocation } from "@tanstack/react-router";
import {
  Activity,
  Disc3,
  FolderCog,
  HardDrive,
  KeyRound,
  LibraryBig,
  Music4,
  UsersRound,
} from "lucide-react";

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

export function NavigationSidebar() {
  const location = useLocation({
    select: (current) => current.pathname,
  });

  return (
    <aside className="flex h-full flex-col gap-4 rounded-lg border border-zinc-800 bg-zinc-950 p-4">
      <div>
        <p className="text-xs tracking-wide text-zinc-500 uppercase">Browse</p>
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
