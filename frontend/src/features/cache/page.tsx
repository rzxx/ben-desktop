import { useCallback, useEffect, useMemo, useState } from "react";
import {
  HardDrive,
  Image,
  Music4,
  Pin,
  RefreshCw,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import {
  type CacheEntryItem,
  type CacheOverview,
  type LibrarySummary,
  type LocalContext,
  type PageInfo,
  Types,
  cleanupCache,
  getActiveLibrary,
  getCacheOverview,
  getLocalContext,
  listCacheEntries,
} from "../../shared/lib/desktop";
import { formatBytes, formatRelativeDate } from "../../shared/lib/format";

const pageSize = 80;

type CacheState = {
  loading: boolean;
  library: LibrarySummary | null;
  local: LocalContext | null;
  overview: CacheOverview | null;
  entries: CacheEntryItem[];
  page: PageInfo | null;
  error: string;
};

const initialState: CacheState = {
  loading: true,
  library: null,
  local: null,
  overview: null,
  entries: [],
  page: null,
  error: "",
};

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function formatDateTime(value?: Date | string | null) {
  if (!value) {
    return "No activity";
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "No activity";
  }
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function kindLabel(kind: string) {
  switch (kind) {
    case "optimized_audio":
      return "Optimized audio";
    case "thumbnail":
      return "Thumbnail";
    default:
      return "Unknown";
  }
}

function kindIcon(kind: string) {
  switch (kind) {
    case "optimized_audio":
      return Music4;
    case "thumbnail":
      return Image;
    default:
      return HardDrive;
  }
}

function entryTarget(entry: CacheEntryItem) {
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

export function CachePage() {
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
          loading: false,
          library: null,
          local,
          overview: null,
          entries: [],
          page: null,
          error: "",
        });
        return;
      }

      const [overview, page] = await Promise.all([
        getCacheOverview(),
        listCacheEntries(offset, pageSize),
      ]);

      setState({
        loading: false,
        library,
        local,
        overview,
        entries: page.Items,
        page: page.Page,
        error: "",
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        loading: false,
        error: describeError(error),
      }));
    }
  }, [offset]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const runCleanup = useCallback(
    async (
      key: string,
      request: Types.CacheCleanupRequest,
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

  const byKind = useMemo(() => {
    return state.overview?.ByKind ?? [];
  }, [state.overview]);

  if (state.loading) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center">
        <div className="rounded-[1.4rem] border border-white/8 bg-black/15 px-5 py-4 text-sm text-white/65">
          Loading cache state...
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto pr-1">
      <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(140deg,rgba(14,165,233,0.14),transparent_38%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
              Cache
            </p>
            <h1 className="mt-3 text-3xl font-semibold text-white">
              Local cache management
            </h1>
            <p className="mt-3 max-w-3xl text-sm leading-6 text-white/55">
              Inspect optimized audio and artwork blobs, see what stays pinned,
              and run reclaim actions against the desktop-core cache service.
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {state.library
                  ? `${state.library.Name} • ${state.library.Role}`
                  : "No active library"}
              </span>
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {state.local?.Device || "Unknown device"}
              </span>
            </div>
          </div>
          <button
            className="action-button"
            onClick={() => {
              void refresh();
            }}
            type="button"
          >
            <RefreshCw className="h-4 w-4" />
            <span>Refresh</span>
          </button>
        </div>
      </section>

      {(state.error || actionError || feedback) && (
        <section className="grid gap-3 xl:grid-cols-2">
          {state.error && (
            <div className="rounded-[1.25rem] border border-amber-400/18 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">
              {state.error}
            </div>
          )}
          {actionError && (
            <div className="rounded-[1.25rem] border border-rose-400/18 bg-rose-400/10 px-4 py-3 text-sm text-rose-100">
              {actionError}
            </div>
          )}
          {feedback && (
            <div className="rounded-[1.25rem] border border-emerald-400/18 bg-emerald-400/10 px-4 py-3 text-sm text-emerald-100">
              {feedback}
            </div>
          )}
        </section>
      )}

      {!state.library || !state.overview ? (
        <section className="rounded-[1.6rem] border border-dashed border-white/10 bg-black/10 px-8 py-10 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/40">
            <ShieldAlert className="h-5 w-5" />
          </div>
          <h2 className="text-lg font-semibold text-white/90">
            No active library
          </h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-white/50">
            Select or join a library before inspecting the local cache.
          </p>
        </section>
      ) : (
        <>
          <section className="grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <HardDrive className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">Overview</h2>
                  <p className="text-sm text-white/48">
                    Capacity, pinned usage, and reclaimable bytes for this
                    device.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.24em] text-white/35 uppercase">
                    Used
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {formatBytes(state.overview.UsedBytes)}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.overview.EntryCount} entries
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.24em] text-white/35 uppercase">
                    Limit
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {formatBytes(state.overview.LimitBytes)}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {formatBytes(state.overview.FreeBytes)} free
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.24em] text-white/35 uppercase">
                    Pinned
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {formatBytes(state.overview.PinnedBytes)}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.overview.PinnedEntries} entries
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.24em] text-white/35 uppercase">
                    Reclaimable
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {formatBytes(state.overview.ReclaimableBytes)}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.overview.UnpinnedEntries} unpinned
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-3">
                {byKind.map((row) => {
                  const Icon = kindIcon(row.Kind);
                  return (
                    <div
                      className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4"
                      key={row.Kind}
                    >
                      <div className="flex items-center gap-2 text-white/72">
                        <Icon className="h-4 w-4" />
                        <p className="text-sm font-semibold text-white">
                          {kindLabel(row.Kind)}
                        </p>
                      </div>
                      <p className="mt-3 text-lg font-semibold text-white">
                        {formatBytes(row.Bytes)}
                      </p>
                      <p className="mt-2 text-sm text-white/55">
                        {row.Entries} entries, {formatBytes(row.PinnedBytes)}{" "}
                        pinned
                      </p>
                    </div>
                  );
                })}
              </div>
            </div>

            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <Trash2 className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Cleanup actions
                  </h2>
                  <p className="text-sm text-white/48">
                    Remove unpinned blobs while preserving pinned offline assets
                    and artwork references.
                  </p>
                </div>
              </div>

              <div className="mt-5 flex flex-wrap gap-3">
                <button
                  className="action-button is-primary"
                  disabled={pendingAction === "cleanup-over-limit"}
                  onClick={() => {
                    void runCleanup(
                      "cleanup-over-limit",
                      new Types.CacheCleanupRequest({
                        Mode: Types.CacheCleanupMode.CacheCleanupOverLimitOnly,
                      }),
                      "Over-limit cleanup complete",
                    );
                  }}
                  type="button"
                >
                  <Trash2 className="h-4 w-4" />
                  <span>Trim over limit</span>
                </button>
                <button
                  className="action-button"
                  disabled={pendingAction === "cleanup-unpinned"}
                  onClick={() => {
                    void runCleanup(
                      "cleanup-unpinned",
                      new Types.CacheCleanupRequest({
                        Mode: Types.CacheCleanupMode.CacheCleanupAllUnpinned,
                      }),
                      "Unpinned cleanup complete",
                    );
                  }}
                  type="button"
                >
                  <Trash2 className="h-4 w-4" />
                  <span>Delete all unpinned</span>
                </button>
              </div>

              <div className="mt-5 space-y-2 text-sm text-white/48">
                <p>
                  Artwork blobs stay pinned while any artwork variant references
                  them.
                </p>
                <p>
                  Optimized audio becomes reclaimable only when the local cache
                  row is unpinned.
                </p>
              </div>
            </div>
          </section>

          <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-lg font-semibold text-white">
                  Pinned scopes
                </h2>
                <p className="text-sm text-white/48">
                  Durable scopes that currently keep blobs retained.
                </p>
              </div>
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {(state.overview.PinScopes ?? []).length} scope
                {(state.overview.PinScopes ?? []).length === 1 ? "" : "s"}
              </span>
            </div>

            <div className="mt-5 flex flex-wrap gap-3">
              {(state.overview.PinScopes ?? []).length === 0 ? (
                <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-black/10 px-4 py-5 text-sm text-white/48">
                  No durable pin scopes recorded.
                </div>
              ) : (
                state.overview.PinScopes.map((scope) => (
                  <div
                    className="rounded-[1.1rem] border border-white/8 bg-black/10 px-4 py-3"
                    key={`${scope.Scope}:${scope.ScopeID}`}
                  >
                    <div className="flex items-center gap-2 text-white/72">
                      <Pin className="h-4 w-4" />
                      <span className="text-sm font-semibold text-white">
                        {scope.Scope}
                      </span>
                    </div>
                    <p className="mt-2 font-mono text-xs text-white/42">
                      {scope.ScopeID}
                    </p>
                    <p className="mt-2 text-sm text-white/55">
                      {scope.BlobCount} blob(s), {formatBytes(scope.Bytes)}
                    </p>
                  </div>
                ))
              )}
            </div>
          </section>

          <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-lg font-semibold text-white">
                  Cache entries
                </h2>
                <p className="text-sm text-white/48">
                  Current page of retained blobs for this device.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button
                  className="action-button"
                  disabled={offset <= 0}
                  onClick={() => {
                    setOffset((current) => Math.max(0, current - pageSize));
                  }}
                  type="button"
                >
                  Previous
                </button>
                <button
                  className="action-button"
                  disabled={!state.page?.HasMore}
                  onClick={() => {
                    setOffset(state.page?.NextOffset ?? offset);
                  }}
                  type="button"
                >
                  Next
                </button>
              </div>
            </div>

            <div className="mt-5 space-y-3">
              {state.entries.length === 0 ? (
                <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-black/10 px-4 py-5 text-sm text-white/48">
                  No cache entries on this page.
                </div>
              ) : (
                state.entries.map((entry) => {
                  const Icon = kindIcon(entry.Kind);
                  return (
                    <div
                      className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4"
                      key={entry.BlobID}
                    >
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="flex h-8 w-8 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/72">
                              <Icon className="h-4 w-4" />
                            </span>
                            <p className="text-sm font-semibold text-white">
                              {kindLabel(entry.Kind)}
                            </p>
                            <span
                              className={`rounded-full border px-2 py-1 text-[0.68rem] tracking-[0.22em] uppercase ${
                                entry.Pinned
                                  ? "border-emerald-400/18 bg-emerald-400/10 text-emerald-100"
                                  : "border-white/10 bg-white/5 text-white/55"
                              }`}
                            >
                              {entry.Pinned ? "Pinned" : "Unpinned"}
                            </span>
                          </div>
                          <p className="mt-2 font-mono text-xs break-all text-white/42">
                            {entry.BlobID}
                          </p>
                          <div className="mt-3 flex flex-wrap gap-3 text-sm text-white/55">
                            <span>{formatBytes(entry.SizeBytes)}</span>
                            <span>{entryTarget(entry)}</span>
                            <span>{formatDateTime(entry.LastAccessed)}</span>
                          </div>
                          {entry.PinScopes.length > 0 && (
                            <div className="mt-3 flex flex-wrap gap-2">
                              {entry.PinScopes.map((scope) => (
                                <span
                                  className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.18em] text-white/52 uppercase"
                                  key={`${entry.BlobID}:${scope.Scope}:${scope.ScopeID}`}
                                >
                                  {scope.Scope}:{scope.ScopeID}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>

                        <button
                          className="action-button"
                          disabled={
                            entry.Pinned ||
                            pendingAction === `delete:${entry.BlobID}`
                          }
                          onClick={() => {
                            void runCleanup(
                              `delete:${entry.BlobID}`,
                              new Types.CacheCleanupRequest({
                                Mode: Types.CacheCleanupMode
                                  .CacheCleanupBlobIDs,
                                BlobIDs: [entry.BlobID],
                              }),
                              "Blob cleanup complete",
                            );
                          }}
                          type="button"
                        >
                          <Trash2 className="h-4 w-4" />
                          <span>Delete blob</span>
                        </button>
                      </div>
                    </div>
                  );
                })
              )}
            </div>

            {state.page && (
              <p className="mt-4 text-sm text-white/48">
                Showing {state.page.Offset + 1}
                {"-"}
                {state.page.Offset + state.page.Returned} of {state.page.Total}.
                Refreshed {formatRelativeDate(new Date())}.
              </p>
            )}
          </section>
        </>
      )}
    </div>
  );
}
