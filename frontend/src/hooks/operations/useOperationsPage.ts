import { useCallback, useEffect, useRef, useState } from "react";
import type {
  ActivityStatus,
  InspectSummary,
  JobSnapshot,
  LibraryCheckpointStatus,
  LibraryOplogDiagnostics,
  LibrarySummary,
  LocalContext,
} from "@/lib/api/models";
import {
  addScanRoots,
  getActiveLibrary,
  getScanRoots,
  pickScanRoot,
  removeScanRoots,
} from "@/lib/api/library";
import {
  getActivityStatus,
  getCheckpointStatus,
  getInspectSummary,
  getLibraryOplogDiagnostics,
  getLocalContext,
} from "@/lib/api/network";

type OperationsState = {
  loading: boolean;
  library: LibrarySummary | null;
  local: LocalContext | null;
  roots: string[];
  checkpoint: LibraryCheckpointStatus | null;
  activity: ActivityStatus | null;
  inspect: InspectSummary | null;
  oplog: LibraryOplogDiagnostics | null;
  error: string;
};

const SUMMARY_REFRESH_DEBOUNCE_MS = 400;

const initialState: OperationsState = {
  loading: true,
  library: null,
  local: null,
  roots: [],
  checkpoint: null,
  activity: null,
  inspect: null,
  oplog: null,
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

export function useOperationsPage() {
  const mountedRef = useRef(true);
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
        setState({
          loading: false,
          library: null,
          local,
          roots: [],
          checkpoint: null,
          activity: null,
          inspect: null,
          oplog: null,
          error: "",
        });
        return;
      }

      const results = await Promise.allSettled([
        getScanRoots(),
        getCheckpointStatus(),
        getActivityStatus(),
        getInspectSummary(),
        getLibraryOplogDiagnostics(library.LibraryID),
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
      mountedRef.current = false;
      if (refreshTimerRef.current !== null) {
        window.clearTimeout(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }
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

  const role = state.local?.Role ?? "";
  const canScan = canProvideLocalMedia(role);
  const canCheckpoint = canManageLibrary(role);
  const scanPhase = state.activity?.Scan?.Phase || "idle";
  const oplogEntityCounts = formatGroupCounts(
    state.oplog?.OplogByEntityType,
  ).slice(0, 6);
  const oplogDeviceCounts = formatGroupCounts(
    state.oplog?.OplogByDeviceID,
  ).slice(0, 6);
  const checkpointNeedsRepublish =
    canCheckpoint &&
    (!state.checkpoint?.CheckpointID || !state.checkpoint?.PublishedAt);

  return {
    actionError,
    canCheckpoint,
    canScan,
    checkpointNeedsRepublish,
    feedback,
    handleAddRoot,
    handleRemoveRoot,
    oplogDeviceCounts,
    oplogEntityCounts,
    pendingAction,
    refresh,
    runAction,
    scanPhase,
    state,
  };
}
