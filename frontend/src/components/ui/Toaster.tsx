import { Toast } from "@base-ui/react/toast";
import { CircleAlert, CircleCheck, Info, LoaderCircle, X } from "lucide-react";
import { toastManager, type ToastType } from "@/components/ui/toast";

const typeIcons: Record<ToastType, typeof Info> = {
  info: Info,
  success: CircleCheck,
  error: CircleAlert,
  loading: LoaderCircle,
};

const typeClasses: Record<ToastType, string> = {
  info: "text-sky-600 dark:text-sky-300",
  success: "text-emerald-600 dark:text-emerald-300",
  error: "text-red-600 dark:text-red-300",
  loading: "text-accent-600 dark:text-accent-300",
};

export function Toaster() {
  return (
    <Toast.Provider limit={5} toastManager={toastManager}>
      <Toast.Portal>
        <Toast.Viewport className="fixed top-12 right-0 left-0 z-[60] mx-auto w-[calc(100%-2rem)] max-w-sm">
          <ToastList />
        </Toast.Viewport>
      </Toast.Portal>
    </Toast.Provider>
  );
}

function ToastList() {
  const { toasts } = Toast.useToastManager();

  return toasts.map((toast) => {
    const type = (toast.type as ToastType | undefined) ?? "info";
    const Icon = typeIcons[type];
    const actionLabel =
      typeof toast.actionProps?.children === "string"
        ? toast.actionProps.children
        : null;

    return (
      <Toast.Root
        key={toast.id}
        swipeDirection="up"
        toast={toast}
        className={[
          "absolute top-0 right-0 left-0 mx-auto w-full origin-top select-none",
          "border-theme-300/75 bg-theme-100-desat/90 text-theme-900 rounded-2xl border shadow-xl shadow-black/15 backdrop-blur-xl backdrop-saturate-150",
          "dark:bg-theme-900/82 dark:text-theme-100 dark:border-white/10 dark:shadow-black/25",
          "[--gap:0.5rem] [--height:var(--toast-frontmost-height,var(--toast-height))] [--offset-y:calc(var(--toast-offset-y)+var(--toast-index)*var(--gap)+var(--toast-swipe-movement-y))] [--peek:0.75rem] [--scale:calc(max(0,1-var(--toast-index)*0.06))] [--shrink:calc(1-var(--scale))]",
          "[transform:translateX(var(--toast-swipe-movement-x))_translateY(calc(var(--toast-swipe-movement-y)+(var(--toast-index)*var(--peek))+(var(--shrink)*var(--height))))_scale(var(--scale))]",
          "after:pointer-events-none after:absolute after:bottom-full after:left-0 after:h-[calc(var(--gap)+1px)] after:w-full after:content-['']",
          "data-[starting-style]:[transform:translateY(-12px)_scale(0.9)] data-[starting-style]:opacity-0",
          "data-[ending-style]:opacity-0",
          "data-[expanded]:[transform:translateX(var(--toast-swipe-movement-x))_translateY(var(--offset-y))]",
          "data-[limited]:opacity-0",
          "[&[data-ending-style]:not([data-limited]):not([data-swipe-direction])]:[transform:translateY(-12px)_scale(0.9)]",
          "data-[ending-style]:data-[swipe-direction=up]:[transform:translateY(calc(var(--toast-swipe-movement-y)-150%))_scale(0.9)]",
          "data-[expanded]:data-[ending-style]:data-[swipe-direction=up]:[transform:translateY(calc(var(--toast-swipe-movement-y)-150%))_scale(0.9)]",
          "h-[var(--height)] data-[expanded]:h-[var(--toast-height)]",
          "z-[calc(1000-var(--toast-index))]",
          "focus-visible:outline-theme-900 dark:focus-visible:outline-theme-100 focus-visible:outline-2 focus-visible:-outline-offset-1",
          "[transition:transform_0.3s_cubic-bezier(0.22,1,0.36,1),opacity_0.3s_ease-out,height_0.2s_ease-out]",
          "data-[ending-style]:[transition:transform_0.12s_ease-out,opacity_0.12s_ease-out,height_0.2s_ease-out]",
        ].join(" ")}
      >
        <Toast.Content className="h-full overflow-hidden p-3 transition-opacity duration-200 data-[behind]:opacity-0 data-[expanded]:opacity-100">
          <div className="flex h-full items-start gap-3">
            <div className={["mt-0.5 shrink-0", typeClasses[type]].join(" ")}>
              <Icon
                className={
                  type === "loading" ? "h-4 w-4 animate-spin" : "h-4 w-4"
                }
              />
            </div>

            <div className="min-w-0 flex-1">
              <Toast.Title className="text-theme-900 dark:text-theme-100 text-sm font-semibold" />
              <Toast.Description className="text-theme-600 dark:text-theme-300 mt-0.5 text-xs" />
            </div>

            {actionLabel ? (
              <Toast.Action className="border-theme-300/75 text-theme-700 hover:bg-theme-200 dark:text-theme-200 dark:hover:bg-theme-800 shrink-0 rounded-md border bg-transparent px-2 py-1 text-xs font-medium transition dark:border-white/10">
                {actionLabel}
              </Toast.Action>
            ) : null}

            <Toast.Close
              aria-label="Dismiss"
              className="text-theme-500 hover:bg-theme-200 hover:text-theme-900 dark:text-theme-400 dark:hover:bg-theme-800 dark:hover:text-theme-100 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition"
            >
              <X className="h-4 w-4" />
            </Toast.Close>
          </div>
        </Toast.Content>
      </Toast.Root>
    );
  });
}
