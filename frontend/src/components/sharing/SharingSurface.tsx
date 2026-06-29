import {
  CheckCircle2,
  Copy,
  KeyRound,
  RefreshCw,
  Router,
  Send,
  Settings2,
  ShieldCheck,
  Trash2,
  UserPlus,
  XCircle,
} from "lucide-react";
import { formatDateTime, formatRelativeDate } from "@/lib/format";
import { useSharingPage } from "@/hooks/sharing/useSharingPage";

const inviteRoles = ["guest", "member", "admin"] as const;

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function normalize(value: string) {
  return value.trim().toLowerCase();
}

function joinTone(status: string) {
  switch (normalize(status)) {
    case "approved":
    case "completed":
      return "border-emerald-400/20 bg-emerald-400/10 text-emerald-700 dark:text-emerald-100";
    case "rejected":
    case "expired":
    case "failed":
      return "border-rose-400/20 bg-rose-400/10 text-rose-700 dark:text-rose-100";
    default:
      return "border-amber-400/25 bg-amber-400/10 text-amber-700 dark:text-amber-100";
  }
}

function hasUsableDate(value: Date | string | null | undefined) {
  if (!value) {
    return false;
  }
  const date = value instanceof Date ? value : new Date(value);
  return !Number.isNaN(date.getTime()) && date.getFullYear() > 1970;
}

function inviteExpiryLabel(
  value: Date | string | null | undefined,
  reusable: boolean,
) {
  if (reusable || !hasUsableDate(value)) {
    return "Reusable";
  }
  return `Expires ${formatDateTime(value)}`;
}

async function copyText(value: string) {
  const text = value.trim();
  if (!text) {
    return;
  }
  if (!navigator.clipboard?.writeText) {
    throw new Error("clipboard is not available");
  }
  await navigator.clipboard.writeText(text);
}

