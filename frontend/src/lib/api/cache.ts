import * as CacheFacade from "../../../bindings/ben/desktop/cachefacade";
import { DEFAULT_PAGE_SIZE, Types } from "./models";

export function getCacheOverview() {
  return CacheFacade.GetCacheOverview();
}

export function listCacheEntries(offset = 0, limit = DEFAULT_PAGE_SIZE) {
  return CacheFacade.ListCacheEntries(
    new Types.CacheEntryListRequest({
      Limit: limit,
      Offset: offset,
    }),
  );
}

export function cleanupCache(req: InstanceType<typeof Types.CacheCleanupRequest>) {
  return CacheFacade.CleanupCache(req);
}
