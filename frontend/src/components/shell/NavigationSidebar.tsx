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
    <aside className="border-theme-300/15 fixed top-8 bottom-0 left-0 z-20 flex w-56 flex-col border-r px-4 pt-4 max-lg:hidden dark:border-white/5">
      <nav className="flex min-h-0 flex-1 flex-col gap-1.5">
        {navItems.map((item) => {
          const Icon = item.icon;
          const active =
            location === item.href || location.startsWith(`${item.href}/`);

          return (
            <Link
              className={[
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition",
                active
                  ? "bg-theme-900 text-theme-50 dark:bg-theme-100 dark:text-theme-900"
                  : "text-theme-700 hover:bg-theme-100 hover:text-theme-950 dark:text-theme-300 dark:hover:text-theme-100 dark:hover:bg-white/6",
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
    </aside>
  );
}
