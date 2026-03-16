import type { ButtonHTMLAttributes, ReactNode } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: ReactNode;
  tone?: "default" | "primary" | "danger" | "quiet";
};

const toneClasses: Record<NonNullable<ButtonProps["tone"]>, string> = {
  danger:
    "border-red-500/30 bg-red-500/10 text-red-100 hover:border-red-400/40 hover:bg-red-500/15",
  default:
    "border-zinc-700 bg-zinc-900 text-zinc-100 hover:border-zinc-600 hover:bg-zinc-800",
  primary:
    "border-zinc-500 bg-zinc-100 text-zinc-950 hover:border-zinc-300 hover:bg-white",
  quiet:
    "border-zinc-800 bg-transparent text-zinc-300 hover:border-zinc-700 hover:bg-zinc-900",
};

export function Button({
  children,
  className = "",
  disabled,
  icon,
  tone = "default",
  type = "button",
  ...props
}: ButtonProps) {
  return (
    <button
      className={[
        "inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50",
        toneClasses[tone],
        className,
      ].join(" ")}
      disabled={disabled}
      type={type}
      {...props}
    >
      {icon}
      <span>{children}</span>
    </button>
  );
}

type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  label: string;
};

export function IconButton({
  className = "",
  disabled,
  label,
  type = "button",
  ...props
}: IconButtonProps) {
  return (
    <button
      aria-label={label}
      className={[
        "inline-flex h-9 w-9 items-center justify-center rounded-md border border-zinc-700 bg-zinc-900 text-zinc-200 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50",
        className,
      ].join(" ")}
      disabled={disabled}
      type={type}
      {...props}
    />
  );
}
