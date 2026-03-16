type NoticeProps = {
  message: string;
  tone?: "default" | "success" | "warning" | "danger";
};

const toneClasses: Record<NonNullable<NoticeProps["tone"]>, string> = {
  danger: "border-red-500/30 bg-red-500/10 text-red-100",
  default: "border-zinc-700 bg-zinc-900 text-zinc-100",
  success: "border-emerald-500/30 bg-emerald-500/10 text-emerald-100",
  warning: "border-amber-500/30 bg-amber-500/10 text-amber-100",
};

export function Notice({ message, tone = "default" }: NoticeProps) {
  return (
    <div className={["rounded-md border px-4 py-3 text-sm", toneClasses[tone]].join(" ")}>
      {message}
    </div>
  );
}
