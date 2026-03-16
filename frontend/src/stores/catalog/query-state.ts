import {
  getDetailRecord,
  getIdQuery,
  getValueQuery,
  type CatalogStore,
} from "@/stores/catalog/store";

type EntityQuerySnapshot<TEntity> = {
  error: string;
  hasMore: boolean;
  isLoading: boolean;
  isRefreshing: boolean;
  items: TEntity[];
  pageInfo: ReturnType<typeof getIdQuery>["pageInfo"];
  status: "idle" | "loading" | "success" | "error";
};

const entityQuerySnapshotCache = new WeakMap<
  CatalogStore,
  Map<string, EntityQuerySnapshot<unknown>>
>();

function isInitialLoading(status: string, hasData: boolean) {
  return status === "loading" && !hasData;
}

function isRefreshingState(isRefreshing: boolean, hasData: boolean) {
  return isRefreshing && hasData;
}

function getCachedEntityQuerySnapshot<TEntity>(
  state: CatalogStore,
  key: string,
) {
  return entityQuerySnapshotCache.get(state)?.get(key) as
    | EntityQuerySnapshot<TEntity>
    | undefined;
}

function setCachedEntityQuerySnapshot<TEntity>(
  state: CatalogStore,
  key: string,
  snapshot: EntityQuerySnapshot<TEntity>,
) {
  const cache = entityQuerySnapshotCache.get(state) ?? new Map();
  cache.set(key, snapshot as EntityQuerySnapshot<unknown>);
  entityQuerySnapshotCache.set(state, cache);
}

export function selectEntityQuery<TEntity>(
  state: CatalogStore,
  key: string,
  lookup: (state: CatalogStore, id: string) => TEntity | undefined,
) {
  const cached = getCachedEntityQuerySnapshot<TEntity>(state, key);
  if (cached) {
    return cached;
  }

  const record = getIdQuery(state, key);
  const items: TEntity[] = [];
  for (const id of record.ids) {
    const entity = lookup(state, id);
    if (entity) {
      items.push(entity);
    }
  }

  const snapshot = {
    error: record.error,
    hasMore: Boolean(record.pageInfo?.HasMore),
    isLoading: isInitialLoading(record.status, items.length > 0),
    isRefreshing: isRefreshingState(record.isRefreshing, items.length > 0),
    items,
    pageInfo: record.pageInfo,
    status: record.status,
  };

  setCachedEntityQuerySnapshot(state, key, snapshot);

  return snapshot;
}

export function selectValueQuery<TEntity>(state: CatalogStore, key: string) {
  const record = getValueQuery<TEntity>(state, key);
  return {
    error: record.error,
    hasMore: Boolean(record.pageInfo?.HasMore),
    isLoading: isInitialLoading(record.status, record.items.length > 0),
    isRefreshing: isRefreshingState(
      record.isRefreshing,
      record.items.length > 0,
    ),
    items: record.items,
    pageInfo: record.pageInfo,
    status: record.status,
  };
}

export function selectDetail<TEntity>(
  record: ReturnType<typeof getDetailRecord<TEntity>>,
) {
  return {
    data: record.data,
    error: record.error,
    isLoading: isInitialLoading(record.status, record.data !== null),
    isRefreshing: isRefreshingState(record.isRefreshing, record.data !== null),
    status: record.status,
  };
}
