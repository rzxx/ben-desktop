import { type MouseEvent, type ReactNode } from "react";
import { Window } from "@wailsio/runtime";
import { Copy, Minus, Square, X } from "lucide-react";
import {
  toggleWindowMaximised,
  useWindowMaximised,
} from "@/hooks/app/useWindowMaximised";

export function TitleBar() {
  const isMaximised = useWindowMaximised();
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
      className="wails-drag fixed inset-x-0 top-0 z-50 flex h-8 items-center justify-between border-b border-zinc-800 bg-zinc-950 select-none"
    >
      <div className="pl-4">
        <p className="text-sm font-medium tracking-wide text-zinc-100">ben</p>
      </div>

      <div className="wails-no-drag flex h-full items-center gap-px">
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
          ? "text-zinc-300 hover:bg-red-500 hover:text-white"
          : "text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100"
      }`}
    >
      {props.children}
    </button>
  );
}
