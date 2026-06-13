# App Observability, Tracing, and Logging Revamp Plan

Date: 2026-06-13
Status: design and implementation plan

This document defines the replacement for the current ad hoc debug systems. The
goal is a single observability system that provides:

- Always-on structured logs suitable for support and user problem reports.
- Activated trace sessions suitable for debugging, regression analysis,
  performance investigations, and AI-assisted root cause analysis.
- Explicit causality across frontend, Wails bindings, backend services,
  goroutines, background jobs, peer/network work, and relayd.
- A hard removal path for the old playback/network debug traces and scattered
  logging.

The design intentionally does not preserve the old debug APIs. It keeps the
existing inspector reports only as deterministic diagnostics, not as the live
trace system.

## Research Summary

The design uses the following sources as the baseline:

- [OpenTelemetry traces](https://opentelemetry.io/docs/concepts/signals/traces/)
  define traces as trees/graphs of spans. A span has name, parent, context,
  timestamps, attributes, events, links, status, and errors. This maps directly
  to app operations and service decisions.
- [OpenTelemetry context propagation](https://opentelemetry.io/docs/concepts/context-propagation/)
  is the standard way to correlate traces, logs, metrics, and causality across
  process or service boundaries.
- [W3C Trace Context](https://www.w3.org/TR/trace-context/) defines
  `traceparent` and `tracestate`, including the sampled flag. We should use
  compatible IDs and propagation even if the first implementation is local.
- [OpenTelemetry Go instrumentation](https://opentelemetry.io/docs/languages/go/instrumentation/)
  gives the right manual instrumentation model: create spans at operation
  boundaries, attach attributes, record events and errors, and propagate
  context.
- [OpenTelemetry Go sampling](https://opentelemetry.io/docs/languages/go/sampling/)
  reinforces that sampling decisions should happen at trace start and propagate
  to child work.
- [OpenTelemetry logs data model](https://opentelemetry.io/docs/specs/otel/logs/data-model/)
  gives the log fields we should mirror: timestamp, severity, body,
  attributes, trace ID, and span ID.
- [OpenTelemetry semantic conventions](https://opentelemetry.io/docs/specs/semconv/)
  give stable names for common attributes. We should use them where they apply
  and use `ben.*` attributes for app-specific data.
- [Go slog](https://pkg.go.dev/log/slog) is the standard structured logging API.
  Its default logger also receives output from the older `log` package when
  `slog.SetDefault` is used, which lets us migrate direct `log.Printf` calls
  incrementally.
- [Go runtime/trace](https://pkg.go.dev/runtime/trace) provides low-level
  execution traces with tasks, regions, and logs. It should be optional and
  tied to performance trace sessions, not always on.
- [Go execution trace tooling](https://go.dev/blog/execution-traces-2024) is
  useful for goroutine scheduling and latency investigations that normal app
  spans cannot explain.
- [Wails v3 application docs](https://v3.wails.io/reference/application/) show
  that the app can receive a `*slog.Logger`, configure log level, and expose
  a raw message handler. The pinned module confirms
  `application.Options.Logger *slog.Logger` and `LogLevel slog.Level`.
- [Wails v3 bridge docs](https://v3.wails.io/concepts/bridge/) describe a direct
  Go/JS bridge. Because it is not standard HTTP, we should explicitly pass
  trace context through Wails service calls instead of depending on transport
  internals.
- [Wails v3 events](https://v3.wails.io/features/events/system/) can be used for
  trace-session status updates and frontend/backend observability events.
- [MDN User Timing](https://developer.mozilla.org/en-US/docs/Web/API/Performance_API/User_timing)
  and the [Performance Timeline specification](https://www.w3.org/TR/performance-timeline/)
  are the right frontend primitives for marks, measures, and performance entry
  collection.
- [OpenTelemetry Browser JS docs](https://opentelemetry.io/docs/languages/js/getting-started/browser/)
  currently describe browser instrumentation as experimental. We should borrow
  the model but implement a small local frontend tracer first.

## Current State

### Existing debug and logging systems

The current codebase has several separate mechanisms:

- `api/types/trace.go` defines a custom `trace-<unixnano>-<seq>` context value.
  It is not W3C-compatible and is not used as a whole-app trace system.
- `internal/desktopcore/network_debug.go` stores an enabled/disabled global
  in-memory ring buffer for network entries.
- `internal/playback/debug_trace.go` stores an enabled/disabled global in-memory
  ring buffer for playback entries.
- `networkdebugutil.go`, `runtime_logger.go`, and the playback service methods
  expose those ad hoc buffers to the frontend.
- `frontend/src/lib/network/debugTrace.ts` and
  `frontend/src/lib/playback/debugTrace.ts` keep frontend-specific buffers and
  debug helpers.
- `frontend/src/components/debug/DebugControlPanel.tsx` exposes playback and
  network trace toggles.
- `corehost.go`, `playbackservice.go`, `internal/playback/session.go`,
  `relayd/main.go`, and transport code use direct `log.Printf` or tiny local
  logger interfaces.
- `relayd` has Prometheus metrics but not structured logs or trace context.

### Existing diagnostics that should stay

The inspector commands under `cmd/beninspect` and `internal/desktopcore` produce
deterministic reports such as recording, album, playback context, cache, and
blob traces. These are valuable read-only diagnostics. They should be preserved,
but renamed conceptually to diagnostic reports so they are not confused with
live trace sessions.

They should later be linkable from a trace bundle:

- A trace session can mention `diagnostic_report_id`.
- A diagnostic report can include `source_trace_id` and `source_session_id`
  when created from an active trace investigation.

## Target Architecture

### Core decision

Build a local `internal/observability` package instead of adopting the full
OpenTelemetry SDK immediately.

Reasons:

- The Go OpenTelemetry logs signal is still not the strongest fit for local
  desktop log files and support bundles.
- Browser OpenTelemetry instrumentation is still experimental.
- Wails is a direct bridge, not HTTP, so automatic propagation will be limited.
- The app needs local, privacy-aware, human-readable and AI-readable bundles.
- The implementation can still use W3C trace IDs, OpenTelemetry-like fields,
  and semantic conventions so a future exporter remains possible.

The package should be designed so an OTLP exporter can be added later without
rewriting instrumentation.

### Package layout

Add:

- `internal/observability`
  - context propagation
  - span lifecycle
  - event/log record schema
  - session activation
  - samplers
  - redaction and summarization
  - rotating file sinks
  - in-memory recent event ring
  - runtime trace integration
- `internal/observability/obslog`
  - `slog.Handler` implementation
  - level policy
  - log file rotation
  - support bundle log selection
- `internal/observability/obstest`
  - test recorder
  - assertions for spans/events/logs
  - deterministic ID/time helpers
- `api/types/observability.go`
  - Wails-safe DTOs for trace context, session config, session status,
    exported records, and redacted summaries.
- `frontend/src/lib/observability`
  - frontend trace context generation
  - Wails API wrapping
  - frontend event batching
  - User Timing integration
  - support bundle helpers
- `frontend/src/components/observability`
  - trace session control panel
  - recent logs/traces view
  - export/share bundle actions

Add a Wails-facing service:

- `ObservabilityFacade`
  - `GetStatus(ctx, carrier)`
  - `SetLogLevel(ctx, carrier, level)`
  - `StartTraceSession(ctx, carrier, config)`
  - `StopTraceSession(ctx, carrier, sessionID)`
  - `ListTraceSessions(ctx, carrier, query)`
  - `ExportTraceSession(ctx, carrier, sessionID, options)`
  - `ClearTraceSessions(ctx, carrier, query)`
  - `RecordFrontendEvents(ctx, carrier, batch)`
  - `GetRecentRecords(ctx, carrier, filter)`
  - `CreateSupportBundle(ctx, carrier, options)`

### Signal model

The system has three main signals:

1. Logs
   - Always on.
   - Structured and privacy-preserving.
   - Rotated on disk.
   - Correlated with trace/span IDs when a span is active.

2. Traces
   - Off by default except for tiny, low-cost request IDs in logs.
   - Activated by user setting, debug UI, CLI/env, or error-triggered capture.
   - Records spans, span events, input summaries, output summaries, errors,
     links, decisions, and performance timings.

3. Profiles and execution traces
   - Optional add-ons for trace sessions.
   - Use `runtime/trace`, `pprof`, and browser `PerformanceObserver`.
   - Intended for performance investigations, not normal support logging.

## Trace Context

### IDs

Use W3C-compatible IDs:

- `trace_id`: 16 random bytes encoded as 32 lowercase hex characters.
- `span_id`: 8 random bytes encoded as 16 lowercase hex characters.
- `trace_flags`: at minimum support the sampled bit.
- `traceparent`: `00-<trace_id>-<span_id>-<flags>`.
- `tracestate`: reserved for future remote exporters.

Remove the old `trace-<unixnano>-<seq>` IDs.

### Context carrier

Add this DTO in `api/types/observability.go`:

```go
type TraceCarrier struct {
    Traceparent string            `json:"traceparent,omitempty"`
    Tracestate  string            `json:"tracestate,omitempty"`
    Baggage     map[string]string `json:"baggage,omitempty"`
}
```

Use `Baggage` only for low-cardinality, non-secret fields such as:

- `ben.session_id`
- `ben.user_action`
- `ben.window_id`
- `ben.library_id` when safe

Do not place secrets, invite tokens, auth material, raw paths, or raw metadata
in baggage.

### Go context API

The core API should look like:

```go
ctx, span := obs.Start(ctx, "desktopcore.catalog.list_albums",
    obs.String("ben.service", "catalog"),
    obs.String("ben.operation", "list_albums"),
)
defer span.End()

span.Event("query.plan", obs.Any("input", summary))
span.SetOutput(outputSummary)
span.RecordError(err)
```

Required operations:

- `Start(ctx, name, attrs...) (context.Context, Span)`
- `FromCarrier(ctx, carrier) context.Context`
- `CarrierFromContext(ctx) TraceCarrier`
- `LinkFromContext(ctx) Link`
- `Event(ctx, name, attrs...)`
- `LogAttrs(ctx, level, msg, attrs...)`
- `RecordError(ctx, err, attrs...)`
- `SetInput(ctx, summary)`
- `SetOutput(ctx, summary)`
- `Suppress(ctx)` for hot paths or privacy-sensitive sections

The implementation must avoid allocations when tracing is inactive. Logging
still runs, but span events and deep summaries should be no-ops unless sampled.

### Async causality

Use span links when work is causally related but not a strict parent/child:

- Wails command queues a background job.
- Playback event causes store update and UI event.
- Sync receives a remote operation that triggers cache/artwork updates.
- Relay request contributes to a desktop network decision.
- Frontend route transition starts parallel API calls.

Rules:

- Parent spans are for work done directly inside the current call.
- Links are for queued, retried, resumed, remote, or fan-out work.
- Every job record should store a trace link to its enqueuing context.

## Always-On Logs

### Logger choice

Use `log/slog` everywhere.

Create one process logger during startup and:

- Pass it to `application.Options.Logger`.
- Configure Wails `LogLevel`.
- Call `slog.SetDefault(logger)` so older `log.Printf` calls are captured while
  they are being replaced.
- Replace tiny logger interfaces with `*slog.Logger` or `obslog.Logger`.

### Sinks

Default sinks:

- Rotating JSONL file sink for production.
- Optional human text sink for development console.
- In-memory recent ring for UI and support bundle previews.

Suggested Windows location:

- Logs: `%LOCALAPPDATA%\Ben\logs\`
- Trace sessions: `%LOCALAPPDATA%\Ben\traces\`
- Support bundles: `%LOCALAPPDATA%\Ben\support-bundles\`

Use the existing app data root if the codebase already centralizes this. The
implementation should hide paths behind `observability.Paths`.

### Rotation and retention

Defaults:

- File size: 10 MB.
- File count: 10 normal log files.
- Error retention: keep last 30 days or 50 MB, whichever comes first.
- Trace sessions: cap total trace storage at 2 GB by default.
- Support bundles: user-created exports are not auto-deleted unless explicitly
  requested.

The retention policy must write a structured retention event when it deletes
old files.

### Log levels

Use this policy:

- `DEBUG`: detailed service decisions useful during development or trace
  sessions.
- `INFO`: lifecycle milestones and user-visible operation outcomes.
- `WARN`: recoverable problems, retries, degraded functionality.
- `ERROR`: operation failed or user-visible functionality broke.

Avoid log-only debugging. If a detail is needed to debug a specific operation,
put it on a span event or span attribute when tracing is active.

### Required log fields

Every log record should include:

- `ts`
- `severity`
- `msg`
- `service`
- `component`
- `operation` when known
- `trace_id` when active
- `span_id` when active
- `session_id`
- `app.version`
- `process.pid`
- `go.version`
- `os`

Optional stable fields:

- `ben.library_id`
- `ben.device_id`
- `ben.peer_id`
- `ben.job_id`
- `ben.track_id`
- `ben.album_id`
- `ben.recording_id`
- `net.peer.name`
- `net.peer.port`
- `db.system`
- `rpc.service`
- `rpc.method`

### Panic and recovery

Add recovery wrappers around:

- Wails service entry points.
- Background job goroutines.
- Event subscriber goroutines.
- Transport callbacks.
- Playback session goroutines.
- Relayd HTTP handlers.

Recoveries must:

- Log at `ERROR`.
- Record the panic on the active span if present.
- Include stack trace in the log file and trace session.
- Return a sanitized error to the caller.

## Activated Trace Sessions

### Session modes

Support these modes:

- `support`: safe default. Captures spans, errors, decisions, and redacted
  summaries. Suitable for user-submitted bundles.
- `debug`: captures more input/output summaries and frontend events. Requires
  explicit opt-in.
- `performance`: captures spans, timings, browser performance entries,
  `runtime/trace`, and optional CPU/memory profiles.
- `full`: maximum detail with strict size limits. Intended for developer
  reproduction only.

### Session config

```go
type TraceSessionConfig struct {
    Mode              string   `json:"mode"`
    Services          []string `json:"services,omitempty"`
    IncludeFrontend   bool     `json:"includeFrontend"`
    IncludeRuntime    bool     `json:"includeRuntime"`
    IncludeProfiles   bool     `json:"includeProfiles"`
    IncludeLogs       bool     `json:"includeLogs"`
    RedactionLevel    string   `json:"redactionLevel"`
    MaxDurationSec    int      `json:"maxDurationSec"`
    MaxBytes          int64    `json:"maxBytes"`
    MaxEventBytes     int      `json:"maxEventBytes"`
    Trigger           string   `json:"trigger"`
}
```

Default support mode:

- All services enabled.
- Frontend enabled.
- Runtime trace disabled.
- Profiles disabled.
- Logs included.
- `RedactionLevel=safe`.
- Max duration: 15 minutes.
- Max bytes: 250 MB.
- Max event bytes: 32 KB.

Performance mode:

- Runtime trace enabled.
- Profiles optional.
- Max duration: 2 minutes by default.
- Max bytes: 500 MB by default.

### Session directory

Each session writes:

```text
<trace-root>/<start-time>-<session-id>/
  manifest.json
  spans.jsonl
  events.jsonl
  logs.jsonl
  metrics.jsonl
  frontend.jsonl
  runtime-trace.out
  cpu.pprof
  heap.pprof
  dropped.json
  summary.md
  ai-summary.json
```

The exported bundle should be a zip with the same files plus:

- `README.md`
- `environment.json`
- `privacy-redaction.json`
- `checksums.sha256`

### Manifest

`manifest.json` should include:

- schema version
- app version, build commit, build time
- OS, arch, Go version, Wails version
- frontend version/build hash
- session config
- start/end timestamps
- enabled services
- redaction level
- file list with sizes and checksums
- dropped record counters
- known app settings that are safe to include

### Record schema

Use line-delimited JSON for streamability and AI readability.

Span record:

```json
{
  "schema_version": 1,
  "signal": "span",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "parent_span_id": "7b9c1a2f3d4e5f60",
  "name": "desktopcore.catalog.list_albums",
  "service": "catalog",
  "component": "desktopcore",
  "kind": "internal",
  "start_unix_nano": 1765530000000000000,
  "end_unix_nano": 1765530000030000000,
  "duration_ms": 30.0,
  "status": "ok",
  "attrs": {
    "ben.library_id": "lib_...",
    "ben.result_count": 48
  },
  "input": {
    "summary": "album list query",
    "fields": {
      "limit": 50,
      "sort": "recent"
    },
    "redacted": false
  },
  "output": {
    "summary": "48 albums",
    "fields": {
      "count": 48,
      "has_more": true
    }
  },
  "links": [
    {
      "trace_id": "causal-trace",
      "span_id": "causal-span",
      "attrs": {
        "ben.link.reason": "queued_by"
      }
    }
  ],
  "error": null
}
```

Event record:

```json
{
  "schema_version": 1,
  "signal": "event",
  "time_unix_nano": 1765530000010000000,
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "name": "catalog.query.decision",
  "service": "catalog",
  "attrs": {
    "ben.decision": "use_cache",
    "ben.reason": "fresh_snapshot"
  }
}
```

Log record:

```json
{
  "schema_version": 1,
  "signal": "log",
  "time_unix_nano": 1765530000020000000,
  "severity": "WARN",
  "msg": "sync retry scheduled",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "service": "sync",
  "attrs": {
    "ben.retry_attempt": 2,
    "ben.retry_delay_ms": 1000
  }
}
```

### AI summary

`ai-summary.json` should be generated at stop/export time and include:

- session metadata
- top errors by service
- slowest spans by service
- critical path traces
- dropped record counts
- retry loops and repeated decisions
- frontend/backend timeline alignment
- suspected regressions if baseline data is available
- file references inside the bundle

This file is for fast future analysis. It does not replace raw JSONL records.

## Privacy And Redaction

### Redaction levels

Use three levels:

- `safe`: default. No raw paths, tokens, invite codes, auth payloads, private
  keys, local usernames, or full metadata dumps.
- `detailed`: includes more record metadata, still no secrets or private keys.
- `private`: developer-only. May include raw local paths and extended metadata
  after explicit confirmation. Still never includes keys, tokens, auth secrets,
  or file bytes.

### Never record

Never record:

- Identity private keys.
- Invite codes or invite bearer tokens.
- Registry auth tokens.
- Raw file bytes or artwork bytes.
- Full database dumps.
- Full local filesystem paths in safe/detailed mode.
- Full peer addresses unless debug/full mode and user opted in.
- Passwords or future account credentials.

### Summarizers

Add typed summarizers for:

- Wails request DTOs.
- Library/catalog queries.
- Playback commands and snapshots.
- Network peer and connection attempts.
- Sync operation batches.
- Cache/blob/artwork work.
- Jobs.
- Errors.

Summarizers should return:

```go
type Summary struct {
    Summary  string         `json:"summary"`
    Fields   map[string]any `json:"fields,omitempty"`
    Redacted bool           `json:"redacted"`
    Dropped  int            `json:"dropped,omitempty"`
}
```

Avoid reflection-based dumps by default. Reflection can be a debug-only fallback
behind redaction and size limits.

## Wails And Frontend Propagation

### Explicit Wails carrier

Do not depend on hidden Wails bridge internals for context propagation. Change
Wails-facing service methods to accept a `TraceCarrier` at the boundary.

Preferred shape:

```go
func (f *LibraryFacade) ListAlbums(ctx context.Context, carrier apitypes.TraceCarrier, req apitypes.ListAlbumsRequest) (..., error)
```

For methods that already take a request struct, embedding the carrier in the
request is acceptable only if it is consistent and generated TypeScript remains
ergonomic.

The frontend should not expose this carrier to normal components. Components
should call local API wrappers, and wrappers should inject carriers.

### Frontend wrapper pattern

Add wrappers under `frontend/src/lib/api`:

```ts
export async function traceWailsCall<TInput, TOutput>(
  service: string,
  method: string,
  input: TInput,
  call: (carrier: TraceCarrier, input: TInput) => Promise<TOutput>,
): Promise<TOutput>
```

Responsibilities:

- Start a frontend span for the user action or API call.
- Create or continue `traceparent`.
- Capture safe input summary.
- Call the generated Wails binding with the carrier.
- Capture safe output summary.
- Record frontend error and status.
- Batch frontend events to `ObservabilityFacade.RecordFrontendEvents`.
- Add User Timing marks/measures when trace session is active.

### Frontend event collection

Capture:

- Route transitions and loader timings.
- Store actions that cause service calls.
- Playback UI commands and progress decisions.
- Artwork/image loading failures and latency.
- Notification and theme service calls.
- Long tasks and relevant PerformanceObserver entries in performance mode.
- Unhandled errors and rejected promises.

Do not capture:

- Raw DOM snapshots.
- Keyboard text input.
- Full component props.
- Raw media metadata beyond safe summaries.

### Debug UI replacement

Replace `DebugControlPanel` with an Observability panel available in developer
or high-verbosity settings:

- Logs tab: recent structured logs, level filter, service filter.
- Trace tab: start/stop session, mode, services, size/duration caps.
- Sessions tab: list, export, delete, reveal in file manager.
- Support tab: create support bundle with safe defaults.
- Performance tab: runtime trace/profile options.

The old playback/network-specific panels and global helpers should be removed.
If a browser console helper is kept, it should be namespaced as:

```ts
window.__benObservability
```

## Runtime Performance Tracing

For `performance` sessions:

- Start `runtime/trace` into `runtime-trace.out`.
- Add `trace.NewTask` around high-level operations when practical.
- Add `trace.WithRegion` around known expensive sections:
  - database queries
  - scanner walks
  - catalog computation
  - sync batch application
  - transcode work
  - artwork decode/resize
  - playback state refresh
- Optionally collect CPU and heap profiles.

Runtime traces are not a replacement for app spans. They explain scheduler and
goroutine behavior when app spans show that an operation is slow.

## Service Instrumentation Rules

### Naming

Use lower snake case for service operation spans:

- `wails.library.list_albums`
- `desktopcore.catalog.list_albums`
- `desktopcore.sync.apply_remote_batch`
- `transport.libp2p.open_stream`
- `playback.session.seek`
- `frontend.playback.click_seek`
- `relayd.http.handle_relay`

Use dot-separated event names:

- `query.plan`
- `decision.cache_hit`
- `retry.scheduled`
- `state.transition`
- `network.dial.started`
- `network.dial.failed`
- `output.summary`

### Span boundaries

Create spans at:

- Wails facade entry and exit.
- Core service public methods.
- Background job execution.
- Event handlers and subscribers.
- Network protocol request handling.
- Remote peer dial/stream operations.
- Database transaction boundaries when meaningful.
- Long-running playback operations.
- Frontend user actions and API calls.

Do not create spans for tiny hot-path getters unless they are part of a sampled
trace and known to be important.

### Inputs and outputs

Every non-trivial span should record:

- Input summary near start.
- Decision events during execution.
- Output summary near end.
- Error with type, message, and safe context on failure.

Use stable IDs and counts rather than full object dumps.

### Errors

Error records should include:

- Go error string.
- Error type or category when known.
- Wrapped error chain when safe.
- Retryability.
- User-visible impact.
- Whether fallback succeeded.

### Cardinality

Avoid high-cardinality attributes in always-on logs. Put high-cardinality data
in trace-only fields and summaries, with caps.

Examples:

- Good log attr: `service=playback`, `operation=seek`, `status=failed`.
- Trace-only attr: `recording_id`, `queue_version`, `peer_id`, `file_basename`.

## Service Coverage Matrix

| Area | Span roots | Important events | Inputs/outputs |
| --- | --- | --- | --- |
| Startup/settings | app startup, config load, core open | settings loaded, fallback used, migration decision | config paths redacted, enabled features |
| Wails facades | every exported method | request received, validation failed, response sent | request/response summaries |
| Library | list/get/search/mutate | query plan, cache hit, db fallback | filters, counts, IDs |
| Scanner/ingest | scan root, import file, metadata parse | file skipped, metadata parsed, duplicate decision | path summary, media tags summary |
| Catalog | list albums/artists/recordings, compute context | sort/filter decisions, anomaly detection | query, result count, pagination |
| Playback service | play, pause, seek, queue, preload | state transition, mpv command, snapshot emit | command summary, queue/status summary |
| Playback session | session refresh, catalog loader, EOF handling | seek plan, position observation, preload decision | current/loading entry, duration/position |
| Cache/blob | blob read/write, cache lookup, eviction | hit/miss, eviction reason, corruption detected | blob IDs, sizes, counts |
| Artwork | resolve, fetch, resize, serve HTTP | cache hit, decode fail, fallback | artwork key, dimensions, size |
| Transcode | start, progress, finish, cancel | ffmpeg command summary, progress, exit | codec/format summary, duration |
| Pin/offline | pin, unpin, download, verify | queue, retry, completion, missing blob | item IDs, byte counts |
| Jobs | enqueue, start, retry, finish | dependency wait, retry scheduled, cancellation | job type, job ID, parent link |
| Sync/oplog | create op, apply local/remote, checkpoint | conflict, idempotent skip, batch applied | op counts, versions |
| Transport/libp2p | start, discover, dial, stream, direct upgrade | peer discovered, dial failed, relay fallback | peer/device IDs, addr summaries |
| Invite/identity | create/accept invite, identity load | validation, trust decision, failure | redacted invite metadata |
| Notifications/theme | notify, update theme | suppressed notification, theme loaded | notification type, theme name |
| Frontend | user action, route load, store action, API call | render-affecting decision, long task, error | route/action summary |
| Relayd | HTTP request, relay session, registry lookup | auth decision, relay open/close, retry | method/path, status, duration |

## Old System Removal

Remove or replace these files and APIs:

- `api/types/trace.go`
- `internal/desktopcore/network_debug.go`
- `internal/playback/debug_trace.go`
- `networkdebugutil.go`
- `runtime_logger.go`
- Playback trace methods in `playbackservice.go`
- Network trace methods in the network facade/service layer
- `frontend/src/lib/network/debugTrace.ts`
- `frontend/src/lib/playback/debugTrace.ts`
- `frontend/src/components/debug/DebugControlPanel.tsx`
- `frontend/src/components/playback/PlaybackDebugPanel.tsx` if it only exists
  for the old trace buffer
- `settings.NetworkTrace`
- `settings.PlaybackTrace`
- Generated bindings for old trace APIs
- Any `window.__benPlaybackDebug`, `window.__benNetworkDebug`, or
  `window.__benDebug` helper

Replace with:

- `ObservabilityFacade`
- `frontend/src/lib/observability`
- `window.__benObservability` only if a console helper is still useful
- service-specific spans and events

The existing inspector report code stays, but docs and command descriptions
should call them diagnostic reports where possible.

## Implementation Plan

### Phase 0: Decisions and scaffolding

Deliverables:

- Add `internal/observability` package skeleton.
- Add `api/types/observability.go`.
- Add schema docs and test fixtures.
- Add redaction policy tests.
- Add no-op tracer implementation and test recorder.

Key tasks:

- Define `TraceCarrier`, `TraceSessionConfig`, `TraceSessionStatus`,
  `TraceRecord`, `Summary`, and `SupportBundleOptions`.
- Implement W3C trace ID/span ID generation and parsing.
- Implement context storage and no-op fast path.
- Implement deterministic clock/ID hooks for tests.

Validation:

- Unit tests for traceparent parse/format.
- Unit tests for context propagation.
- Unit tests for redaction.

### Phase 1: Structured logging foundation

Deliverables:

- App-wide `slog` setup.
- Rotating JSONL log sink.
- In-memory recent log ring.
- Wails logger integration.
- `slog.SetDefault`.

Key tasks:

- Replace `runtime_logger.go` with observability logger setup.
- Pass logger to Wails `application.Options`.
- Replace direct `log.Printf` in app startup and playback service paths.
- Add recovery logging helpers.
- Add `relayd` structured logger initialization.

Validation:

- Logs are JSONL and parse cleanly.
- Existing `log.Printf` calls are captured.
- Log files rotate under test.
- Log records contain process/app metadata.

### Phase 2: Trace sessions and storage

Deliverables:

- Trace session manager.
- Session directory writer.
- Span/event/log JSONL sinks.
- Limits and dropped counters.
- `ObservabilityFacade` backend methods.

Key tasks:

- Implement `StartTraceSession` and `StopTraceSession`.
- Implement service filters and mode defaults.
- Implement max duration and max bytes enforcement.
- Implement manifest generation.
- Implement recent record query.
- Implement export zip with checksums.

Validation:

- Start/stop sessions from Go tests.
- Size and duration limits stop or drop records correctly.
- Export bundle contains valid manifest and checksums.

### Phase 3: Wails propagation and frontend library

Deliverables:

- Frontend observability library.
- API wrapper convention.
- Frontend event batching.
- Performance marks/measures when tracing is active.
- Observability panel replacing old debug controls.

Key tasks:

- Add generated DTO mapping for `TraceCarrier`.
- Update Wails service method signatures or request structs to receive carriers.
- Wrap frontend API calls.
- Add unhandled error/rejection capture.
- Add route/store/playback user action spans.
- Batch records with backpressure and drop counters.

Validation:

- A frontend user action produces one trace across frontend and backend.
- Batching stops when tracing is disabled.
- Dropped frontend events are counted.
- UI can start, stop, list, and export trace sessions.

### Phase 4: Backend service instrumentation

Deliverables:

- Spans and events across core services.
- Input/output summarizers.
- Async links for jobs/events.
- Panic recovery wrappers around goroutines.

Suggested order:

1. Wails facades and `corehost`.
2. Jobs/events infrastructure.
3. Playback service and `internal/playback`.
4. Library/catalog/storage read paths.
5. Cache/blob/artwork/transcode.
6. Sync/oplog/checkpoint.
7. Transport/libp2p/invite/identity.
8. Scanner/ingest/offline/pin.

Validation:

- Each area has focused tests using `obstest`.
- Representative operations show correct parent/child spans.
- Background jobs contain links to their enqueuing spans.
- Errors appear both in logs and active spans.

### Phase 5: Remove old debug systems

Deliverables:

- Delete old playback and network trace buffers.
- Delete old frontend debug trace modules and panels.
- Remove old settings fields and generated bindings.
- Replace old debug UI with Observability panel.

Key tasks:

- Remove `NetworkTrace.Enabled` and `PlaybackTrace.Enabled`.
- Remove backend methods that only served old debug dumps.
- Replace network debug calls with spans/events.
- Replace playback debug calls with spans/events.
- Update tests and generated frontend bindings.

Validation:

- `rg "DebugTrace|NetworkDebug|PlaybackTrace|__benPlaybackDebug|__benNetworkDebug"`
  finds no live old-system references.
- Observability trace sessions cover playback and network flows.

### Phase 6: Performance mode

Deliverables:

- Runtime trace integration.
- CPU/heap profile capture.
- Browser PerformanceObserver integration.
- Performance summary generation.

Key tasks:

- Guard runtime tracing so only one performance session can run at once.
- Add runtime tasks/regions around expensive backend work.
- Capture frontend marks/measures/long tasks in performance mode.
- Generate slow-span and critical-path summaries.

Validation:

- `runtime-trace.out` opens with Go trace tooling.
- Profiles are present only when requested.
- Performance session overhead is acceptable on a representative library.

### Phase 7: Relayd instrumentation

Deliverables:

- Structured relayd logs.
- Trace context extraction/injection for HTTP requests.
- Request spans and relay session spans.
- Correlation with existing Prometheus metrics.

Key tasks:

- Replace `log.Printf`/`log.Fatalf` with `slog`.
- Add middleware for `traceparent`.
- Add spans for request handling, auth decisions, registry lookups, and relay
  open/close.
- Preserve Prometheus metrics.

Validation:

- HTTP logs contain trace IDs when provided.
- Metrics remain unchanged.
- Relayd tests cover middleware and structured logs.

### Phase 8: Documentation and rollout

Deliverables:

- Developer instrumentation guide.
- Support bundle guide.
- Privacy/redaction guide.
- Regression checklist.
- Examples of good and bad span events.

Key tasks:

- Document service naming and field naming.
- Document how to add summarizers.
- Document how to run and inspect trace sessions.
- Add a short "when to log vs when to trace" guide.

Validation:

- New code review checklist includes observability coverage.
- A reproduction trace can be used to explain a known playback or network bug.

## Testing Strategy

### Unit tests

Required tests:

- Trace ID generation and parsing.
- Carrier extraction/injection.
- Context parent/child behavior.
- Span link behavior.
- Session start/stop.
- JSONL record encoding.
- Rotation and retention.
- Redaction and summarization.
- Dropped counter behavior.
- `slog` handler attribute behavior.

### Integration tests

Required tests:

- Wails facade call creates backend spans from frontend carrier.
- Background job receives a link to the caller span.
- Playback seek creates a trace with command, session, backend, and UI events.
- Network dial failure creates clear events and error records.
- Support bundle export includes manifest, logs, spans, frontend records, and
  checksums.

### Regression tests

Add golden trace tests for stable flows:

- App startup with no library.
- Library list query.
- Playback seek.
- Cache miss followed by blob fetch.
- Sync batch apply.
- Failed peer dial with fallback.

Golden tests should assert shape and important fields, not exact timestamps.

### Performance tests

Measure overhead:

- Tracing disabled.
- Support mode active.
- Debug mode active.
- Performance mode active.

Targets:

- Disabled tracing should add near-zero overhead beyond always-on logging.
- Support mode should be acceptable during normal reproduction.
- Full/performance modes can be heavier but must honor size and duration caps.

## Required Checks

For implementation PRs, run the repository checks from `AGENTS.md`:

```sh
goimports -w .
golangci-lint run ./...
go test ./...
govulncheck ./...
```

Race tests on Windows should use MSYS2 MINGW64 with libmpv installed:

```sh
CGO_ENABLED=1 CC=gcc go test -race ./...
```

Frontend checks from `frontend/`:

```sh
bun format
bun lint
bun typecheck
bun run test:run
```

Docs-only changes do not require running the full suite, but any code phase
must run the relevant checks before completion.

## Risks

- Instrumentation volume can become too high. Mitigation: strict caps,
  summarizers, service filters, and dropped counters.
- Privacy mistakes are easy in traces. Mitigation: default safe redaction,
  typed summarizers, and tests for forbidden fields.
- Wails generated bindings will churn. Mitigation: centralize frontend API
  wrappers and keep carrier handling out of components.
- Runtime trace/profile collection can be expensive. Mitigation: performance
  mode only, short duration defaults, and single active runtime trace.
- Too many span events can make traces hard to read. Mitigation: naming rules,
  service coverage matrix, and AI summary generation.
- Full removal of old debug systems will break existing debug habits. Mitigation:
  provide equivalent or better Observability panel workflows before deleting
  old UI.

## Open Decisions

- Exact app data root should be confirmed against existing settings/storage
  path conventions before implementation.
- Decide whether Wails carrier is always a separate method argument or embedded
  in request structs for methods that already have a request DTO.
- Decide whether support bundles should include recent logs outside the active
  trace session by default.
- Decide how much frontend render timing is valuable without adding React
  profiler-specific complexity.
- Decide whether to add a future OTLP exporter option for developer builds.

## Definition Of Done

The revamp is complete when:

- Always-on logs are structured, rotated, and available in support bundles.
- Trace sessions can be started/stopped/exported from the app.
- Frontend, Wails, backend services, background jobs, network flows, playback,
  and relayd can be correlated with W3C-compatible trace IDs.
- Old playback/network debug buffers, settings, frontend helpers, and UI panels
  are removed.
- Each major service has spans, decision events, errors, and input/output
  summaries.
- Privacy redaction is enforced and tested.
- Performance sessions can capture runtime traces/profiles and browser timing.
- Generated trace bundles are useful for both human review and AI-assisted bug
  diagnosis.
- All repository checks pass for code changes.
