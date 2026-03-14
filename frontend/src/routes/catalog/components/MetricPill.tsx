export function MetricPill({ label }: { label: string }) {
  return (
    <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
      {label}
    </span>
  );
}
