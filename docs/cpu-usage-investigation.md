# CPU Usage Investigation Report

Date: 2026-06-13

This report is based on static code tracing of the Wails desktop app. It was not produced from a runtime CPU profile. No existing `pprof`, `runtime/pprof`, or `runtime/trace` hook was found in the app code, so the next verification step should be an instrumented local run.

## Summary

The app has a small number of fixed-frequency wakeups and several event fanouts that can turn one state change into database, filesystem, and Wails event work.

The strongest always-on Go-side suspect is the notification network-sync poll loop, which runs every 500 ms after app startup. The strongest playback-time suspect is the playback session ticker, also every 500 ms, which reads mpv properties, checks preload state, writes session state to SQLite, updates platform media controls, and emits Wails transport events.

The strongest bursty contributors are availability invalidation fanout, pin refresh/reconcile work, unresolved artwork retries, transport presence/catchup, and scanner/artwork sync work after filesystem or peer events.

## Most Likely CPU Sources

### 1. Always-on network sync notification polling

Confidence: high for idle periodic wakeups.

`NotificationsFacade.ServiceStartup` starts `runNetworkSyncPollLoop` unconditionally:

- `notificationsfacade.go:85`
- `notificationsfacade.go:1058`

That loop wakes every 500 ms and calls `pollNetworkSyncStatus`. The poll calls `runtime.NetworkStatus()`, which goes through:

- `runtime_adapters.go:117`
- `service_operator.go:176`
- `service_transport.go:967`

`TransportService.NetworkStatus` does more than read an atomic field. It:

- Ensures local context.
- Reads active runtime state.
- Calls the libp2p status reporter when transport is running.
- Enumerates current libp2p peers in `appendNetworkStatus`.
- Queries `peer_sync_states` ordered by recent update every poll.

Relevant code:

- `internal/desktopcore/service_transport.go:967`
- `internal/desktopcore/service_transport.go:990`
- `internal/desktopcore/service_transport.go:1016`
- `internal/desktopcore/transport_libp2p.go:1171`

Why it matches the symptom:

- The reported spike period is around half a second.
- It happens even when playback is stopped.
- It happens regardless of peer state because the poll loop is unconditional.
- On a high-performance desktop power plan, short wakeups are more visible in Task Manager than on a conservative notebook power plan.

Optimization options:

- Replace the 500 ms poll with event-driven network sync notifications.
- Poll every 5-15 seconds while idle, and temporarily increase frequency only during active sync.
- Cache the latest `peer_sync_states` summary and update it only when sync state changes.
- Split `NetworkStatus` into a cheap in-memory status read and a heavier diagnostics read.
- Avoid enumerating libp2p peers twice per second unless a peer status panel is open.

### 2. Playback session ticker

Confidence: high when playing.

The playback session uses a 500 ms position ticker:

- `internal/playback/session.go:16`
- `internal/playback/session.go:3438`
- `internal/playback/session.go:3456`

Each tick while playing does:

- `refreshPosition("ticker")`, which reads `backend.PositionMS()` and `backend.DurationMS()`.
- `preloadNext(context.Background())`.
- `publishSnapshot(s.Snapshot())`.

Relevant code:

- `internal/playback/session.go:3472`
- `internal/playback/session.go:2644`
- `internal/playback/session.go:2763`
- `internal/playback/session.go:4279`

`publishSnapshot` persists every snapshot. The SQLite store normalizes and JSON marshals the snapshot, writes shuffle preference, and upserts the playback session row:

- `internal/playback/store_sqlite.go:127`

Then `PlaybackService.handlePlaybackSnapshot` updates platform media controls/window title and emits a Wails transport event for every snapshot:

- `playbackservice.go:519`
- `playbackservice.go:539`

Why it matters:

- This path is guaranteed to run twice per second during playback.
- It includes mpv IPC/property reads, SQLite writes, JSON marshal, Wails event dispatch, and frontend store updates.
- It explains a regular 0.5 second pulse while playing.

It does not fully explain idle spikes because the ticker is stopped on pause/stop paths. The idle half-second pattern points more strongly at network notification polling.

Optimization options:

- Do not persist playback state on every position tick.
- Persist on play, pause, seek, track change, queue change, shutdown, and maybe every 10 seconds while playing.
- Do not emit authoritative transport position every 500 ms if the frontend already derives progress from `positionCapturedAtMs`.
- Emit transport events on semantic changes, plus a lower-frequency heartbeat if needed.
- Move `preloadNext` off the position ticker. Schedule it when position crosses a threshold or when next-preparation state changes.
- Avoid updating Windows media timeline every 500 ms if the platform can extrapolate position.

