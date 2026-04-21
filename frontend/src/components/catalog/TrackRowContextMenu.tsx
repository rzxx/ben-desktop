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
import type { PinState, PinSubjectKind } from "@/lib/api/models";
import { startPin, unpin } from "@/lib/api/pin";
import { formatCount } from "@/lib/format";

export type TrackRowRecordingIdentity = {
  libraryRecordingId?: string | null;
  pinSubjectKind?: PinSubjectKind | null;
  pinRecordingId?: string | null;
  recordingId?: string | null;
};

type PinActionState = {
  busy: boolean;
  error: string;
  job: JobSnapshot | null;
  targetId: string;
};

const menuPopupClass =
  "border-theme-300/75 bg-white/94 min-w-60 rounded-xl border p-1.5 shadow-2xl shadow-theme-900/14 backdrop-blur-xl outline-none dark:border-theme-500/15 dark:bg-theme-900 dark:shadow-black/40";
const menuItemClass =
  "text-theme-900 flex min-w-0 items-center gap-3 rounded-lg px-3 py-2 text-sm outline-none transition data-[disabled]:pointer-events-none data-[disabled]:opacity-45 data-[highlighted]:bg-theme-100 data-[highlighted]:text-theme-950 dark:text-theme-100 dark:data-[highlighted]:bg-white/[0.08] dark:data-[highlighted]:text-white";
const menuSeparatorClass = "mx-1 my-1 h-px bg-theme-200 dark:bg-white/8";

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
  pinState = null,
  recording,
  title,
}: {
  actionable: boolean;
  children: ReactNode | ((state: { open: boolean }) => ReactNode);
  onQueue: () => void;
  pinState?: PinState | null;
  recording?: TrackRowRecordingIdentity;
  title: string;
}) {
  const [createOpen, setCreateOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [menuError, setMenuError] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [pendingPlaylistId, setPendingPlaylistId] = useState("");
  const [pinActionState, setPinActionState] = useState<PinActionState>({
    busy: false,
    error: "",
    job: null,
    targetId: "",
  });
  const [playlists, setPlaylists] = useState<PlaylistListItem[]>([]);
  const attemptedLoadRef = useRef(false);
  const loadedPlaylistsRef = useRef(false);
  const canAddToPlaylist = hasPlaylistIdentity(recording);
  const pinDirect = Boolean(pinState?.Direct);
  const pinCovered = Boolean(pinState?.Covered);
  const pinTargetId =
    recording?.pinRecordingId?.trim() ||
    recording?.libraryRecordingId?.trim() ||
    recording?.recordingId?.trim() ||
    "";
  const pinSubjectKind =
    recording?.pinSubjectKind ?? ("recording_cluster" as PinSubjectKind);
  const currentPinActionState =
    pinActionState.targetId === pinTargetId
      ? pinActionState
      : {
          busy: false,
          error: "",
          job: null,
          targetId: pinTargetId,
        };
  const trackedPinJob = useJobSnapshot(currentPinActionState.job);
  const pinBusy = currentPinActionState.busy || isJobActive(trackedPinJob);

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
      setPlaylists(page.Items.filter((playlist) => !playlist.IsReserved));
      loadedPlaylistsRef.current = true;
    } catch (error) {
      setMenuError(toErrorMessage(error));
    } finally {
      setLoading(false);
    }
  }, [canAddToPlaylist, loading]);

  async function addToPlaylist(playlistId: string) {
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
  }

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

    setPinActionState({
      busy: true,
      error: "",
      job: currentPinActionState.job,
      targetId: pinTargetId,
    });

    try {
      if (pinDirect) {
        await unpin({
          ID: pinTargetId,
          Kind: pinSubjectKind,
        });
        setPinActionState({
          busy: false,
          error: "",
          job: null,
          targetId: pinTargetId,
        });
      } else {
        const job = await startPin({
          ID: pinTargetId,
          Kind: pinSubjectKind,
        });
        setPinActionState({
          busy: false,
          error: "",
          job,
          targetId: pinTargetId,
        });
      }
    } catch (error) {
      setPinActionState({
        busy: false,
        error: toErrorMessage(error),
        job: currentPinActionState.job,
        targetId: pinTargetId,
      });
    }
  }, [currentPinActionState.job, pinDirect, pinSubjectKind, pinTargetId]);

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
                <ListPlus className="text-theme-600 h-4 w-4 shrink-0 dark:text-white/70" />
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
                      <LoaderCircle className="text-theme-500 h-4 w-4 animate-spin dark:text-white/60" />
                    ) : (
                      <Download className="text-theme-600 h-4 w-4 shrink-0 dark:text-white/70" />
                    )}
                    <span className="min-w-0 flex-1 truncate">
                      {pinDirect
                        ? "Unpin track"
                        : pinCovered
                          ? "Pin track directly"
                          : "Pin track"}
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
                      <FolderPlus className="text-theme-600 h-4 w-4 shrink-0 dark:text-white/70" />
                      <span className="min-w-0 flex-1 truncate">
                        Add to playlist
                      </span>
                      <ChevronRight className="text-theme-500 h-4 w-4 shrink-0 dark:text-white/45" />
                    </ContextMenu.SubmenuTrigger>
                    <ContextMenu.Portal>
                      <ContextMenu.Positioner alignOffset={-6} sideOffset={6}>
                        <ContextMenu.Popup className={menuPopupClass}>
                          {loading ? (
                            <ContextMenu.Item
                              className={menuItemClass}
                              disabled
                            >
                              <LoaderCircle className="text-theme-500 h-4 w-4 animate-spin dark:text-white/60" />
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
                                      <LoaderCircle className="text-theme-500 h-4 w-4 animate-spin dark:text-white/60" />
                                    ) : (
                                      <Plus className="text-theme-600 h-4 w-4 shrink-0 dark:text-white/70" />
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
                            <Plus className="text-theme-600 h-4 w-4 shrink-0 dark:text-white/70" />
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

              {currentPinActionState.error ? (
                <>
                  <ContextMenu.Separator className={menuSeparatorClass} />
                  <ContextMenu.Item className={menuItemClass} disabled>
                    <span className="text-red-200">
                      {currentPinActionState.error}
                    </span>
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
