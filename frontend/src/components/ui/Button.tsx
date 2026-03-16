import type { ButtonHTMLAttributes, ReactNode } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: ReactNode;
  tone?: "default" | "primary" | "danger" | "quiet";
};

const toneClasses: Record<NonNullable<ButtonProps["tone"]>, string> = {
  danger:
    "border-red-500/30 bg-red-500/10 text-red-100 hover:border-red-400/40 hover:bg-red-500/15",
  default:
    "border-white/10 bg-white/[0.06] text-theme-100 hover:border-white/18 hover:bg-white/[0.09]",
  primary:
    "border-theme-100 bg-theme-100 text-theme-950 hover:border-white hover:bg-white",
  quiet:
    "border-white/8 bg-transparent text-theme-300 hover:border-white/14 hover:bg-white/[0.05]",
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
        "inline-flex h-9 w-9 items-center justify-center rounded-full border border-white/10 bg-white/[0.06] text-theme-200 transition hover:border-white/18 hover:bg-white/[0.1] disabled:cursor-default disabled:opacity-50",
        className,
      ].join(" ")}
      disabled={disabled}
      type={type}
      {...props}
    />
  );
}
