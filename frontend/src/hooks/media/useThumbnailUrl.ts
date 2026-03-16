import type { ArtworkRef } from "@/lib/api/models";
import { resolveThumbnailURL } from "@/lib/api/playback";
import { useResolvedUrl } from "./useResolvedUrl";

function thumbnailCacheKey(thumb?: ArtworkRef | null) {
  const blobId = thumb?.BlobID?.trim() ?? "";
  const mime = thumb?.MIME?.trim() ?? "";
  const fileExt = thumb?.FileExt?.trim() ?? "";
  const variant = thumb?.Variant?.trim() ?? "";
  if (!blobId) {
    return "";
  }
  return `${blobId}|${mime}|${fileExt}|${variant}`;
}

export function useThumbnailUrl(thumb?: ArtworkRef | null) {
  const cacheKey = thumbnailCacheKey(thumb);
  return useResolvedUrl(
    cacheKey,
    thumb ? () => resolveThumbnailURL(thumb) : undefined,
  );
}
