import { useEffect, useState } from "react";
import { type ArtworkRef, resolveThumbnailURL } from "./desktop";

const thumbnailUrlCache = new Map<string, string>();
const thumbnailPendingCache = new Map<string, Promise<string>>();

async function resolveCached(
  key: string,
  cache: Map<string, string>,
  pending: Map<string, Promise<string>>,
  load: () => Promise<string>,
) {
  if (cache.has(key)) {
    return cache.get(key) ?? "";
  }
  if (!pending.has(key)) {
    pending.set(
      key,
      load()
        .then((value) => {
          cache.set(key, value);
          pending.delete(key);
          return value;
        })
        .catch((error) => {
          pending.delete(key);
          throw error;
        }),
    );
  }
  return pending.get(key) ?? Promise.resolve("");
}

function thumbnailCacheKey(thumb?: ArtworkRef | null) {
  const blobId = thumb?.BlobID?.trim() ?? "";
  const mime = thumb?.MIME?.trim() ?? "";
  const variant = thumb?.Variant?.trim() ?? "";
  if (!blobId) {
    return "";
  }
  return `${blobId}|${mime}|${variant}`;
}

export function useThumbnailUrl(thumb?: ArtworkRef | null) {
  const [state, setState] = useState<{ key: string; url: string }>({
    key: "",
    url: "",
  });

  const cacheKey = thumbnailCacheKey(thumb);
  const cachedUrl = cacheKey ? (thumbnailUrlCache.get(cacheKey) ?? "") : "";

  useEffect(() => {
    if (!cacheKey || !thumb) {
      return;
    }
    let active = true;
    void resolveCached(cacheKey, thumbnailUrlCache, thumbnailPendingCache, () =>
      resolveThumbnailURL(thumb),
    )
      .then((value) => {
        if (active) {
          setState({
            key: cacheKey,
            url: value,
          });
        }
      })
      .catch(() => {
        if (active) {
          setState({
            key: cacheKey,
            url: "",
          });
        }
      });
    return () => {
      active = false;
    };
  }, [cacheKey, thumb]);

  if (!cacheKey) {
    return "";
  }

  if (state.key === cacheKey) {
    return state.url;
  }

  return cachedUrl;
}
