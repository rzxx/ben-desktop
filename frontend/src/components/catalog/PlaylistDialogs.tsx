import { useState } from "react";
import { LoaderCircle, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { ModalDialog } from "@/components/ui/ModalDialog";

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
          className="border-theme-300/75 text-theme-900 focus:border-theme-500/65 dark:border-theme-500/15 dark:bg-theme-950 dark:text-theme-100 mt-2 w-full rounded-xl border bg-white px-3 py-2 transition outline-none dark:focus:border-white/18"
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
      {error ? (
        <p className="mt-3 text-sm text-red-600 dark:text-red-300">{error}</p>
      ) : null}
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
      {error ? (
        <p className="text-sm text-red-600 dark:text-red-300">{error}</p>
      ) : null}
    </ModalDialog>
  );
}
