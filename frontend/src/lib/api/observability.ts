import * as ObservabilityFacade from "../../../bindings/ben/desktop/observabilityfacade";
import { Types } from "./models";
import {
  createTraceCarrier,
  installObservabilityWindow,
  setFrontendTracingActive,
} from "@/lib/observability/trace";

export type ObservabilityStatus = Types.ObservabilityStatus;
export type TraceSessionConfig = Types.TraceSessionConfig;
export type TraceSessionStatus = Types.TraceSessionStatus;
export type TraceSessionSummary = Types.TraceSessionSummary;
export type TraceRecord = Types.TraceRecord;
export type TraceExportResult = Types.TraceExportResult;

export async function getObservabilityStatus() {
  const status = await ObservabilityFacade.GetStatus(createTraceCarrier());
  setFrontendTracingActive(status.traceSession.active);
  return status;
}

export async function setLogLevel(level: string) {
  const status = await ObservabilityFacade.SetLogLevel(
    createTraceCarrier(),
    level,
  );
  setFrontendTracingActive(status.traceSession.active);
  return status;
}

export async function startTraceSession(
  config: InstanceType<typeof Types.TraceSessionConfig>,
) {
  const status = await ObservabilityFacade.StartTraceSession(
    createTraceCarrier(),
    config,
  );
  setFrontendTracingActive(status.active);
  return status;
}

export async function stopTraceSession(sessionId = "") {
  const status = await ObservabilityFacade.StopTraceSession(
    createTraceCarrier(),
    sessionId,
  );
  setFrontendTracingActive(status.active);
  return status;
}

export function listTraceSessions(limit = 20) {
  return ObservabilityFacade.ListTraceSessions(
    createTraceCarrier(),
    new Types.TraceSessionQuery({ limit }),
  );
}

export function exportTraceSession(sessionId: string, includeLogs = true) {
  return ObservabilityFacade.ExportTraceSession(
    createTraceCarrier(),
    sessionId,
    new Types.TraceExportOptions({ includeLogs }),
  );
}

export function getRecentTraceRecords(limit = 100, signal = "", service = "") {
  return ObservabilityFacade.GetRecentRecords(
    createTraceCarrier(),
    new Types.RecentTraceFilter({ limit, signal, service }),
  );
}

export function makeDefaultTraceSessionConfig(mode = "support") {
  return new Types.TraceSessionConfig({
    mode,
    includeFrontend: true,
    includeLogs: true,
    redactionLevel: "safe",
    trigger: "user",
  });
}

export function installObservabilityHelpers() {
  installObservabilityWindow();
}
