import { beforeEach, describe, expect, test, vi } from "vitest";

const playbackApi = vi.hoisted(() => ({
  clearQueue: vi.fn(),
  getPlaybackSnapshot: vi.fn(),
  nextTrack: vi.fn(),
  playAlbum: vi.fn(),
  playAlbumTrack: vi.fn(),
  playLiked: vi.fn(),
  playLikedTrack: vi.fn(),
  playPlaylist: vi.fn(),
  playPlaylistTrack: vi.fn(),
  playRecording: vi.fn(),
  playTracks: vi.fn(),
  playTracksFrom: vi.fn(),
  previousTrack: vi.fn(),
  queueAlbum: vi.fn(),
  queueLikedTrack: vi.fn(),
  queuePlaylist: vi.fn(),
  queuePlaylistTrack: vi.fn(),
  queueRecording: vi.fn(),
  removeQueuedEntry: vi.fn(),
  seekTo: vi.fn(),
  selectQueueEntry: vi.fn(),
  setRepeatMode: vi.fn(),
  setShuffle: vi.fn(),
  setVolume: vi.fn(),
  shuffleTracks: vi.fn(),
  togglePlayback: vi.fn(),
}));

const playbackEventHandlers = vi.hoisted(
  () => new Map<string, (event: { data: unknown }) => void>(),
);

vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return {
    ...actual,
    Events: {
      ...actual.Events,
      On: vi.fn(
        (eventName: string, handler: (event: { data: unknown }) => void) => {
          playbackEventHandlers.set(eventName, handler);
          return () => {
            if (playbackEventHandlers.get(eventName) === handler) {
              playbackEventHandlers.delete(eventName);
            }
          };
        },
      ),
    },
  };
});

vi.mock("@/lib/api/playback", () => ({
  PLAYBACK_QUEUE_EVENT_NAME: "playback:queue",
  PLAYBACK_TRANSPORT_EVENT_NAME: "playback:transport",
  clearQueue: playbackApi.clearQueue,
  getPlaybackSnapshot: playbackApi.getPlaybackSnapshot,
  nextTrack: playbackApi.nextTrack,
  playAlbum: playbackApi.playAlbum,
  playAlbumTrack: playbackApi.playAlbumTrack,
  playLiked: playbackApi.playLiked,
  playLikedTrack: playbackApi.playLikedTrack,
  playPlaylist: playbackApi.playPlaylist,
  playPlaylistTrack: playbackApi.playPlaylistTrack,
  playRecording: playbackApi.playRecording,
  playTracks: playbackApi.playTracks,
  playTracksFrom: playbackApi.playTracksFrom,
  previousTrack: playbackApi.previousTrack,
  queueAlbum: playbackApi.queueAlbum,
  queueLikedTrack: playbackApi.queueLikedTrack,
  queuePlaylist: playbackApi.queuePlaylist,
  queuePlaylistTrack: playbackApi.queuePlaylistTrack,
  queueRecording: playbackApi.queueRecording,
  removeQueuedEntry: playbackApi.removeQueuedEntry,
  seekTo: playbackApi.seekTo,
  selectQueueEntry: playbackApi.selectQueueEntry,
  setRepeatMode: playbackApi.setRepeatMode,
  setShuffle: playbackApi.setShuffle,
  setVolume: playbackApi.setVolume,
  shuffleTracks: playbackApi.shuffleTracks,
  togglePlayback: playbackApi.togglePlayback,
}));

