import type { PropsWithChildren } from "react";

type BadgeProps = PropsWithChildren<{
  tone?: "default" | "success" | "warning" | "danger";
  className?: string;
}>;

const toneClasses: Record<NonNullable<BadgeProps["tone"]>, string> = {
  danger: "border-red-500/30 bg-red-500/10 text-red-100",
  default: "border-zinc-700 bg-zinc-900 text-zinc-300",
  success: "border-emerald-500/30 bg-emerald-500/10 text-emerald-100",
  warning: "border-amber-500/30 bg-amber-500/10 text-amber-100",
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
