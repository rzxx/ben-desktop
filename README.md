# ben-desktop

Wails 3 desktop app with a React + TypeScript frontend managed by Bun.

## Frontend

Run these from frontend/package.json

```bash
bun install
bun run dev
bun run lint
bun run format
```

The frontend is configured with Tailwind CSS v4, ESLint flat config, and Prettier with the Tailwind plugin.

## App

For the full desktop app workflow, use:

```bash
wails3 dev
wails3 build
```

Playback uses `libmpv` by default. On Windows, place `libmpv.dll` at `build/windows/runtime/libmpv.dll` before `wails3 build` or packaging so the built app and NSIS installer include it. To intentionally build without mpv support, use the `nompv` build tag.

Windows thumbnail toolbar icons are compiled into the Windows binary from `internal/platform/thumbbar`, so they do not need separate runtime path resolution or installer entries.

## Inspector

`beninspect` is a separate read-only inspector for tracing music identity, context resolution, and cache attribution directly from the SQLite library database. It does not start the main desktop runtime, run migrations, or mutate library state.

Run it directly:

```bash
go run ./cmd/beninspect env resolve-context --db ./library.db --blob-root ./blobs
go run ./cmd/beninspect music trace-recording --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --id <recording-or-cluster>
go run ./cmd/beninspect music trace-album --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --id <album-or-cluster>
go run ./cmd/beninspect music trace-context --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --kind album --id <album-id>
go run ./cmd/beninspect cache trace-recording --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --id <recording-or-cluster>
go run ./cmd/beninspect cache trace-blob --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --blob-id <blob-id>
```

Task wrappers are also available:

```bash
task inspect CLI_ARGS="env resolve-context --db ./library.db --blob-root ./blobs"
task inspect:recording ID=<recording-or-cluster> CLI_ARGS="--db ./library.db --blob-root ./blobs --library-id <library> --device-id <device>"
```

All inspector commands emit JSON by default and include `schema_version` so traces can be consumed by tests, agents, and shell automation.

## Playback Service

The desktop host exposes a `PlaybackService`.

Host lifecycle hooks:

- `ServiceStartup`
- `ServiceShutdown`

Frontend-callable methods:

Snapshot and events:

- `GetPlaybackSnapshot`
- `SubscribePlaybackEvents`
- `JobsFacade.SubscribeJobEvents`

Queue mutation:

- `ReplaceQueue`
- `AppendToQueue`
- `RemoveQueueItem`
- `MoveQueueItem`
- `SelectQueueIndex`
- `ClearQueue`

Transport:

- `Play`
- `Pause`
- `TogglePlayback`
- `Next`
- `Previous`
- `SeekTo`
- `SetVolume`
- `SetRepeatMode`
- `SetShuffle`

Helper queue builders:

- `PlayAlbum`
- `QueueAlbum`
- `PlayPlaylist`
- `QueuePlaylist`
- `PlayRecording`
- `QueueRecording`
- `PlayLiked`
