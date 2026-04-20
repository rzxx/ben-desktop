import { useEffect, useMemo, useState } from "react";
import { Activity, Radio, SlidersHorizontal } from "lucide-react";
import { useDebugControlPanelSnapshot } from "@/hooks/debug/useDebugControlPanelSnapshot";
import { useNetworkDebugSnapshot } from "@/hooks/network/useNetworkDebugSnapshot";
import { usePlaybackDebugSnapshot } from "@/hooks/playback/usePlaybackDebugSnapshot";
import {
  installDebugControlWindow,
  setDebugControlPanelVisible,
} from "@/lib/debug/controlPanel";
import {
  clearNetworkDebugState,
  copyNetworkDebugDump,
  installNetworkDebugWindow,
  refreshNetworkDebugState,
  setNetworkTraceEnabled,
  syncNetworkTraceEnabled,
} from "@/lib/network/debugTrace";
import {
  clearPlaybackDebugState,
  copyPlaybackDebugDump,
  installPlaybackDebugWindow,
  setPlaybackTraceEnabled,
  syncPlaybackTraceEnabled,
} from "@/lib/playback/debugTrace";

type DebugTab = "playback" | "network";

function formatTraceValue(value: number | null | undefined) {
  if (value == null) {
    return "-";
  }
  return `${Math.round(value)}`;
}

