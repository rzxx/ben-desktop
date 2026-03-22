import type { ReactNode } from "react";

export function ModalDialog({
  actions,
  children,
  description,
  onClose,
  open,
  title,
}: {
  actions?: ReactNode;
  children?: ReactNode;
  description?: string;
  onClose: () => void;
  open: boolean;
  title: string;
}) {
  if (!open) {
    return null;
  }

  return (
    <div
      className="fixed inset-0 z-60 flex items-center justify-center bg-black/60 px-4 py-6 backdrop-blur-sm"
      onClick={onClose}
      role="presentation"
    >
      <div
        className="border-theme-500/15 bg-theme-900 w-full max-w-lg rounded-2xl border p-5 shadow-2xl shadow-black/40"
        onClick={(event) => {
          event.stopPropagation();
        }}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-theme-100 text-lg font-semibold">{title}</h2>
            {description ? (
              <p className="text-theme-500 mt-1 text-sm">{description}</p>
            ) : null}
          </div>
          <button
            aria-label="Close dialog"
            className="text-theme-500 hover:text-theme-100 rounded p-1 transition-colors"
            onClick={onClose}
            type="button"
          >
            x
          </button>
        </div>
        {children ? <div className="mt-4">{children}</div> : null}
        {actions ? (
          <div className="mt-5 flex flex-wrap justify-end gap-2">{actions}</div>
        ) : null}
      </div>
    </div>
  );
}
