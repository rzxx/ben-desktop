import { useState } from "react";
import { artistLetter } from "@/lib/format";

type ArtworkTileProps = {
  src?: string;
  title: string;
  subtitle?: string;
  alt: string;
  square?: boolean;
  rounded?: "soft" | "full";
  className?: string;
};

export function ArtworkTile({
  src,
  title,
  subtitle,
  alt,
  square = true,
  className = "",
}: ArtworkTileProps) {
  const [loadState, setLoadState] = useState<{ failed: boolean; src: string }>({
    failed: false,
    src: "",
  });

  const visibleSrc =
    src && !(loadState.failed && loadState.src === src) ? src : "";

  return (
    <div
      className={[
        "border-theme-300/70 bg-theme-100/80 relative overflow-hidden border dark:border-white/8 dark:bg-white/5",
        square ? "aspect-square" : "aspect-4/3",
        className,
      ].join(" ")}
    >
      {visibleSrc ? (
        <img
          alt={alt}
          className="h-full w-full object-cover"
          loading="lazy"
          onError={() => {
            setLoadState({
              failed: true,
              src: visibleSrc,
            });
          }}
          src={visibleSrc}
        />
      ) : (
        <div className="from-theme-100 to-theme-50 flex h-full w-full flex-col justify-between bg-linear-to-br via-white p-4 dark:bg-white/5">
          <span className="text-theme-500 text-xs tracking-wide uppercase">
            {subtitle || "Library"}
          </span>
          <span className="text-theme-900 dark:text-theme-100 text-4xl font-semibold">
            {artistLetter(title)}
          </span>
        </div>
      )}
    </div>
  );
}