import { PlaybackModels, Types } from "@/lib/api/models";
import { usePlaybackStore } from "./store";

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function makeSnapshot(queueVersion = 3) {
  return PlaybackModels.SessionSnapshot.createFrom({
    currentEntry: {
      entryId: "ctx-1",
      origin: PlaybackModels.EntryOrigin.EntryOriginContext,
      contextIndex: 0,
      item: {
        recordingId: "rec-1",
        title: "Track 1",
      },
    },
    repeatMode: PlaybackModels.RepeatMode.RepeatOff,
    shuffle: false,
    volume: 75,
    status: PlaybackModels.Status.StatusPlaying,
    positionMs: 1_500,
    positionCapturedAtMs: 42_000,
    durationMs: 180_000,
    lastError: "",
    contextQueue: {
      kind: PlaybackModels.ContextKind.ContextKindTracks,
      id: "tracks",
      title: "All tracks",
      entries: [
        {
          entryId: "ctx-1",
          origin: PlaybackModels.EntryOrigin.EntryOriginContext,
          contextIndex: 0,
          item: {
            recordingId: "rec-1",
            title: "Track 1",
          },
        },
      ],
      totalCount: 1_500,
      currentIndex: 0,
      resumeIndex: 0,
      windowStart: 0,
      windowCount: 1,
      hasBefore: false,
      hasAfter: true,
      loading: false,
      sourceVersion: 11,
    },
    userQueue: [
      {
        entryId: "queued-1",
        origin: PlaybackModels.EntryOrigin.EntryOriginQueued,
        item: {
          recordingId: "rec-q1",
          title: "Queued 1",
        },
      },
    ],
    entryAvailability: {
      "ctx-1": Types.RecordingPlaybackAvailability.createFrom({
        RecordingID: "rec-1",
        State: Types.RecordingAvailabilityState.AvailabilityPlayableLocalFile,
      }),
    },
    queueLength: 1_501,
    queueVersion,
  });
}

function makeTransportEvent(entryId: string, positionCapturedAtMs: number) {
  return PlaybackModels.TransportEventSnapshot.createFrom({
    currentEntry: {
      entryId,
      origin: PlaybackModels.EntryOrigin.EntryOriginContext,
      contextIndex: 1,
      item: {
        recordingId: `rec-${entryId}`,
        title: `Track ${entryId}`,
      },
    },
    status: PlaybackModels.Status.StatusPaused,
    positionMs: 4_000,
    positionCapturedAtMs,
    durationMs: 181_000,
  });
}

function emitPlaybackEvent(eventName: string, payload: unknown) {
  const handler = playbackEventHandlers.get(eventName);
  if (!handler) {
    throw new Error(`missing playback event handler for ${eventName}`);
  }
  handler({ data: payload });
}

function createTransportFromSnapshot(snapshot: PlaybackModels.SessionSnapshot) {
  return {
    currentEntry: snapshot.currentEntry ?? null,
    loadingEntry: snapshot.loadingEntry ?? null,
    loadingPreparation: snapshot.loadingPreparation ?? null,
    repeatMode: snapshot.repeatMode,
    shuffle: snapshot.shuffle,
    volume: snapshot.volume,
    status: snapshot.status,
    positionMs: snapshot.positionMs,
    positionCapturedAtMs: snapshot.positionCapturedAtMs ?? 0,
    durationMs: snapshot.durationMs ?? null,
    lastError: snapshot.lastError ?? "",
  };
}

