import type { ButtonHTMLAttributes, ReactNode } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  icon?: ReactNode;
  tone?: "default" | "primary" | "danger" | "quiet";
};

const toneClasses: Record<NonNullable<ButtonProps["tone"]>, string> = {
  danger:
    "border-red-500/25 bg-red-500/10 text-red-700 enabled:hover:border-red-500/35 enabled:hover:bg-red-500/14 dark:text-red-100 dark:enabled:hover:border-red-400/40 dark:enabled:hover:bg-red-500/15",
  default:
    "border-theme-300/75 bg-theme-50/78 text-theme-900 enabled:hover:border-theme-400/75 enabled:hover:bg-theme-100/90 dark:border-white/10 dark:bg-white/[0.06] dark:text-theme-100 dark:enabled:hover:border-white/18 dark:enabled:hover:bg-white/[0.09]",
  primary:
    "border-theme-900 bg-theme-900 text-theme-50 enabled:hover:border-theme-800 enabled:hover:bg-theme-800 dark:border-theme-100 dark:bg-theme-100 dark:text-theme-950 dark:enabled:hover:border-white dark:enabled:hover:bg-white",
  quiet:
    "border-theme-300/70 bg-transparent text-theme-700 enabled:hover:border-theme-400/70 enabled:hover:bg-theme-100/75 dark:border-white/8 dark:text-theme-300 dark:enabled:hover:border-white/14 dark:enabled:hover:bg-white/[0.05]",
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
        "text-theme-700 border-theme-300/75 enabled:hover:border-theme-400/75 enabled:hover:bg-theme-100/90 dark:text-theme-200 inline-flex h-9 w-9 items-center justify-center rounded-full border bg-white/78 transition disabled:cursor-default disabled:opacity-50 dark:border-white/10 dark:bg-white/6 dark:enabled:hover:border-white/18 dark:enabled:hover:bg-white/10",
        className,
      ].join(" ")}
      disabled={disabled}
      type={type}
      {...props}
    />
  );
}
