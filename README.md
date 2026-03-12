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