### 3. Frontend playback progress animation

Confidence: high for webview CPU while playing.

`usePlayerProgress` starts a `requestAnimationFrame` loop while there is an active entry and playback is playing:

- `frontend/src/hooks/playback/usePlayerProgress.ts:191`

That updates React state every frame:

- `frontend/src/hooks/playback/usePlayerProgress.ts:198`

Why it matters:

- This is expected to show webview CPU while playing.
- It should stop when playback is paused or when no active entry exists.
- It does not explain Go process CPU.

Optimization options:

- Update progress at 4-10 Hz instead of every frame.
- Drive the visual progress bar with a ref/CSS transform rather than React state.
- Keep frame-rate updates only while the player bar is visible.

### 4. Unresolved artwork retry loop

Confidence: medium-high for repeated Go and webview work on screens with missing artwork.

`useResolvedUrl` retries empty or failed URL resolves forever. Backoff reaches 15 seconds and then stays there:

- `frontend/src/hooks/media/useResolvedUrl.ts:5`
- `frontend/src/hooks/media/useResolvedUrl.ts:82`
- `frontend/src/hooks/media/useResolvedUrl.ts:115`

This feeds thumbnail and recording artwork hooks:

- `frontend/src/hooks/media/useThumbnailUrl.ts`
- `frontend/src/hooks/media/useRecordingArtworkUrl.ts`

The Go calls include:

- `PlaybackFacade.ResolveThumbnailURL` in `facades.go:626`
- `PlaybackFacade.ResolveAlbumArtworkURL` in `facades.go:642`
- `PlaybackFacade.ResolveRecordingArtworkURL` in `facades.go:653`
- `PlaybackService.ResolveArtworkRef` in `internal/desktopcore/service_playback.go:801`
- `PlaybackService.ResolveAlbumArtwork` in `internal/desktopcore/service_playback.go:825`
- `PlaybackService.ResolveRecordingArtwork` in `internal/desktopcore/service_playback.go:872`

Why it matters:

- A visible list/grid with multiple missing artworks can create many independent retry timers.
- Each retry crosses the Wails boundary and may run DB/path checks.
- This can produce ongoing low-level CPU even when no playback or peer activity is happening.

Optimization options:

- Negative-cache missing artwork by key.
- Retry only after catalog/artwork invalidation events.
- Cap retry attempts per mounted hook.
- Add a global retry scheduler to coalesce missing-artwork checks.
- Track missing artwork as a known state in the catalog instead of treating it like a transient failure.

### 5. Pin refresh and reconcile churn

Confidence: medium-high for bursty background work and availability invalidation amplification.

Startup schedules all pin scope refreshes:

- `internal/desktopcore/app.go:451`

Pin service also subscribes to catalog changes and schedules refreshes:

- `internal/desktopcore/service_pin.go:36`
- `internal/desktopcore/service_pin.go:48`

Refresh timers are one per scope:

- `internal/desktopcore/service_pin.go:1037`
- `internal/desktopcore/service_pin.go:1069`

Refresh jobs do significant work:

- Resolve the pin root and scope.
- Resolve all member recordings for album/playlist scopes.
- Check local paths and cached encodings per recording.
- Fetch/materialize missing encodings when needed.
- Reconcile pin members and blob refs.
- Emit pin availability invalidation.

Relevant code:

- `internal/desktopcore/service_pin.go:1108`
- `internal/desktopcore/service_pin.go:1191`
- `internal/desktopcore/service_pin.go:1343`

The `reconcileScope` path deletes and recreates `PinMember` and `PinBlobRef` rows in batches, then updates the root even when the computed result may be unchanged:

- `internal/desktopcore/service_pin.go:1396`
- `internal/desktopcore/service_pin.go:1406`
- `internal/desktopcore/service_pin.go:1412`
- `internal/desktopcore/service_pin.go:1421`
- `internal/desktopcore/service_pin.go:1427`

Why it matters:

- Pin refresh is not a sub-second loop, but it can be expensive when triggered.
- A broad availability invalidation can schedule pending pin refreshes.
- A pin refresh emits availability invalidations, which can trigger frontend availability refetches.
- The current model is closer to "event happened, recompute broad state" than "specific reason, specific action".

