import {
  Activity,
  CircleAlert,
  Clock3,
  FolderOpen,
  HardDrive,
  Minus,
  Plus,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import type { LibraryCheckpointStatus } from "@/lib/api/models";
import { NotificationCard } from "@/components/notifications/NotificationCard";
import {
  startCompactCheckpoint,
  startPublishCheckpoint,
  startSyncNow,
} from "@/lib/api/network";
import { startLibraryRepair } from "@/lib/api/library";
import { formatCount, formatDateTime } from "@/lib/format";
import { isNotificationActive } from "@/lib/notifications";
import { useOperationsPage } from "@/hooks/operations/useOperationsPage";
import { useNotificationsStore } from "@/stores/notifications/store";

function normalizeRole(role: string) {
  return role.trim().toLowerCase();
}

function checkpointSummary(status: LibraryCheckpointStatus | null) {
  if (!status || !status.CheckpointID) {
    return "No published checkpoint";
  }
  return `${status.AckedDevices}/${status.TotalDevices} devices covered`;
}

export function OperationsSurface() {
  const {
    actionError,
    canCheckpoint,
    canScan,
    checkpointNeedsRepublish,
    feedback,
    handleAddRoot,
    handleRemoveRoot,
    maintenance,
    oplogDeviceCounts,
    oplogEntityCounts,
    pendingAction,
    refresh,
    runAction,
    scanPhase,
    state,
  } = useOperationsPage();
  const notifications = useNotificationsStore((store) => store.notifications);
  const activeNotifications = notifications.filter((notification) =>
    isNotificationActive(notification.phase),
  );
  const recentNotifications = notifications.filter(
    (notification) => !isNotificationActive(notification.phase),
  );
  const userNotificationCount = notifications.filter(
    (notification) => notification.audience === "user",
  ).length;
  const systemNotificationCount = notifications.filter(
    (notification) => notification.audience === "system",
  ).length;

  if (state.loading) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center">
        <div className="rounded-[1.4rem] border border-white/8 bg-black/15 px-5 py-4 text-sm text-white/65">
          Loading operations state...
        </div>
      </div>
    );
  }

  return (
    <div className="ben-scrollbar ben-shell-scroll-offset flex h-full min-h-0 flex-col gap-4 overflow-y-auto pr-1">
      <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(135deg,rgba(14,165,233,0.16),transparent_42%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
              Operations
            </p>
            <h1 className="mt-3 text-3xl font-semibold text-white">
              Desktop core controls
            </h1>
            <p className="mt-3 max-w-3xl text-sm leading-6 text-white/55">
              Manual actions, background work, playback preparation, scan
              activity, and transcodes now converge into one normalized work
              feed. This page keeps the deeper operator diagnostics while
              exposing the same notification IDs and state transitions used by
              shell toasts and the activity center.
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {state.library
                  ? `${state.library.Name} • ${state.library.Role}`
                  : "No active library"}
              </span>
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                Scan {scanPhase}
              </span>
              {maintenance?.RepairRequired && (
                <span className="rounded-full border border-amber-300/20 bg-amber-300/12 px-3 py-1 text-xs tracking-[0.2em] text-amber-100 uppercase">
                  Repair recommended
                </span>
              )}
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {checkpointSummary(state.checkpoint)}
              </span>
            </div>
          </div>
          <button
            className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
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

      {!state.library ? (
        <section className="rounded-[1.6rem] border border-dashed border-white/10 bg-black/10 px-8 py-10 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/40">
            <CircleAlert className="h-5 w-5" />
          </div>
          <h2 className="text-lg font-semibold text-white/90">
            No active library
          </h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-white/50">
            Select or create a library before running repair or checkpoint
            operations.
          </p>
          {state.local && (
            <div className="mt-5 inline-flex flex-wrap justify-center gap-2">
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.22em] text-white/52 uppercase">
                {state.local.Device || "Unknown device"}
              </span>
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.22em] text-white/52 uppercase">
                {state.local.DeviceID || "No device id"}
              </span>
            </div>
          )}
        </section>
      ) : (
        <>
          <section className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <Activity className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Runtime context
                  </h2>
                  <p className="text-sm text-white/48">
                    Active library, local device identity, and current scan
                    activity.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2">
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Library
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.library.Name}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.library.LibraryID}
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Device
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.local?.Device || "Unknown device"}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.local?.PeerID || "No peer identity"}
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Role
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white capitalize">
                    {normalizeRole(state.local?.Role ?? "") || "unknown"}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {canScan
                      ? "Can contribute local media"
                      : "Cannot contribute local media"}
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Scan activity
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white capitalize">
                    {scanPhase}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {formatCount(
                      state.activity?.Scan?.TracksDone ?? 0,
                      "track",
                    )}{" "}
                    of{" "}
                    {formatCount(
                      state.activity?.Scan?.TracksTotal ?? 0,
                      "track",
                    )}
                  </p>
                </div>
              </div>
            </div>

            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <ShieldCheck className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Checkpoint state
                  </h2>
                  <p className="text-sm text-white/48">
                    Latest published checkpoint and device coverage.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2">
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Checkpoint id
                  </p>
                  <p className="mt-2 font-mono text-sm break-all text-white/80">
                    {state.checkpoint?.CheckpointID ||
                      "No published checkpoint"}
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Coverage
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.checkpoint?.AckedDevices ?? 0}/
                    {state.checkpoint?.TotalDevices ?? 0}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.checkpoint?.Compactable
                      ? "Ready to compact"
                      : "Waiting for device coverage"}
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Entries
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {formatCount(state.checkpoint?.EntryCount ?? 0, "op")}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {formatCount(state.checkpoint?.ChunkCount ?? 0, "chunk")}
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Published
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {formatDateTime(state.checkpoint?.PublishedAt)}
                  </p>
                  {checkpointNeedsRepublish && (
                    <p className="mt-2 text-sm text-amber-100">
                      Protocol epoch v2 is active. Publish a fresh checkpoint
                      after the privacy scrub before relying on checkpoint sync.
                    </p>
                  )}
                  {state.checkpoint?.LastError && (
                    <p className="mt-2 text-sm text-rose-200">
                      {state.checkpoint.LastError}
                    </p>
                  )}
                </div>
              </div>
            </div>
          </section>

          <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <HardDrive className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Manual actions
                  </h2>
                  <p className="text-sm text-white/48">
                    Actions return immediately with a job handle and continue in
                    the background.
                  </p>
                </div>
              </div>

              {maintenance?.RepairRequired && (
                <div className="mt-5 rounded-[1.2rem] border border-amber-300/20 bg-amber-300/10 px-4 py-4 text-sm text-amber-100">
                  <p className="font-semibold">Repair recommended</p>
                  <p className="mt-2 text-amber-50/85">
                    Automatic scans detected library state that needs an
                    explicit repair run.
                  </p>
                  {maintenance.Reason && (
                    <p className="mt-2 text-xs tracking-[0.22em] text-amber-100/70 uppercase">
                      {maintenance.Reason}
                    </p>
                  )}
                  {maintenance.Detail && (
                    <p className="mt-2 text-sm text-amber-50/80">
                      {maintenance.Detail}
                    </p>
                  )}
                </div>
              )}

              <div className="mt-5 flex flex-wrap gap-3">
                <button
                  className="inline-flex items-center gap-2 rounded-md border border-zinc-500 bg-zinc-100 px-3 py-2 text-sm text-zinc-950 transition hover:bg-white disabled:cursor-default disabled:opacity-50"
                  disabled={!canScan || pendingAction === "repair-library"}
                  onClick={() => {
                    void runAction(
                      "repair-library",
                      startLibraryRepair,
                      "Started",
                    );
                  }}
                  type="button"
                >
                  <RefreshCw className="h-4 w-4" />
                  <span>Repair library</span>
                </button>
                <button
                  className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                  disabled={pendingAction === "sync-now"}
                  onClick={() => {
                    void runAction("sync-now", startSyncNow, "Started");
                  }}
                  type="button"
                >
                  <RefreshCw className="h-4 w-4" />
                  <span>Sync now</span>
                </button>
                <button
                  className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                  disabled={
                    !canCheckpoint || pendingAction === "checkpoint-publish"
                  }
                  onClick={() => {
                    void runAction(
                      "checkpoint-publish",
                      startPublishCheckpoint,
                      "Started",
                    );
                  }}
                  type="button"
                >
                  <ShieldCheck className="h-4 w-4" />
                  <span>Publish checkpoint</span>
                </button>
                <button
                  className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                  disabled={
                    !canCheckpoint || pendingAction === "checkpoint-compact"
                  }
                  onClick={() => {
                    void runAction(
                      "checkpoint-compact",
                      () => startCompactCheckpoint(false),
                      "Started",
                    );
                  }}
                  type="button"
                >
                  <Clock3 className="h-4 w-4" />
                  <span>Compact checkpoint</span>
                </button>
                <button
                  className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                  disabled={
                    !canCheckpoint || pendingAction === "checkpoint-force"
                  }
                  onClick={() => {
                    void runAction(
                      "checkpoint-force",
                      () => startCompactCheckpoint(true),
                      "Started force compaction",
                    );
                  }}
                  type="button"
                >
                  <CircleAlert className="h-4 w-4" />
                  <span>Force compact</span>
                </button>
              </div>

              <p className="mt-4 text-sm text-white/48">
                Repair requires owner, admin, or member role. Checkpoint
                actions require admin or owner role.
              </p>
            </div>

            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <FolderOpen className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Scan roots
                  </h2>
                  <p className="text-sm text-white/48">
                    Per-device roots for the active library. Roots stay
                    local-only and are excluded from sync and checkpoints.
                  </p>
                </div>
                {canScan && (
                  <div className="ml-auto">
                    <button
                      className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                      disabled={pendingAction === "root-config:add"}
                      onClick={() => {
                        void handleAddRoot();
                      }}
                      type="button"
                    >
                      <Plus className="h-4 w-4" />
                      <span>Add root</span>
                    </button>
                  </div>
                )}
              </div>

              <div className="mt-5 space-y-3">
                {state.roots.length === 0 ? (
                  <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-black/10 px-4 py-5 text-sm text-white/48">
                    No scan roots configured for this device.
                  </div>
                ) : (
                  state.roots.map((root) => {
                    return (
                      <div
                        className="flex flex-wrap items-center justify-between gap-3 rounded-[1.2rem] border border-white/8 bg-black/10 px-4 py-3"
                        key={root}
                      >
                        <div className="min-w-0">
                          <p className="truncate font-mono text-sm text-white/80">
                            {root}
                          </p>
                          <p className="mt-1 text-xs tracking-[0.22em] text-white/32 uppercase">
                            Local root
                          </p>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          {canScan && (
                            <button
                              className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                              disabled={pendingAction === `root-config:remove:${root}`}
                              onClick={() => {
                                void handleRemoveRoot(root);
                              }}
                              type="button"
                            >
                              <Minus className="h-4 w-4" />
                              <span>Remove</span>
                            </button>
                          )}
                        </div>
                      </div>
                    );
                  })
                )}
              </div>
            </div>
          </section>

          <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <Activity className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Diagnostics
                  </h2>
                  <p className="text-sm text-white/48">
                    Inspect counts and operator diagnostics for the active
                    library.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2">
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Libraries
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.inspect?.Libraries ?? 0}
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.inspect?.Devices ?? 0} devices,{" "}
                    {state.inspect?.Memberships ?? 0} memberships
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Catalog
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.inspect?.Content ?? 0} sources
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.inspect?.Albums ?? 0} albums,{" "}
                    {state.inspect?.Recordings ?? 0} recordings
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Oplog
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.inspect?.OplogEntries ?? 0} entries
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.inspect?.DeviceClocks ?? 0} device clocks
                  </p>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Media
                  </p>
                  <p className="mt-2 text-lg font-semibold text-white">
                    {state.inspect?.Encodings ?? 0} encodings
                  </p>
                  <p className="mt-2 text-sm text-white/55">
                    {state.inspect?.ArtworkVariants ?? 0} artwork variants
                  </p>
                </div>
              </div>
            </div>

            <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <ShieldCheck className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Oplog spread
                  </h2>
                  <p className="text-sm text-white/48">
                    Highest-volume entity types and devices in the active
                    library oplog.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-2">
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Entity types
                  </p>
                  <div className="mt-3 space-y-2 text-sm text-white/70">
                    {oplogEntityCounts.length === 0 ? (
                      <p className="text-white/48">No oplog diagnostics yet.</p>
                    ) : (
                      oplogEntityCounts.map(([name, count]) => (
                        <div
                          className="flex items-center justify-between gap-3"
                          key={name}
                        >
                          <span className="truncate font-mono text-xs text-white/58">
                            {name}
                          </span>
                          <span className="text-white">{count}</span>
                        </div>
                      ))
                    )}
                  </div>
                </div>
                <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                  <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                    Devices
                  </p>
                  <div className="mt-3 space-y-2 text-sm text-white/70">
                    {oplogDeviceCounts.length === 0 ? (
                      <p className="text-white/48">
                        No device oplog diagnostics yet.
                      </p>
                    ) : (
                      oplogDeviceCounts.map(([name, count]) => (
                        <div
                          className="flex items-center justify-between gap-3"
                          key={name}
                        >
                          <span className="truncate font-mono text-xs text-white/58">
                            {name}
                          </span>
                          <span className="text-white">{count}</span>
                        </div>
                      ))
                    )}
                  </div>
                </div>
              </div>
            </div>
          </section>
        </>
      )}

      <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
              <Clock3 className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">Work feed</h2>
              <p className="text-sm text-white/48">
                Shared notification stream used by the shell activity center,
                Base UI toasts, and this debug surface.
              </p>
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
              {formatCount(activeNotifications.length, "active")}
            </span>
            <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
              {formatCount(userNotificationCount, "user")}
            </span>
            <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
              {formatCount(systemNotificationCount, "system")}
            </span>
          </div>
        </div>

        <div className="mt-5 grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
          <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
            <div className="mb-3 flex items-center justify-between gap-3">
              <h3 className="text-sm font-semibold text-white">Active work</h3>
              <span className="text-[0.68rem] tracking-[0.2em] text-white/35 uppercase">
                {formatCount(activeNotifications.length, "item")}
              </span>
            </div>
            {activeNotifications.length === 0 ? (
              <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-white/[0.03] px-4 py-5 text-sm text-white/42">
                No active notifications right now.
              </div>
            ) : (
              <div className="space-y-3">
                {activeNotifications.map((notification) => (
                  <NotificationCard
                    key={notification.id}
                    notification={notification}
                  />
                ))}
              </div>
            )}
          </div>

          <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
            <div className="mb-3 flex items-center justify-between gap-3">
              <h3 className="text-sm font-semibold text-white">
                Recent history
              </h3>
              <span className="text-[0.68rem] tracking-[0.2em] text-white/35 uppercase">
                {formatCount(recentNotifications.length, "item")}
              </span>
            </div>
            {recentNotifications.length === 0 ? (
              <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-white/[0.03] px-4 py-5 text-sm text-white/42">
                No completed or failed notifications captured yet.
              </div>
            ) : (
              <div className="space-y-3">
                {recentNotifications.map((notification) => (
                  <NotificationCard
                    key={notification.id}
                    notification={notification}
                  />
                ))}
              </div>
            )}
          </div>
        </div>
      </section>
    </div>
  );
}
