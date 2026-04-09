import {
  CheckCircle2,
  Copy,
  KeyRound,
  Link2,
  RefreshCw,
  Router,
  Send,
  ShieldCheck,
  UserPlus,
  XCircle,
} from "lucide-react";
import {
  approveJoinRequest,
  cancelJoinSession,
  createInviteCode,
  rejectJoinRequest,
  revokeIssuedInvite,
  startFinalizeJoinSession,
  startJoinFromInvite,
} from "@/lib/api/invite";
import { Types } from "@/lib/api/models";
import { formatDateTime, formatRelativeDate } from "@/lib/format";
import { useSharingPage } from "@/hooks/sharing/useSharingPage";

const inviteExpiryHourOptions = [1, 6, 24, 72];
const inviteRoles = ["guest", "member", "admin"] as const;
const durationHour = 60 * 60 * 1_000_000_000;

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function normalizeRole(role: string) {
  return role.trim().toLowerCase();
}

function sessionTone(status: string) {
  switch (normalizeRole(status)) {
    case "approved":
    case "completed":
      return "border-emerald-400/18 bg-emerald-400/10 text-emerald-700 dark:text-emerald-100";
    case "rejected":
    case "expired":
    case "failed":
      return "border-rose-400/18 bg-rose-400/10 text-rose-700 dark:text-rose-100";
    default:
      return "border-sky-400/18 bg-sky-400/10 text-sky-700 dark:text-sky-100";
  }
}

function requestTone(status: string) {
  switch (normalizeRole(status)) {
    case "approved":
      return "border-emerald-400/18 bg-emerald-400/10 text-emerald-700 dark:text-emerald-100";
    case "rejected":
    case "expired":
      return "border-rose-400/18 bg-rose-400/10 text-rose-700 dark:text-rose-100";
    default:
      return "border-amber-400/18 bg-amber-400/10 text-amber-700 dark:text-amber-100";
  }
}

async function copyText(value: string) {
  if (!value.trim()) {
    return;
  }
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  throw new Error("clipboard is not available");
}

