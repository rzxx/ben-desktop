import { useState, type ReactNode } from "react";
import { artistLetter } from "@/lib/format";

type ArtworkTileProps = {
  src?: string;
  title: string;
  fallback?: ReactNode;
  alt: string;
  className?: string;
};

export function ArtworkTile({
  src,
  title,
  fallback,
  alt,
  className = "",
}: ArtworkTileProps) {
  const [failedSrc, setFailedSrc] = useState("");
  const visibleSrc = src && failedSrc !== src ? src : "";

  return (
    <div
      className={[
        "border-theme-300/75 relative aspect-square overflow-hidden border bg-white/82 dark:border-white/10 dark:bg-white/[0.06]",
        className,
      ].join(" ")}
    >
      {visibleSrc ? (
        <img
          alt={alt}
          className="h-full w-full object-cover"
          loading="lazy"
          onError={() => setFailedSrc(visibleSrc)}
          src={visibleSrc}
        />
      ) : (
        <div
          aria-label={alt}
          className="flex h-full w-full items-center justify-center"
          role="img"
        >
          <span className="text-theme-900 dark:text-theme-100 text-4xl font-semibold">
            {fallback ?? artistLetter(title)}
          </span>
        </div>
      )}
    </div>
  );
}
