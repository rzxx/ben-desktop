import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useRef, useState } from "react";
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
import {
  type ActivityStatus,
  DesktopCoreModels,
  type InspectSummary,
  type JobSnapshot,
  type LibraryOplogDiagnostics,
  type LibraryCheckpointStatus,
  type LibrarySummary,
  type LocalContext,
  addScanRoots,
  getActiveLibrary,
  getActivityStatus,
  getCheckpointStatus,
  getInspectSummary,
  getLibraryOplogDiagnostics,
  getLocalContext,
  getScanRoots,
  listJobs,
  pickScanRoot,
  removeScanRoots,
  startCompactCheckpoint,
  startLibraryRescan,
  startPublishCheckpoint,
  startRootRescan,
  startSyncNow,
  subscribeJobEvents,
} from "../../shared/lib/desktop";
import { formatCount } from "../../shared/lib/format";

type OperationsState = {
  loading: boolean;
  library: LibrarySummary | null;
  local: LocalContext | null;
  roots: string[];
  checkpoint: LibraryCheckpointStatus | null;
  activity: ActivityStatus | null;
  inspect: InspectSummary | null;
  oplog: LibraryOplogDiagnostics | null;
  jobs: JobSnapshot[];
  error: string;
};

const SUMMARY_REFRESH_DEBOUNCE_MS = 400;
const MAX_VISIBLE_JOBS = 12;

const initialState: OperationsState = {
  loading: true,
  library: null,
  local: null,
  roots: [],
  checkpoint: null,
  activity: null,
  inspect: null,
  oplog: null,
  jobs: [],
  error: "",
};

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function normalizeRole(role: string) {
  return role.trim().toLowerCase();
}

function canProvideLocalMedia(role: string) {
  const normalized = normalizeRole(role);
  return (
    normalized === "owner" || normalized === "admin" || normalized === "member"
  );
}

