import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  InviteJoinRequestRecord,
  InviteRecord,
  JoinSession,
  JobSnapshot,
  LibrarySummary,
  LocalContext,
  NetworkStatus,
} from "@/lib/api/models";
import { DesktopCoreModels } from "@/lib/api/models";
import {
  getLocalContext,
  getNetworkStatus,
  startConnectPeer,
} from "@/lib/api/network";
import { getActiveLibrary } from "@/lib/api/library";
import {
  getJoinSession,
  listActiveInvites,
  listJoinRequests,
} from "@/lib/api/invite";
import { subscribeJobEvents } from "@/lib/api/jobs";

type SharingState = {
  loading: boolean;
  library: LibrarySummary | null;
  local: LocalContext | null;
  network: NetworkStatus | null;
  invites: InviteRecord[];
  requests: InviteJoinRequestRecord[];
  trackedSession: JoinSession | null;
  error: string;
};

const localStorageSessionKey = "ben.desktop.sharing.joinSessionId";
const sharingRefreshIntervalMs = 2000;

const initialState: SharingState = {
  loading: true,
  library: null,
  local: null,
  network: null,
  invites: [],
  requests: [],
  trackedSession: null,
  error: "",
};

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function normalizeRole(role: string) {
  return role.trim().toLowerCase();
}

function canManageLibrary(role: string) {
  const normalized = normalizeRole(role);
  return normalized === "owner" || normalized === "admin";
}

export function useSharingPage() {
  const connectJobIdRef = useRef("");
  const [state, setState] = useState<SharingState>(initialState);
  const [pendingAction, setPendingAction] = useState("");
  const [feedback, setFeedback] = useState("");
  const [actionError, setActionError] = useState("");
  const [peerAddress, setPeerAddress] = useState("");
  const [connectJob, setConnectJob] = useState<JobSnapshot | null>(null);
  const [inviteRole, setInviteRole] = useState<"guest" | "member" | "admin">(
    "member",
  );
  const [inviteUses, setInviteUses] = useState("1");
  const [inviteExpiryHours, setInviteExpiryHours] = useState("24");
  const [latestInvite, setLatestInvite] = useState<InviteRecord | null>(
    null,
  );
  const [inviteCode, setInviteCode] = useState("");
  const [joinDeviceName, setJoinDeviceName] = useState("");
  const [trackedSessionId, setTrackedSessionId] = useState("");
  const [approvalRoles, setApprovalRoles] = useState<Record<string, string>>(
    {},
  );

  useEffect(() => {
    const stored = window.localStorage.getItem(localStorageSessionKey) ?? "";
    if (stored.trim()) {
      setTrackedSessionId(stored.trim());
    }
  }, []);

  useEffect(() => {
    if (trackedSessionId.trim()) {
      window.localStorage.setItem(
        localStorageSessionKey,
        trackedSessionId.trim(),
      );
      return;
    }
    window.localStorage.removeItem(localStorageSessionKey);
  }, [trackedSessionId]);

  const refresh = useCallback(async () => {
    try {
      const [{ library, found }, local, network] = await Promise.all([
        getActiveLibrary(),
        getLocalContext(),
        getNetworkStatus().catch(() => null),
      ]);

      const requests =
        found && library.LibraryID ? listJoinRequests("") : Promise.resolve([]);
      const invites =
        found && library.LibraryID ? listActiveInvites() : Promise.resolve([]);
      const session = trackedSessionId.trim()
        ? getJoinSession(trackedSessionId.trim()).catch(() => null)
        : Promise.resolve(null);

      const [inviteRows, requestRows, trackedSession] = await Promise.all([
        invites,
        requests,
        session,
      ]);

      setState({
        loading: false,
        library: found ? library : null,
        local,
        network,
        invites: inviteRows,
        requests: requestRows,
        trackedSession,
        error: "",
      });
      setApprovalRoles((current) => {
        const next = { ...current };
        for (const request of requestRows) {
          if (!next[request.RequestID]) {
            next[request.RequestID] = request.RequestedRole || "member";
          }
        }
        return next;
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        loading: false,
        error: describeError(error),
      }));
    }
  }, [trackedSessionId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
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
            snapshot.kind !== "connect-peer" ||
            snapshot.jobId !== connectJobIdRef.current
          ) {
            return;
          }
          setConnectJob(snapshot);
        });
      })
      .catch((error) => {
        if (!disposed) {
          setActionError(describeError(error));
        }
      });

    return () => {
      disposed = true;
      stopListening?.();
    };
  }, []);

  const runAction = useCallback(
    async (key: string, action: () => Promise<void | JoinSession>) => {
      setPendingAction(key);
      setActionError("");
      setFeedback("");
      try {
        await action();
        await refresh();
      } catch (error) {
        setActionError(describeError(error));
      } finally {
        setPendingAction("");
      }
    },
    [refresh],
  );

  const queueConnectPeer = useCallback(async () => {
    const job = await startConnectPeer(peerAddress.trim());
    connectJobIdRef.current = job.jobId;
    setConnectJob(job);
    setFeedback(`Queued connect-peer job ${job.jobId}`);
  }, [peerAddress]);

  const manageLibrary = canManageLibrary(state.local?.Role ?? "");
  const pendingRequests = useMemo(
    () =>
      state.requests.filter(
        (request) => normalizeRole(request.Status) === "pending",
      ),
    [state.requests],
  );

  useEffect(() => {
    const trackedStatus = normalizeRole(state.trackedSession?.Status ?? "");
    const shouldPoll =
      pendingRequests.length > 0 ||
      (trackedStatus !== "" &&
        trackedStatus !== "completed" &&
        trackedStatus !== "rejected" &&
        trackedStatus !== "expired" &&
        trackedStatus !== "failed");
    if (!shouldPoll) {
      return;
    }

    const handle = window.setInterval(() => {
      void refresh();
    }, sharingRefreshIntervalMs);
    return () => {
      window.clearInterval(handle);
    };
  }, [pendingRequests.length, refresh, state.trackedSession?.Status]);

  return {
    actionError,
    approvalRoles,
    connectJob,
    feedback,
    inviteCode,
    inviteExpiryHours,
    inviteRole,
    inviteUses,
    joinDeviceName,
    latestInvite,
    manageLibrary,
    peerAddress,
    pendingAction,
    pendingRequests,
    queueConnectPeer,
    refresh,
    runAction,
    setActionError,
    setApprovalRoles,
    setFeedback,
    setInviteCode,
    setInviteExpiryHours,
    setInviteRole,
    setInviteUses,
    setJoinDeviceName,
    setLatestInvite,
    setPeerAddress,
    setTrackedSessionId,
    state,
    trackedSessionId,
  };
}
