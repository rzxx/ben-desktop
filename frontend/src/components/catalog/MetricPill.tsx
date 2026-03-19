export function MetricPill({ label }: { label: string }) {
  return (
    <span className="text-theme-300 inline-flex items-center rounded-full border border-white/10 bg-white/[0.05] px-2.5 py-1 text-xs tracking-wide uppercase">
      {label}
    </span>
  );
}