export function SharingSurface() {
  const {
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
  } = useSharingPage();

  if (state.loading) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center">
        <div className="border-theme-300/70 text-theme-900 rounded-[1.4rem] border bg-white/82 px-5 py-4 text-sm dark:border-white/8 dark:bg-black/15 dark:text-white/65">
          Loading sharing surface...
        </div>
      </div>
    );
  }

  return (
    <div className="ben-scrollbar ben-shell-scroll-offset flex h-full min-h-0 flex-col gap-4 overflow-y-auto pr-1">
      <section className="border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(140deg,rgba(34,197,94,0.14),transparent_36%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6 dark:border-white/8">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-theme-500 text-[0.68rem] tracking-[0.35em] uppercase dark:text-white/35">
              Sharing
            </p>
            <h1 className="text-theme-900 mt-3 text-3xl font-semibold dark:text-white">
              Invite, join, and peer controls
            </h1>
            <p className="text-theme-600 mt-3 max-w-3xl text-sm leading-6 dark:text-white/55">
              The desktop host already exposes invite, approval, join-session,
              and peer-connect facades. This page wires those flows into the
              Wails UI so library sharing no longer stops at backend-only
              contracts.
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
              <span className="border-theme-300/75 bg-theme-100 text-theme-500 rounded-full border px-3 py-1 text-xs tracking-[0.2em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/52">
                {pendingRequests.length} pending request
                {pendingRequests.length === 1 ? "" : "s"}
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

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:border-white/8 dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
          <div className="flex items-center gap-3">
            <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
              <Router className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                Peer connect
              </h2>
              <p className="text-theme-600 text-sm dark:text-white/48">
                Trigger a manual `connect + catch-up` attempt against a peer
                address.
              </p>
            </div>
          </div>

          <div className="mt-5 space-y-3">
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Peer address
              </span>
              <input
                className="border-theme-300/75 text-theme-900 mt-2 w-full rounded-[1rem] border bg-white/82 px-4 py-3 text-sm transition outline-none focus:border-sky-400/45 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setPeerAddress(event.target.value);
                }}
                placeholder="memory://owner or libp2p multiaddr"
                value={peerAddress}
              />
            </label>
            <button
              className="border-theme-900 bg-theme-900 text-theme-50 hover:bg-theme-800 inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-500 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
              disabled={!peerAddress.trim() || pendingAction === "connect-peer"}
              onClick={() => {
                void runAction("connect-peer", async () => {
                  await queueConnectPeer();
                });
              }}
              type="button"
            >
              <Link2 className="h-4 w-4" />
              <span>Connect peer</span>
            </button>
            <p className="text-theme-900 text-sm dark:text-white/45">
              Peer connect now runs through the async jobs path, so the UI can
              track resolution and catch-up without blocking.
            </p>
            {connectJob && (
              <div className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <p className="text-theme-900 text-sm font-semibold dark:text-white">
                      Latest connect job
                    </p>
                    <p className="text-theme-600 mt-2 text-sm dark:text-white/55">
                      {connectJob.message || "No status message yet"}
                    </p>
                    {connectJob.error && (
                      <p className="mt-2 text-sm text-rose-700 dark:text-rose-100">
                        {connectJob.error}
                      </p>
                    )}
                  </div>
                  <div className="text-theme-500 text-right text-xs dark:text-white/42">
                    <div className="uppercase">
                      {connectJob.phase || "queued"}
                    </div>
                    <div className="text-theme-900 mt-1 font-mono text-[0.68rem] tracking-[0.18em] dark:text-white/28">
                      {connectJob.jobId}
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

        <div className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:border-white/8 dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
          <div className="flex items-center gap-3">
            <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
              <UserPlus className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                Join from invite
              </h2>
              <p className="text-theme-600 text-sm dark:text-white/48">
                Start or resume a join session even when this device is not
                already in a library.
              </p>
            </div>
          </div>

          <div className="mt-5 space-y-3">
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Invite code
              </span>
              <textarea
                className="border-theme-300/75 text-theme-900 mt-2 min-h-28 w-full rounded-[1rem] border bg-white/82 px-4 py-3 text-sm transition outline-none focus:border-sky-400/45 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setInviteCode(event.target.value);
                }}
                placeholder="ben-invite-v1..."
                value={inviteCode}
              />
            </label>
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Device name override
              </span>
              <input
                className="border-theme-300/75 text-theme-900 mt-2 w-full rounded-[1rem] border bg-white/82 px-4 py-3 text-sm transition outline-none focus:border-sky-400/45 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setJoinDeviceName(event.target.value);
                }}
                placeholder={state.local?.Device || "Use current device name"}
                value={joinDeviceName}
              />
            </label>
            <div className="flex flex-wrap gap-3">
              <button
                className="border-theme-900 bg-theme-900 text-theme-50 hover:bg-theme-800 inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-500 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                disabled={!inviteCode.trim() || pendingAction === "start-join"}
                onClick={() => {
                  void runAction("start-join", async () => {
                    const session = await startJoinFromInvite(
                      new Types.JoinFromInviteInput({
                        InviteCode: inviteCode.trim(),
                        DeviceName: joinDeviceName.trim(),
                      }),
                    );
                    setTrackedSessionId(session.SessionID);
                    setFeedback(`Started join session ${session.SessionID}`);
                  });
                }}
                type="button"
              >
                <Send className="h-4 w-4" />
                <span>Start join</span>
              </button>
              <button
                className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                disabled={
                  !trackedSessionId.trim() ||
                  pendingAction === "refresh-session"
                }
                onClick={() => {
                  void refresh();
                  setFeedback("Refreshed tracked session");
                }}
                type="button"
              >
                <RefreshCw className="h-4 w-4" />
                <span>Refresh session</span>
              </button>
            </div>
          </div>
        </div>
      </section>

      {state.trackedSession && (
        <section className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:border-white/8 dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                  Tracked join session
                </h2>
                <span
                  className={`rounded-full border px-2 py-1 text-[0.68rem] tracking-[0.24em] uppercase ${sessionTone(
                    state.trackedSession.Status,
                  )}`}
                >
                  {state.trackedSession.Status || "pending"}
                </span>
              </div>
              <p className="text-theme-600 mt-2 text-sm dark:text-white/55">
                {state.trackedSession.Message || "No join status message yet"}
              </p>
            </div>
            <div className="flex flex-wrap gap-3">
              <button
                className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                onClick={() => {
                  void copyText(state.trackedSession?.SessionID ?? "")
                    .then(() => {
                      setFeedback("Copied join session id");
                    })
                    .catch((error) => {
                      setActionError(describeError(error));
                    });
                }}
                type="button"
              >
                <Copy className="h-4 w-4" />
                <span>Copy session id</span>
              </button>
              <button
                className="border-theme-900 bg-theme-900 text-theme-50 hover:bg-theme-800 inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-500 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                disabled={
                  normalizeRole(state.trackedSession.Status) !== "approved" ||
                  pendingAction === "finalize-session"
                }
                onClick={() => {
                  void runAction("finalize-session", async () => {
                    const job = await startFinalizeJoinSession(
                      state.trackedSession?.SessionID ?? "",
                    );
                    setFeedback(`Queued finalize join job ${job.jobId}`);
                  });
                }}
                type="button"
              >
                <CheckCircle2 className="h-4 w-4" />
                <span>Finalize join</span>
              </button>
              <button
                className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                disabled={
                  normalizeRole(state.trackedSession.Status) === "completed" ||
                  pendingAction === "cancel-session"
                }
                onClick={() => {
                  void runAction("cancel-session", async () => {
                    await cancelJoinSession(
                      state.trackedSession?.SessionID ?? "",
                    );
                    setFeedback("Canceled join session");
                  });
                }}
                type="button"
              >
                <XCircle className="h-4 w-4" />
                <span>Cancel</span>
              </button>
            </div>
          </div>

          <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <div className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10">
              <p className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Session
              </p>
              <p className="text-theme-800 mt-2 font-mono text-sm break-all dark:text-white/80">
                {state.trackedSession.SessionID}
              </p>
            </div>
            <div className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10">
              <p className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Role
              </p>
              <p className="text-theme-900 mt-2 text-lg font-semibold capitalize dark:text-white">
                {state.trackedSession.Role || "pending"}
              </p>
            </div>
            <div className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10">
              <p className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Request
              </p>
              <p className="text-theme-800 mt-2 font-mono text-sm break-all dark:text-white/80">
                {state.trackedSession.RequestID || "No request id"}
              </p>
            </div>
            <div className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10">
              <p className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                Updated
              </p>
              <p className="text-theme-900 mt-2 text-lg font-semibold dark:text-white">
                {formatDateTime(state.trackedSession.UpdatedAt)}
              </p>
            </div>
          </div>
        </section>
      )}

      {state.library ? (
        <>
          <section className="grid gap-4 xl:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)]">
            <div className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:border-white/8 dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
              <div className="flex items-center gap-3">
                <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                  <KeyRound className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                    Issue invite
                  </h2>
                  <p className="text-theme-600 text-sm dark:text-white/48">
                    Create share codes for the active library. Invite management
                    requires owner or admin role.
                  </p>
                </div>
              </div>

              <div className="mt-5 grid gap-3 sm:grid-cols-3">
                <label className="block">
                  <span className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                    Role
                  </span>
                  <select
                    className="border-theme-300/75 text-theme-900 mt-2 w-full rounded-[1rem] border bg-white/82 px-4 py-3 text-sm transition outline-none focus:border-sky-400/45 dark:border-white/10 dark:bg-black/15 dark:text-white"
                    disabled={!manageLibrary}
                    onChange={(event) => {
                      setInviteRole(
                        event.target.value as (typeof inviteRoles)[number],
                      );
                    }}
                    value={inviteRole}
                  >
                    {inviteRoles.map((role) => (
                      <option
                        className="bg-theme-50 text-theme-900 dark:bg-theme-900 dark:text-theme-100"
                        key={role}
                        value={role}
                      >
                        {role}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block">
                  <span className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                    Uses
                  </span>
                  <input
                    className="border-theme-300/75 text-theme-900 mt-2 w-full rounded-[1rem] border bg-white/82 px-4 py-3 text-sm transition outline-none focus:border-sky-400/45 dark:border-white/10 dark:bg-black/15 dark:text-white"
                    disabled={!manageLibrary}
                    min="1"
                    onChange={(event) => {
                      setInviteUses(event.target.value);
                    }}
                    step="1"
                    type="number"
                    value={inviteUses}
                  />
                </label>
                <label className="block">
                  <span className="text-theme-500 text-[0.68rem] tracking-[0.24em] uppercase dark:text-white/35">
                    Expiry
                  </span>
                  <select
                    className="border-theme-300/75 text-theme-900 mt-2 w-full rounded-[1rem] border bg-white/82 px-4 py-3 text-sm transition outline-none focus:border-sky-400/45 dark:border-white/10 dark:bg-black/15 dark:text-white"
                    disabled={!manageLibrary}
                    onChange={(event) => {
                      setInviteExpiryHours(event.target.value);
                    }}
                    value={inviteExpiryHours}
                  >
                    {inviteExpiryHourOptions.map((hours) => (
                      <option
                        className="bg-theme-50 text-theme-900 dark:bg-theme-900 dark:text-theme-100"
                        key={hours}
                        value={String(hours)}
                      >
                        {hours}h
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              <div className="mt-4 flex flex-wrap gap-3">
                <button
                  className="border-theme-900 bg-theme-900 text-theme-50 hover:bg-theme-800 inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-500 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                  disabled={!manageLibrary || pendingAction === "create-invite"}
                  onClick={() => {
                    void runAction("create-invite", async () => {
                      const result = await createInviteCode(
                        new Types.InviteCodeRequest({
                          Role: inviteRole,
                          Uses: Math.max(
                            1,
                            Number.parseInt(inviteUses, 10) || 1,
                          ),
                          Expires:
                            Math.max(
                              1,
                              Number.parseInt(inviteExpiryHours, 10) || 24,
                            ) * durationHour,
                        }),
                      );
                      setLatestInvite(result);
                      setFeedback(`Created ${result.Role} invite`);
                    });
                  }}
                  type="button"
                >
                  <KeyRound className="h-4 w-4" />
                  <span>Create invite</span>
                </button>
                {!manageLibrary && (
                  <span className="border-theme-300/75 bg-theme-100 text-theme-500 rounded-full border px-3 py-2 text-xs tracking-[0.2em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/42">
                    Read only for {normalizeRole(state.local?.Role ?? "member")}
                  </span>
                )}
              </div>

              {latestInvite && (
                <div className="mt-4 rounded-[1.25rem] border border-emerald-400/18 bg-emerald-400/10 p-4">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <p className="text-theme-900 text-sm font-semibold dark:text-white">
                        Latest invite
                      </p>
                      <p className="mt-1 text-xs tracking-[0.22em] text-emerald-700 uppercase dark:text-emerald-100/72">
                        Expires {formatDateTime(latestInvite.ExpiresAt)}
                      </p>
                    </div>
                    <button
                      className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                      onClick={() => {
                        void copyText(latestInvite.InviteCode)
                          .then(() => {
                            setFeedback("Copied invite code");
                          })
                          .catch((error) => {
                            setActionError(describeError(error));
                          });
                      }}
                      type="button"
                    >
                      <Copy className="h-4 w-4" />
                      <span>Copy code</span>
                    </button>
                  </div>
                  <p className="border-theme-300/75 text-theme-800 mt-3 rounded-[1rem] border bg-white/82 p-3 font-mono text-xs break-all dark:border-white/10 dark:bg-black/15 dark:text-white/82">
                    {latestInvite.InviteCode}
                  </p>
                </div>
              )}
            </div>

            <div className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:border-white/8 dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
              <div className="flex items-center gap-3">
                <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                  <ShieldCheck className="h-5 w-5" />
                </div>
                <div>
                  <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                    Pending approvals
                  </h2>
                  <p className="text-theme-600 text-sm dark:text-white/48">
                    Approve or reject join requests for the active library.
                  </p>
                </div>
              </div>

              <div className="mt-5 space-y-3">
                {state.requests.length === 0 ? (
                  <div className="border-theme-300/75 text-theme-600 rounded-[1.2rem] border border-dashed bg-white/78 px-4 py-5 text-sm dark:border-white/10 dark:bg-black/10 dark:text-white/48">
                    No join requests recorded for this library yet.
                  </div>
                ) : (
                  state.requests.map((request) => (
                    <div
                      className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10"
                      key={request.RequestID}
                    >
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div>
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="text-theme-900 text-sm font-semibold dark:text-white">
                              {request.DeviceName || request.DeviceID}
                            </p>
                            <span
                              className={`rounded-full border px-2 py-1 text-[0.68rem] tracking-[0.22em] uppercase ${requestTone(
                                request.Status,
                              )}`}
                            >
                              {request.Status || "pending"}
                            </span>
                          </div>
                          <p className="text-theme-600 mt-2 text-sm dark:text-white/55">
                            {request.Message || "Join request pending"}
                          </p>
                          <div className="text-theme-500 mt-3 flex flex-wrap gap-2 text-xs tracking-[0.18em] uppercase dark:text-white/35">
                            <span>{request.RequestedRole || "member"}</span>
                            <span>{formatRelativeDate(request.CreatedAt)}</span>
                            <span className="text-theme-900 font-mono tracking-normal normal-case dark:text-white/45">
                              {request.RequestID}
                            </span>
                          </div>
                        </div>

                        {normalizeRole(request.Status) === "pending" &&
                        manageLibrary ? (
                          <div className="flex flex-wrap gap-2">
                            <select
                              className="border-theme-300/75 bg-theme-100 text-theme-900 rounded-full border px-3 py-2 text-xs tracking-[0.18em] uppercase outline-none dark:border-white/10 dark:bg-white/5 dark:text-white"
                              onChange={(event) => {
                                setApprovalRoles((current) => ({
                                  ...current,
                                  [request.RequestID]: event.target.value,
                                }));
                              }}
                              value={
                                approvalRoles[request.RequestID] ||
                                request.RequestedRole
                              }
                            >
                              {inviteRoles.map((role) => (
                                <option
                                  className="bg-theme-50 text-theme-900 dark:bg-theme-900 dark:text-theme-100"
                                  key={role}
                                  value={role}
                                >
                                  {role}
                                </option>
                              ))}
                            </select>
                            <button
                              className="border-theme-900 bg-theme-900 text-theme-50 hover:bg-theme-800 inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-500 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                              disabled={
                                pendingAction === `approve:${request.RequestID}`
                              }
                              onClick={() => {
                                void runAction(
                                  `approve:${request.RequestID}`,
                                  async () => {
                                    await approveJoinRequest(
                                      request.RequestID,
                                      approvalRoles[request.RequestID] ||
                                        request.RequestedRole ||
                                        "member",
                                    );
                                    setFeedback(
                                      `Approved ${request.DeviceName || request.DeviceID}`,
                                    );
                                  },
                                );
                              }}
                              type="button"
                            >
                              <CheckCircle2 className="h-4 w-4" />
                              <span>Approve</span>
                            </button>
                            <button
                              className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                              disabled={
                                pendingAction === `reject:${request.RequestID}`
                              }
                              onClick={() => {
                                void runAction(
                                  `reject:${request.RequestID}`,
                                  async () => {
                                    await rejectJoinRequest(
                                      request.RequestID,
                                      "rejected from desktop sharing page",
                                    );
                                    setFeedback(
                                      `Rejected ${request.DeviceName || request.DeviceID}`,
                                    );
                                  },
                                );
                              }}
                              type="button"
                            >
                              <XCircle className="h-4 w-4" />
                              <span>Reject</span>
                            </button>
                          </div>
                        ) : null}
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          </section>

          <section className="border-theme-300/70 shadow-theme-900/8 dark:border-theme-300/70 rounded-[1.6rem] border bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.9))] p-5 shadow-sm dark:border-white/8 dark:bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] dark:shadow-none">
            <div className="flex items-center gap-3">
              <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-11 w-11 items-center justify-center rounded-2xl border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                <KeyRound className="h-5 w-5" />
              </div>
              <div>
                <h2 className="text-theme-900 text-lg font-semibold dark:text-white">
                  Issued invites
                </h2>
                <p className="text-theme-600 text-sm dark:text-white/48">
                  Active and historical invite tokens for this library.
                </p>
              </div>
            </div>

            <div className="mt-5 space-y-3">
              {state.invites.length === 0 ? (
                <div className="border-theme-300/75 text-theme-600 rounded-[1.2rem] border border-dashed bg-white/78 px-4 py-5 text-sm dark:border-white/10 dark:bg-black/10 dark:text-white/48">
                  No invites have been issued for this library yet.
                </div>
              ) : (
                state.invites.map((invite) => (
                  <div
                    className="border-theme-300/70 rounded-[1.2rem] border bg-white/78 p-4 dark:border-white/8 dark:bg-black/10"
                    key={invite.InviteID}
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="text-theme-900 text-sm font-semibold capitalize dark:text-white">
                            {invite.Role} invite
                          </p>
                          <span
                            className={`rounded-full border px-2 py-1 text-[0.68rem] tracking-[0.22em] uppercase ${requestTone(
                              invite.Status,
                            )}`}
                          >
                            {invite.Status}
                          </span>
                        </div>
                        <p className="text-theme-600 mt-2 text-sm dark:text-white/55">
                          {invite.RedemptionCount}/{invite.MaxUses} redemption
                          {invite.MaxUses === 1 ? "" : "s"} used
                        </p>
                        <p className="text-theme-500 mt-2 font-mono text-xs break-all dark:text-white/42">
                          {invite.InviteCode}
                        </p>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <button
                          className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                          onClick={() => {
                            void copyText(invite.InviteCode)
                              .then(() => {
                                setFeedback("Copied invite code");
                              })
                              .catch((error) => {
                                setActionError(describeError(error));
                              });
                          }}
                          type="button"
                        >
                          <Copy className="h-4 w-4" />
                          <span>Copy</span>
                        </button>
                        <button
                          className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex items-center gap-2 rounded-md border bg-white/82 px-3 py-2 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-600 dark:hover:bg-zinc-800"
                          disabled={
                            !manageLibrary ||
                            normalizeRole(invite.Status) !== "active" ||
                            pendingAction === `revoke:${invite.InviteID}`
                          }
                          onClick={() => {
                            void runAction(
                              `revoke:${invite.InviteID}`,
                              async () => {
                                await revokeIssuedInvite(
                                  invite.InviteID,
                                  "revoked from desktop sharing page",
                                );
                                setFeedback("Revoked invite");
                              },
                            );
                          }}
                          type="button"
                        >
                          <XCircle className="h-4 w-4" />
                          <span>Revoke</span>
                        </button>
                      </div>
                    </div>

                    <div className="text-theme-500 mt-3 flex flex-wrap gap-3 text-xs tracking-[0.18em] uppercase dark:text-white/35">
                      <span>Expires {formatDateTime(invite.ExpiresAt)}</span>
                      <span>
                        Created {formatRelativeDate(invite.CreatedAt)}
                      </span>
                    </div>
                  </div>
                ))
              )}
            </div>
          </section>
        </>
      ) : (
        <section className="border-theme-300/75 rounded-[1.6rem] border border-dashed bg-white/78 px-8 py-10 text-center dark:border-white/10 dark:bg-black/10">
          <div className="border-theme-300/75 bg-theme-100 text-theme-400 mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border dark:border-white/10 dark:bg-white/5 dark:text-white/40">
            <UserPlus className="h-5 w-5" />
          </div>
          <h2 className="text-theme-900 text-lg font-semibold dark:text-white/90">
            Join flow works without an active library
          </h2>
          <p className="text-theme-500 mx-auto mt-2 max-w-2xl text-sm dark:text-white/50">
            Peer connect, invite issuance, and join request management depend on
            an active library. Starting or refreshing a join session from an
            invite code remains available so a fresh device can enter the
            system.
          </p>
        </section>
      )}
    </div>
  );
}
