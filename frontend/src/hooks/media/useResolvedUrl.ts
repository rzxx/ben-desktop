import { useEffect, useRef, useState } from "react";

const resolvedUrlCache = new Map<string, string>();
const pendingResolvedUrlCache = new Map<string, Promise<string>>();
const retryDelaysMs = [1000, 2500, 5000, 10000, 15000];

async function resolveCached(key: string, load: () => Promise<string>) {
  if (resolvedUrlCache.has(key)) {
    return resolvedUrlCache.get(key) ?? "";
  }
  if (!pendingResolvedUrlCache.has(key)) {
    pendingResolvedUrlCache.set(
      key,
      load()
        .then((value) => {
          if (value) {
            resolvedUrlCache.set(key, value);
          }
          pendingResolvedUrlCache.delete(key);
          return value;
        })
        .catch((error) => {
          pendingResolvedUrlCache.delete(key);
          throw error;
        }),
    );
  }
  return pendingResolvedUrlCache.get(key) ?? Promise.resolve("");
}

function retryDelayMs(attempt: number) {
  return retryDelaysMs[Math.min(attempt, retryDelaysMs.length - 1)];
}

export function useResolvedUrl(cacheKey: string, load?: () => Promise<string>) {
  const [state, setState] = useState({
    key: "",
    retryAttempt: 0,
    url: "",
  });
  const loadRef = useRef(load);

  useEffect(() => {
    loadRef.current = load;
  }, [load]);

  const cachedUrl = cacheKey ? (resolvedUrlCache.get(cacheKey) ?? "") : "";
  const retryAttempt = state.key === cacheKey ? state.retryAttempt : 0;

  useEffect(() => {
    const currentLoad = loadRef.current;
    if (!cacheKey || !currentLoad) {
      return;
    }

    let active = true;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    void resolveCached(cacheKey, currentLoad)
      .then((value) => {
        if (!active) {
          return;
        }

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
      })
      .catch(() => {
        if (!active) {
          return;
        }

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