function canManageLibrary(role: string) {
  const normalized = normalizeRole(role);
  return normalized === "owner" || normalized === "admin";
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

function checkpointSummary(status: LibraryCheckpointStatus | null) {
  if (!status || !status.CheckpointID) {
    return "No published checkpoint";
  }
  return `${status.AckedDevices}/${status.TotalDevices} devices covered`;
}

function timeValue(value?: Date | string | null) {
  if (!value) {
    return 0;
  }
  const date = value instanceof Date ? value : new Date(value);
  const timestamp = date.getTime();
  return Number.isNaN(timestamp) ? 0 : timestamp;
}

function sortJobs(jobs: JobSnapshot[]) {
  return [...jobs].sort((left, right) => {
    const updatedDiff = timeValue(right.updatedAt) - timeValue(left.updatedAt);
    if (updatedDiff !== 0) {
      return updatedDiff;
    }
    const createdDiff = timeValue(right.createdAt) - timeValue(left.createdAt);
    if (createdDiff !== 0) {
      return createdDiff;
    }
    return left.jobId.localeCompare(right.jobId);
  });
}

function upsertJobSnapshot(jobs: JobSnapshot[], snapshot: JobSnapshot) {
  return sortJobs([
    snapshot,
    ...jobs.filter((job) => job.jobId !== snapshot.jobId),
  ]);
}

function jobKindLabel(kind: string) {
  switch (kind) {
    case "scan-library":
      return "Library scan";
    case "scan-root":
      return "Root scan";
    case "publish-checkpoint":
      return "Publish checkpoint";
    case "compact-checkpoint":
      return "Compact checkpoint";
    case "install-checkpoint":
      return "Checkpoint install";
    case "sync-now":
      return "Manual sync";
    case "connect-peer":
      return "Connect peer";
    case "join-session":
      return "Join session";
    default:
      return kind || "Job";
  }
}

function jobPhaseClasses(phase: string) {
  switch (phase) {
    case "completed":
      return "border-emerald-400/20 bg-emerald-400/12 text-emerald-100";
    case "failed":
      return "border-rose-400/20 bg-rose-400/12 text-rose-100";
    case "running":
      return "border-sky-400/20 bg-sky-400/12 text-sky-100";
    default:
      return "border-white/10 bg-white/6 text-white/75";
  }
}

function formatGroupCounts(
  entries?: Array<{ Key: string; Count: number }> | null,
) {
  if (!entries) {
    return [];
  }
  return entries
    .filter((entry) => Number(entry.Count) > 0)
    .sort(
      (left, right) =>
        Number(right.Count) - Number(left.Count) ||
        left.Key.localeCompare(right.Key),
    )
    .map((entry) => [entry.Key, entry.Count] as const);
}

function JobRow({ job }: { job: JobSnapshot }) {
  const progress = Math.max(
    0,
    Math.min(100, Math.round((job.progress ?? 0) * 100)),
  );

  return (
    <div className="rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm font-semibold text-white">
              {jobKindLabel(job.kind)}
            </p>
            <span
              className={`rounded-full border px-2 py-1 text-[0.65rem] tracking-[0.24em] uppercase ${jobPhaseClasses(job.phase)}`}
            >
              {job.phase || "queued"}
            </span>
          </div>
          <p className="mt-2 text-sm text-white/55">
            {job.message || "No status message yet"}
          </p>
          {job.error && (
            <p className="mt-2 text-sm text-rose-200">{job.error}</p>
          )}
        </div>
        <div className="text-right text-xs text-white/42">
          <div>{formatDateTime(job.updatedAt)}</div>
          <div className="mt-1 font-mono text-[0.68rem] tracking-[0.18em] text-white/28 uppercase">
            {job.jobId}
          </div>
        </div>
      </div>
      <div className="mt-3 h-2 overflow-hidden rounded-full bg-white/8">
        <div
          className="h-full rounded-full bg-[linear-gradient(90deg,rgba(249,115,22,0.9),rgba(14,165,233,0.72))] transition-[width] duration-300"
          style={{ width: `${progress}%` }}
        />
      </div>
    </div>
  );
}

export function OperationsPage() {
  const mountedRef = useRef(true);
  const activeLibraryIdRef = useRef("");
  const refreshTimerRef = useRef<number | null>(null);
  const [state, setState] = useState<OperationsState>(initialState);
  const [pendingAction, setPendingAction] = useState("");
  const [feedback, setFeedback] = useState("");
  const [actionError, setActionError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const [{ library, found }, local] = await Promise.all([
        getActiveLibrary(),
        getLocalContext(),
      ]);

      if (!mountedRef.current) {
        return;
      }

      if (!found || !library.LibraryID) {
        activeLibraryIdRef.current = "";
        setState({
          loading: false,
          library: null,
          local,
          roots: [],
          checkpoint: null,
          activity: null,
          inspect: null,
          oplog: null,
          jobs: [],
          error: "",
        });
        return;
      }

      activeLibraryIdRef.current = library.LibraryID;
      const results = await Promise.allSettled([
        getScanRoots(),
        getCheckpointStatus(),
        getActivityStatus(),
        getInspectSummary(),
        getLibraryOplogDiagnostics(library.LibraryID),
        listJobs(library.LibraryID),
      ]);

      if (!mountedRef.current) {
        return;
      }

      const [
        rootsResult,
        checkpointResult,
        activityResult,
        inspectResult,
        oplogResult,
        jobsResult,
      ] = results;
      const nextError = results.find((result) => result.status === "rejected");

      setState({
        loading: false,
        library,
        local,
        roots: rootsResult.status === "fulfilled" ? rootsResult.value : [],
        checkpoint:
          checkpointResult.status === "fulfilled"
            ? checkpointResult.value
            : null,
        activity:
          activityResult.status === "fulfilled" ? activityResult.value : null,
        inspect:
          inspectResult.status === "fulfilled" ? inspectResult.value : null,
        oplog: oplogResult.status === "fulfilled" ? oplogResult.value : null,
        jobs:
          jobsResult.status === "fulfilled" ? sortJobs(jobsResult.value) : [],
        error:
          nextError?.status === "rejected"
            ? describeError(nextError.reason)
            : "",
      });
    } catch (error) {
      if (!mountedRef.current) {
        return;
      }
      setState((current) => ({
        ...current,
        loading: false,
        error: describeError(error),
      }));
    }
  }, []);

  const scheduleRefresh = useCallback(
    (delay = SUMMARY_REFRESH_DEBOUNCE_MS) => {
      if (!mountedRef.current) {
        return;
      }
      if (refreshTimerRef.current !== null) {
        window.clearTimeout(refreshTimerRef.current);
      }
      refreshTimerRef.current = window.setTimeout(
        () => {
          refreshTimerRef.current = null;
          void refresh();
        },
        Math.max(0, delay),
      );
    },
    [refresh],
  );

  useEffect(() => {
    mountedRef.current = true;
    void refresh();

    let disposed = false;
    let stopListening: (() => void) | undefined;

    void subscribeJobEvents()
      .then((eventName) => {
        if (disposed) {
          return;
        }
        stopListening = Events.On(eventName, (event) => {
          const snapshot = DesktopCoreModels.JobSnapshot.createFrom(event.data);
          if (
            !activeLibraryIdRef.current ||
            snapshot.libraryId !== activeLibraryIdRef.current
          ) {
            return;
          }
          setState((current) => ({
            ...current,
            jobs: upsertJobSnapshot(current.jobs, snapshot),
          }));
          scheduleRefresh();
        });
      })
      .catch((error) => {
        if (!mountedRef.current) {
          return;
        }
        setState((current) => ({
          ...current,
          error: describeError(error),
        }));
      });

    const handleWindowFocus = () => {
      scheduleRefresh(0);
    };
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        scheduleRefresh(0);
      }
    };
    window.addEventListener("focus", handleWindowFocus);
    document.addEventListener("visibilitychange", handleVisibilityChange);

    return () => {
      disposed = true;
      mountedRef.current = false;
      if (refreshTimerRef.current !== null) {
        window.clearTimeout(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }
      stopListening?.();
      window.removeEventListener("focus", handleWindowFocus);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [refresh, scheduleRefresh]);

  const runAction = useCallback(
    async (
      key: string,
      action: () => Promise<JobSnapshot>,
      successLabel: string,
    ) => {
      setPendingAction(key);
      setFeedback("");
      setActionError("");
      try {
        const job = await action();
        if (!mountedRef.current) {
          return;
        }
        setFeedback(`${successLabel}: ${jobKindLabel(job.kind)} queued`);
        if (
          activeLibraryIdRef.current &&
          job.libraryId === activeLibraryIdRef.current
        ) {
          setState((current) => ({
            ...current,
            jobs: upsertJobSnapshot(current.jobs, job),
          }));
        }
        scheduleRefresh(0);
      } catch (error) {
        if (!mountedRef.current) {
          return;
        }
        setActionError(describeError(error));
      } finally {
        if (mountedRef.current) {
          setPendingAction("");
        }
      }
    },
    [scheduleRefresh],
  );

  const role = state.local?.Role ?? "";
  const canScan = canProvideLocalMedia(role);
  const canCheckpoint = canManageLibrary(role);
  const scanPhase = state.activity?.Scan?.Phase || "idle";
  const visibleJobs = state.jobs.slice(0, MAX_VISIBLE_JOBS);
  const oplogEntityCounts = formatGroupCounts(
    state.oplog?.OplogByEntityType,
  ).slice(0, 6);
  const oplogDeviceCounts = formatGroupCounts(
    state.oplog?.OplogByDeviceID,
  ).slice(0, 6);
  const checkpointNeedsRepublish =
    canCheckpoint &&
    (!state.checkpoint?.CheckpointID || !state.checkpoint?.PublishedAt);

  const handleAddRoot = useCallback(async () => {
    setPendingAction("scan-root:add");
    setFeedback("");
    setActionError("");
    try {
      const selectedRoot = await pickScanRoot(state.roots[0] ?? "");
      if (!selectedRoot) {
        return;
      }
      await addScanRoots([selectedRoot]);
      setFeedback(`Added scan root ${selectedRoot}`);
      await refresh();
    } catch (error) {
      setActionError(describeError(error));
    } finally {
      setPendingAction("");
    }
  }, [refresh, state.roots]);

  const handleRemoveRoot = useCallback(
    async (root: string) => {
      setPendingAction(`scan-root:remove:${root}`);
      setFeedback("");
      setActionError("");
      try {
        await removeScanRoots([root]);
        setFeedback(`Removed scan root ${root}`);
        await refresh();
      } catch (error) {
        setActionError(describeError(error));
      } finally {
        setPendingAction("");
      }
    },
    [refresh],
  );

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
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto pr-1">
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
              Manual scan and checkpoint actions now use the async desktop-core
              job API with Wails job events feeding this page. Manual sync now
              uses the same async job path, so long-running network catch-up no
              longer blocks the operations screen.
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
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {checkpointSummary(state.checkpoint)}
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

      {!state.library ? (
        <section className="rounded-[1.6rem] border border-dashed border-white/10 bg-black/10 px-8 py-10 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/40">
            <CircleAlert className="h-5 w-5" />
          </div>
          <h2 className="text-lg font-semibold text-white/90">
            No active library
          </h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-white/50">
            Select or create a library before running manual scan or checkpoint
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
                    {normalizeRole(role) || "unknown"}
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

              <div className="mt-5 flex flex-wrap gap-3">
                <button
                  className="action-button is-primary"
                  disabled={!canScan || pendingAction === "scan-library"}
                  onClick={() => {
                    void runAction(
                      "scan-library",
                      startLibraryRescan,
                      "Started",
                    );
                  }}
                  type="button"
                >
                  <RefreshCw className="h-4 w-4" />
                  <span>Scan library</span>
                </button>
                <button
                  className="action-button"
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
                  className="action-button"
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
                  className="action-button"
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
                  className="action-button"
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
                Scan actions require owner, admin, or member role. Checkpoint
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
                      className="action-button"
                      disabled={pendingAction === "scan-root:add"}
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
                    const key = `scan-root:${root}`;
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
                            Root scan
                          </p>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          <button
                            className="action-button"
                            disabled={!canScan || pendingAction === key}
                            onClick={() => {
                              void runAction(
                                key,
                                () => startRootRescan(root),
                                "Started",
                              );
                            }}
                            type="button"
                          >
                            <RefreshCw className="h-4 w-4" />
                            <span>Scan root</span>
                          </button>
                          {canScan && (
                            <button
                              className="action-button"
                              disabled={
                                pendingAction === `scan-root:remove:${root}`
                              }
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

          <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
                  <Clock3 className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">
                    Recent jobs
                  </h2>
                  <p className="text-sm text-white/48">
                    Jobs stream from desktop-core events for the active library.
                  </p>
                </div>
              </div>
              <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs tracking-[0.2em] text-white/52 uppercase">
                {formatCount(state.jobs.length, "job")}
              </span>
            </div>

            <div className="mt-5 space-y-3">
              {visibleJobs.length === 0 ? (
                <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-black/10 px-4 py-5 text-sm text-white/48">
                  No async jobs recorded for this library yet.
                </div>
              ) : (
                visibleJobs.map((job) => <JobRow job={job} key={job.jobId} />)
              )}
            </div>
          </section>
        </>
      )}
    </div>
  );
}