function formatTimestamp(timestampMs: number | null | undefined) {
  if (
    timestampMs == null ||
    !Number.isFinite(timestampMs) ||
    timestampMs <= 0
  ) {
    return "-";
  }
  return new Date(timestampMs).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function summarizeNetworkStatus(
  snapshot: ReturnType<typeof useNetworkDebugSnapshot>,
) {
  if (!snapshot.enabled) {
    return "Network trace is disabled";
  }
  if (snapshot.error) {
    return snapshot.error;
  }
  if (!snapshot.status) {
    return "Waiting for network status";
  }
  if (!snapshot.status.Running) {
    return "Runtime transport is not running";
  }
  if (snapshot.status.LastSyncError?.trim()) {
    return snapshot.status.LastSyncError.trim();
  }
  if (snapshot.status.ActivePeerID?.trim()) {
    return `Active peer ${snapshot.status.ActivePeerID}`;
  }
  return "Waiting for network activity";
}

export function DebugControlPanel() {
  const panel = useDebugControlPanelSnapshot();
  const playback = usePlaybackDebugSnapshot();
  const network = useNetworkDebugSnapshot();
  const [requestedTab, setRequestedTab] = useState<DebugTab>("playback");
  const [copyStatus, setCopyStatus] = useState("");

  useEffect(() => {
    installDebugControlWindow();
    installPlaybackDebugWindow();
    installNetworkDebugWindow();
    void syncPlaybackTraceEnabled();
    void syncNetworkTraceEnabled();
  }, []);

  const tab: DebugTab =
    requestedTab === "playback" && !playback.enabled && network.enabled
      ? "network"
      : requestedTab === "network" && !network.enabled && playback.enabled
        ? "playback"
        : requestedTab;

  const recentPlaybackTrace = useMemo(
    () => playback.trace.slice(-18).reverse(),
    [playback.trace],
  );
  const recentNetworkTrace = useMemo(
    () => network.trace.slice(-18).reverse(),
    [network.trace],
  );

  const copyCurrentDump = async () => {
    if (tab === "network") {
      return copyNetworkDebugDump();
    }
    return copyPlaybackDebugDump();
  };

  const clearCurrentTrace = async () => {
    if (tab === "network") {
      return clearNetworkDebugState();
    }
    return clearPlaybackDebugState();
  };

  if (!panel.visible) {
    return (
      <button
        className="bg-theme-900/85 text-theme-50 fixed right-4 bottom-24 z-50 rounded-md px-2 py-1 text-[11px] font-medium shadow-lg shadow-black/30"
        onClick={() => {
          setDebugControlPanelVisible(true);
        }}
        type="button"
      >
        Debug
      </button>
    );
  }

  return (
    <aside className="bg-theme-950/92 text-theme-50 fixed top-12 right-4 z-50 w-[32rem] max-w-[calc(100vw-2rem)] rounded-xl border border-white/10 p-3 shadow-2xl shadow-black/40 backdrop-blur-xl">
      <div className="flex items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Debug Controls</h2>
          <p className="text-theme-300 text-[11px]">
            Global traces for playback and networking. Window helper:
            <code className="ml-1">__benDebug.toggle()</code>
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            className="rounded-md border border-white/15 px-2 py-1 text-[11px]"
            onClick={() => {
              void clearCurrentTrace().then(() => {
                setCopyStatus("");
              });
            }}
            type="button"
          >
            Clear
          </button>
          <button
            className="rounded-md border border-white/15 px-2 py-1 text-[11px]"
            onClick={() => {
              setDebugControlPanelVisible(false);
            }}
            type="button"
          >
            Hide
          </button>
          <button
            className="bg-accent-200 text-accent-950 rounded-md px-2 py-1 text-[11px] font-medium"
            onClick={() => {
              void copyCurrentDump()
                .then(() => {
                  setCopyStatus("Copied");
                  window.setTimeout(() => setCopyStatus(""), 1500);
                })
                .catch((error) => {
                  setCopyStatus(String(error));
                });
            }}
            type="button"
          >
            Copy Dump
          </button>
        </div>
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2">
        <button
          className={`rounded-md border px-3 py-2 text-left text-[11px] transition ${
            playback.enabled
              ? "border-emerald-400/30 bg-emerald-400/12"
              : "border-white/10 bg-white/5"
          }`}
          onClick={() => {
            void setPlaybackTraceEnabled(!playback.enabled);
          }}
          type="button"
        >
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">Playback Trace</span>
            <span>{playback.enabled ? "On" : "Off"}</span>
          </div>
          <p className="text-theme-300 mt-1">
            Capture frontend and backend playback timing state.
          </p>
        </button>
        <button
          className={`rounded-md border px-3 py-2 text-left text-[11px] transition ${
            network.enabled
              ? "border-sky-400/30 bg-sky-400/12"
              : "border-white/10 bg-white/5"
          }`}
          onClick={() => {
            void setNetworkTraceEnabled(!network.enabled);
          }}
          type="button"
        >
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">Network Trace</span>
            <span>{network.enabled ? "On" : "Off"}</span>
          </div>
          <p className="text-theme-300 mt-1">
            Capture dial, connect, disconnect, and sync activity.
          </p>
        </button>
      </div>

      <div className="mt-3 flex items-center gap-2 text-[11px]">
        <button
          className={`rounded-md border px-2 py-1 ${
            tab === "playback"
              ? "border-white/30 bg-white/12"
              : "border-white/10"
          }`}
          onClick={() => {
            setRequestedTab("playback");
          }}
          type="button"
        >
          <span className="inline-flex items-center gap-1">
            <Activity className="h-3.5 w-3.5" />
            Playback
          </span>
        </button>
        <button
          className={`rounded-md border px-2 py-1 ${
            tab === "network"
              ? "border-white/30 bg-white/12"
              : "border-white/10"
          }`}
          onClick={() => {
            setRequestedTab("network");
          }}
          type="button"
        >
          <span className="inline-flex items-center gap-1">
            <Radio className="h-3.5 w-3.5" />
            Network
          </span>
        </button>
        {tab === "network" ? (
          <button
            className="ml-auto rounded-md border border-white/10 px-2 py-1"
            onClick={() => {
              void refreshNetworkDebugState();
            }}
            type="button"
          >
            <span className="inline-flex items-center gap-1">
              <SlidersHorizontal className="h-3.5 w-3.5" />
              Refresh
            </span>
          </button>
        ) : null}
      </div>

      {copyStatus ? (
        <p className="text-theme-300 mt-2 text-[11px]">{copyStatus}</p>
      ) : null}

      {tab === "playback" ? (
        <>
          <div className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-[11px] tabular-nums">
            <span className="text-theme-300">enabled</span>
            <span>{playback.enabled ? "yes" : "no"}</span>
            <span className="text-theme-300">entry</span>
            <span className="truncate">
              {playback.liveState.currentEntryId || "-"}
            </span>
            <span className="text-theme-300">status</span>
            <span>{playback.liveState.status || "-"}</span>
            <span className="text-theme-300">transport</span>
            <span>
              {formatTraceValue(playback.liveState.transportPositionMs)}
            </span>
            <span className="text-theme-300">transport capture</span>
            <span>
              {formatTraceValue(playback.liveState.transportCapturedAtMs)}
            </span>
            <span className="text-theme-300">shown</span>
            <span>{formatTraceValue(playback.liveState.shownPositionMs)}</span>
            <span className="text-theme-300">draft</span>
            <span>{formatTraceValue(playback.liveState.draftPositionMs)}</span>
            <span className="text-theme-300">pending seek</span>
            <span>{formatTraceValue(playback.liveState.pendingSeekMs)}</span>
          </div>

          <div className="mt-3">
            <h3 className="text-theme-300 text-[11px] font-medium tracking-wide uppercase">
              Recent Playback Events
            </h3>
            <div className="mt-2 max-h-64 space-y-1 overflow-auto pr-1 text-[11px]">
              {recentPlaybackTrace.length === 0 ? (
                <div className="text-theme-300 rounded-md border border-white/8 bg-white/5 px-3 py-2">
                  No playback events captured yet.
                </div>
              ) : (
                recentPlaybackTrace.map((entry) => (
                  <div
                    className="rounded-md border border-white/8 bg-white/5 px-2 py-1"
                    key={`${entry.timestampMs}-${entry.kind}-${entry.seekRequestId ?? ""}-${entry.positionMs ?? ""}`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">{entry.kind}</span>
                      <span className="text-theme-300">
                        {entry.timestampMs}ms
                      </span>
                    </div>
                    <div className="text-theme-300 mt-1 flex flex-wrap gap-x-2 gap-y-1">
                      {entry.seekRequestId ? (
                        <span>seek={entry.seekRequestId}</span>
                      ) : null}
                      {entry.positionMs != null ? (
                        <span>pos={entry.positionMs}</span>
                      ) : null}
                      {entry.shownPositionMs != null ? (
                        <span>shown={entry.shownPositionMs}</span>
                      ) : null}
                      {entry.pendingSeekMs != null ? (
                        <span>pending={entry.pendingSeekMs}</span>
                      ) : null}
                      {entry.message ? <span>{entry.message}</span> : null}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </>
      ) : (
        <>
          <div className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-[11px] tabular-nums">
            <span className="text-theme-300">enabled</span>
            <span>{network.enabled ? "yes" : "no"}</span>
            <span className="text-theme-300">running</span>
            <span>{network.status?.Running ? "yes" : "no"}</span>
            <span className="text-theme-300">summary</span>
            <span className="truncate">{summarizeNetworkStatus(network)}</span>
            <span className="text-theme-300">mode</span>
            <span>{network.status?.Mode || "-"}</span>
            <span className="text-theme-300">reason</span>
            <span>{network.status?.Reason || "-"}</span>
            <span className="text-theme-300">active peer</span>
            <span className="truncate">
              {network.status?.ActivePeerID || network.status?.PeerID || "-"}
            </span>
            <span className="text-theme-300">last error</span>
            <span className="truncate">
              {network.status?.LastSyncError || network.error || "-"}
            </span>
          </div>

          <div className="mt-3">
            <h3 className="text-theme-300 text-[11px] font-medium tracking-wide uppercase">
              Recent Network Events
            </h3>
            <div className="mt-2 max-h-64 space-y-1 overflow-auto pr-1 text-[11px]">
              {recentNetworkTrace.length === 0 ? (
                <div className="text-theme-300 rounded-md border border-white/8 bg-white/5 px-3 py-2">
                  No network events captured yet.
                </div>
              ) : (
                recentNetworkTrace.map((entry) => (
                  <div
                    className="rounded-md border border-white/8 bg-white/5 px-2 py-1"
                    key={`${entry.TimestampMS}-${entry.Kind}-${entry.PeerID}-${entry.Address}`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">
                          {entry.Kind || "event"}
                        </span>
                        {entry.Level ? (
                          <span className="rounded-full border border-white/10 px-1.5 py-0.5 text-[10px] uppercase">
                            {entry.Level}
                          </span>
                        ) : null}
                      </div>
                      <span className="text-theme-300">
                        {formatTimestamp(entry.TimestampMS)}
                      </span>
                    </div>
                    <div className="text-theme-300 mt-1 flex flex-wrap gap-x-2 gap-y-1">
                      {entry.Message ? <span>{entry.Message}</span> : null}
                      {entry.PeerID ? <span>peer={entry.PeerID}</span> : null}
                      {entry.Address ? <span>addr={entry.Address}</span> : null}
                      {entry.DeviceID ? (
                        <span>device={entry.DeviceID}</span>
                      ) : null}
                      {entry.Reason ? <span>reason={entry.Reason}</span> : null}
                      {entry.Error ? <span>error={entry.Error}</span> : null}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </>
      )}
    </aside>
  );
}
