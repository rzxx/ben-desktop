import { useEffect, useRef, useState } from "react";

const resolvedUrlCache = new Map<string, string>();
const pendingResolvedUrlCache = new Map<string, Promise<string>>();
const missingResolvedUrlCache = new Set<string>();
const cacheInvalidationListeners = new Set<() => void>();
let resolvedUrlCacheEpoch = 0;

export function invalidateResolvedUrlCache(
  shouldInvalidate: (key: string) => boolean = () => true,
) {
  let changed = false;
  for (const key of Array.from(resolvedUrlCache.keys())) {
    if (shouldInvalidate(key)) {
      resolvedUrlCache.delete(key);
      changed = true;
    }
  }
  for (const key of Array.from(missingResolvedUrlCache.keys())) {
    if (shouldInvalidate(key)) {
      missingResolvedUrlCache.delete(key);
      changed = true;
    }
  }
  for (const key of Array.from(pendingResolvedUrlCache.keys())) {
    if (shouldInvalidate(key)) {
      pendingResolvedUrlCache.delete(key);
      changed = true;
    }
  }
  if (!changed) {
    return;
  }
  resolvedUrlCacheEpoch += 1;
  for (const listener of cacheInvalidationListeners) {
    listener();
  }
}

function subscribeResolvedUrlInvalidations(listener: () => void) {
  cacheInvalidationListeners.add(listener);
  return () => {
    cacheInvalidationListeners.delete(listener);
  };
}

async function resolveCached(key: string, load: () => Promise<string>) {
  if (resolvedUrlCache.has(key)) {
    return resolvedUrlCache.get(key) ?? "";
  }
  if (missingResolvedUrlCache.has(key)) {
    return "";
  }
  if (!pendingResolvedUrlCache.has(key)) {
    const epoch = resolvedUrlCacheEpoch;
    const pending = load()
      .then((value) => {
        if (epoch === resolvedUrlCacheEpoch) {
          if (value) {
            resolvedUrlCache.set(key, value);
          } else {
            missingResolvedUrlCache.add(key);
          }
        }
        return value;
      })
      .catch((error) => {
        if (epoch === resolvedUrlCacheEpoch) {
          missingResolvedUrlCache.add(key);
        }
        throw error;
      })
      .finally(() => {
        if (pendingResolvedUrlCache.get(key) === pending) {
          pendingResolvedUrlCache.delete(key);
        }
      });
    pendingResolvedUrlCache.set(key, pending);
  }
  return pendingResolvedUrlCache.get(key) ?? Promise.resolve("");
}

export function useResolvedUrl(cacheKey: string, load?: () => Promise<string>) {
  const [state, setState] = useState({
    key: "",
    url: "",
  });
  const [cacheRevision, setCacheRevision] = useState(0);
  const loadRef = useRef(load);

  useEffect(() => {
    loadRef.current = load;
  }, [load]);

  useEffect(
    () =>
      subscribeResolvedUrlInvalidations(() =>
        setCacheRevision((value) => value + 1),
      ),
    [],
  );

  const cachedUrl = cacheKey ? (resolvedUrlCache.get(cacheKey) ?? "") : "";

  useEffect(() => {
    const currentLoad = loadRef.current;
    if (!cacheKey || !currentLoad) {
      return;
    }

    let active = true;

    void resolveCached(cacheKey, currentLoad)
      .then((value) => {
        if (!active) {
          return;
        }

        setState((current) => {
          if (current.key === cacheKey && current.url === value) {
            return current;
          }
          return {
            key: cacheKey,
            url: value,
          };
        });
      })
      .catch(() => {
        if (!active) {
          return;
        }

        setState((current) => {
          if (current.key === cacheKey && current.url === "") {
            return current;
          }
          return {
            key: cacheKey,
            url: "",
          };
        });
      });

    return () => {
      active = false;
    };
  }, [cacheKey, cacheRevision]);

  if (!cacheKey) {
    return "";
  }

  if (state.key === cacheKey) {
    return state.url;
  }

  return cachedUrl;
}
