import { ContextMenu } from "@base-ui/react/context-menu";
import {
  ChevronRight,
  Download,
  FolderPlus,
  ListPlus,
  LoaderCircle,
  Plus,
} from "lucide-react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { PlaylistNameDialog } from "@/components/catalog/PlaylistDialogs";
import { isJobActive, useJobSnapshot } from "@/hooks/jobs/useJobSnapshot";
import {
  addPlaylistItem,
  createPlaylist,
  listPlaylistsPage,
} from "@/lib/api/catalog";
import type { JobSnapshot, PlaylistListItem } from "@/lib/api/models";
import {
  startPinRecordingOffline,
  unpinRecordingOffline,
} from "@/lib/api/playback";
import { formatCount } from "@/lib/format";

export type TrackRowRecordingIdentity = {
  libraryRecordingId?: string | null;
  pinRecordingId?: string | null;
  recordingId?: string | null;
};

const menuPopupClass =
  "border-theme-500/15 bg-theme-900 min-w-60 rounded-xl border p-1.5 shadow-2xl shadow-black/40 backdrop-blur-xl outline-none";
const menuItemClass =
  "text-theme-100 flex min-w-0 items-center gap-3 rounded-lg px-3 py-2 text-sm outline-none transition data-[disabled]:pointer-events-none data-[disabled]:opacity-45 data-[highlighted]:bg-white/[0.08] data-[highlighted]:text-white";
const menuSeparatorClass = "mx-1 my-1 h-px bg-white/8";

function toErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function hasPlaylistIdentity(recording?: TrackRowRecordingIdentity) {
  return Boolean(
    recording?.libraryRecordingId?.trim() || recording?.recordingId?.trim(),
  );
}