function createQueueFromSnapshot(snapshot: PlaybackModels.SessionSnapshot) {
  const entryAvailability: Record<string, Types.RecordingPlaybackAvailability> =
    {};
  for (const [key, value] of Object.entries(snapshot.entryAvailability ?? {})) {
    if (value == null) {
      continue;
    }
    entryAvailability[key] = value;
  }
  return {
    contextQueue: snapshot.contextQueue
      ? {
          title: snapshot.contextQueue.title ?? "",
          entries: [...(snapshot.contextQueue.entries ?? [])],
          hasBefore: snapshot.contextQueue.hasBefore ?? false,
          hasAfter: snapshot.contextQueue.hasAfter ?? false,
          totalCount: snapshot.contextQueue.totalCount ?? 0,
          currentIndex: snapshot.contextQueue.currentIndex ?? -1,
          resumeIndex: snapshot.contextQueue.resumeIndex ?? -1,
          windowStart: snapshot.contextQueue.windowStart ?? 0,
          windowCount: snapshot.contextQueue.windowCount ?? 0,
          loading: snapshot.contextQueue.loading ?? false,
          sourceVersion: snapshot.contextQueue.sourceVersion ?? 0,
          source: snapshot.contextQueue.source ?? null,
          anchor: snapshot.contextQueue.anchor ?? null,
          shuffleBag: [...(snapshot.contextQueue.shuffleBag ?? [])],
        }
      : null,
    userQueue: [...snapshot.userQueue],
    entryAvailability,
    queueLength: snapshot.queueLength ?? 0,
    queueVersion: snapshot.queueVersion ?? 0,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  playbackEventHandlers.clear();
  playbackApi.clearQueue.mockResolvedValue(undefined);
  playbackApi.getPlaybackSnapshot.mockResolvedValue(makeSnapshot());
  playbackApi.nextTrack.mockResolvedValue(undefined);
  playbackApi.playAlbum.mockResolvedValue(undefined);
  playbackApi.playAlbumTrack.mockResolvedValue(undefined);
  playbackApi.playLiked.mockResolvedValue(undefined);
  playbackApi.playLikedTrack.mockResolvedValue(undefined);
  playbackApi.playPlaylist.mockResolvedValue(undefined);
  playbackApi.playPlaylistTrack.mockResolvedValue(undefined);
  playbackApi.playRecording.mockResolvedValue(undefined);
  playbackApi.playTracks.mockResolvedValue(undefined);
  playbackApi.playTracksFrom.mockResolvedValue(undefined);
  playbackApi.previousTrack.mockResolvedValue(undefined);
  playbackApi.queueAlbum.mockResolvedValue(undefined);
  playbackApi.queueLikedTrack.mockResolvedValue(undefined);
  playbackApi.queuePlaylist.mockResolvedValue(undefined);
  playbackApi.queuePlaylistTrack.mockResolvedValue(undefined);
  playbackApi.queueRecording.mockResolvedValue(undefined);
  playbackApi.removeQueuedEntry.mockResolvedValue(undefined);
  playbackApi.seekTo.mockResolvedValue(undefined);
  playbackApi.selectQueueEntry.mockResolvedValue(undefined);
  playbackApi.setRepeatMode.mockResolvedValue(undefined);
  playbackApi.setShuffle.mockResolvedValue(undefined);
  playbackApi.setVolume.mockResolvedValue(undefined);
  playbackApi.shuffleTracks.mockResolvedValue(undefined);
  playbackApi.togglePlayback.mockResolvedValue(undefined);

  usePlaybackStore.setState((state) => ({
    ...state,
    error: "",
    started: false,
    generation: 0,
    transportStateSequence: 0,
    transport: null,
    queue: null,
    stopListening: undefined,
  }));
});

