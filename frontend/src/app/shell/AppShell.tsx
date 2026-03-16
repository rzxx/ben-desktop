import type { PropsWithChildren } from "react";
import { useEffect, useRef } from "react";
import { PlaybackLoadingPanel } from "@/components/playback/PlaybackLoadingPanel";
import { PlayerBar } from "@/components/playback/PlayerBar";
import { QueueSidebar } from "@/components/playback/QueueSidebar";
import { ThemeRuntime } from "@/components/theme/ThemeRuntime";
import { NavigationSidebar } from "@/components/shell/NavigationSidebar";
import { TitleBar } from "@/components/shell/TitleBar";
import { usePlaybackStore } from "@/stores/playback/store";

const PLAYER_SCROLL_BUFFER_REM = 1;

export function AppShell({ children }: PropsWithChildren) {
  const bootstrap = usePlaybackStore((state) => state.bootstrap);
  const teardown = usePlaybackStore((state) => state.teardown);
  const shellRef = useRef<HTMLDivElement | null>(null);
  const playerBarRef = useRef<HTMLDivElement | null>(null);
  const queueSidebarRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    void bootstrap();
    return () => {
      teardown();
    };
  }, [bootstrap, teardown]);

  useEffect(() => {
    const shell = shellRef.current;
    const playerBar = playerBarRef.current;
    if (!shell || !playerBar) {
      return;
    }

    const syncShellLayout = () => {
      const playerBarRect = playerBar.getBoundingClientRect();
      const rootFontSize = Number.parseFloat(
        window.getComputedStyle(document.documentElement).fontSize,
      );
      const playerScrollBuffer =
        (Number.isFinite(rootFontSize) ? rootFontSize : 16) *
        PLAYER_SCROLL_BUFFER_REM;
      const clearance = Math.max(
        0,
        Math.round(window.innerHeight - playerBarRect.top + playerScrollBuffer),
      );
      shell.style.setProperty("--shell-player-clearance", `${clearance}px`);

      const queueSidebar = queueSidebarRef.current;
      if (!queueSidebar) {
        shell.style.setProperty("--shell-queue-scroll-clearance", "0px");
        return;
      }

      const queueSidebarRect = queueSidebar.getBoundingClientRect();
      const overlapWidth = Math.max(
        0,
        Math.min(playerBarRect.right, queueSidebarRect.right) -
          Math.max(playerBarRect.left, queueSidebarRect.left),
      );
      const overlapHeight = Math.max(
        0,
        Math.min(playerBarRect.bottom, queueSidebarRect.bottom) -
          Math.max(playerBarRect.top, queueSidebarRect.top),
      );
      const queueScrollClearance =
        overlapWidth > 0 ? Math.round(overlapHeight + playerScrollBuffer) : 0;
      shell.style.setProperty(
        "--shell-queue-scroll-clearance",
        `${queueScrollClearance}px`,
      );
    };

    const observer = new ResizeObserver(() => {
      syncShellLayout();
    });

    observer.observe(playerBar);
    const queueSidebar = queueSidebarRef.current;
    if (queueSidebar) {
      observer.observe(queueSidebar);
    }

    window.addEventListener("resize", syncShellLayout);
    syncShellLayout();

    return () => {
      observer.disconnect();
      window.removeEventListener("resize", syncShellLayout);
      shell.style.removeProperty("--shell-player-clearance");
      shell.style.removeProperty("--shell-queue-scroll-clearance");
    };
  }, []);

  return (
    <div className="text-theme-100 h-screen overflow-hidden" ref={shellRef}>
      <div className="bg-theme-950 fixed -z-100 h-screen w-screen"></div>
      <ThemeRuntime />
      <TitleBar />
      <div className="pointer-events-none fixed inset-x-0 top-12 right-4 z-40 flex justify-end">
        <PlaybackLoadingPanel className="pointer-events-auto w-full max-w-md" />
      </div>

      <NavigationSidebar />

      <div
        className="fixed top-8 right-0 bottom-0 z-20 hidden w-80 max-xl:hidden xl:block"
        ref={queueSidebarRef}
      >
        <QueueSidebar />
      </div>

      <main className="fixed top-8 right-0 bottom-0 left-0 z-10 lg:left-56 xl:right-80">
        <div className="h-full px-6 pt-4 max-lg:px-4">
          <div className="h-full">{children}</div>
        </div>
      </main>

      <div className="pointer-events-none fixed inset-x-0 bottom-4 z-40 flex justify-center px-4">
        <div
          className="pointer-events-auto w-full max-w-[min(72rem,calc(100vw-19rem))] max-lg:max-w-none"
          ref={playerBarRef}
        >
          <PlayerBar />
        </div>
      </div>
    </div>
  );
}
