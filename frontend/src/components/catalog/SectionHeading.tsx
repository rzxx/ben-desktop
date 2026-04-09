import type { ReactNode } from "react";

export function SectionHeading({
  eyebrow,
  title,
  meta,
  actions,
  description,
}: {
  eyebrow?: string;
  title: string;
  meta?: ReactNode;
  actions?: ReactNode;
  description?: string;
}) {
  return (
    <section className="flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        {eyebrow ? (
          <p className="text-theme-500 mb-2 text-[11px] tracking-[0.28em] uppercase">
            {eyebrow}
          </p>
        ) : null}
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-theme-900 dark:text-theme-100 text-xl font-semibold">
            {title}
          </h1>
          {meta ? <div className="flex flex-wrap gap-2">{meta}</div> : null}
        </div>
        {description ? (
          <p className="text-theme-500 mt-2 max-w-3xl text-sm">{description}</p>
        ) : null}
      </div>
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </section>
  );
}
