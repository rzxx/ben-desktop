export function MetricPill({ label }: { label: string }) {
  return (
    <span className="inline-flex items-center rounded-full border border-zinc-700 bg-zinc-900 px-2.5 py-1 text-xs tracking-wide text-zinc-300 uppercase">
      {label}
    </span>
  );
}
