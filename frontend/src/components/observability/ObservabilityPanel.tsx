import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity,
  Download,
  Play,
  RefreshCw,
  Square,
  TerminalSquare,
} from "lucide-react";
import {
  exportTraceSession,
  getObservabilityStatus,
  getRecentTraceRecords,
  installObservabilityHelpers,
  listTraceSessions,
  makeDefaultTraceSessionConfig,
  startTraceSession,
  stopTraceSession,
  type ObservabilityStatus,
  type TraceRecord,
  type TraceSessionSummary,
} from "@/lib/api/observability";

export function ObservabilityPanel() {
  const [status, setStatus] = useState<ObservabilityStatus | null>(null);
  const [recent, setRecent] = useState<TraceRecord[]>([]);
  const [sessions, setSessions] = useState<TraceSessionSummary[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [exportPath, setExportPath] = useState("");

  const activeSessionId = status?.traceSession.sessionId ?? "";
  const active = status?.traceSession.active ?? false;

  const refresh = useCallback(async () => {
    const [nextStatus, nextRecent, nextSessions] = await Promise.all([
      getObservabilityStatus(),
      getRecentTraceRecords(80),
      listTraceSessions(8),
    ]);
    setStatus(nextStatus);
    setRecent(nextRecent);
    setSessions(nextSessions);
  }, []);

  useEffect(() => {
    installObservabilityHelpers();
  }, []);

  useEffect(() => {
    const refreshPanel = async () => {
      try {
        await refresh();
        setError("");
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      }
    };
    const initialTimer = window.setTimeout(() => {
      void refreshPanel();
    }, 0);
    const timer = window.setInterval(
      () => {
        void refreshPanel();
      },
      active ? 2000 : 5000,
    );
    return () => {
      window.clearTimeout(initialTimer);
      window.clearInterval(timer);
    };
  }, [active, refresh]);

  const recentRows = useMemo(() => recent.slice(-12).reverse(), [recent]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    setExportPath("");
    try {
      await action();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="wails-no-drag border-theme-300/30 bg-theme-50/95 text-theme-900 dark:bg-theme-950/95 dark:text-theme-100 fixed top-10 right-4 z-60 w-[min(34rem,calc(100vw-2rem))] overflow-hidden rounded border shadow-xl backdrop-blur dark:border-white/10">
      <header className="border-theme-300/30 flex items-center justify-between border-b px-3 py-2 dark:border-white/10">
        <div className="flex min-w-0 items-center gap-2">
          <Activity className="text-accent-600 dark:text-accent-300 h-4 w-4" />
          <h2 className="truncate text-sm font-semibold">Observability</h2>
        </div>
        <div className="flex items-center gap-1">
          <button
            className="text-theme-600 hover:bg-theme-200 hover:text-theme-950 dark:text-theme-300 dark:hover:bg-theme-800 dark:hover:text-theme-50 rounded p-1.5 disabled:opacity-50"
            disabled={busy}
            onClick={() => void run(refresh)}
            title="Refresh"
            type="button"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
          {active ? (
            <button
              className="rounded p-1.5 text-red-700 hover:bg-red-100 disabled:opacity-50 dark:text-red-300 dark:hover:bg-red-950"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await stopTraceSession(activeSessionId);
                })
              }
              title="Stop trace"
              type="button"
            >
              <Square className="h-4 w-4" />
            </button>
          ) : (
            <button
              className="rounded p-1.5 text-emerald-700 hover:bg-emerald-100 disabled:opacity-50 dark:text-emerald-300 dark:hover:bg-emerald-950"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await startTraceSession(makeDefaultTraceSessionConfig());
                })
              }
              title="Start trace"
              type="button"
            >
              <Play className="h-4 w-4" />
            </button>
          )}
          <button
            className="text-theme-600 hover:bg-theme-200 hover:text-theme-950 dark:text-theme-300 dark:hover:bg-theme-800 dark:hover:text-theme-50 rounded p-1.5 disabled:opacity-50"
            disabled={busy || !activeSessionId}
            onClick={() =>
              void run(async () => {
                const result = await exportTraceSession(activeSessionId, true);
                setExportPath(result.path);
              })
            }
            title="Export trace"
            type="button"
          >
            <Download className="h-4 w-4" />
          </button>
        </div>
      </header>

      <div className="grid gap-3 p-3 text-xs">
        <div className="grid grid-cols-2 gap-2">
          <Metric label="Log level" value={status?.logLevel ?? "INFO"} />
          <Metric label="Trace" value={active ? "active" : "idle"} />
          <Metric label="Session" value={activeSessionId || "none"} />
          <Metric
            label="Records"
            value={`${status?.traceSession.recordsWritten ?? 0}`}
          />
        </div>

        {error ? (
          <div className="rounded border border-red-300 bg-red-50 px-2 py-1.5 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-100">
            {error}
          </div>
        ) : null}
        {exportPath ? (
          <div className="rounded border border-emerald-300 bg-emerald-50 px-2 py-1.5 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-100">
            {exportPath}
          </div>
        ) : null}

        <div>
          <div className="text-theme-500 dark:text-theme-400 mb-1 flex items-center gap-1">
            <TerminalSquare className="h-3.5 w-3.5" />
            <span>Recent records</span>
          </div>
          <div className="border-theme-300/30 max-h-56 overflow-auto rounded border dark:border-white/10">
            {recentRows.length === 0 ? (
              <div className="text-theme-500 dark:text-theme-400 px-2 py-3">
                No records
              </div>
            ) : (
              recentRows.map((record, index) => (
                <div
                  className="border-theme-300/20 grid grid-cols-[5rem_6rem_1fr] gap-2 border-b px-2 py-1.5 last:border-b-0 dark:border-white/8"
                  key={`${record.timeUnixNano ?? record.startUnixNano ?? index}:${record.name ?? index}`}
                >
                  <span className="text-theme-500 dark:text-theme-400 truncate">
                    {record.signal}
                  </span>
                  <span className="truncate">{record.service || "-"}</span>
                  <span className="truncate">
                    {record.name || record.message}
                  </span>
                </div>
              ))
            )}
          </div>
        </div>

        {sessions.length > 0 ? (
          <div className="grid gap-1">
            {sessions.map((session) => (
              <button
                className="border-theme-300/25 hover:bg-theme-100 dark:hover:bg-theme-900 grid grid-cols-[1fr_auto] gap-2 rounded border px-2 py-1.5 text-left dark:border-white/10"
                key={session.sessionId}
                onClick={() =>
                  void run(async () => {
                    const result = await exportTraceSession(
                      session.sessionId,
                      true,
                    );
                    setExportPath(result.path);
                  })
                }
                type="button"
              >
                <span className="truncate">{session.sessionId}</span>
                <span className="text-theme-500 dark:text-theme-400">
                  {session.mode || "support"}
                </span>
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="border-theme-300/25 min-w-0 rounded border px-2 py-1.5 dark:border-white/10">
      <div className="text-theme-500 dark:text-theme-400">{label}</div>
      <div className="truncate font-medium">{value}</div>
    </div>
  );
}
