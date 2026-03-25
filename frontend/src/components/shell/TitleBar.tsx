import { type MouseEvent, type ReactNode } from "react";
import { Window } from "@wailsio/runtime";
import {
  Bell,
  BellRing,
  Bug,
  Copy,
  Minus,
  PanelRightClose,
  PanelRightOpen,
  Square,
  X,
} from "lucide-react";
import {
  toggleWindowMaximised,
  useWindowMaximised,
} from "@/hooks/app/useWindowMaximised";
import { useNotificationsStore } from "@/stores/notifications/store";
import { useUIStore } from "@/stores/ui/store";

export function TitleBar() {
  const isMaximised = useWindowMaximised();
  const preferences = useNotificationsStore((state) => state.preferences);
  const toggleCenter = useNotificationsStore((state) => state.toggleCenter);
  const setVerbosity = useNotificationsStore((state) => state.setVerbosity);
  const isQueueSidebarOpen = useUIStore((state) => state.isQueueSidebarOpen);
  const toggleQueueSidebar = useUIStore((state) => state.toggleQueueSidebar);
  const handleDoubleClick = (event: MouseEvent<HTMLElement>) => {
    if (event.button !== 0) {
      return;
    }

    const target = event.target;
    if (!(target instanceof HTMLElement)) {
      return;
    }

    if (target.closest(".wails-no-drag")) {
      return;
    }

    void toggleWindowMaximised();
  };

  return (
    <header
      onDoubleClick={handleDoubleClick}
      className="wails-drag fixed inset-x-0 top-0 z-50 flex h-8 items-center justify-between border-b border-white/3 select-none"
    >
      <div className="pl-4">
        <p className="text-theme-100 text-sm font-medium tracking-wide">ben</p>
      </div>

      <div className="wails-no-drag flex h-full items-center gap-px">
        <ControlButton
          label="Open notifications"
          onClick={() => {
            toggleCenter();
          }}
        >
          <NotificationGlyph verbosity={preferences.verbosity} />
        </ControlButton>
        <WideControlButton
          label={`Notification level: ${preferences.verbosity}`}
          onClick={() => {
            void setVerbosity(nextVerbosity(preferences.verbosity));
          }}
        >
          <span className="truncate">
            {verbosityLabel(preferences.verbosity)}
          </span>
        </WideControlButton>
        <ControlButton
          label={
            isQueueSidebarOpen ? "Hide playback queue" : "Show playback queue"
          }
          onClick={() => {
            toggleQueueSidebar();
          }}
        >
          {isQueueSidebarOpen ? (
            <PanelRightClose
              size={15}
              strokeWidth={1}
              absoluteStrokeWidth={true}
            />
          ) : (
            <PanelRightOpen
              size={15}
              strokeWidth={1}
              absoluteStrokeWidth={true}
            />
          )}
        </ControlButton>
        <ControlButton
          label="Minimise"
          onClick={() => {
            void Window.Minimise();
          }}
        >
          <Minus size={14} strokeWidth={1} absoluteStrokeWidth={true} />
        </ControlButton>
        <ControlButton
          label={isMaximised ? "Restore" : "Maximise"}
          onClick={() => {
            void toggleWindowMaximised();
          }}
        >
          {isMaximised ? (
            <Copy
              size={12}
              strokeWidth={1}
              absoluteStrokeWidth={true}
              className="scale-x-[-1]"
            />
          ) : (
            <Square size={12} strokeWidth={0.75} absoluteStrokeWidth={true} />
          )}
        </ControlButton>
        <ControlButton
          danger
          label="Close"
          onClick={() => {
            void Window.Close();
          }}
        >
          <X size={16} strokeWidth={1.25} absoluteStrokeWidth={true} />
        </ControlButton>
      </div>
    </header>
  );
}

type ControlButtonProps = {
  label: string;
  danger?: boolean;
  onClick: () => void;
  children: ReactNode;
};

function ControlButton(props: ControlButtonProps) {
  return (
    <button
      type="button"
      aria-label={props.label}
      title={props.label}
      onClick={props.onClick}
      className={`wails-no-drag inline-flex h-full w-12 items-center justify-center transition-colors ${
        props.danger
          ? "text-theme-200 hover:text-theme-100 hover:bg-red-500"
          : "text-theme-200 hover:bg-theme-800 hover:text-theme-100"
      }`}
    >
      {props.children}
    </button>
  );
}

function WideControlButton(props: ControlButtonProps) {
  return (
    <button
      type="button"
      aria-label={props.label}
      title={props.label}
      onClick={props.onClick}
      className="wails-no-drag text-theme-200 hover:bg-theme-800 hover:text-theme-100 inline-flex h-full max-w-28 items-center justify-center px-3 text-[0.65rem] tracking-[0.18em] uppercase transition-colors"
    >
      {props.children}
    </button>
  );
}

function NotificationGlyph({ verbosity }: { verbosity?: string }) {
  switch (verbosity) {
    case "important":
      return <Bell className="h-4 w-4" />;
    case "everything":
      return <Bug className="h-4 w-4" />;
    default:
      return <BellRing className="h-4 w-4" />;
  }
}

function verbosityLabel(verbosity?: string) {
  switch (verbosity) {
    case "important":
      return "Important";
    case "everything":
      return "Everything";
    default:
      return "User";
  }
}

function nextVerbosity(verbosity?: string) {
  switch (verbosity) {
    case "important":
      return "user_activity";
    case "user_activity":
      return "everything";
    default:
      return "important";
  }
}
