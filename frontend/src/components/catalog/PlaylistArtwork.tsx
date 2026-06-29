import type { ComponentProps } from "react";
import { GlobeOff, Heart } from "lucide-react";
import { ArtworkTile } from "@/components/ui/ArtworkTile";

type PlaylistArtworkProps = Omit<
  ComponentProps<typeof ArtworkTile>,
  "fallback"
> & {
  kind?: string;
};

export function PlaylistArtwork({ kind, src, ...props }: PlaylistArtworkProps) {
  const Icon = kind === "liked" ? Heart : kind === "offline" ? GlobeOff : null;

  return (
    <ArtworkTile
      fallback={Icon ? <Icon aria-hidden className="size-[1em]" /> : undefined}
      src={Icon ? undefined : src}
      {...props}
    />
  );
}
