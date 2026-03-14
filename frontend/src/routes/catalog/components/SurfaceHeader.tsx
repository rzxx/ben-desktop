import type { ReactNode } from "react";
import { ArtworkTile } from "../../../shared/ui/ArtworkTile";
import { useThumbnailUrl } from "../../../shared/lib/use-thumbnail-url";
import type { ArtworkRef } from "../../../shared/lib/desktop";

export function ActionButton({
  icon,
  label,
  onClick,
  priority = "secondary",
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  priority?: "primary" | "secondary";
}) {
  return (
    <button
      className={`action-button ${priority === "primary" ? "is-primary" : ""}`}
      onClick={onClick}
      type="button"
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

export function PageHeader({
  actions,
  description,
  eyebrow,
  meta,
  title,
}: {
  actions?: ReactNode;
  description: string;
  eyebrow: string;
  meta?: ReactNode;
  title: string;
}) {
  return (
    <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(135deg,rgba(249,115,22,0.12),transparent_45%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
            {eyebrow}
          </p>
          <h1 className="mt-3 text-3xl font-semibold text-white">{title}</h1>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-white/55">
            {description}
          </p>
          {meta && <div className="mt-4 flex flex-wrap gap-2">{meta}</div>}
        </div>
        {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      </div>
    </section>
  );
}

export function DetailHero({
  actions,
  artworkUrl,
  eyebrow,
  meta,
  subtitle,
  thumb,
  title,
}: {
  actions?: ReactNode;
  artworkUrl?: string;
  eyebrow: string;
  meta?: ReactNode;
  subtitle: string;
  thumb?: ArtworkRef;
  title: string;
}) {
  const thumbUrl = useThumbnailUrl(thumb);
  const visibleArtworkUrl = artworkUrl || thumbUrl;

  return (
    <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(135deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
      <div className="flex flex-col gap-6 xl:flex-row">
        <ArtworkTile
          alt={title}
          className="h-52 w-52 shrink-0"
          src={visibleArtworkUrl}
          title={title}
        />
        <div className="flex min-w-0 flex-1 flex-col justify-end">
          <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
            {eyebrow}
          </p>
          <h1 className="mt-3 text-4xl font-semibold text-white">{title}</h1>
          <p className="mt-4 max-w-3xl text-sm leading-6 text-white/55">
            {subtitle}
          </p>
          {meta && <div className="mt-4 flex flex-wrap gap-2">{meta}</div>}
          {actions && (
            <div className="mt-5 flex flex-wrap gap-2">{actions}</div>
          )}
        </div>
      </div>
    </section>
  );
}
