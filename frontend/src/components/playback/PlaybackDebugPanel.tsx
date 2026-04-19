import { useEffect, useState } from "react";
import { usePlaybackDebugSnapshot } from "@/hooks/playback/usePlaybackDebugSnapshot";
import {
  clearPlaybackDebugState,
  copyPlaybackDebugDump,
  installPlaybackDebugWindow,
  setPlaybackDebugPanelVisible,
  syncPlaybackTraceEnabled,
} from "@/lib/playback/debugTrace";

function formatTraceValue(value: number | null | undefined) {
  if (value == null) {
    return "-";
  }
  return `${Math.round(value)}`;
}

export function PlaybackDebugPanel() {
  const snapshot = usePlaybackDebugSnapshot();
  const [copyStatus, setCopyStatus] = useState("");

  useEffect(() => {
    installPlaybackDebugWindow();
    void syncPlaybackTraceEnabled();
  }, []);

  if (!snapshot.enabled) {
    return null;
  }

  if (!snapshot.visible) {
    return (
      <button
        className="bg-theme-900/85 text-theme-50 fixed right-4 bottom-24 z-50 rounded-md px-2 py-1 text-[11px] font-medium shadow-lg shadow-black/30"
        onClick={() => {
          setPlaybackDebugPanelVisible(true);
        }}
        type="button"
      >
        Playback Trace
      </button>
    );
  }

  const recentTrace = snapshot.trace.slice(-18).reverse();

  return (
    <aside className="bg-theme-950/92 text-theme-50 fixed top-12 right-4 z-50 w-[28rem] max-w-[calc(100vw-2rem)] rounded-xl border border-white/10 p-3 shadow-2xl shadow-black/40 backdrop-blur-xl">
      <div className="flex items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Playback Trace</h2>
          <p className="text-theme-300 text-[11px]">
            Use Copy Dump after a repro. Window helper:
            <code className="ml-1">__benPlaybackDebug.copy()</code>
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            className="rounded-md border border-white/15 px-2 py-1 text-[11px]"
            onClick={() => {
              void clearPlaybackDebugState();
              setCopyStatus("");
            }}
            type="button"
          >
            Clear
          </button>
          <button
            className="rounded-md border border-white/15 px-2 py-1 text-[11px]"
            onClick={() => {
              setPlaybackDebugPanelVisible(false);
            }}
            type="button"
          >
            Hide
          </button>
          <button
            className="bg-accent-200 text-accent-950 rounded-md px-2 py-1 text-[11px] font-medium"
            onClick={() => {
              void copyPlaybackDebugDump()
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

      <div className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-[11px] tabular-nums">
        <span className="text-theme-300">entry</span>
        <span className="truncate">
          {snapshot.liveState.currentEntryId || "-"}
        </span>
        <span className="text-theme-300">status</span>
        <span>{snapshot.liveState.status || "-"}</span>
        <span className="text-theme-300">transport</span>
        <span>{formatTraceValue(snapshot.liveState.transportPositionMs)}</span>
        <span className="text-theme-300">transport capture</span>
        <span>
          {formatTraceValue(snapshot.liveState.transportCapturedAtMs)}
        </span>
        <span className="text-theme-300">shown</span>
        <span>{formatTraceValue(snapshot.liveState.shownPositionMs)}</span>
        <span className="text-theme-300">draft</span>
        <span>{formatTraceValue(snapshot.liveState.draftPositionMs)}</span>
        <span className="text-theme-300">pending seek</span>
        <span>{formatTraceValue(snapshot.liveState.pendingSeekMs)}</span>
        <span className="text-theme-300">pending request</span>
        <span className="truncate">
          {snapshot.liveState.pendingSeekRequestId || "-"}
        </span>
        <span className="text-theme-300">dragging</span>
        <span>{snapshot.liveState.isDragging ? "yes" : "no"}</span>
      </div>

      {copyStatus ? (
        <p className="text-theme-300 mt-2 text-[11px]">{copyStatus}</p>
      ) : null}

      <div className="mt-3">
        <h3 className="text-theme-300 text-[11px] font-medium tracking-wide uppercase">
          Recent Frontend Events
        </h3>
        <div className="mt-2 max-h-64 space-y-1 overflow-auto pr-1 text-[11px]">
          {recentTrace.map((entry) => (
            <div
              className="rounded-md border border-white/8 bg-white/5 px-2 py-1"
              key={`${entry.timestampMs}-${entry.kind}-${entry.seekRequestId ?? ""}-${entry.positionMs ?? ""}`}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium">{entry.kind}</span>
                <span className="text-theme-300">{entry.timestampMs}ms</span>
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
          ))}
        </div>
      </div>
    </aside>
  );
}
