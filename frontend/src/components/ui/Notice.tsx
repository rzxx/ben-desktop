type NoticeProps = {
  message: string;
  tone?: "default" | "success" | "warning" | "danger";
};

const toneClasses: Record<NonNullable<NoticeProps["tone"]>, string> = {
  danger:
    "border-red-500/25 bg-red-500/10 text-red-700 dark:border-red-500/30 dark:text-red-100",
  default:
    "border-theme-300/75 bg-white/82 text-theme-800 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100",
  success:
    "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:border-emerald-500/30 dark:text-emerald-100",
  warning:
    "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:border-amber-500/30 dark:text-amber-100",
};

export function Notice({ message, tone = "default" }: NoticeProps) {
  return (
    <div
      className={[
        "rounded-md border px-4 py-3 text-sm",
        toneClasses[tone],
      ].join(" ")}
    >
      {message}
    </div>
  );
}
