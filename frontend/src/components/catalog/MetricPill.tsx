export function MetricPill({ label }: { label: string }) {
  return (
    <span className="text-theme-700 border-theme-300/75 dark:text-theme-300 inline-flex items-center rounded-full border bg-white/78 px-2.5 py-1 text-xs tracking-wide uppercase dark:border-white/10 dark:bg-white/[0.05]">
      {label}
    </span>
  );
}
