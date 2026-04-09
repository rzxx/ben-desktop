import type { ReactNode } from "react";
import {
  HardDrive,
  Image,
  Music4,
  Pin,
  RefreshCw,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { Types } from "@/lib/api/models";
import { formatBytes, formatRelativeDate } from "@/lib/format";
import { entryTarget, useCachePage } from "@/hooks/cache/useCachePage";
import { formatDateTime } from "@/lib/format";

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

export function CacheSurface() {
  const {
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
  } = useCachePage();

  if (state.loading) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center">
        <div className="border-theme-300/70 text-theme-900 rounded-[1.4rem] border bg-white/82 px-5 py-4 text-sm dark:border-white/8 dark:bg-black/15 dark:text-white/65">
          Loading cache state...
        </div>
      </div>
    );
  }

  return (
    <div className="ben-scrollbar ben-shell-scroll-offset flex h-full min-h-0 flex-col gap-4 overflow-y-auto pr-1">
      <section className="border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(140deg,rgba(14,165,233,0.14),transparent_38%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6 dark:border-white/8">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-theme-500 text-[0.68rem] tracking-[0.35em] uppercase dark:text-white/35">
              Cache
            </p>
            <h1 className="text-theme-900 mt-3 text-3xl font-semibold dark:text-white">
              Local cache management
            </h1>
            <p className="text-theme-600 mt-3 max-w-3xl text-sm leading-6 dark:text-white/55">
              Inspect optimized audio and artwork blobs, see what stays pinned,
              and run reclaim actions against the desktop-core cache service.
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              <span className="border-theme-300/75 bg-theme-100 text-theme-500 rounded-full border px-3 py-1 text-xs tracking-[0.2em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/52">
                {state.library
                  ? `${state.library.Name} • ${state.library.Role}`
                  : "No active library"}
              </span>
              <span className="border-theme-300/75 bg-theme-100 text-theme-500 rounded-full border px-3 py-1 text-xs tracking-[0.2em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/52">
                {state.local?.Device || "Unknown device"}
              </span>
            </div>
          </div>
          <button
            className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
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
            <div className="rounded-[1.25rem] border border-amber-400/18 bg-amber-400/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-100">
              {state.error}
            </div>
          )}
          {actionError && (
            <div className="rounded-[1.25rem] border border-rose-400/18 bg-rose-400/10 px-4 py-3 text-sm text-rose-700 dark:text-rose-100">
              {actionError}
            </div>
          )}
          {feedback && (
            <div className="rounded-[1.25rem] border border-emerald-400/18 bg-emerald-400/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-100">
              {feedback}
            </div>
          )}
        </section>
      )}

      {!state.library || !state.overview ? (
        <section className="border-theme-300/75 rounded-[1.6rem] border border-dashed bg-white/78 px-8 py-10 text-center dark:border-white/10 dark:bg-black/10">
          <div className="border-theme-300/75 bg-theme-100 text-theme-400 mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border dark:border-white/10 dark:bg-white/5 dark:text-white/40">
            <ShieldAlert className="h-5 w-5" />
          </div>
          <h2 className="text-theme-900 text-lg font-semibold dark:text-white/90">
            No active library
          </h2>
          <p className="text-theme-500 mx-auto mt-2 max-w-md text-sm dark:text-white/50">
            Select or join a library before inspecting the local cache.
          </p>
        </section>
      ) : (
        <>
          <section className="grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
            <div className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
              <div className="flex items-center gap-3">
                <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                  <HardDrive className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                    Overview
                  </h2>
                  <p className="text-theme-600 text-sm dark:text-white/48">
                    Capacity, pinned usage, and reclaimable bytes for this
                    device.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <StatCard
                  body={`${state.overview.EntryCount} entries`}
                  title="Used"
                >
                  {formatBytes(state.overview.UsedBytes)}
                </StatCard>
                <StatCard
                  body={`${formatBytes(state.overview.FreeBytes)} free`}
                  title="Limit"
                >
                  {formatBytes(state.overview.LimitBytes)}
                </StatCard>
                <StatCard
                  body={`${state.overview.PinnedEntries} entries`}
                  title="Pinned"
                >
                  {formatBytes(state.overview.PinnedBytes)}
                </StatCard>
                <StatCard
                  body={`${state.overview.UnpinnedEntries} unpinned`}
                  title="Reclaimable"
                >
                  {formatBytes(state.overview.ReclaimableBytes)}
                </StatCard>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-3">
                {byKind.map((row) => {
                  const Icon = kindIcon(row.Kind);
                  return (
                    <div
                      className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10"
                      key={row.Kind}
                    >
                      <div className="text-theme-700 flex items-center gap-2 dark:text-white/72">
                        <Icon className="h-4 w-4" />
                        <p className="text-theme-900 text-sm font-semibold dark:text-white">
                          {kindLabel(row.Kind)}
                        </p>
                      </div>
                      <p className="text-theme-900 mt-3 text-lg font-semibold dark:text-white">
                        {formatBytes(row.Bytes)}
                      </p>
                      <p className="text-theme-600 mt-2 text-sm dark:text-white/55">
                        {row.Entries} entries, {formatBytes(row.PinnedBytes)}{" "}
                        pinned
                      </p>
                    </div>
                  );
                })}
              </div>
            </div>

            <div className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
              <div className="flex items-center gap-3">
                <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                  <Trash2 className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                    Cleanup actions
                  </h2>
                  <p className="text-theme-600 text-sm dark:text-white/48">
                    Remove unpinned blobs while preserving pinned offline assets
                    and artwork references.
                  </p>
                </div>
              </div>

              <div className="mt-5 flex flex-wrap gap-3">
                <button
                  className="border-theme-900 bg-theme-900 text-theme-50 hover:bg-theme-800 inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-500 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
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
                  className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
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

              <div className="text-theme-600 mt-5 space-y-2 text-sm dark:text-white/48">
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

          <section className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                  Pinned scopes
                </h2>
                <p className="text-theme-600 text-sm dark:text-white/48">
                  Durable scopes that currently keep blobs retained.
                </p>
              </div>
              <span className="border-theme-300/75 bg-theme-100 text-theme-500 rounded-full border px-3 py-1 text-xs tracking-[0.2em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/52">
                {(state.overview.PinScopes ?? []).length} scope
                {(state.overview.PinScopes ?? []).length === 1 ? "" : "s"}
              </span>
            </div>

            <div className="mt-5 flex flex-wrap gap-3">
              {(state.overview.PinScopes ?? []).length === 0 ? (
                <div className="border-theme-300/75 text-theme-600 rounded-[1.2rem] border border-dashed bg-white/78 px-4 py-5 text-sm dark:border-white/10 dark:bg-black/10 dark:text-white/48">
                  No durable pin scopes recorded.
                </div>
              ) : (
                state.overview.PinScopes.map((scope) => (
                  <div
                    className="border-theme-300/70 rounded-[1.1rem] border bg-white/78 px-4 py-3 dark:border-white/8 dark:bg-black/10"
                    key={`${scope.Scope}:${scope.ScopeID}`}
                  >
                    <div className="text-theme-700 flex items-center gap-2 dark:text-white/72">
                      <Pin className="h-4 w-4" />
                      <span className="text-theme-900 text-sm font-semibold dark:text-white">
                        {scope.Scope}
                      </span>
                    </div>
                    <p className="text-theme-500 mt-2 font-mono text-xs dark:text-white/42">
                      {scope.ScopeID}
                    </p>
                    <p className="text-theme-600 mt-2 text-sm dark:text-white/55">
                      {scope.BlobCount} blob(s), {formatBytes(scope.Bytes)}
                    </p>
                  </div>
                ))
              )}
            </div>
          </section>

          <section className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                  Cache entries
                </h2>
                <p className="text-theme-600 text-sm dark:text-white/48">
                  Current page of retained blobs for this device.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button
                  className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                  disabled={offset <= 0}
                  onClick={() => {
                    setOffset((current) => Math.max(0, current - pageSize));
                  }}
                  type="button"
                >
                  Previous
                </button>
                <button
                  className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
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
                <div className="border-theme-300/75 text-theme-600 rounded-[1.2rem] border border-dashed bg-white/78 px-4 py-5 text-sm dark:border-white/10 dark:bg-black/10 dark:text-white/48">
                  No cache entries on this page.
                </div>
              ) : (
                state.entries.map((entry) => {
                  const Icon = kindIcon(entry.Kind);
                  return (
                    <div
                      className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10"
                      key={entry.BlobID}
                    >
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-8 w-8 items-center justify-center rounded-full border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                              <Icon className="h-4 w-4" />
                            </span>
                            <p className="text-theme-900 text-sm font-semibold dark:text-white">
                              {kindLabel(entry.Kind)}
                            </p>
                            <span
                              className={`rounded-full border px-2 py-1 text-[0.68rem] tracking-[0.22em] uppercase ${
                                entry.Pinned
                                  ? "border-emerald-400/18 bg-emerald-400/10 text-emerald-700 dark:text-emerald-100"
                                  : "border-theme-300/75 bg-theme-100 text-theme-600 dark:border-white/10 dark:bg-white/5 dark:text-white/55"
                              }`}
                            >
                              {entry.Pinned ? "Pinned" : "Unpinned"}
                            </span>
                          </div>
                          <p className="text-theme-500 mt-2 font-mono text-xs break-all dark:text-white/42">
                            {entry.BlobID}
                          </p>
                          <div className="text-theme-600 mt-3 flex flex-wrap gap-3 text-sm dark:text-white/55">
                            <span>{formatBytes(entry.SizeBytes)}</span>
                            <span>{entryTarget(entry)}</span>
                            <span>{formatDateTime(entry.LastAccessed)}</span>
                          </div>
                          {entry.PinScopes.length > 0 && (
                            <div className="mt-3 flex flex-wrap gap-2">
                              {entry.PinScopes.map((scope) => (
                                <span
                                  className="border-theme-300/75 bg-theme-100 text-theme-500 rounded-full border px-3 py-1 text-xs tracking-[0.18em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/52"
                                  key={`${entry.BlobID}:${scope.Scope}:${scope.ScopeID}`}
                                >
                                  {scope.Scope}:{scope.ScopeID}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>

                        <button
                          className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                          disabled={
                            entry.Pinned ||
                            pendingAction === `delete:${entry.BlobID}`
                          }
                          onClick={() => {
                            void runCleanup(
                              `delete:${entry.BlobID}`,
                              new Types.CacheCleanupRequest({
                                BlobIDs: [entry.BlobID],
                                Mode: Types.CacheCleanupMode
                                  .CacheCleanupBlobIDs,
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
              <p className="text-theme-600 mt-4 text-sm dark:text-white/48">
                Showing {state.page.Offset + 1}-
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

function StatCard({
  body,
  children,
  title,
}: {
  body: string;
  children: ReactNode;
  title: string;
}) {
  return (
    <div className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10">
      <p className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
        {title}
      </p>
      <p className="text-theme-900 mt-2 text-lg font-semibold dark:text-white">
        {children}
      </p>
      <p className="text-theme-600 mt-2 text-sm dark:text-white/55">{body}</p>
    </div>
  );
}