Optimization options:

- Add diff/no-op detection before rewriting pin rows.
- Emit availability invalidation only when pin state actually changes.
- Use reason-specific pin scheduling: peer connected, track liked, playlist member changed, encoding cached, etc.
- Coalesce pin refresh jobs per library and run a bounded worker queue.
- Keep a dirty set of affected pin roots instead of scheduling all/pending roots in broad cases.

### 6. Availability invalidation fanout

Confidence: medium-high for bursty Go CPU after pin, peer, sync, scan, and catalog events.

Frontend catalog event handling can force availability refetches:

- `frontend/src/app/router/CatalogEventsRuntime.tsx:171`
- `frontend/src/app/router/CatalogEventsRuntime.tsx:184`
- `frontend/src/app/router/CatalogEventsRuntime.tsx:212`
- `frontend/src/app/router/CatalogEventsRuntime.tsx:225`

On `InvalidateAll`, it refetches every loaded track and album availability record:

- `frontend/src/app/router/CatalogEventsRuntime.tsx:28`
- `frontend/src/app/router/CatalogEventsRuntime.tsx:36`

Availability loaders chunk work but only dedupe in-flight exact chunk keys:

- `frontend/src/lib/catalog/loader-availability.ts:18`
- `frontend/src/lib/catalog/loader-availability.ts:21`
- `frontend/src/lib/catalog/loader-availability.ts:74`
- `frontend/src/lib/catalog/loader-shared.ts:19`
- `frontend/src/lib/catalog/loader-shared.ts:53`

Backend track availability is heavy:

- `internal/desktopcore/service_playback.go:1213`

It does variant resolution, local path checks, cached blob checks, network status reads, aggregate source/cache fact queries, and pin lookups.

Filesystem checks happen inside availability:

- `internal/desktopcore/service_playback.go:1789`
- `internal/desktopcore/service_playback.go:1843`

Aggregate DB queries happen in:

- `internal/desktopcore/service_playback.go:2849`

Why it matters:

- One backend invalidation can become multiple Wails calls.
- Each Wails call can run several SQL queries and filesystem stats.
- Force refresh bypasses most cache protection.

Optimization options:

- Debounce and coalesce availability invalidations in the frontend.
- Add a short cooldown for force refresh by ID.
- Prefer targeted changed IDs over `InvalidateAll`.
- Add backend generation/version fields so the frontend can skip refetch when availability generation did not change.
- Split local static facts from volatile peer-online facts.

### 7. Transport presence, sync, and peer events

Confidence: medium for periodic bursts, low for half-second idle spikes.

The transport background loop runs every 15 seconds:

- `internal/desktopcore/service_transport.go:310`

Each tick announces presence, maintains invite reachability, and schedules catchup:

- `internal/desktopcore/service_transport.go:328`
- `internal/desktopcore/service_transport.go:329`
- `internal/desktopcore/service_transport.go:330`

Local oplog mutations schedule peer update broadcast and checkpoint maintenance after the event sync delay:

- `internal/desktopcore/service_oplog.go:130`
- `internal/desktopcore/service_transport.go:335`

Peer catchup emits availability invalidate-all when it applies any ops:

- `internal/desktopcore/service_sync.go:622`
- `internal/desktopcore/service_sync.go:629`

Presence updates write device rows and emit availability invalidate-all when a peer transitions online/offline:

- `internal/desktopcore/service_transport.go:1643`
- `internal/desktopcore/service_transport.go:1670`
- `internal/desktopcore/service_transport.go:1691`
- `internal/desktopcore/service_transport.go:1731`

Why it matters:

- The 15 second loop does not match the observed 0.5 second pulse by itself.
- Sync and peer transitions can trigger broad availability refreshes.
- `upsertDevicePresence` updates `last_seen_at` on every presence contact even if the peer was already online.

Optimization options:

- Do not write `last_seen_at` on every presence if the previous value is recent enough.
- Emit availability invalidation only on actual online/offline transitions or affected-device asset changes.
- Keep peer heartbeat/presence separate from catalog availability refresh.
- Avoid catchup work when no peers are known or all peers have fresh sync state.

### 8. Scanner and artwork generation

Confidence: low for idle periodic spikes, medium for filesystem-event bursts.

The scan watcher is fsnotify-driven with a 350 ms debounce:

- `internal/desktopcore/watcher.go:18`
- `internal/desktopcore/watcher.go:175`

