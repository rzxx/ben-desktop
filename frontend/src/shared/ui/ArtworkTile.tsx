import { useState } from "react";
import { artistLetter } from "../lib/format";

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
        "artwork-tile relative overflow-hidden border border-white/10 bg-white/5 shadow-[0_18px_50px_rgba(0,0,0,0.25)]",
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
        <div className="artwork-tile__fallback flex h-full w-full flex-col justify-between bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.4),transparent_55%),linear-gradient(180deg,rgba(15,23,42,0.2),rgba(15,23,42,0.95))] p-4">
          <span className="text-xs tracking-[0.35em] text-white/45 uppercase">
            {subtitle || "Library"}
          </span>
          <span className="text-4xl font-semibold text-white/85">
            {artistLetter(title)}
          </span>
        </div>
      )}
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-slate-950/70 to-transparent" />
    </div>
  );
}
