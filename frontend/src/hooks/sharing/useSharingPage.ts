import { useCallback, useEffect, useMemo, useState } from "react";
import type {
  InviteJoinRequestRecord,
  InviteRecord,
  JoinAttempt,
  LibraryRelayConfig,
  LibrarySummary,
  LocalContext,
  NetworkStatus,
} from "@/lib/api/models";
import { getLocalContext, getNetworkStatus } from "@/lib/api/network";
import {
  getActiveLibrary,
  getLibraryRelayConfig,
  updateLibraryRelayConfig,
} from "@/lib/api/library";
import {
  approveJoinRequest,
  cancelJoinAttempt,
  createInvite,
  deleteInvite,
  getJoinAttempt,
  listActiveInvites,
  listJoinRequests,
  rejectJoinRequest,
  startJoinFromInvite,
} from "@/lib/api/invite";
import { Types } from "@/lib/api/models";

type SharingState = {
  loading: boolean;
  library: LibrarySummary | null;
  local: LocalContext | null;
  network: NetworkStatus | null;
  relay: LibraryRelayConfig | null;
  invites: InviteRecord[];
  requests: InviteJoinRequestRecord[];
  joinAttempt: JoinAttempt | null;
  error: string;
};

const sharingRefreshIntervalMs = 2000;
type InviteRole = "guest" | "member" | "admin";

const initialState: SharingState = {
  loading: true,
  library: null,
  local: null,
  network: null,
  relay: null,
  invites: [],
  requests: [],
  joinAttempt: null,
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

function parseRelayBootstrapAddrs(value: string) {
  return value
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function stringifyRelayBootstrapAddrs(value: string[] | undefined) {
  return (value ?? []).join("\n");
}

export function useSharingPage() {
  const [state, setState] = useState<SharingState>(initialState);
  const [pendingAction, setPendingAction] = useState("");
  const [feedback, setFeedback] = useState("");
  const [actionError, setActionError] = useState("");
  const [inviteRole, setInviteRole] = useState<InviteRole>("member");
  const [inviteReusable, setInviteReusable] = useState(false);
  const [inviteCode, setInviteCode] = useState("");
  const [joinDeviceName, setJoinDeviceName] = useState("");
  const [joinAttemptId, setJoinAttemptId] = useState("");
  const [relayOpen, setRelayOpen] = useState(false);
  const [relayRegistryURL, setRelayRegistryURL] = useState("");
  const [relayBootstrapText, setRelayBootstrapText] = useState("");

  const refresh = useCallback(async () => {
    try {
      const [{ library, found }, local, network] = await Promise.all([
        getActiveLibrary(),
        getLocalContext(),
        getNetworkStatus().catch(() => null),
      ]);

      const hasLibrary = found && Boolean(library.LibraryID);
      const [invites, requests, relay, joinAttempt] = await Promise.all([
        hasLibrary ? listActiveInvites() : Promise.resolve([]),
        hasLibrary ? listJoinRequests() : Promise.resolve([]),
        hasLibrary
          ? getLibraryRelayConfig(library.LibraryID).catch(() => null)
          : Promise.resolve(null),
        joinAttemptId.trim()
          ? getJoinAttempt(joinAttemptId.trim()).catch(() => null)
          : Promise.resolve(null),
      ]);

      setState({
        loading: false,
        library: hasLibrary ? library : null,
        local,
        network,
        relay,
        invites,
        requests,
        joinAttempt,
        error: "",
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        loading: false,
        error: describeError(error),
      }));
    }
  }, [joinAttemptId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!state.relay || relayOpen) {
      return;
    }
    setRelayRegistryURL(state.relay.RegistryURL);
    setRelayBootstrapText(
      stringifyRelayBootstrapAddrs(state.relay.RelayBootstrapAddrs),
    );
  }, [relayOpen, state.relay]);

  const pendingRequests = useMemo(() => state.requests, [state.requests]);
  const manageLibrary = canManageLibrary(state.local?.Role ?? "");
  const shouldPoll =
    pendingRequests.length > 0 || Boolean(state.joinAttempt?.Pending);

  useEffect(() => {
    if (!shouldPoll) {
      return;
    }
    const handle = window.setInterval(() => {
      void refresh();
    }, sharingRefreshIntervalMs);
    return () => {
      window.clearInterval(handle);
    };
  }, [refresh, shouldPoll]);

  const runAction = useCallback(
    async (key: string, action: () => Promise<void>) => {
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

  const createInviteAction = useCallback(async () => {
    const invite = await createInvite(
      new Types.InviteCreateRequest({
        Role: inviteRole,
        Reusable: inviteReusable,
      }),
    );
    setFeedback(`Created ${inviteReusable ? "reusable" : "single-use"} invite`);
    await navigator.clipboard?.writeText?.(invite.InviteCode);
  }, [inviteReusable, inviteRole]);

  const startJoinAction = useCallback(async () => {
    const attempt = await startJoinFromInvite(
      new Types.JoinFromInviteInput({
        InviteCode: inviteCode.trim(),
        DeviceName: joinDeviceName.trim(),
      }),
    );
    setJoinAttemptId(attempt.AttemptID);
    setFeedback(attempt.Message || "Join request sent");
  }, [inviteCode, joinDeviceName]);

  const cancelJoinAction = useCallback(async () => {
    const attemptID = state.joinAttempt?.AttemptID || joinAttemptId;
    if (!attemptID.trim()) {
      return;
    }
    await cancelJoinAttempt(attemptID);
    setJoinAttemptId("");
    setFeedback("Join attempt cancelled");
  }, [joinAttemptId, state.joinAttempt?.AttemptID]);

  const approveRequestAction = useCallback(async (requestID: string) => {
    await approveJoinRequest(requestID);
    setFeedback("Approved join request");
  }, []);

  const rejectRequestAction = useCallback(async (requestID: string) => {
    await rejectJoinRequest(requestID);
    setFeedback("Rejected join request");
  }, []);

  const revokeInviteAction = useCallback(async (inviteID: string) => {
    await deleteInvite(inviteID);
    setFeedback("Revoked invite");
  }, []);

  const saveRelayAction = useCallback(async () => {
    const libraryID = state.library?.LibraryID ?? "";
    if (!libraryID) {
      return;
    }
    const relay = await updateLibraryRelayConfig(
      new Types.UpdateLibraryRelayConfigRequest({
        LibraryID: libraryID,
        RegistryURL: relayRegistryURL.trim(),
        RelayBootstrapAddrs: parseRelayBootstrapAddrs(relayBootstrapText),
      }),
    );
    setRelayOpen(false);
    setRelayRegistryURL(relay.RegistryURL);
    setRelayBootstrapText(
      stringifyRelayBootstrapAddrs(relay.RelayBootstrapAddrs),
    );
    setFeedback("Updated relay settings");
  }, [relayBootstrapText, relayRegistryURL, state.library?.LibraryID]);

  return {
    actionError,
    createInviteAction,
    inviteCode,
    inviteReusable,
    inviteRole,
    joinDeviceName,
    manageLibrary,
    pendingAction,
    pendingRequests,
    relayBootstrapText,
    relayOpen,
    relayRegistryURL,
    refresh,
    runAction,
    setActionError,
    setFeedback,
    setInviteCode,
    setInviteReusable,
    setInviteRole,
    setJoinDeviceName,
    setRelayBootstrapText,
    setRelayOpen,
    setRelayRegistryURL,
    state,
    feedback,
    startJoinAction,
    cancelJoinAction,
    approveRequestAction,
    rejectRequestAction,
    revokeInviteAction,
    saveRelayAction,
  };
}
