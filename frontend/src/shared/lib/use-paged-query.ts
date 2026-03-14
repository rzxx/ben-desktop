import {
  startTransition,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import type { PageInfo } from "./desktop";

type PageResult<T> = {
  Items: T[];
  Page: PageInfo;
};

type QueryState<T> = {
  items: T[];
  pageInfo: PageInfo | null;
  loading: boolean;
  loadingMore: boolean;
  error: string;
};

type Options<T> = {
  key: string;
  fetchPage: (offset: number) => Promise<PageResult<T>>;
};

export function usePagedQuery<T>({ key, fetchPage }: Options<T>) {
  const [state, setState] = useState<QueryState<T>>({
    items: [],
    pageInfo: null,
    loading: true,
    loadingMore: false,
    error: "",
  });
  const requestKeyRef = useRef(key);
  const inFlightOffsetRef = useRef<number | null>(null);

  useEffect(() => {
    requestKeyRef.current = key;
  }, [key]);

  useEffect(() => {
    let active = true;
    const requestKey = key;
    requestKeyRef.current = requestKey;
    inFlightOffsetRef.current = 0;

    startTransition(() => {
      setState({
        items: [],
        pageInfo: null,
        loading: true,
        loadingMore: false,
        error: "",
      });
    });

    void fetchPage(0)
      .then((page) => {
        if (!active || requestKeyRef.current !== requestKey) {
          return;
        }
        startTransition(() => {
          setState({
            items: page.Items,
            pageInfo: page.Page,
            loading: false,
            loadingMore: false,
            error: "",
          });
        });
      })
      .catch((error: unknown) => {
        if (!active || requestKeyRef.current !== requestKey) {
          return;
        }
        startTransition(() => {
          setState((current) => ({
            ...current,
            loading: false,
            loadingMore: false,
            error: error instanceof Error ? error.message : String(error),
          }));
        });
      })
      .finally(() => {
        if (
          requestKeyRef.current === requestKey &&
          inFlightOffsetRef.current === 0
        ) {
          inFlightOffsetRef.current = null;
        }
      });

    return () => {
      active = false;
    };
  }, [fetchPage, key]);

  const loadMore = useCallback(() => {
    if (state.loading || state.loadingMore || !state.pageInfo?.HasMore) {
      return;
    }

    const nextOffset = state.pageInfo.NextOffset;
    const requestKey = key;
    if (inFlightOffsetRef.current === nextOffset) {
      return;
    }
    inFlightOffsetRef.current = nextOffset;

    startTransition(() => {
      setState((current) => ({
        ...current,
        loadingMore: true,
      }));
    });

    void fetchPage(nextOffset)
      .then((page) => {
        if (requestKeyRef.current !== requestKey) {
          return;
        }
        startTransition(() => {
          setState((current) => ({
            items: [...current.items, ...page.Items],
            pageInfo: page.Page,
            loading: false,
            loadingMore: false,
            error: "",
          }));
        });
      })
      .catch((error: unknown) => {
        if (requestKeyRef.current !== requestKey) {
          return;
        }
        startTransition(() => {
          setState((current) => ({
            ...current,
            loading: false,
            loadingMore: false,
            error: error instanceof Error ? error.message : String(error),
          }));
        });
      })
      .finally(() => {
        if (
          requestKeyRef.current === requestKey &&
          inFlightOffsetRef.current === nextOffset
        ) {
          inFlightOffsetRef.current = null;
        }
      });
  }, [fetchPage, key, state.loading, state.loadingMore, state.pageInfo]);

  return {
    ...state,
    hasMore: Boolean(state.pageInfo?.HasMore),
    loadMore,
  };
}
