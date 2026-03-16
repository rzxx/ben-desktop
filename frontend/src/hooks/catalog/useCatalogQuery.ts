import { useCallback } from "react";
import type { PageInfo } from "@/lib/api";
import { useShallow } from "zustand/react/shallow";
import { useCatalogStore, type CatalogStore } from "@/stores/catalog/store";

type QuerySnapshot<TData> = {
  data: TData;
  error: string;
  isLoading: boolean;
  isRefreshing: boolean;
  status: "idle" | "loading" | "success" | "error";
};

type InfiniteSnapshot<TItem> = {
  error: string;
  hasMore: boolean;
  isLoading: boolean;
  isRefreshing: boolean;
  items: TItem[];
  pageInfo: PageInfo | null;
  status: "idle" | "loading" | "success" | "error";
};

export function useStoreQuery<TData>(
  select: (state: CatalogStore) => QuerySnapshot<TData>,
  refetchAction: () => Promise<void> | void,
) {
  const snapshot = useCatalogStore(useShallow(select));
  const refetch = useCallback(async () => {
    await refetchAction();
  }, [refetchAction]);

  return {
    ...snapshot,
    refetch,
  };
}

export function useStoreInfiniteQuery<TItem>(
  select: (state: CatalogStore) => InfiniteSnapshot<TItem>,
  options: {
    fetchNextPage: () => Promise<void> | void;
    refetch: () => Promise<void> | void;
  },
) {
  const snapshot = useCatalogStore(useShallow(select));
  const fetchNextPage = useCallback(async () => {
    await options.fetchNextPage();
  }, [options]);
  const refetch = useCallback(async () => {
    await options.refetch();
  }, [options]);

  return {
    ...snapshot,
    fetchNextPage,
    refetch,
  };
}


