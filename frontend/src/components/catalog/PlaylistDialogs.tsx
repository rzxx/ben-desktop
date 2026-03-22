import { useEffect, useState } from "react";
import { LoaderCircle, Plus, Trash2 } from "lucide-react";
import type { PlaylistListItem } from "@/lib/api/models";
import {
  addPlaylistItem,
  createPlaylist,
  listPlaylistsPage,
} from "@/lib/api/catalog";
import { Button } from "@/components/ui/Button";
import { ModalDialog } from "@/components/ui/ModalDialog";

type PlaylistIdentity = {
  libraryRecordingId?: string | null;
  recordingId?: string | null;
};

export function PlaylistNameDialog({
  confirmLabel,
  description,
  initialValue = "",
  onClose,
  onConfirm,
  open,
  title,
}: {
  confirmLabel: string;
  description?: string;
  initialValue?: string;
  onClose: () => void;
  onConfirm: (name: string) => Promise<void>;
  open: boolean;
  title: string;
}) {
  if (!open) {
    return null;
  }

  return (
    <PlaylistNameDialogBody
      confirmLabel={confirmLabel}
      description={description}
      initialValue={initialValue}
      key={`${title}:${initialValue}`}
      onClose={onClose}
      onConfirm={onConfirm}
      title={title}
    />
  );
}

function PlaylistNameDialogBody({
  confirmLabel,
  description,
  initialValue = "",
  onClose,
  onConfirm,
  title,
}: {
  confirmLabel: string;
  description?: string;
  initialValue?: string;
  onClose: () => void;
  onConfirm: (name: string) => Promise<void>;
  title: string;
}) {
  const [name, setName] = useState(initialValue);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  return (
    <ModalDialog
      actions={
        <>
          <Button onClick={onClose} tone="quiet">
            Cancel
          </Button>
          <Button
            disabled={submitting || !name.trim()}
            icon={
              submitting ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : undefined
            }
            onClick={() => {
              setSubmitting(true);
              setError("");
              void onConfirm(name.trim())
                .then(() => {
                  onClose();
                })
                .catch((nextError) => {
                  setError(
                    nextError instanceof Error
                      ? nextError.message
                      : String(nextError),
                  );
                })
                .finally(() => {
                  setSubmitting(false);
                });
            }}
            tone="primary"
          >
            {confirmLabel}
          </Button>
        </>
      }
      description={description}
      onClose={onClose}
      open
      title={title}
    >
      <label className="block">
        <span className="text-theme-500 text-xs tracking-[0.18em] uppercase">
          Name
        </span>
        <input
          autoFocus
          className="border-theme-500/15 bg-theme-950 text-theme-100 mt-2 w-full rounded-xl border px-3 py-2 transition outline-none focus:border-white/18"
          onChange={(event) => {
            setName(event.target.value);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter" && name.trim() && !submitting) {
              event.preventDefault();
              setSubmitting(true);
              setError("");
              void onConfirm(name.trim())
                .then(() => {
                  onClose();
                })
                .catch((nextError) => {
                  setError(
                    nextError instanceof Error
                      ? nextError.message
                      : String(nextError),
                  );
                })
                .finally(() => {
                  setSubmitting(false);
                });
            }
          }}
          placeholder="Playlist name"
          type="text"
          value={name}
        />
      </label>
      {error ? <p className="mt-3 text-sm text-red-300">{error}</p> : null}
    </ModalDialog>
  );
}

export function ConfirmPlaylistDeleteDialog({
  description,
  onClose,
  onConfirm,
  open,
  title,
}: {
  description: string;
  onClose: () => void;
  onConfirm: () => Promise<void>;
  open: boolean;
  title: string;
}) {
  if (!open) {
    return null;
  }

  return (
    <ConfirmPlaylistDeleteDialogBody
      description={description}
      key={title}
      onClose={onClose}
      onConfirm={onConfirm}
      title={title}
    />
  );
}

function ConfirmPlaylistDeleteDialogBody({
  description,
  onClose,
  onConfirm,
  title,
}: {
  description: string;
  onClose: () => void;
  onConfirm: () => Promise<void>;
  title: string;
}) {
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  return (
    <ModalDialog
      actions={
        <>
          <Button onClick={onClose} tone="quiet">
            Cancel
          </Button>
          <Button
            disabled={submitting}
            icon={
              submitting ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Trash2 className="h-4 w-4" />
              )
            }
            onClick={() => {
              setSubmitting(true);
              setError("");
              void onConfirm()
                .then(() => {
                  onClose();
                })
                .catch((nextError) => {
                  setError(
                    nextError instanceof Error
                      ? nextError.message
                      : String(nextError),
                  );
                })
                .finally(() => {
                  setSubmitting(false);
                });
            }}
            tone="danger"
          >
            Delete playlist
          </Button>
        </>
      }
      description={description}
      onClose={onClose}
      open
      title={title}
    >
      {error ? <p className="text-sm text-red-300">{error}</p> : null}
    </ModalDialog>
  );
}

