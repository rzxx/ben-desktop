import { Dialog } from "@base-ui/react/dialog";
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
  return (
    <Dialog.Root
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
      }}
      open={open}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="bg-theme-950/22 fixed inset-0 z-60 backdrop-blur-sm dark:bg-black/60" />
        <div className="fixed inset-0 z-60 flex items-center justify-center px-4 py-6">
          <Dialog.Popup className="border-theme-300/70 shadow-theme-900/14 dark:border-theme-500/15 dark:bg-theme-900 w-full max-w-lg rounded-2xl border bg-white/94 p-5 shadow-2xl outline-none dark:shadow-black/40">
            <div className="flex items-start justify-between gap-4">
              <div>
                <Dialog.Title className="text-theme-900 dark:text-theme-100 text-lg font-semibold">
                  {title}
                </Dialog.Title>
                {description ? (
                  <Dialog.Description className="text-theme-500 mt-1 text-sm">
                    {description}
                  </Dialog.Description>
                ) : null}
              </div>
              <Dialog.Close
                aria-label="Close dialog"
                className="text-theme-500 hover:text-theme-900 dark:hover:text-theme-100 rounded p-1 transition-colors"
              >
                x
              </Dialog.Close>
            </div>
            {children ? <div className="mt-4">{children}</div> : null}
            {actions ? (
              <div className="mt-5 flex flex-wrap justify-end gap-2">
                {actions}
              </div>
            ) : null}
          </Dialog.Popup>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
