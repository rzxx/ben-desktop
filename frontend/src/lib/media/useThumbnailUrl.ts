import { useEffect, useRef, useState } from "react";
import type { ArtworkRef } from "@/lib/api/models";
import { resolveThumbnailURL } from "@/lib/api/playback";

const thumbnailUrlCache = new Map<string, string>();
const thumbnailPendingCache = new Map<string, Promise<string>>();
const thumbnailRetryDelaysMS = [1000, 2500, 5000, 10000, 15000];

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
          if (value) {
            cache.set(key, value);
          }
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
  const fileExt = thumb?.FileExt?.trim() ?? "";
  const variant = thumb?.Variant?.trim() ?? "";
  if (!blobId) {
    return "";
  }
  return `${blobId}|${mime}|${fileExt}|${variant}`;
}

function retryDelayMs(attempt: number) {
  return thumbnailRetryDelaysMS[
    Math.min(attempt, thumbnailRetryDelaysMS.length - 1)
  ];
}

export function useResolvedUrl(cacheKey: string, load?: () => Promise<string>) {
  const [state, setState] = useState<{
    key: string;
    retryAttempt: number;
    url: string;
  }>({
    key: "",
    retryAttempt: 0,
    url: "",
  });
  const loadRef = useRef(load);

  useEffect(() => {
    loadRef.current = load;
  }, [load]);

  const cachedUrl = cacheKey ? (thumbnailUrlCache.get(cacheKey) ?? "") : "";
  const retryAttempt = state.key === cacheKey ? state.retryAttempt : 0;

  useEffect(() => {
    const currentLoad = loadRef.current;
    if (!cacheKey || !currentLoad) {
      return;
    }
    let active = true;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    void resolveCached(cacheKey, thumbnailUrlCache, thumbnailPendingCache, () =>
      currentLoad(),
    )
      .then((value) => {
        if (active) {
          setState((current) => {
            const nextRetryAttempt =
              current.key === cacheKey ? current.retryAttempt : 0;
            if (
              current.key === cacheKey &&
              nextRetryAttempt === current.retryAttempt &&
              current.url === value
            ) {
              return current;
            }
            return {
              key: cacheKey,
              retryAttempt: nextRetryAttempt,
              url: value,
            };
          });
          if (!value) {
            retryTimer = setTimeout(() => {
              setState((current) => ({
                key: cacheKey,
                retryAttempt:
                  current.key === cacheKey ? current.retryAttempt + 1 : 1,
                url: current.key === cacheKey ? current.url : "",
              }));
            }, retryDelayMs(retryAttempt));
          }
        }
      })
      .catch(() => {
        if (active) {
          setState((current) => {
            const nextRetryAttempt =
              current.key === cacheKey ? current.retryAttempt : 0;
            if (
              current.key === cacheKey &&
              nextRetryAttempt === current.retryAttempt &&
              current.url === ""
            ) {
              return current;
            }
            return {
              key: cacheKey,
              retryAttempt: nextRetryAttempt,
              url: "",
            };
          });
          retryTimer = setTimeout(() => {
            setState((current) => ({
              key: cacheKey,
              retryAttempt:
                current.key === cacheKey ? current.retryAttempt + 1 : 1,
              url: "",
            }));
          }, retryDelayMs(retryAttempt));
        }
      });
    return () => {
      active = false;
      if (retryTimer) {
        clearTimeout(retryTimer);
      }
    };
  }, [cacheKey, retryAttempt]);

  if (!cacheKey) {
    return "";
  }

  if (state.key === cacheKey) {
    return state.url;
  }

  return cachedUrl;
}

export function useThumbnailUrl(thumb?: ArtworkRef | null) {
  const cacheKey = thumbnailCacheKey(thumb);
  return useResolvedUrl(
    cacheKey,
    thumb ? () => resolveThumbnailURL(thumb) : undefined,
  );
}

