import { useCallback, useEffect, useMemo, useState } from "react";
import type {
  CacheEntryItem,
  CacheOverview,
  LibrarySummary,
  LocalContext,
  PageInfo,
} from "@/lib/api/models";
import {
  cleanupCache,
  getCacheOverview,
  listCacheEntries,
} from "@/lib/api/cache";
import { getActiveLibrary } from "@/lib/api/library";
import { getLocalContext } from "@/lib/api/network";
import { Types } from "@/lib/api/models";
import { formatBytes } from "@/lib/format";

const pageSize = 80;

type CacheState = {
  entries: CacheEntryItem[];
  error: string;
  library: LibrarySummary | null;
  loading: boolean;
  local: LocalContext | null;
  overview: CacheOverview | null;
  page: PageInfo | null;
};

const initialState: CacheState = {
  entries: [],
  error: "",
  library: null,
  loading: true,
  local: null,
  overview: null,
  page: null,
};

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export function entryTarget(entry: CacheEntryItem) {
  if (entry.RecordingID) {
    return `Recording ${entry.RecordingID}`;
  }
  if (entry.AlbumID) {
    return `Album ${entry.AlbumID}`;
  }
  if (entry.PlaylistID) {
    return `Playlist ${entry.PlaylistID}`;
  }
  if (entry.ThumbnailScope && entry.ThumbnailScopeID) {
    return `${entry.ThumbnailScope} artwork ${entry.ThumbnailScopeID}`;
  }
  return "No linked entity";
}

export function useCachePage() {
  const [state, setState] = useState<CacheState>(initialState);
  const [offset, setOffset] = useState(0);
  const [pendingAction, setPendingAction] = useState("");
  const [feedback, setFeedback] = useState("");
  const [actionError, setActionError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const [{ library, found }, local] = await Promise.all([
        getActiveLibrary(),
        getLocalContext(),
      ]);
      if (!found || !library.LibraryID) {
        setState({
          entries: [],
          error: "",
          library: null,
          loading: false,
          local,
          overview: null,
          page: null,
        });
        return;
      }

      const [overview, page] = await Promise.all([
        getCacheOverview(),
        listCacheEntries(offset, pageSize),
      ]);

      setState({
        entries: page.Items,
        error: "",
        library,
        loading: false,
        local,
        overview,
        page: page.Page,
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        error: describeError(error),
        loading: false,
      }));
    }
  }, [offset]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const runCleanup = useCallback(
    async (
      key: string,
      request: InstanceType<typeof Types.CacheCleanupRequest>,
      success: string,
    ) => {
      setPendingAction(key);
      setActionError("");
      setFeedback("");
      try {
        const result = await cleanupCache(request);
        setFeedback(
          `${success}: removed ${result.DeletedBlobs.length} blob(s), ${formatBytes(
            result.DeletedBytes,
          )}`,
        );
        await refresh();
      } catch (error) {
        setActionError(describeError(error));
      } finally {
        setPendingAction("");
      }
    },
    [refresh],
  );

  const byKind = useMemo(() => state.overview?.ByKind ?? [], [state.overview]);

  return {
    actionError,
    byKind,
    feedback,
    offset,
    pageSize,
    pendingAction,
    refresh,
    runCleanup,
    setOffset,
    state,
  };
}


