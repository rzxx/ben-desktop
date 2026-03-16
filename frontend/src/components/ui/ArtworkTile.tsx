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
  rounded = "soft",
  className = "",
}: ArtworkTileProps) {
  const [loadState, setLoadState] = useState<{ failed: boolean; src: string }>({
    failed: false,
    src: "",
  });

  const shape =
    rounded === "full"
      ? "rounded-full"
      : "rounded-[1.2rem] sm:rounded-[1.35rem]";
  const visibleSrc =
    src && !(loadState.failed && loadState.src === src) ? src : "";

  return (
    <div
      className={[
        "relative overflow-hidden border border-zinc-800 bg-zinc-900",
        square ? "aspect-square" : "aspect-[4/3]",
        shape,
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
        <div className="flex h-full w-full flex-col justify-between bg-zinc-900 p-4">
          <span className="text-xs uppercase tracking-wide text-zinc-500">
            {subtitle || "Library"}
          </span>
          <span className="text-4xl font-semibold text-zinc-100">
            {artistLetter(title)}
          </span>
        </div>
      )}
    </div>
  );
}