export function TrackRowContextMenu({
  actionable,
  children,
  onQueue,
  pinned = false,
  recording,
  title,
}: {
  actionable: boolean;
  children: ReactNode | ((state: { open: boolean }) => ReactNode);
  onQueue: () => void;
  pinned?: boolean;
  recording?: TrackRowRecordingIdentity;
  title: string;
}) {
  const [createOpen, setCreateOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [menuError, setMenuError] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [pendingPlaylistId, setPendingPlaylistId] = useState("");
  const [pinActionBusy, setPinActionBusy] = useState(false);
  const [pinError, setPinError] = useState("");
  const [pinJob, setPinJob] = useState<JobSnapshot | null>(null);
  const [playlists, setPlaylists] = useState<PlaylistListItem[]>([]);
  const attemptedLoadRef = useRef(false);
  const loadedPlaylistsRef = useRef(false);
  const canAddToPlaylist = hasPlaylistIdentity(recording);
  const pinTargetId =
    recording?.pinRecordingId?.trim() ||
    recording?.libraryRecordingId?.trim() ||
    recording?.recordingId?.trim() ||
    "";
  const trackedPinJob = useJobSnapshot(pinJob);
  const pinBusy = pinActionBusy || isJobActive(trackedPinJob);

  const ensurePlaylistsLoaded = useCallback(async () => {
    if (
      !canAddToPlaylist ||
      attemptedLoadRef.current ||
      loadedPlaylistsRef.current ||
      loading
    ) {
      return;
    }

    attemptedLoadRef.current = true;
    setLoading(true);
    setMenuError("");

    try {
      const page = await listPlaylistsPage(0, 1000);
      setPlaylists(page.Items.filter((playlist) => playlist.Kind !== "liked"));
      loadedPlaylistsRef.current = true;
    } catch (error) {
      setMenuError(toErrorMessage(error));
    } finally {
      setLoading(false);
    }
  }, [canAddToPlaylist, loading]);

  const addToPlaylist = useCallback(
    async (playlistId: string) => {
      if (!canAddToPlaylist) {
        return;
      }

      setPendingPlaylistId(playlistId);
      setMenuError("");

      try {
        await addPlaylistItem({
          libraryRecordingId: recording?.libraryRecordingId ?? "",
          playlistId,
          recordingId: recording?.recordingId ?? "",
        });
        setMenuOpen(false);
      } catch (error) {
        setMenuError(toErrorMessage(error));
      } finally {
        setPendingPlaylistId("");
      }
    },
    [canAddToPlaylist, recording?.libraryRecordingId, recording?.recordingId],
  );

  useEffect(() => {
    setPinError("");
    setPinJob(null);
  }, [pinTargetId]);

  useEffect(() => {
    if (!menuOpen) {
      attemptedLoadRef.current = false;
      return;
    }

    if (
      !canAddToPlaylist ||
      loadedPlaylistsRef.current ||
      attemptedLoadRef.current ||
      loading
    ) {
      return;
    }

    void ensurePlaylistsLoaded();
  }, [canAddToPlaylist, ensurePlaylistsLoaded, loading, menuOpen]);

  const toggleOfflinePin = useCallback(async () => {
    if (!pinTargetId) {
      return;
    }

    setPinActionBusy(true);
    setPinError("");

    try {
      if (pinned) {
        await unpinRecordingOffline(pinTargetId);
        setPinJob(null);
      } else {
        const job = await startPinRecordingOffline(pinTargetId);
        setPinJob(job);
      }
    } catch (error) {
      setPinError(toErrorMessage(error));
    } finally {
      setPinActionBusy(false);
    }
  }, [pinTargetId, pinned]);

  return (
    <>
      <ContextMenu.Root
        onOpenChange={(nextOpen) => {
          setMenuOpen(nextOpen);
          if (nextOpen && canAddToPlaylist) {
            void ensurePlaylistsLoaded();
          }
        }}
        open={menuOpen}
      >
        <ContextMenu.Trigger className="block">
          {typeof children === "function"
            ? children({ open: menuOpen })
            : children}
        </ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Positioner sideOffset={8}>
            <ContextMenu.Popup className={menuPopupClass}>
              <ContextMenu.Item
                className={menuItemClass}
                disabled={!actionable}
                onClick={() => {
                  onQueue();
                }}
              >
                <ListPlus className="h-4 w-4 shrink-0 text-white/70" />
                <span className="min-w-0 flex-1 truncate">Add to queue</span>
              </ContextMenu.Item>

              {pinTargetId ? (
                <>
                  <ContextMenu.Separator className={menuSeparatorClass} />
                  <ContextMenu.Item
                    className={menuItemClass}
                    closeOnClick={false}
                    disabled={pinBusy}
                    onClick={() => {
                      void toggleOfflinePin();
                    }}
                  >
                    {pinBusy ? (
                      <LoaderCircle className="h-4 w-4 animate-spin text-white/60" />
                    ) : (
                      <Download className="h-4 w-4 shrink-0 text-white/70" />
                    )}
                    <span className="min-w-0 flex-1 truncate">
                      {pinned ? "Unpin track" : "Pin track"}
                    </span>
                  </ContextMenu.Item>
                </>
              ) : null}

              {canAddToPlaylist ? (
                <>
                  <ContextMenu.Separator className={menuSeparatorClass} />
                  <ContextMenu.SubmenuRoot
                    onOpenChange={(nextOpen) => {
                      if (nextOpen) {
                        void ensurePlaylistsLoaded();
                      }
                    }}
                  >
                    <ContextMenu.SubmenuTrigger className={menuItemClass}>
                      <FolderPlus className="h-4 w-4 shrink-0 text-white/70" />
                      <span className="min-w-0 flex-1 truncate">
                        Add to playlist
                      </span>
                      <ChevronRight className="h-4 w-4 shrink-0 text-white/45" />
                    </ContextMenu.SubmenuTrigger>
                    <ContextMenu.Portal>
                      <ContextMenu.Positioner alignOffset={-6} sideOffset={6}>
                        <ContextMenu.Popup className={menuPopupClass}>
                          {loading ? (
                            <ContextMenu.Item
                              className={menuItemClass}
                              disabled
                            >
                              <LoaderCircle className="h-4 w-4 animate-spin text-white/60" />
                              <span>Loading playlists...</span>
                            </ContextMenu.Item>
                          ) : null}

                          {!loading && menuError ? (
                            <ContextMenu.Item
                              className={menuItemClass}
                              disabled
                            >
                              <span className="text-red-200">{menuError}</span>
                            </ContextMenu.Item>
                          ) : null}

                          {!loading && playlists.length > 0
                            ? playlists.map((playlist) => {
                                const busy =
                                  pendingPlaylistId === playlist.PlaylistID;

                                return (
                                  <ContextMenu.Item
                                    className={menuItemClass}
                                    closeOnClick={false}
                                    disabled={Boolean(pendingPlaylistId)}
                                    key={playlist.PlaylistID}
                                    onClick={() => {
                                      void addToPlaylist(playlist.PlaylistID);
                                    }}
                                  >
                                    {busy ? (
                                      <LoaderCircle className="h-4 w-4 animate-spin text-white/60" />
                                    ) : (
                                      <Plus className="h-4 w-4 shrink-0 text-white/70" />
                                    )}
                                    <span className="min-w-0 flex-1">
                                      <span className="block truncate">
                                        {playlist.Name}
                                      </span>
                                      <span className="text-theme-500 block text-[11px]">
                                        {formatCount(
                                          playlist.ItemCount,
                                          "track",
                                        )}
                                      </span>
                                    </span>
                                  </ContextMenu.Item>
                                );
                              })
                            : null}

                          {!loading && playlists.length === 0 && !menuError ? (
                            <ContextMenu.Item
                              className={menuItemClass}
                              disabled
                            >
                              <span>No normal playlists yet.</span>
                            </ContextMenu.Item>
                          ) : null}

                          <ContextMenu.Separator
                            className={menuSeparatorClass}
                          />
                          <ContextMenu.Item
                            className={menuItemClass}
                            onClick={() => {
                              setCreateOpen(true);
                              setMenuOpen(false);
                            }}
                          >
                            <Plus className="h-4 w-4 shrink-0 text-white/70" />
                            <span className="min-w-0 flex-1 truncate">
                              New playlist...
                            </span>
                          </ContextMenu.Item>
                        </ContextMenu.Popup>
                      </ContextMenu.Positioner>
                    </ContextMenu.Portal>
                  </ContextMenu.SubmenuRoot>
                </>
              ) : null}

              {pinError ? (
                <>
                  <ContextMenu.Separator className={menuSeparatorClass} />
                  <ContextMenu.Item className={menuItemClass} disabled>
                    <span className="text-red-200">{pinError}</span>
                  </ContextMenu.Item>
                </>
              ) : null}
            </ContextMenu.Popup>
          </ContextMenu.Positioner>
        </ContextMenu.Portal>
      </ContextMenu.Root>

      <PlaylistNameDialog
        confirmLabel="Create playlist"
        description={`Create a playlist and add "${title}" to it.`}
        onClose={() => {
          setCreateOpen(false);
        }}
        onConfirm={async (name) => {
          const playlist = await createPlaylist(name.trim());
          loadedPlaylistsRef.current = false;
          await addPlaylistItem({
            libraryRecordingId: recording?.libraryRecordingId ?? "",
            playlistId: playlist.PlaylistID,
            recordingId: recording?.recordingId ?? "",
          });
        }}
        open={createOpen}
        title="New playlist"
      />
    </>
  );
}
