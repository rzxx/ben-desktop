import type { ReactNode } from "react";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { Button } from "@/components/ui/Button";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import type { ArtworkRef } from "@/lib/api/models";

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
    <Button
      icon={icon}
      onClick={onClick}
      tone={priority === "primary" ? "primary" : "default"}
    >
      {label}
    </Button>
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
    <section className="rounded-lg border border-zinc-800 bg-zinc-950 p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-xs tracking-wide text-zinc-500 uppercase">
            {eyebrow}
          </p>
          <h1 className="mt-3 text-3xl font-semibold text-zinc-100">{title}</h1>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-zinc-400">
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
    <section className="rounded-lg border border-zinc-800 bg-zinc-950 p-6">
      <div className="flex flex-col gap-6 xl:flex-row">
        <ArtworkTile
          alt={title}
          className="h-52 w-52 shrink-0"
          src={visibleArtworkUrl}
          title={title}
        />
        <div className="flex min-w-0 flex-1 flex-col justify-end">
          <p className="text-xs tracking-wide text-zinc-500 uppercase">
            {eyebrow}
          </p>
          <h1 className="mt-3 text-4xl font-semibold text-zinc-100">{title}</h1>
          <p className="mt-4 max-w-3xl text-sm leading-6 text-zinc-400">
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
