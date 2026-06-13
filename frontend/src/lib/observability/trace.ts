import * as ObservabilityFacade from "../../../bindings/ben/desktop/observabilityfacade";
import { Types } from "@/lib/api/models";

const FLUSH_DELAY_MS = 750;
const MAX_BATCH_SIZE = 100;

type TraceAttrs = Record<string, unknown>;

let active = false;
let pending: Types.TraceRecord[] = [];
let flushTimer: number | undefined;

export function setFrontendTracingActive(nextActive: boolean) {
  active = nextActive;
  if (!active) {
    pending = [];
    if (flushTimer !== undefined) {
      window.clearTimeout(flushTimer);
      flushTimer = undefined;
    }
  }
}

export function isFrontendTracingActive() {
  return active;
}

export function createTraceCarrier(): Types.TraceCarrier {
  const traceId = randomHex(16);
  const spanId = randomHex(8);
  return new Types.TraceCarrier({
    traceparent: `00-${traceId}-${spanId}-01`,
  });
}

export async function traceWailsCall<T>(
  service: string,
  method: string,
  input: TraceAttrs | undefined,
  call: () => Promise<T>,
): Promise<T> {
  if (!active) {
    return call();
  }

  const traceId = randomHex(16);
  const spanId = randomHex(8);
  const startUnixNano = nowUnixNano();
  const mark = `ben:${service}.${method}:${spanId}`;
  performance.mark(`${mark}:start`);

  try {
    const output = await call();
    const endUnixNano = nowUnixNano();
    performance.mark(`${mark}:end`);
    performance.measure(
      `ben.${service}.${method}`,
      `${mark}:start`,
      `${mark}:end`,
    );
    enqueueRecord(
      new Types.TraceRecord({
        schemaVersion: 1,
        signal: "span",
        traceId,
        spanId,
        name: `frontend.${service}.${method}`,
        service,
        component: "frontend",
        kind: "client",
        startUnixNano,
        endUnixNano,
        durationMs: Math.max(0, (endUnixNano - startUnixNano) / 1_000_000),
        status: "ok",
        input: input
          ? new Types.TraceSummary({
              summary: "frontend call input",
              fields: input,
              redacted: true,
            })
          : undefined,
        output: summarizeOutput(output),
      }),
    );
    return output;
  } catch (error) {
    const endUnixNano = nowUnixNano();
    enqueueRecord(
      new Types.TraceRecord({
        schemaVersion: 1,
        signal: "span",
        traceId,
        spanId,
        name: `frontend.${service}.${method}`,
        service,
        component: "frontend",
        kind: "client",
        startUnixNano,
        endUnixNano,
        durationMs: Math.max(0, (endUnixNano - startUnixNano) / 1_000_000),
        status: "error",
        input: input
          ? new Types.TraceSummary({
              summary: "frontend call input",
              fields: input,
              redacted: true,
            })
          : undefined,
        error: new Types.TraceError({
          message: error instanceof Error ? error.message : String(error),
          type: error instanceof Error ? error.name : "Error",
        }),
      }),
    );
    throw error;
  } finally {
    performance.clearMarks(`${mark}:start`);
    performance.clearMarks(`${mark}:end`);
  }
}

export function recordFrontendEvent(
  name: string,
  attrs: TraceAttrs = {},
  service = "frontend",
) {
  if (!active) {
    return;
  }
  enqueueRecord(
    new Types.TraceRecord({
      schemaVersion: 1,
      signal: "event",
      timeUnixNano: nowUnixNano(),
      name,
      service,
      component: "frontend",
      attrs,
    }),
  );
}

export function installObservabilityWindow() {
  const target = window as typeof window & {
    __benObservability?: {
      active: () => boolean;
      flush: () => Promise<void>;
      event: (name: string, attrs?: TraceAttrs, service?: string) => void;
      carrier: () => Types.TraceCarrier;
    };
  };
  target.__benObservability = {
    active: isFrontendTracingActive,
    flush: flushFrontendTraceRecords,
    event: recordFrontendEvent,
    carrier: createTraceCarrier,
  };
}

export async function flushFrontendTraceRecords() {
  if (pending.length === 0) {
    return;
  }
  const records = pending;
  pending = [];
  flushTimer = undefined;
  await ObservabilityFacade.RecordFrontendEvents(
    createTraceCarrier(),
    new Types.FrontendTraceBatch({ records }),
  );
}

function enqueueRecord(record: Types.TraceRecord) {
  pending.push(record);
  if (pending.length >= MAX_BATCH_SIZE) {
    void flushFrontendTraceRecords();
    return;
  }
  if (flushTimer === undefined) {
    flushTimer = window.setTimeout(() => {
      void flushFrontendTraceRecords();
    }, FLUSH_DELAY_MS);
  }
}

function summarizeOutput(output: unknown): Types.TraceSummary {
  if (Array.isArray(output)) {
    return new Types.TraceSummary({
      summary: "array output",
      fields: { count: output.length },
    });
  }
  if (output && typeof output === "object") {
    return new Types.TraceSummary({
      summary: "object output",
      fields: { keys: Object.keys(output).slice(0, 16) },
    });
  }
  return new Types.TraceSummary({
    summary: typeof output,
  });
}

function nowUnixNano() {
  return Date.now() * 1_000_000;
}

function randomHex(bytes: number) {
  const data = new Uint8Array(bytes);
  crypto.getRandomValues(data);
  return Array.from(data, (value) => value.toString(16).padStart(2, "0")).join(
    "",
  );
}
