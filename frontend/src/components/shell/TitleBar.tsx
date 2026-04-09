import { type MouseEvent, type ReactNode } from "react";
import { Window } from "@wailsio/runtime";
import {
  Bell,
  BellRing,
  Bug,
  Copy,
  Monitor,
  Minus,
  Moon,
  PanelRightClose,
  PanelRightOpen,
  Square,
  Sun,
  X,
} from "lucide-react";
import {
  toggleWindowMaximised,
  useWindowMaximised,
} from "@/hooks/app/useWindowMaximised";
import { useNotificationsStore } from "@/stores/notifications/store";
import { useThemeStore } from "@/stores/theme/store";
import { useUIStore } from "@/stores/ui/store";

export function TitleBar() {
  const isMaximised = useWindowMaximised();
  const preferences = useNotificationsStore((state) => state.preferences);
  const toggleCenter = useNotificationsStore((state) => state.toggleCenter);
  const setVerbosity = useNotificationsStore((state) => state.setVerbosity);
  const themePreferences = useThemeStore((state) => state.preferences);
  const setThemeMode = useThemeStore((state) => state.setMode);
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
      className="wails-drag border-theme-300/15 bg-theme-50/80 text-theme-900 dark:bg-theme-950/72 dark:text-theme-100 fixed inset-x-0 top-0 z-50 flex h-8 items-center justify-between border-b backdrop-blur-xl select-none dark:border-white/3"
    >
      <div className="pl-4">
        <p className="text-sm font-medium tracking-wide">ben</p>
      </div>

      <div className="wails-no-drag flex h-full items-center gap-px">
        <div className="flex h-full items-center px-1.5">
          <ThemeSwitch
            mode={themePreferences.mode}
            onChange={(mode) => {
              void setThemeMode(mode);
            }}
          />
        </div>
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
          ? "text-theme-700 dark:text-theme-200 dark:hover:text-theme-100 hover:bg-red-500 hover:text-white"
          : "text-theme-700 hover:bg-theme-200 hover:text-theme-950 dark:text-theme-200 dark:hover:bg-theme-800 dark:hover:text-theme-100"
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
      className="wails-no-drag text-theme-700 hover:bg-theme-200 hover:text-theme-950 dark:text-theme-200 dark:hover:bg-theme-800 dark:hover:text-theme-100 inline-flex h-full max-w-28 items-center justify-center px-3 text-[0.65rem] tracking-[0.18em] uppercase transition-colors"
    >
      {props.children}
    </button>
  );
}

function ThemeSwitch({
  mode,
  onChange,
}: {
  mode?: string;
  onChange: (mode: string) => void;
}) {
  return (
    <div className="border-theme-500/15 dark:bg-theme-900 flex h-6 items-center rounded-full border p-0.5 dark:border-white/8 dark:shadow-none">
      <ThemeSwitchButton
        active={mode === "system"}
        label="Use system theme"
        onClick={() => {
          onChange("system");
        }}
      >
        <Monitor className="h-3.5 w-3.5" />
      </ThemeSwitchButton>
      <ThemeSwitchButton
        active={mode === "light"}
        label="Use light theme"
        onClick={() => {
          onChange("light");
        }}
      >
        <Sun className="h-3.5 w-3.5" />
      </ThemeSwitchButton>
      <ThemeSwitchButton
        active={mode === "dark"}
        label="Use dark theme"
        onClick={() => {
          onChange("dark");
        }}
      >
        <Moon className="h-3.5 w-3.5" />
      </ThemeSwitchButton>
    </div>
  );
}

function ThemeSwitchButton({
  active,
  label,
  onClick,
  children,
}: ControlButtonProps & { active: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={`wails-no-drag inline-flex h-5 w-5 items-center justify-center rounded-full transition-colors ${
        active
          ? "bg-theme-950 text-theme-50 dark:bg-theme-100 dark:text-theme-950"
          : "text-theme-600 hover:bg-theme-200 hover:text-theme-950 dark:text-theme-300 dark:hover:text-theme-100 dark:hover:bg-white/8"
      }`}
    >
      {children}
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