describe("playback store authoritative slices", () => {
  test("bootstraps transport and queue slices from snapshot", () => {
    usePlaybackStore.getState().bootstrapFromSnapshot(makeSnapshot());

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-1");
    expect(state.transport?.status).toBe(PlaybackModels.Status.StatusPlaying);
    expect(state.transport?.positionCapturedAtMs).toBe(42_000);
    expect(state.queue?.contextQueue?.title).toBe("All tracks");
    expect(state.queue?.queueVersion).toBe(3);
    expect(state.queue?.entryAvailability["ctx-1"]?.RecordingID).toBe("rec-1");
  });

  test("transport events do not mutate the queue slice", () => {
    usePlaybackStore.getState().bootstrapFromSnapshot(makeSnapshot());

    const queueBefore = usePlaybackStore.getState().queue;
    usePlaybackStore
      .getState()
      .applyTransportEvent(makeTransportEvent("ctx-2", 77_000));

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-2");
    expect(state.transport?.status).toBe(PlaybackModels.Status.StatusPaused);
    expect(state.transport?.positionCapturedAtMs).toBe(77_000);
    expect(state.queue).toBe(queueBefore);
  });

  test("queue events do not mutate the transport slice", () => {
    usePlaybackStore.getState().bootstrapFromSnapshot(makeSnapshot());

    const transportBefore = usePlaybackStore.getState().transport;
    usePlaybackStore.getState().applyQueueEvent({
      contextQueue: {
        title: "Rebased tracks",
        entries: [
          {
            entryId: "ctx-2",
            item: {
              recordingId: "rec-2",
              title: "Track 2",
              subtitle: "",
              durationMs: 0,
              artworkRef: "",
            },
          },
        ],
        totalCount: 1_499,
        currentIndex: 1,
        resumeIndex: 1,
        windowStart: 1,
        windowCount: 1,
        hasBefore: true,
        hasAfter: true,
        loading: false,
        sourceVersion: 12,
      },
      userQueue: [],
      entryAvailability: {
        "ctx-2": Types.RecordingPlaybackAvailability.createFrom({
          RecordingID: "rec-2",
          State: Types.RecordingAvailabilityState.AvailabilityPlayableLocalFile,
        }),
      },
      queueLength: 1_499,
      queueVersion: 4,
    });

    const state = usePlaybackStore.getState();
    expect(state.transport).toBe(transportBefore);
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-1");
    expect(state.queue?.contextQueue?.title).toBe("Rebased tracks");
    expect(state.queue?.queueVersion).toBe(4);
  });

  test("ignores stale queue events and accepts newer ones", () => {
    usePlaybackStore.getState().bootstrapFromSnapshot(makeSnapshot(5));

    usePlaybackStore.getState().applyQueueEvent({
      contextQueue: {
        title: "Stale queue",
        entries: [],
        totalCount: 10,
        currentIndex: 0,
        resumeIndex: 0,
        windowStart: 0,
        windowCount: 0,
        hasBefore: false,
        hasAfter: false,
        loading: false,
        sourceVersion: 8,
      },
      userQueue: [],
      entryAvailability: {},
      queueLength: 10,
      queueVersion: 4,
    });
    expect(usePlaybackStore.getState().queue?.contextQueue?.title).toBe(
      "All tracks",
    );

    usePlaybackStore.getState().applyQueueEvent({
      contextQueue: {
        title: "Fresh queue",
        entries: [],
        totalCount: 9,
        currentIndex: 0,
        resumeIndex: 0,
        windowStart: 0,
        windowCount: 0,
        hasBefore: false,
        hasAfter: false,
        loading: false,
        sourceVersion: 9,
      },
      userQueue: [],
      entryAvailability: {},
      queueLength: 9,
      queueVersion: 6,
    });
    expect(usePlaybackStore.getState().queue?.contextQueue?.title).toBe(
      "Fresh queue",
    );
    expect(usePlaybackStore.getState().queue?.queueVersion).toBe(6);
  });

  test("action success does not directly apply returned snapshots after startup", async () => {
    usePlaybackStore.getState().bootstrapFromSnapshot(makeSnapshot());
    usePlaybackStore.setState({ started: true, generation: 1 });

    playbackApi.togglePlayback.mockResolvedValue(undefined);

    await usePlaybackStore.getState().togglePlayback();

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-1");
    expect(state.error).toBe("");
  });

  test("action failure recovers transport from a fresh snapshot", async () => {
    usePlaybackStore.getState().bootstrapFromSnapshot(makeSnapshot());
    usePlaybackStore.setState({ started: true, generation: 1 });

    playbackApi.nextTrack.mockRejectedValue(new Error("next failed"));
    const recoveredSnapshot = makeSnapshot(8);
    recoveredSnapshot.currentEntry = PlaybackModels.SessionEntry.createFrom({
      entryId: "ctx-2",
      origin: PlaybackModels.EntryOrigin.EntryOriginContext,
      contextIndex: 1,
      item: {
        recordingId: "rec-2",
        title: "Track 2",
      },
    });
    recoveredSnapshot.positionMs = 4_000;
    recoveredSnapshot.positionCapturedAtMs = 80_000;
    playbackApi.getPlaybackSnapshot.mockResolvedValue(recoveredSnapshot);

    await usePlaybackStore.getState().next();

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-2");
    expect(state.transport?.positionMs).toBe(4_000);
    expect(state.transport?.positionCapturedAtMs).toBe(80_000);
    expect(state.error).toBe("next failed");
  });

  test("bootstrap preserves newer transport events that arrive before the snapshot resolves", async () => {
    const initialSnapshot = deferred<PlaybackModels.SessionSnapshot>();
    playbackApi.getPlaybackSnapshot.mockImplementationOnce(
      () => initialSnapshot.promise,
    );

    const bootstrapPromise = usePlaybackStore.getState().bootstrap();

    emitPlaybackEvent(
      "playback:transport",
      makeTransportEvent("ctx-live", 90_000),
    );

    initialSnapshot.resolve(makeSnapshot());
    await bootstrapPromise;

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-live");
    expect(state.transport?.positionCapturedAtMs).toBe(90_000);
    expect(state.queue?.contextQueue?.title).toBe("All tracks");
  });

  test("teardown invalidates an older bootstrap snapshot after a new bootstrap starts", async () => {
    const firstSnapshot = deferred<PlaybackModels.SessionSnapshot>();
    const secondSnapshot = makeSnapshot(6);
    secondSnapshot.currentEntry = PlaybackModels.SessionEntry.createFrom({
      entryId: "ctx-new",
      origin: PlaybackModels.EntryOrigin.EntryOriginContext,
      contextIndex: 2,
      item: {
        recordingId: "rec-new",
        title: "Track New",
      },
    });
    secondSnapshot.positionMs = 7_000;
    secondSnapshot.positionCapturedAtMs = 95_000;

    playbackApi.getPlaybackSnapshot
      .mockImplementationOnce(() => firstSnapshot.promise)
      .mockResolvedValueOnce(secondSnapshot);

    const firstBootstrap = usePlaybackStore.getState().bootstrap();
    usePlaybackStore.getState().teardown();
    const secondBootstrap = usePlaybackStore.getState().bootstrap();

    await secondBootstrap;
    firstSnapshot.resolve(makeSnapshot());
    await firstBootstrap;

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-new");
    expect(state.transport?.positionMs).toBe(7_000);
    expect(state.transport?.positionCapturedAtMs).toBe(95_000);
  });

  test("failed actions from an older generation cannot apply stale recovery snapshots", async () => {
    const initialSnapshot = makeSnapshot();
    usePlaybackStore.setState((state) => ({
      ...state,
      started: true,
      generation: 1,
      transport: createTransportFromSnapshot(initialSnapshot),
      queue: createQueueFromSnapshot(initialSnapshot),
    }));

    const staleRecovery = deferred<PlaybackModels.SessionSnapshot>();
    const freshBootstrapSnapshot = makeSnapshot(9);
    freshBootstrapSnapshot.currentEntry =
      PlaybackModels.SessionEntry.createFrom({
        entryId: "ctx-fresh",
        origin: PlaybackModels.EntryOrigin.EntryOriginContext,
        contextIndex: 3,
        item: {
          recordingId: "rec-fresh",
          title: "Track Fresh",
        },
      });
    freshBootstrapSnapshot.positionMs = 8_500;
    freshBootstrapSnapshot.positionCapturedAtMs = 99_000;

    playbackApi.nextTrack.mockRejectedValue(new Error("next failed"));
    playbackApi.getPlaybackSnapshot
      .mockImplementationOnce(() => staleRecovery.promise)
      .mockResolvedValueOnce(freshBootstrapSnapshot);

    const nextPromise = usePlaybackStore.getState().next();
    await Promise.resolve();
    usePlaybackStore.getState().teardown();
    const bootstrapPromise = usePlaybackStore.getState().bootstrap();

    await bootstrapPromise;
    staleRecovery.resolve(makeSnapshot());
    await nextPromise;

    const state = usePlaybackStore.getState();
    expect(state.transport?.currentEntry?.entryId).toBe("ctx-fresh");
    expect(state.transport?.positionCapturedAtMs).toBe(99_000);
    expect(state.error).toBe("");
  });
});
