import * as CacheFacade from "../../../bindings/ben/desktop/cachefacade";
import { DEFAULT_PAGE_SIZE, Types } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export function getCacheOverview() {
  return traceWailsCall("cache", "get_cache_overview", undefined, () =>
    CacheFacade.GetCacheOverview(),
  );
}

export function listCacheEntries(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return traceWailsCall("cache", "list_cache_entries", { offset, limit }, () =>
    CacheFacade.ListCacheEntries(
      new Types.CacheEntryListRequest({
        Limit: limit,
        Offset: offset,
      }),
    ),
  );
}

export function cleanupCache(
  req: InstanceType<typeof Types.CacheCleanupRequest>,
) {
  return traceWailsCall("cache", "cleanup_cache", {}, () =>
    CacheFacade.CleanupCache(req),
  );
}