export function AddToPlaylistDialog({
  onClose,
  open,
  recording,
  title,
}: {
  onClose: () => void;
  open: boolean;
  recording: PlaylistIdentity;
  title: string;
}) {
  if (!open) {
    return null;
  }

  return (
    <AddToPlaylistDialogBody
      key={`${recording.libraryRecordingId ?? ""}:${recording.recordingId ?? ""}:${title}`}
      onClose={onClose}
      recording={recording}
      title={title}
    />
  );
}

function AddToPlaylistDialogBody({
  onClose,
  recording,
  title,
}: {
  onClose: () => void;
  recording: PlaylistIdentity;
  title: string;
}) {
  const [creating, setCreating] = useState(false);
  const [createName, setCreateName] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [playlists, setPlaylists] = useState<PlaylistListItem[]>([]);

  useEffect(() => {
    void listPlaylistsPage(0, 1000)
      .then((page) => {
        setPlaylists(
          page.Items.filter((playlist) => playlist.Kind !== "liked"),
        );
      })
      .catch((nextError) => {
        setError(
          nextError instanceof Error ? nextError.message : String(nextError),
        );
      })
      .finally(() => {
        setLoading(false);
      });
  }, []);

  async function addToPlaylist(playlistId: string) {
    setError("");
    await addPlaylistItem({
      libraryRecordingId: recording.libraryRecordingId ?? "",
      playlistId,
      recordingId: recording.recordingId ?? "",
    });
    onClose();
  }

  return (
    <ModalDialog
      actions={
        <Button onClick={onClose} tone="quiet">
          Close
        </Button>
      }
      description="Add this song to one of your playlists or create a new one."
      onClose={onClose}
      open
      title={`Add "${title}"`}
    >
      <div className="space-y-4">
        <div className="space-y-2">
          {loading ? (
            <p className="text-theme-500 text-sm">Loading playlists...</p>
          ) : playlists.length > 0 ? (
            playlists.map((playlist) => (
              <button
                className="border-theme-500/15 flex w-full items-center justify-between rounded-xl border px-3 py-3 text-left transition hover:border-white/18 hover:bg-white/[0.06]"
                key={playlist.PlaylistID}
                onClick={() => {
                  void addToPlaylist(playlist.PlaylistID).catch((nextError) => {
                    setError(
                      nextError instanceof Error
                        ? nextError.message
                        : String(nextError),
                    );
                  });
                }}
                type="button"
              >
                <span>
                  <span className="text-theme-100 block text-sm font-medium">
                    {playlist.Name}
                  </span>
                  <span className="text-theme-500 block text-xs">
                    {playlist.ItemCount} tracks
                  </span>
                </span>
                <Plus className="text-theme-400 h-4 w-4" />
              </button>
            ))
          ) : (
            <p className="text-theme-500 text-sm">
              No normal playlists yet. Create one below.
            </p>
          )}
        </div>

        <div className="border-theme-500/15 rounded-xl border p-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-theme-100 text-sm font-medium">
                Create playlist
              </p>
              <p className="text-theme-500 text-xs">
                Make a new playlist and add this song immediately.
              </p>
            </div>
            <Button
              onClick={() => {
                setCreating((current) => !current);
              }}
              tone="quiet"
            >
              {creating ? "Hide" : "Create"}
            </Button>
          </div>

          {creating ? (
            <div className="mt-3 flex flex-wrap gap-2">
              <input
                className="border-theme-500/15 bg-theme-950 text-theme-100 min-w-0 flex-1 rounded-xl border px-3 py-2 transition outline-none focus:border-white/18"
                onChange={(event) => {
                  setCreateName(event.target.value);
                }}
                placeholder="New playlist name"
                type="text"
                value={createName}
              />
              <Button
                disabled={!createName.trim()}
                onClick={() => {
                  setError("");
                  void createPlaylist(createName.trim())
                    .then((playlist) => addToPlaylist(playlist.PlaylistID))
                    .catch((nextError) => {
                      setError(
                        nextError instanceof Error
                          ? nextError.message
                          : String(nextError),
                      );
                    });
                }}
                tone="primary"
              >
                Create and add
              </Button>
            </div>
          ) : null}
        </div>

        {error ? <p className="text-sm text-red-300">{error}</p> : null}
      </div>
    </ModalDialog>
  );
}