export function SharingSurface() {
  const {
    actionError,
    approveRequestAction,
    cancelJoinAction,
    createInviteAction,
    feedback,
    inviteCode,
    inviteReusable,
    inviteRole,
    joinDeviceName,
    manageLibrary,
    pendingAction,
    pendingRequests,
    refresh,
    rejectRequestAction,
    relayBootstrapText,
    relayOpen,
    relayRegistryURL,
    revokeInviteAction,
    runAction,
    saveRelayAction,
    setActionError,
    setFeedback,
    setInviteCode,
    setInviteReusable,
    setInviteRole,
    setJoinDeviceName,
    setRelayBootstrapText,
    setRelayOpen,
    setRelayRegistryURL,
    startJoinAction,
    state,
  } = useSharingPage();

  const libraryName = state.library?.Name || "No active library";
  const relayAddrs =
    state.network?.RelayBootstrapAddrs?.length ??
    state.relay?.RelayBootstrapAddrs?.length ??
    0;
  const canCreateInvite = manageLibrary && Boolean(state.library);
  const activeJoin = state.joinAttempt;

  if (state.loading) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center">
        <div className="border-theme-300/70 text-theme-700 rounded-lg border bg-white/80 px-4 py-3 text-sm dark:border-white/10 dark:bg-white/5 dark:text-white/65">
          Loading sharing...
        </div>
      </div>
    );
  }

  return (
    <div className="ben-scrollbar ben-shell-scroll-offset flex h-full min-h-0 flex-col gap-5 overflow-y-auto pr-1">
      <section className="border-theme-300/70 border-b pb-5 dark:border-white/8">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-theme-500 text-[0.68rem] tracking-[0.28em] uppercase dark:text-white/38">
              Sharing
            </p>
            <h1 className="text-theme-950 mt-2 text-2xl font-semibold dark:text-white">
              Invites
            </h1>
            <div className="mt-3 flex flex-wrap gap-2">
              <span className="border-theme-300/75 bg-theme-100/70 text-theme-700 rounded-full border px-3 py-1 text-xs tracking-[0.16em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/58">
                {libraryName}
              </span>
              <span className="border-theme-300/75 bg-theme-100/70 text-theme-700 rounded-full border px-3 py-1 text-xs tracking-[0.16em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/58">
                {state.invites.length} active
              </span>
              <span className="border-theme-300/75 bg-theme-100/70 text-theme-700 rounded-full border px-3 py-1 text-xs tracking-[0.16em] uppercase dark:border-white/10 dark:bg-white/5 dark:text-white/58">
                {pendingRequests.length} pending
              </span>
            </div>
          </div>
          <button
            className="border-theme-300/75 text-theme-900 hover:border-theme-400/75 hover:bg-theme-100 inline-flex h-10 items-center gap-2 rounded-md border bg-white/82 px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-white/10 dark:bg-white/5 dark:text-white dark:hover:bg-white/10"
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
        <section className="grid gap-2">
          {state.error && (
            <div className="rounded-md border border-amber-400/20 bg-amber-400/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-100">
              {state.error}
            </div>
          )}
          {actionError && (
            <div className="rounded-md border border-rose-400/20 bg-rose-400/10 px-3 py-2 text-sm text-rose-700 dark:text-rose-100">
              {actionError}
            </div>
          )}
          {feedback && (
            <div className="rounded-md border border-emerald-400/20 bg-emerald-400/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-100">
              {feedback}
            </div>
          )}
        </section>
      )}

      <section className="border-theme-300/70 rounded-lg border bg-white/78 p-4 dark:border-white/8 dark:bg-white/[0.035]">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-10 w-10 shrink-0 items-center justify-center rounded-md border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
              <Router className="h-5 w-5" />
            </div>
            <div className="min-w-0">
              <h2 className="text-theme-950 text-base font-semibold dark:text-white">
                Relay
              </h2>
              <p className="text-theme-600 truncate text-sm dark:text-white/52">
                {state.relay?.RegistryURL ||
                  state.network?.RegistryURL ||
                  "No registry configured"}
              </p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="border-theme-300/75 text-theme-600 rounded-full border px-3 py-1 text-xs tracking-[0.16em] uppercase dark:border-white/10 dark:text-white/42">
              {relayAddrs} bootstrap
            </span>
            <button
              className="border-theme-300/75 text-theme-900 hover:bg-theme-100 inline-flex h-9 items-center gap-2 rounded-md border bg-white/82 px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-white/10 dark:bg-white/5 dark:text-white dark:hover:bg-white/10"
              disabled={!state.library || pendingAction === "relay"}
              onClick={() => {
                setRelayOpen(!relayOpen);
              }}
              type="button"
            >
              <Settings2 className="h-4 w-4" />
              <span>{relayOpen ? "Close" : "Edit"}</span>
            </button>
          </div>
        </div>

        {relayOpen && (
          <div className="border-theme-300/70 mt-4 grid gap-3 border-t pt-4 dark:border-white/8">
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.22em] uppercase dark:text-white/35">
                Registry URL
              </span>
              <input
                className="border-theme-300/75 text-theme-950 mt-2 h-11 w-full rounded-md border bg-white/82 px-3 text-sm transition outline-none focus:border-sky-400/60 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setRelayRegistryURL(event.target.value);
                }}
                value={relayRegistryURL}
              />
            </label>
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.22em] uppercase dark:text-white/35">
                Bootstrap addrs
              </span>
              <textarea
                className="border-theme-300/75 text-theme-950 mt-2 min-h-24 w-full rounded-md border bg-white/82 px-3 py-2 font-mono text-xs transition outline-none focus:border-sky-400/60 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setRelayBootstrapText(event.target.value);
                }}
                value={relayBootstrapText}
              />
            </label>
            <div className="flex justify-end">
              <button
                className="border-theme-950 bg-theme-950 text-theme-50 hover:bg-theme-800 inline-flex h-10 items-center gap-2 rounded-md border px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-200 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                disabled={pendingAction === "relay"}
                onClick={() => {
                  void runAction("relay", saveRelayAction);
                }}
                type="button"
              >
                <CheckCircle2 className="h-4 w-4" />
                <span>Save</span>
              </button>
            </div>
          </div>
        )}
      </section>

      <section className="grid gap-4 xl:grid-cols-2">
        <div className="border-theme-300/70 rounded-lg border bg-white/78 p-4 dark:border-white/8 dark:bg-white/[0.035]">
          <div className="flex items-center gap-3">
            <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-10 w-10 items-center justify-center rounded-md border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
              <KeyRound className="h-5 w-5" />
            </div>
            <h2 className="text-theme-950 text-base font-semibold dark:text-white">
              Create Invite
            </h2>
          </div>

          <div className="mt-4 grid gap-3">
            <div>
              <span className="text-theme-500 text-[0.68rem] tracking-[0.22em] uppercase dark:text-white/35">
                Role
              </span>
              <div className="mt-2 grid grid-cols-3 gap-2">
                {inviteRoles.map((role) => (
                  <button
                    className={`h-10 rounded-md border px-3 text-sm capitalize transition ${
                      inviteRole === role
                        ? "border-theme-950 bg-theme-950 text-theme-50 dark:border-zinc-100 dark:bg-zinc-100 dark:text-zinc-950"
                        : "border-theme-300/75 text-theme-800 hover:bg-theme-100 bg-white/82 dark:border-white/10 dark:bg-white/5 dark:text-white/70 dark:hover:bg-white/10"
                    }`}
                    disabled={!canCreateInvite}
                    key={role}
                    onClick={() => {
                      setInviteRole(role);
                    }}
                    type="button"
                  >
                    {role}
                  </button>
                ))}
              </div>
            </div>

            <div className="border-theme-300/75 flex items-center justify-between gap-3 rounded-md border bg-white/70 px-3 py-2 dark:border-white/10 dark:bg-black/15">
              <div>
                <p className="text-theme-900 text-sm font-medium dark:text-white">
                  Reusable
                </p>
                <p className="text-theme-500 text-xs dark:text-white/42">
                  {inviteReusable ? "No expiry" : "Single-use, 24h"}
                </p>
              </div>
              <button
                aria-pressed={inviteReusable}
                className={`h-6 w-11 rounded-full p-1 transition ${
                  inviteReusable
                    ? "bg-theme-950 dark:bg-zinc-100"
                    : "bg-theme-300 dark:bg-white/16"
                }`}
                disabled={!canCreateInvite}
                onClick={() => {
                  setInviteReusable(!inviteReusable);
                }}
                type="button"
              >
                <span
                  className={`block h-4 w-4 rounded-full bg-white transition dark:bg-zinc-950 ${
                    inviteReusable ? "translate-x-5" : "translate-x-0"
                  }`}
                />
              </button>
            </div>

            <button
              className="border-theme-950 bg-theme-950 text-theme-50 hover:bg-theme-800 inline-flex h-11 items-center justify-center gap-2 rounded-md border px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-200 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
              disabled={!canCreateInvite || pendingAction === "create-invite"}
              onClick={() => {
                void runAction("create-invite", createInviteAction);
              }}
              type="button"
            >
              <KeyRound className="h-4 w-4" />
              <span>Create</span>
            </button>
          </div>
        </div>

        <div className="border-theme-300/70 rounded-lg border bg-white/78 p-4 dark:border-white/8 dark:bg-white/[0.035]">
          <div className="flex items-center gap-3">
            <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-10 w-10 items-center justify-center rounded-md border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
              <UserPlus className="h-5 w-5" />
            </div>
            <h2 className="text-theme-950 text-base font-semibold dark:text-white">
              Join With Code
            </h2>
          </div>

          <div className="mt-4 grid gap-3">
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.22em] uppercase dark:text-white/35">
                Invite code
              </span>
              <textarea
                className="border-theme-300/75 text-theme-950 mt-2 min-h-24 w-full rounded-md border bg-white/82 px-3 py-2 font-mono text-xs transition outline-none focus:border-sky-400/60 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setInviteCode(event.target.value);
                }}
                placeholder="ben-invite-v4..."
                value={inviteCode}
              />
            </label>
            <label className="block">
              <span className="text-theme-500 text-[0.68rem] tracking-[0.22em] uppercase dark:text-white/35">
                Device name
              </span>
              <input
                className="border-theme-300/75 text-theme-950 mt-2 h-11 w-full rounded-md border bg-white/82 px-3 text-sm transition outline-none focus:border-sky-400/60 dark:border-white/10 dark:bg-black/15 dark:text-white"
                onChange={(event) => {
                  setJoinDeviceName(event.target.value);
                }}
                placeholder={state.local?.Device || "Current device"}
                value={joinDeviceName}
              />
            </label>
            <div className="flex flex-wrap gap-2">
              <button
                className="border-theme-950 bg-theme-950 text-theme-50 hover:bg-theme-800 inline-flex h-10 items-center gap-2 rounded-md border px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-200 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                disabled={!inviteCode.trim() || pendingAction === "start-join"}
                onClick={() => {
                  void runAction("start-join", startJoinAction);
                }}
                type="button"
              >
                <Send className="h-4 w-4" />
                <span>Join</span>
              </button>
              {activeJoin?.Pending && (
                <button
                  className="border-theme-300/75 text-theme-900 hover:bg-theme-100 inline-flex h-10 items-center gap-2 rounded-md border bg-white/82 px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-white/10 dark:bg-white/5 dark:text-white dark:hover:bg-white/10"
                  disabled={pendingAction === "cancel-join"}
                  onClick={() => {
                    void runAction("cancel-join", cancelJoinAction);
                  }}
                  type="button"
                >
                  <XCircle className="h-4 w-4" />
                  <span>Cancel</span>
                </button>
              )}
            </div>

            {activeJoin && (
              <div
                className={`rounded-md border px-3 py-2 text-sm ${joinTone(
                  activeJoin.Status,
                )}`}
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="font-medium capitalize">
                    {activeJoin.Status || "pending"}
                  </span>
                  {activeJoin.Role && (
                    <span className="text-xs tracking-[0.16em] uppercase">
                      {activeJoin.Role}
                    </span>
                  )}
                </div>
                {activeJoin.Message && (
                  <p className="mt-1 text-sm opacity-80">
                    {activeJoin.Message}
                  </p>
                )}
              </div>
            )}
          </div>
        </div>
      </section>

      {state.library && (
        <section className="grid gap-4 xl:grid-cols-2">
          <div className="border-theme-300/70 rounded-lg border bg-white/78 p-4 dark:border-white/8 dark:bg-white/[0.035]">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-10 w-10 items-center justify-center rounded-md border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                  <KeyRound className="h-5 w-5" />
                </div>
                <h2 className="text-theme-950 text-base font-semibold dark:text-white">
                  Active Invites
                </h2>
              </div>
            </div>

            <div className="mt-4 grid gap-2">
              {state.invites.length === 0 ? (
                <div className="border-theme-300/75 text-theme-500 rounded-md border border-dashed bg-white/55 px-3 py-4 text-sm dark:border-white/10 dark:bg-black/10 dark:text-white/42">
                  No active invites.
                </div>
              ) : (
                state.invites.map((invite) => (
                  <div
                    className="border-theme-300/70 rounded-md border bg-white/82 p-3 dark:border-white/8 dark:bg-black/15"
                    key={invite.InviteID}
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-theme-950 text-sm font-semibold capitalize dark:text-white">
                            {invite.Role || "member"}
                          </span>
                          <span className="border-theme-300/75 text-theme-500 rounded-full border px-2 py-0.5 text-[0.68rem] tracking-[0.14em] uppercase dark:border-white/10 dark:text-white/42">
                            {inviteExpiryLabel(
                              invite.ExpiresAt,
                              invite.Reusable,
                            )}
                          </span>
                        </div>
                        <p className="text-theme-500 mt-2 font-mono text-xs break-all dark:text-white/45">
                          {invite.InviteCode}
                        </p>
                        <p className="text-theme-500 mt-2 text-xs dark:text-white/35">
                          Created {formatRelativeDate(invite.CreatedAt)}
                        </p>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <button
                          className="border-theme-300/75 text-theme-900 hover:bg-theme-100 inline-flex h-9 items-center gap-2 rounded-md border bg-white/82 px-3 text-sm transition dark:border-white/10 dark:bg-white/5 dark:text-white dark:hover:bg-white/10"
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
                          className="border-theme-300/75 text-theme-900 hover:bg-theme-100 inline-flex h-9 items-center gap-2 rounded-md border bg-white/82 px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-white/10 dark:bg-white/5 dark:text-white dark:hover:bg-white/10"
                          disabled={
                            !manageLibrary ||
                            pendingAction === `revoke:${invite.InviteID}`
                          }
                          onClick={() => {
                            void runAction(
                              `revoke:${invite.InviteID}`,
                              async () => revokeInviteAction(invite.InviteID),
                            );
                          }}
                          type="button"
                        >
                          <Trash2 className="h-4 w-4" />
                          <span>Revoke</span>
                        </button>
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="border-theme-300/70 rounded-lg border bg-white/78 p-4 dark:border-white/8 dark:bg-white/[0.035]">
            <div className="flex items-center gap-3">
              <div className="border-theme-300/75 bg-theme-100 text-theme-700 flex h-10 w-10 items-center justify-center rounded-md border dark:border-white/10 dark:bg-white/5 dark:text-white/72">
                <ShieldCheck className="h-5 w-5" />
              </div>
              <h2 className="text-theme-950 text-base font-semibold dark:text-white">
                Pending Requests
              </h2>
            </div>

            <div className="mt-4 grid gap-2">
              {pendingRequests.length === 0 ? (
                <div className="border-theme-300/75 text-theme-500 rounded-md border border-dashed bg-white/55 px-3 py-4 text-sm dark:border-white/10 dark:bg-black/10 dark:text-white/42">
                  No pending requests.
                </div>
              ) : (
                pendingRequests.map((request) => (
                  <div
                    className="border-theme-300/70 rounded-md border bg-white/82 p-3 dark:border-white/8 dark:bg-black/15"
                    key={request.RequestID}
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-theme-950 text-sm font-semibold dark:text-white">
                            {request.DeviceName || request.DeviceID}
                          </span>
                          <span className="rounded-full border border-amber-400/25 bg-amber-400/10 px-2 py-0.5 text-[0.68rem] tracking-[0.14em] text-amber-700 uppercase dark:text-amber-100">
                            {request.Role || "member"}
                          </span>
                        </div>
                        <p className="text-theme-500 mt-2 font-mono text-xs break-all dark:text-white/45">
                          {request.DeviceFingerprint ||
                            request.PeerID ||
                            request.DeviceID}
                        </p>
                        <p className="text-theme-500 mt-2 text-xs dark:text-white/35">
                          Requested {formatRelativeDate(request.CreatedAt)}
                        </p>
                      </div>
                      {manageLibrary && (
                        <div className="flex flex-wrap gap-2">
                          <button
                            className="border-theme-950 bg-theme-950 text-theme-50 hover:bg-theme-800 inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-zinc-200 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-white"
                            disabled={
                              pendingAction === `approve:${request.RequestID}`
                            }
                            onClick={() => {
                              void runAction(
                                `approve:${request.RequestID}`,
                                async () =>
                                  approveRequestAction(request.RequestID),
                              );
                            }}
                            type="button"
                          >
                            <CheckCircle2 className="h-4 w-4" />
                            <span>Approve</span>
                          </button>
                          <button
                            className="border-theme-300/75 text-theme-900 hover:bg-theme-100 inline-flex h-9 items-center gap-2 rounded-md border bg-white/82 px-3 text-sm transition disabled:cursor-default disabled:opacity-50 dark:border-white/10 dark:bg-white/5 dark:text-white dark:hover:bg-white/10"
                            disabled={
                              pendingAction === `reject:${request.RequestID}`
                            }
                            onClick={() => {
                              void runAction(
                                `reject:${request.RequestID}`,
                                async () =>
                                  rejectRequestAction(request.RequestID),
                              );
                            }}
                            type="button"
                          >
                            <XCircle className="h-4 w-4" />
                            <span>Reject</span>
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </section>
      )}
    </div>
  );
}