The scan coordinator is request/channel driven, not a permanent poll loop:

- `internal/desktopcore/scan_coordinator.go:204`

Delta scans can run path stats, ingest paths, reconcile root presence, rebuild scoped catalog materialization, and reconcile artwork:

- `internal/desktopcore/service_ingest_scan.go:313`
- `internal/desktopcore/service_ingest_scan.go:376`
- `internal/desktopcore/service_ingest_scan.go:441`
- `internal/desktopcore/service_ingest_scan.go:453`

Artwork reconciliation can run ffmpeg-based extraction/rendering when artwork is missing or stale:

- `internal/desktopcore/service_artwork.go:350`
- `internal/desktopcore/service_artwork.go:389`
- `internal/desktopcore/service_artwork.go:425`
- `internal/desktopcore/service_artwork.go:706`

Why it matters:

- Not likely to be a constant idle loop.
- Can be expensive if a watched library root is noisy.
- Cloud folders, media player metadata writes, or antivirus interactions can cause filesystem event bursts.

Optimization options:

- Add counters for watch events by path/type.
- Ignore temp files and known non-media churn earlier.
- Batch artwork reconciliation and skip if all relevant source mtimes are unchanged.

### 9. Debug traces

Confidence: low as primary cause.

Backend playback trace recording exits immediately when disabled:

- `internal/playback/debug_trace.go:48`

Frontend playback trace recording and live-state updates also return when disabled:

- `frontend/src/lib/playback/debugTrace.ts:154`
- `frontend/src/lib/playback/debugTrace.ts:177`

Network debug frontend polling only runs when enabled and uses a 2 second interval:

- `frontend/src/lib/network/debugTrace.ts:73`

There is still minor overhead where disabled backend trace entries are constructed before `RecordDebugTrace` checks the flag, for example:

- `internal/playback/session.go:2672`
- `playbackservice.go:540`

Optimization options:

- Guard trace entry construction with `DebugTraceEnabled()` on hot paths.
- Keep this low priority unless profiles show allocation noise.

## Why Desktop and Notebook Behave Differently

This code has several short wakeups. A desktop power plan with higher clocks and less aggressive parking can make half-second bursts visible as 4-5 percent CPU in Task Manager. A notebook power plan may smooth, defer, or reduce visible scheduling. Task Manager percentages can also make one briefly active core look like a small but noticeable total CPU spike.

That difference does not mean the work is harmless. It means the app probably has short, repeated bursts rather than one long-running CPU loop.

## Recommended Profiling Plan

Add a temporary guarded profiler:

```go
// Enable only when BEN_PPROF_ADDR is set, for example 127.0.0.1:6060.
import _ "net/http/pprof"

go func() {
    _ = http.ListenAndServe(addr, nil)
}()
```

Capture 60 second CPU profiles for:

1. Idle, app open, no playback.
2. Idle on a catalog/grid screen with missing artwork.
3. Playing with the player visible.
4. Playing with the player hidden or minimized if possible.
5. Peer online/offline transition.
6. Startup after selecting an active library with pins.

Also add low-overhead aggregate counters printed every 10 seconds while profiling:

- `network_sync_poll.count`, total duration, max duration.
- `network_status.count`, total duration, max duration.
- `playback_tick.count`, total duration, max duration.
- `playback_snapshot_save.count`, total duration, max duration.
- Wails event counts by event name.
- `availability.track.list.count`, IDs per request, total duration.
- `availability.album.list.count`, IDs per request, total duration.
- `artwork.resolve.count`, hit/miss/error by kind.
- `pin_refresh.count`, scope, member count, pending count, duration.
- `catalog_availability_invalidation.count`, invalidate-all vs targeted.
- `watch_event.count`, event op and path classification.

A useful initial threshold: log any operation that takes more than 10 ms, but keep aggregate counters for everything.

## First Optimization Order

1. Change the 500 ms network notification poll to event-driven or backoff-when-idle.
2. Stop saving playback state to SQLite every 500 ms.
3. Reduce playback transport event frequency and let frontend extrapolate progress.
4. Negative-cache missing artwork and remove infinite per-component retry loops.
5. Debounce/coalesce frontend availability refetches.
6. Make pin reconcile no-op aware and emit invalidations only on real changes.
7. Narrow peer/sync availability invalidations to affected IDs or affected device state.

This order targets the most likely always-on CPU first, then the known playback pulse, then the event fanout paths that turn normal background activity into visible bursts.
