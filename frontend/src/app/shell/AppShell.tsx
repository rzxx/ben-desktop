import type { PropsWithChildren } from "react";
import { useEffect } from "react";
import { PlaybackLoadingPanel } from "@/components/playback/PlaybackLoadingPanel";
import { PlayerBar } from "@/components/playback/PlayerBar";
import { QueueSidebar } from "@/components/playback/QueueSidebar";
import { NavigationSidebar } from "@/components/shell/NavigationSidebar";
import { TitleBar } from "@/components/shell/TitleBar";
import { usePlaybackStore } from "@/stores/playback/store";

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
