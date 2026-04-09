import type { PropsWithChildren } from "react";

type BadgeProps = PropsWithChildren<{
  tone?: "default" | "success" | "warning" | "danger";
  className?: string;
}>;

const toneClasses: Record<NonNullable<BadgeProps["tone"]>, string> = {
  danger:
    "border-red-500/25 bg-red-500/10 text-red-700 dark:border-red-500/30 dark:text-red-100",
  default:
    "border-theme-300/75 bg-white/75 text-theme-700 dark:border-white/10 dark:bg-white/[0.05] dark:text-theme-300",
  success:
    "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:border-emerald-500/30 dark:text-emerald-100",
  warning:
    "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:border-amber-500/30 dark:text-amber-100",
};

export function Badge({
  children,
  className = "",
  tone = "default",
}: BadgeProps) {
  return (
    <span
      className={[
        "inline-flex items-center rounded-full border px-2.5 py-1 text-xs tracking-wide uppercase",
        toneClasses[tone],
        className,
      ].join(" ")}
    >
      {children}
    </span>
  );
}
