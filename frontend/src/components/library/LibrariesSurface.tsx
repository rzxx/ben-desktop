import type { ReactNode } from "react";
import {
  CircleAlert,
  FolderCog,
  Plus,
  RefreshCw,
  UsersRound,
} from "lucide-react";
import { formatCount, formatRelativeDate } from "@/lib/format";
import { MetricPill } from "@/components/catalog/MetricPill";
import {
  normalizeLibraryRole,
  useLibrariesPage,
} from "@/hooks/library/useLibrariesPage";
import { formatDateTime } from "@/lib/format";

function EmptyState({
  body,
  icon,
  title,
}: {
  body: string;
  icon: ReactNode;
  title: string;
}) {
  return (
    <div className="rounded-[1.6rem] border border-dashed border-white/10 bg-black/10 px-8 py-10 text-center">
      <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/40">
        {icon}
      </div>
      <h2 className="text-lg font-semibold text-white/90">{title}</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-white/50">{body}</p>
    </div>
  );
}

export function LibrariesSurface() {
  const {
    actionError,
    actions,
    canManage,
    createName,
    notice,
    pendingAction,
    refresh,
    renameName,
    runAction,
    setCreateName,
    setRenameName,
    state,
  } = useLibrariesPage();

  if (state.loading) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center">
        <div className="rounded-[1.4rem] border border-white/8 bg-black/15 px-5 py-4 text-sm text-white/65">
          Loading library management...
        </div>
      </div>
    );
  }

  return (
    <div className="ben-scrollbar ben-shell-scroll-offset flex h-full min-h-0 flex-col gap-4 overflow-y-auto pr-1">
      <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(135deg,rgba(34,197,94,0.16),transparent_42%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
              Libraries
            </p>
            <h1 className="mt-3 text-3xl font-semibold text-white">
              Library management
            </h1>
            <p className="mt-3 max-w-3xl text-sm leading-6 text-white/55">
              Manage desktop-core libraries directly through the rewritten Wails
              facades. This screen covers library selection, lifecycle actions,
              and active-library membership controls.
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              <MetricPill
                label={formatCount(state.libraries.length, "library")}
              />
              <MetricPill
                label={
                  state.active
                    ? `Active: ${state.active.Name}`
                    : "No active library"
                }
              />
              <MetricPill
                label={
                  state.local?.Device ||
                  state.local?.DeviceID ||
                  "Unknown device"
                }
              />
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

        <form
          className="mt-5 flex flex-wrap gap-3"
          onSubmit={(event) => {
            event.preventDefault();
            const nextName = createName.trim();
            if (!nextName) {
              return;
            }
            void runAction(
              "create-library",
              async () => {
                await actions.createLibrary(nextName);
                setCreateName("");
              },
              `Created library ${nextName}`,
            );
          }}
        >
          <input
            className="min-w-[220px] flex-1 rounded-[1rem] border border-white/10 bg-black/15 px-4 py-3 text-sm text-white transition outline-none placeholder:text-white/24 focus:border-emerald-300/50"
            onChange={(event) => {
              setCreateName(event.target.value);
            }}
            placeholder="New library name"
            value={createName}
          />
          <button
            className="inline-flex items-center gap-2 rounded-md border border-zinc-500 bg-zinc-100 px-3 py-2 text-sm text-zinc-950 transition hover:bg-white disabled:cursor-default disabled:opacity-50"
            disabled={pendingAction === "create-library"}
            type="submit"
          >
            <Plus className="h-4 w-4" />
            <span>Create library</span>
          </button>
        </form>
      </section>

      {(state.error || actionError || notice) && (
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
          {notice && (
            <div className="rounded-[1.25rem] border border-emerald-400/18 bg-emerald-400/10 px-4 py-3 text-sm text-emerald-100">
              {notice}
            </div>
          )}
        </section>
      )}

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
        <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
              <FolderCog className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">
                Your libraries
              </h2>
              <p className="text-sm text-white/48">
                Select which library owns the active desktop runtime.
              </p>
            </div>
          </div>

          <div className="mt-5 space-y-3">
            {state.libraries.length === 0 ? (
              <EmptyState
                body="Create your first library to start the rewritten desktop-core runtime."
                icon={<FolderCog className="h-5 w-5" />}
                title="No libraries yet"
              />
            ) : (
              state.libraries.map((library) => {
                const isActive = library.IsActive;
                return (
                  <div
                    className={`rounded-[1.2rem] border px-4 py-4 ${
                      isActive
                        ? "border-emerald-300/30 bg-emerald-300/10"
                        : "border-white/8 bg-black/10"
                    }`}
                    key={library.LibraryID}
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="text-base font-semibold text-white">
                            {library.Name}
                          </p>
                          <span className="rounded-full border border-white/10 bg-white/5 px-2 py-1 text-[0.65rem] tracking-[0.22em] text-white/52 uppercase">
                            {library.Role}
                          </span>
                          {isActive && (
                            <span className="rounded-full border border-emerald-300/30 bg-emerald-300/12 px-2 py-1 text-[0.65rem] tracking-[0.22em] text-emerald-100 uppercase">
                              Active
                            </span>
                          )}
                        </div>
                        <p className="mt-2 text-sm break-all text-white/50">
                          {library.LibraryID}
                        </p>
                        <p className="mt-1 text-sm text-white/42">
                          Joined {formatRelativeDate(library.JoinedAt)}
                        </p>
                      </div>
                      {!isActive && (
                        <button
                          className="inline-flex items-center gap-2 rounded-md border border-zinc-500 bg-zinc-100 px-3 py-2 text-sm text-zinc-950 transition hover:bg-white disabled:cursor-default disabled:opacity-50"
                          disabled={
                            pendingAction === `select:${library.LibraryID}`
                          }
                          onClick={() => {
                            void runAction(
                              `select:${library.LibraryID}`,
                              () => actions.selectLibrary(library.LibraryID),
                              `Activated ${library.Name}`,
                            );
                          }}
                          type="button"
                        >
                          <span>Activate</span>
                        </button>
                      )}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        <div className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.045),rgba(255,255,255,0.015))] p-5">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-white/72">
              <UsersRound className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">
                Active library
              </h2>
              <p className="text-sm text-white/48">
                Rename the active library and manage its current members.
              </p>
            </div>
          </div>

          {!state.active ? (
            <div className="mt-5">
              <EmptyState
                body="Activate a library to inspect members and run management actions."
                icon={<CircleAlert className="h-5 w-5" />}
                title="No active library"
              />
            </div>
          ) : (
            <>
              <div className="mt-5 rounded-[1.2rem] border border-white/8 bg-black/10 p-4">
                <p className="text-[0.68rem] tracking-[0.26em] text-white/35 uppercase">
                  Active library
                </p>
                <p className="mt-2 text-lg font-semibold text-white">
                  {state.active.Name}
                </p>
                <p className="mt-1 text-sm text-white/48">
                  Role {state.active.Role}
                </p>

                <form
                  className="mt-4 flex flex-wrap gap-3"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const nextName = renameName.trim();
                    if (!nextName) {
                      return;
                    }
                    void runAction(
                      "rename-library",
                      () =>
                        actions.renameLibrary(
                          state.active!.LibraryID,
                          nextName,
                        ),
                      `Renamed library to ${nextName}`,
                    );
                  }}
                >
                  <input
                    className="min-w-[220px] flex-1 rounded-[1rem] border border-white/10 bg-black/15 px-4 py-3 text-sm text-white transition outline-none placeholder:text-white/24 focus:border-emerald-300/50"
                    disabled={!canManage}
                    onChange={(event) => {
                      setRenameName(event.target.value);
                    }}
                    placeholder="Library name"
                    value={renameName}
                  />
                  <button
                    className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                    disabled={!canManage || pendingAction === "rename-library"}
                    type="submit"
                  >
                    <span>Rename</span>
                  </button>
                  <button
                    className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                    disabled={pendingAction === "leave-library"}
                    onClick={() => {
                      void runAction(
                        "leave-library",
                        () => actions.leaveLibrary(state.active!.LibraryID),
                        `Left ${state.active!.Name}`,
                      );
                    }}
                    type="button"
                  >
                    <span>Leave</span>
                  </button>
                  <button
                    className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                    disabled={!canManage || pendingAction === "delete-library"}
                    onClick={() => {
                      if (
                        !window.confirm(
                          `Delete library "${state.active!.Name}"?`,
                        )
                      ) {
                        return;
                      }
                      void runAction(
                        "delete-library",
                        () => actions.deleteLibrary(state.active!.LibraryID),
                        `Deleted ${state.active!.Name}`,
                      );
                    }}
                    type="button"
                  >
                    <span>Delete</span>
                  </button>
                </form>
              </div>

              <div className="mt-5 space-y-3">
                {state.members.map((member) => {
                  const isLocalDevice =
                    member.DeviceID === state.local?.DeviceID;
                  const normalizedRole = normalizeLibraryRole(member.Role);
                  const roleOptions =
                    normalizedRole === "owner"
                      ? ["owner", "admin", "member", "guest"]
                      : ["admin", "member", "guest"];

                  return (
                    <div
                      className="rounded-[1.2rem] border border-white/8 bg-black/10 px-4 py-4"
                      key={member.DeviceID}
                    >
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="font-semibold text-white">
                              {member.DeviceID}
                            </p>
                            {isLocalDevice && (
                              <span className="rounded-full border border-white/10 bg-white/5 px-2 py-1 text-[0.65rem] tracking-[0.22em] text-white/52 uppercase">
                                This device
                              </span>
                            )}
                          </div>
                          <p className="mt-2 text-sm text-white/48">
                            Peer {member.PeerID || "unassigned"}
                          </p>
                          <p className="mt-1 text-sm text-white/42">
                            Last seen {formatDateTime(member.LastSeenAt)}
                          </p>
                          <p className="mt-1 text-sm text-white/42">
                            Last successful sync{" "}
                            {formatDateTime(member.LastSyncSuccessAt)}
                          </p>
                          {member.LastSyncError && (
                            <p className="mt-2 text-sm text-rose-200">
                              {member.LastSyncError}
                            </p>
                          )}
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          <select
                            className="rounded-[0.95rem] border border-white/10 bg-black/20 px-3 py-2 text-sm text-white outline-none"
                            defaultValue={normalizedRole}
                            disabled={
                              !canManage ||
                              isLocalDevice ||
                              normalizedRole === "owner"
                            }
                            onChange={(event) => {
                              const nextRole = event.target.value;
                              void runAction(
                                `role:${member.DeviceID}`,
                                () =>
                                  actions.updateLibraryMemberRole(
                                    member.DeviceID,
                                    nextRole,
                                  ),
                                `Updated ${member.DeviceID} to ${nextRole}`,
                              );
                            }}
                          >
                            {roleOptions.map((role) => (
                              <option key={role} value={role}>
                                {role}
                              </option>
                            ))}
                          </select>
                          <button
                            className="inline-flex items-center gap-2 rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-100 transition hover:border-zinc-600 hover:bg-zinc-800 disabled:cursor-default disabled:opacity-50"
                            disabled={
                              !canManage ||
                              isLocalDevice ||
                              pendingAction === `remove:${member.DeviceID}`
                            }
                            onClick={() => {
                              if (
                                !window.confirm(
                                  `Remove ${member.DeviceID} from the library?`,
                                )
                              ) {
                                return;
                              }
                              void runAction(
                                `remove:${member.DeviceID}`,
                                () =>
                                  actions.removeLibraryMember(member.DeviceID),
                                `Removed ${member.DeviceID}`,
                              );
                            }}
                            type="button"
                          >
                            <span>Remove</span>
                          </button>
                        </div>
                      </div>
                    </div>
                  );
                })}

                {state.members.length === 0 && (
                  <div className="rounded-[1.2rem] border border-dashed border-white/10 bg-black/10 px-4 py-5 text-sm text-white/48">
                    No members found for the active library.
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </section>
    </div>
  );
}
