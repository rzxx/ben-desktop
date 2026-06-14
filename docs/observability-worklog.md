# Observability Worklog

Started: 2026-06-13

## Goal

Replace the old playback/network debug traces and scattered logging with the
whole-app observability system described in `docs/observability-tracing-logging-plan.md`.

## Current Focus

- Implementation pass complete.

## Decisions

- Use local `internal/observability` instead of full OTel SDK for the first
  implementation.
- Use W3C-compatible `traceparent` IDs.
- Use `log/slog` as the always-on logger; remove old app `log.Printf` call
  sites rather than keeping a hidden legacy path.
- Prefer explicit Wails `TraceCarrier` arguments/request fields over bridge
  internals.
- Trace sessions are opt-in, local JSONL artifacts with manifest/summary/export
  zip; always-on logs are rotating JSONL.
- Frontend emits only while backend trace session is active; wrapper creates
  spans around Wails calls and sends batched frontend events.

## Progress

- Added planning document.
- Added backend observability package: IDs, context propagation, spans/events,
  redaction, rotating logs, trace sessions, export, recent record buffers.
- Added Wails observability facade and generated bindings.
- Wired app startup to initialize `slog`/observability and expose the facade.
- Removed legacy playback/network debug trace systems, DTOs, panels, hooks, and
  Wails methods.
- Added trace coverage to high-value facades, core startup, playback seek/store
  paths, network events, artwork serving, theme generation, relayd requests, and
  platform integration errors.
- Added frontend observability API wrappers, trace carrier propagation,
  runtime error/performance event capture, and an observability control panel.
- Migrated settings from old debug toggles to observability log level.

## Verification

- `goimports -w .`: pass.
- `golangci-lint run ./...`: pass.
- `govulncheck ./...`: pass.
- `go test ./...`: pass when `bin/` (which contains the project's own
  `libmpv.dll`) is prepended to PATH; this works in PowerShell as well as
  MSYS2 MINGW64.
- `CGO_ENABLED=1 CC=gcc go test -race ./...`: pass when `bin/` and MinGW gcc
  are on PATH. In MSYS2 MINGW64 this is stable. In PowerShell with the same
  PATH setup it can be flaky because of a pre-existing data race in
  `github.com/libp2p/go-libp2p/p2p/net/nat/internal/nat.randomPort()`
  (concurrent use of a non-thread-safe `math/rand` source); the suite passed
  on a subsequent retry.
- `frontend/`: `bun format`, `bun lint`, `bun typecheck`,
  `bun run test:run`: pass.

## Next

- Optional follow-up: broaden trace coverage further as new services/features are
  added.
