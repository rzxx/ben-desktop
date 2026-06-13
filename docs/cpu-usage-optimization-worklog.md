# CPU Usage Optimization Worklog

Date: 2026-06-13

This is a working checklist for closing the static CPU usage report without hiding latency behind broad throttles. Decisions should prefer pushed state, no-op detection, and centralized scheduling over independent polling loops.

## Decisions

- Network sync notifications: replace the unconditional 500 ms frontend-facing poll with backend-pushed status updates. Keep a cheap initial load and explicit refresh path only for UI startup/manual reads.
- Playback ticker: keep position freshness for playback correctness, but stop persisting and broadcasting full snapshots on every 500 ms tick. Persist on semantic changes and occasional checkpoints only.
- Frontend playback progress: avoid React state updates every animation frame for simple progress movement. Prefer CSS/custom-property driven visuals when possible, with React only receiving semantic playback state.
- Artwork resolution: stop infinite per-component Wails retries for known-missing artwork. Cache negative results until an artwork/catalog invalidation can make the answer change.
- Availability fanout: prefer targeted IDs and generation/no-op checks over broad force refetches. Coalesce frontend fetches only as a safety valve, not as the main optimization.
- Pin reconciliation: detect no-op reconciles before rewriting rows or emitting availability invalidations.
- Transport presence/sync: separate heartbeat freshness from availability changes. Only emit availability invalidations on effective online/offline or asset-affecting changes.
- Scanner/artwork generation: keep the watcher event-driven, add early filtering/counters where cheap, and avoid work when source mtimes have not changed.
- Debug traces: guard hot-path trace payload construction when tracing is disabled.

## Progress

- [x] Network sync notification polling
- [x] Playback session ticker persistence and event fanout
- [x] Frontend playback progress animation
- [x] Missing artwork retry behavior
- [x] Pin refresh/reconcile no-op behavior
- [x] Availability invalidation fanout
- [x] Transport presence/sync invalidation
- [x] Scanner/artwork burst controls
- [x] Debug trace hot-path guards
- [x] Format and verification

## Verification

- `goimports -w .` completed.
- `golangci-lint run ./...` passed.
- `go test ./...` passed after adding `bin/` to `PATH` so `libmpv.dll` and its dependencies are resolvable.
- `govulncheck ./...` passed with no called vulnerabilities.
- `bun format` passed with no final changes.
- `bun lint` passed.
- `bun typecheck` passed.
- `bun run test:run` passed: 14 files, 70 tests.
- Windows race tests passed in MSYS2 MINGW64 with `CGO_ENABLED=1 CC=gcc go test -race ./...` (bin/ on PATH for libmpv, Go from `C:\Program Files\Go\bin`).
