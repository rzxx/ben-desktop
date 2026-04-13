# ben-desktop

`ben-desktop` is a desktop-first music library app built with Go, Wails, React, and TypeScript. It combines local library scanning, catalog browsing, queue-based playback, invite-driven sharing between devices, cache and pin management, and operator-style diagnostics in one desktop host.

This repository is not just a media player UI. It is the desktop runtime for the broader `ben` system: the place where local files are indexed, library state is materialized into SQLite, playback assets are prepared, peer sync is coordinated, and library operations are exposed through a native desktop shell.

## Why this project exists

Most personal music tools handle only one part of the problem:

- a player without strong library operations
- a library index without playback
- a sync layer without good operator visibility
- a desktop app without a clear model for multi-device ownership

`ben-desktop` exists to unify those pieces. The goal is a local-first music application where a device can contribute media, participate in a shared library, prepare playback assets, and expose the operational state needed to understand what the system is doing.

## What the app does

- Creates and manages libraries, including the currently active library on the device
- Scans local filesystem roots and materializes albums, artists, tracks, artwork, and playlist state
- Plays music through a queue-based playback runtime with shuffle, repeat, seek, volume, and queue controls
- Supports playlists and liked tracks as first-class catalog objects
- Prepares optimized playback assets and artwork variants for faster or more reliable playback
- Shares libraries across devices with invite codes, join approval flows, and peer connectivity
- Tracks local cache usage, pinned blobs, reclaimable data, and cache cleanup actions
- Exposes repair, sync, checkpoint, and diagnostics workflows for operating the library runtime
- Ships a separate read-only inspector CLI for tracing catalog, context, and cache decisions

## How it works

At a high level, the app follows this flow:

1. On startup, the desktop host opens the core runtime, which uses SQLite for structured state, a blob store for binary assets, and a device identity key for membership and peer flows.
2. A user creates or selects an active library.
3. Each device contributes local scan roots. File watching and scan jobs ingest media metadata and artwork into the catalog.
4. The catalog surfaces albums, artists, tracks, playlists, liked songs, and availability information to the frontend.
5. Playback requests resolve the best available source, prepare optimized assets when needed, cache them locally, and play them through the desktop playback backend.
6. Shared-library features use invite flows and a libp2p-based transport layer to connect peers, exchange state, and keep devices caught up.
7. Operations and cache surfaces expose what the runtime is doing so the system is understandable when something needs repair, republishing, cleanup, or manual intervention.

## Main product surfaces

- `Libraries`: create, select, rename, leave, delete, and inspect membership for libraries
- `Albums`, `Artists`, `Tracks`: browse the catalog built from scanned media
- `Playlists`: manage playlists and liked songs
- `Playback`: queue, transport controls, and current-session state
- `Sharing`: issue invites, approve joins, resume join sessions, and manually connect peers
- `Cache`: inspect optimized audio and artwork blobs, pinned scopes, and cleanup actions
- `Operations`: repair, publish checkpoints, compact checkpoints, inspect diagnostics, and watch the work feed

## Architecture summary

### Backend

- Go `1.25`
- Wails `v3` desktop host
- SQLite persistence through GORM
- `fsnotify` for scan watching
- `libp2p` for peer transport and sync
- `ffmpeg` for transcodes and artwork extraction/rendering
- optional `libmpv` playback backend

### Frontend

- React `19`
- TypeScript
- Vite
- Bun
- TanStack Router
- Zustand

## Repository layout

```text
.
|-- api/                   Shared API types used by the desktop host and frontend bindings
|-- build/                 Wails, platform packaging, icons, Docker, and task helpers
|-- cmd/beninspect/        Read-only inspector CLI for debugging catalog/context/cache state
|-- frontend/              React + TypeScript application rendered inside the Wails shell
|-- internal/desktopcore/  Core runtime: library, ingest, sync, checkpoints, cache, invites
|-- internal/playback/     Playback backend, queue/session logic, and availability helpers
|-- main.go                Wails application entry point
|-- facades.go             Desktop services exposed to the frontend
`-- Taskfile.yml           Main developer task entry points
```

## Runtime data and settings

By default, the core runtime stores its data under the OS user config directory:

- runtime data root: `.../ben/v2`
- SQLite database: `.../ben/v2/library.db`
- blob storage: `.../ben/v2/blobs`
- device identity key: `.../ben/v2/identity.key`

Desktop UI settings are stored separately at:

- `.../ben-desktop/settings.json`

The exact base path depends on the platform because it comes from `os.UserConfigDir()`.

## Prerequisites

To work on this project locally you will typically want:

- Go `1.25+`
- Bun
- Wails CLI for v3 projects
- `ffmpeg` available on `PATH` or configured through settings
- `task` for the Taskfile-driven workflow

For full playback support:

- `libmpv`

Windows note:

- place `libmpv.dll` at `build/windows/runtime/libmpv.dll` before packaging if you want Windows builds to include mpv support
- if you intentionally want to build without mpv, use the `nompv` build tag

## Getting started

### Install dependencies

```bash
cd frontend
bun install
cd ..
```

### Run the desktop app in development

```bash
task dev
```

This starts the Wails desktop workflow defined in `build/config.yml`.

### Build the app

```bash
task build
```

### Package the app

```bash
task package
```

### Frontend-only commands

Run these from `frontend/`:

```bash
bun run dev
bun run build
bun run test:run
bun run typecheck
bun run lint
bun run format:check
```

### Backend tests

Run from the repository root:

```bash
go test ./...
```

## Server mode

The project also includes a server build path with no native GUI:

```bash
task build:server
task run:server
```

There is also Docker support for the server build:

```bash
task build:docker
task run:docker
```

Desktop remains the primary product surface in this repository; server mode is useful when you want the same core runtime without the native shell.

## Inspector CLI

`beninspect` is a separate read-only debugging tool for tracing how the runtime resolves library, playback, and cache state. It does not boot the full desktop UI and does not mutate library state.

Examples:

```bash
go run ./cmd/beninspect env resolve-context --db ./library.db --blob-root ./blobs
go run ./cmd/beninspect music trace-recording --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --id <recording-or-cluster>
go run ./cmd/beninspect cache trace-blob --db ./library.db --blob-root ./blobs --library-id <library> --device-id <device> --blob-id <blob-id>
```

Task wrappers are also available:

```bash
task inspect CLI_ARGS="env resolve-context --db ./library.db --blob-root ./blobs"
task inspect:recording ID=<recording-or-cluster> CLI_ARGS="--db ./library.db --blob-root ./blobs --library-id <library> --device-id <device>"
```

All inspector commands emit JSON, which makes them suitable for tests, tooling, and shell automation.

## Development notes

- The frontend talks to the Go host through Wails-generated bindings in `frontend/bindings/`.
- The desktop host exposes facades for libraries, catalog, playback, sharing, jobs, cache, theme, and notifications.
- Playback preparation, cache retention, and pinning are separate concerns; a track can exist in the catalog before it is fully prepared for local playback.
- Scan roots are device-local configuration. They are not treated as shared library state.
- Operations are designed to be job-driven, so long-running work can be tracked from both the UI and runtime notifications.

## In one sentence

`ben-desktop` is a local-first desktop application for building, operating, playing, and sharing a music library across devices without separating playback from the underlying library runtime.
