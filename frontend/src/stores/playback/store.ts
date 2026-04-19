import { Events } from "@wailsio/runtime";
import { create } from "zustand";
import {
  PLAYBACK_QUEUE_EVENT_NAME,
  PLAYBACK_TRANSPORT_EVENT_NAME,
  clearQueue,
  getPlaybackSnapshot,
  nextTrack,
  playAlbum,
  playAlbumTrack,
  playLiked,
  playLikedTrack,
  playPlaylist,
  playPlaylistTrack,
  playRecording,
  playTracks,
  playTracksFrom,
  previousTrack,
  queueAlbum,
  queueLikedTrack,
  queuePlaylist,
  queuePlaylistTrack,
  queueRecording,
  removeQueuedEntry,
  selectQueueEntry,
  setRepeatMode,
  setShuffle,
  setVolume,
  seekTo,
  shuffleTracks,
  togglePlayback,
} from "@/lib/api/playback";
import { PlaybackModels, Types, type SessionSnapshot } from "@/lib/api/models";
import {
  recordPlaybackDebugTrace,
  syncPlaybackTraceEnabled,
  updatePlaybackDebugLiveState,
} from "@/lib/playback/debugTrace";

type PlaybackTransportState = {
  currentEntry: PlaybackModels.SessionEntry | null;
  loadingEntry: PlaybackModels.SessionEntry | null;
  loadingPreparation: PlaybackModels.EntryPreparation | null;
  repeatMode: SessionSnapshot["repeatMode"];
  shuffle: boolean;
  volume: number;
  status: SessionSnapshot["status"];
  positionMs: number;
  positionCapturedAtMs: number;
  durationMs: number | null;
  lastError: string;
};

type PlaybackQueueContextState = {
  title: string;
  entries: PlaybackModels.SessionEntry[];
  hasBefore: boolean;
  hasAfter: boolean;
  totalCount: number;
  currentIndex: number;
  resumeIndex: number;
  windowStart: number;
  windowCount: number;
  loading: boolean;
  sourceVersion: number;
  source: PlaybackModels.PlaybackSourceDescriptor | null;
  anchor: PlaybackModels.PlaybackSourceAnchor | null;
  shuffleBag: number[];
};

type PlaybackQueueState = {
  contextQueue: PlaybackQueueContextState | null;
  userQueue: PlaybackModels.SessionEntry[];
  entryAvailability: Record<string, Types.RecordingPlaybackAvailability>;
  queueLength: number;
  queueVersion: number;
};

type PlaybackStore = {
  error: string;
  started: boolean;
  generation: number;
  transportStateSequence: number;
  transport: PlaybackTransportState | null;
  queue: PlaybackQueueState | null;
  stopListening?: () => void;
  bootstrap: () => Promise<void>;
  teardown: () => void;
  bootstrapFromSnapshot: (
    snapshot: SessionSnapshot,
    options?: BootstrapFromSnapshotOptions,
  ) => void;
  applyTransportEvent: (payload: PlaybackTransportEvent) => void;
  applyQueueEvent: (payload: PlaybackQueueEvent) => void;
  togglePlayback: () => Promise<void>;
  next: () => Promise<void>;
  previous: () => Promise<void>;
  seekTo: (
    positionMs: number,
    expectedEntryId?: string,
    debugRequestId?: string,
  ) => Promise<SessionSnapshot | null>;
  setVolume: (volume: number) => Promise<void>;
  setShuffle: (enabled: boolean) => Promise<void>;
  setRepeatMode: (mode: string) => Promise<void>;
  playAlbum: (albumId: string) => Promise<void>;
  playAlbumTrack: (albumId: string, recordingId: string) => Promise<void>;
  queueAlbum: (albumId: string) => Promise<void>;
  playPlaylist: (playlistId: string) => Promise<void>;
  playPlaylistTrack: (playlistId: string, itemId: string) => Promise<void>;
  queuePlaylist: (playlistId: string) => Promise<void>;
  queuePlaylistTrack: (playlistId: string, itemId: string) => Promise<void>;
  playRecording: (recordingId: string) => Promise<void>;
  queueRecording: (recordingId: string) => Promise<void>;
  playLiked: () => Promise<void>;
  playLikedTrack: (recordingId: string) => Promise<void>;
  queueLikedTrack: (recordingId: string) => Promise<void>;
  playTracks: () => Promise<void>;
  shuffleTracks: () => Promise<void>;
  playTracksFrom: (recordingId: string) => Promise<void>;
  selectEntry: (entryId: string) => Promise<void>;
  removeQueuedEntry: (entryId: string) => Promise<void>;
  clearQueue: () => Promise<void>;
};

type BootstrapFromSnapshotOptions = {
  preserveNewerTransport?: boolean;
  transportStateSequenceFloor?: number;
};

type PlaybackTransportEvent = PlaybackModels.TransportEventSnapshot;
type PlaybackQueueEvent = PlaybackModels.QueueEventSnapshot;

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function isPendingPlaybackSnapshot(snapshot: SessionSnapshot | null) {
  return snapshot?.status === "pending" && Boolean(snapshot.loadingItem);
}

function createSessionItem(
  item: PlaybackModels.SessionItemEvent | null | undefined,
) {
  if (item == null) {
    return null;
  }
  return PlaybackModels.SessionItem.createFrom({
    libraryRecordingId: item.libraryRecordingId ?? "",
    variantRecordingId: item.variantRecordingId ?? "",
    recordingId: item.recordingId ?? "",
    title: item.title ?? "",
    subtitle: item.subtitle ?? "",
    durationMs: item.durationMs ?? 0,
    artworkRef: item.artworkRef ?? "",
    sourceKind: item.sourceKind ?? "",
    sourceId: item.sourceId ?? "",
    sourceItemId: item.sourceItemId ?? "",
    albumId: item.albumId ?? "",
    variantAlbumId: item.variantAlbumId ?? item.albumId ?? "",
  });
}

function createSessionEntry(
  entry: PlaybackModels.QueueEntryEventSnapshot | null | undefined,
  fallbackOrigin = "context",
) {
  if (entry == null) {
    return null;
  }

  const item = createSessionItem(entry.item);
  return PlaybackModels.SessionEntry.createFrom({
    entryId: entry.entryId ?? "",
    origin: entry.origin ?? fallbackOrigin,
    contextIndex: entry.contextIndex ?? 0,
    item: item ?? PlaybackModels.SessionItem.createFrom(),
  });
}

function isSessionEntry(
  entry: PlaybackModels.SessionEntry | null,
): entry is PlaybackModels.SessionEntry {
  return entry != null;
}

function createTransportFromSnapshot(
  snapshot: SessionSnapshot,
): PlaybackTransportState {
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

function createQueueContextFromSnapshot(
  queue: SessionSnapshot["contextQueue"],
): PlaybackQueueContextState | null {
  if (queue == null) {
    return null;
  }
  return {
    title: queue.title ?? "",
    entries: [...(queue.entries ?? [])],
    hasBefore: queue.hasBefore ?? false,
    hasAfter: queue.hasAfter ?? false,
    totalCount: queue.totalCount ?? 0,
    currentIndex: queue.currentIndex ?? -1,
    resumeIndex: queue.resumeIndex ?? -1,
    windowStart: queue.windowStart ?? 0,
    windowCount: queue.windowCount ?? 0,
    loading: queue.loading ?? false,
    sourceVersion: queue.sourceVersion ?? 0,
    source: queue.source ?? null,
    anchor: queue.anchor ?? null,
    shuffleBag: [...(queue.shuffleBag ?? [])],
  };
}

function createQueueFromSnapshot(
  snapshot: SessionSnapshot,
): PlaybackQueueState {
  const entryAvailability: Record<string, Types.RecordingPlaybackAvailability> =
    {};
  for (const [key, value] of Object.entries(snapshot.entryAvailability ?? {})) {
    if (value == null) {
      continue;
    }
    entryAvailability[key] = value;
  }
  return {
    contextQueue: createQueueContextFromSnapshot(snapshot.contextQueue ?? null),
    userQueue: [...snapshot.userQueue],
    entryAvailability,
    queueLength: snapshot.queueLength ?? 0,
    queueVersion: snapshot.queueVersion ?? 0,
  };
}

function createTransportFromEvent(
  payload: PlaybackTransportEvent,
): PlaybackTransportState {
  const event = PlaybackModels.TransportEventSnapshot.createFrom(payload);
  return {
    currentEntry: createSessionEntry(event.currentEntry) ?? null,
    loadingEntry: createSessionEntry(event.loadingEntry) ?? null,
    loadingPreparation:
      event.loadingPreparation == null ? null : event.loadingPreparation,
    repeatMode: event.repeatMode ?? PlaybackModels.RepeatMode.$zero,
    shuffle: event.shuffle ?? false,
    volume: event.volume ?? 0,
    status: event.status ?? PlaybackModels.Status.$zero,
    positionMs: event.positionMs ?? 0,
    positionCapturedAtMs: event.positionCapturedAtMs ?? 0,
    durationMs: event.durationMs ?? null,
    lastError: event.lastError ?? "",
  };
}

function createQueueContextFromEvent(
  payload: PlaybackModels.ContextQueueEventSnapshot | null | undefined,
): PlaybackQueueContextState | null {
  if (payload == null) {
    return null;
  }
  return {
    title: payload.title ?? "",
    entries: (payload.entries ?? [])
      .map((entry) => createSessionEntry(entry, "context"))
      .filter(isSessionEntry),
    hasBefore: payload.hasBefore ?? false,
    hasAfter: payload.hasAfter ?? false,
    totalCount: payload.totalCount ?? 0,
    currentIndex: payload.currentIndex ?? -1,
    resumeIndex: payload.resumeIndex ?? -1,
    windowStart: payload.windowStart ?? 0,
    windowCount: payload.windowCount ?? 0,
    loading: payload.loading ?? false,
    sourceVersion: payload.sourceVersion ?? 0,
    source:
      payload.source == null
        ? null
        : PlaybackModels.PlaybackSourceDescriptor.createFrom(payload.source),
    anchor:
      payload.anchor == null
        ? null
        : PlaybackModels.PlaybackSourceAnchor.createFrom(payload.anchor),
    shuffleBag: [...(payload.shuffleBag ?? [])],
  };
}

function createQueueFromEvent(payload: PlaybackQueueEvent): PlaybackQueueState {
  const event = PlaybackModels.QueueEventSnapshot.createFrom(payload);
  const entryAvailability: Record<string, Types.RecordingPlaybackAvailability> =
    {};
  for (const [key, value] of Object.entries(event.entryAvailability ?? {})) {
    if (value == null) {
      continue;
    }
    entryAvailability[key] = value;
  }
  return {
    contextQueue: createQueueContextFromEvent(event.contextQueue ?? null),
    userQueue: (event.userQueue ?? [])
      .map((entry) => createSessionEntry(entry, "queued"))
      .filter(isSessionEntry),
    entryAvailability,
    queueLength: event.queueLength ?? 0,
    queueVersion: event.queueVersion ?? 0,
  };
}

function preserveNewestQueueState(
  current: PlaybackQueueState | null,
  incoming: PlaybackQueueState,
) {
  if (!current) {
    return incoming;
  }
  if (incoming.queueVersion >= current.queueVersion) {
    return incoming;
  }
  return current;
}

async function recoverPlaybackSnapshot(
  shouldApply: () => boolean,
  captureTransportStateSequence: () => number,
  applySnapshotToStore: (
    snapshot: SessionSnapshot,
    options?: BootstrapFromSnapshotOptions,
  ) => void,
) {
  const transportStateSequenceFloor = captureTransportStateSequence();
  try {
    const snapshot = await getPlaybackSnapshot();
    if (shouldApply()) {
      applySnapshotToStore(snapshot, {
        preserveNewerTransport: true,
        transportStateSequenceFloor,
      });
    }
    return snapshot;
  } catch {
    return null;
  }
}

async function runPlaybackAction(
  runner: () => Promise<unknown>,
  shouldApply: () => boolean,
  captureTransportStateSequence: () => number,
  applySnapshotToStore: (
    snapshot: SessionSnapshot,
    options?: BootstrapFromSnapshotOptions,
  ) => void,
  setError: (message: string) => void,
) {
  try {
    await runner();
    if (!shouldApply()) {
      return;
    }
    setError("");
  } catch (error) {
    const recoveredSnapshot = await recoverPlaybackSnapshot(
      shouldApply,
      captureTransportStateSequence,
      applySnapshotToStore,
    );

    if (!shouldApply()) {
      return;
    }

    if (isPendingPlaybackSnapshot(recoveredSnapshot)) {
      setError("");
      return;
    }

    setError(errorMessage(error));
  }
}

export const usePlaybackStore = create<PlaybackStore>((set, get) => ({
  error: "",
  started: false,
  generation: 0,
  transportStateSequence: 0,
  transport: null,
  queue: null,
  stopListening: undefined,

  bootstrapFromSnapshot: (snapshot, options) => {
    const nextTransport = createTransportFromSnapshot(snapshot);
    const state = get();
    const preserveTransport =
      options?.preserveNewerTransport === true &&
      state.transport != null &&
      options.transportStateSequenceFloor != null &&
      state.transportStateSequence > options.transportStateSequenceFloor;
    const resolvedTransport = preserveTransport
      ? (state.transport ?? nextTransport)
      : nextTransport;
    const nextQueue = preserveNewestQueueState(
      state.queue,
      createQueueFromSnapshot(snapshot),
    );
    set({
      transport: resolvedTransport,
      queue: nextQueue,
      error: resolvedTransport.lastError,
      transportStateSequence: preserveTransport
        ? state.transportStateSequence
        : state.transportStateSequence + 1,
    });
    updatePlaybackDebugLiveState({
      currentEntryId: resolvedTransport.currentEntry?.entryId ?? "",
      status: resolvedTransport.status ?? "",
      transportPositionMs: resolvedTransport.positionMs,
      transportCapturedAtMs: resolvedTransport.positionCapturedAtMs,
    });
    recordPlaybackDebugTrace({
      kind: "store:bootstrapFromSnapshot",
      currentEntryId: resolvedTransport.currentEntry?.entryId ?? "",
      status: resolvedTransport.status ?? "",
      positionMs: resolvedTransport.positionMs,
      positionCapturedAtMs: resolvedTransport.positionCapturedAtMs,
      message: preserveTransport
        ? "preserved newer transport state"
        : undefined,
    });
  },

  applyTransportEvent: (payload) => {
    const transport = createTransportFromEvent(payload);
    const nextTransportStateSequence = get().transportStateSequence + 1;
    set({
      transport,
      error: transport.lastError,
      transportStateSequence: nextTransportStateSequence,
    });
    updatePlaybackDebugLiveState({
      currentEntryId: transport.currentEntry?.entryId ?? "",
      status: transport.status ?? "",
      transportPositionMs: transport.positionMs,
      transportCapturedAtMs: transport.positionCapturedAtMs,
    });
    recordPlaybackDebugTrace({
      kind: "store:transportEvent",
      currentEntryId: transport.currentEntry?.entryId ?? "",
      status: transport.status ?? "",
      positionMs: transport.positionMs,
      positionCapturedAtMs: transport.positionCapturedAtMs,
    });
  },

  applyQueueEvent: (payload) => {
    const nextQueue = createQueueFromEvent(payload);
    const currentQueue = get().queue;
    if (currentQueue && nextQueue.queueVersion < currentQueue.queueVersion) {
      return;
    }
    set({ queue: nextQueue });
  },

  bootstrap: async () => {
    if (get().started) {
      return;
    }
    const generation = get().generation + 1;
    set({ started: true, generation });
    void syncPlaybackTraceEnabled();
    const shouldApply = () => {
      const state = get();
      return state.started && state.generation === generation;
    };

    const stopTransportListening = Events.On(
      PLAYBACK_TRANSPORT_EVENT_NAME,
      (event) => {
        if (!shouldApply()) {
          return;
        }
        get().applyTransportEvent(event.data as PlaybackTransportEvent);
      },
    );
    const stopQueueListening = Events.On(PLAYBACK_QUEUE_EVENT_NAME, (event) => {
      if (!shouldApply()) {
        return;
      }
      get().applyQueueEvent(event.data as PlaybackQueueEvent);
    });
    const stopListening = () => {
      stopTransportListening();
      stopQueueListening();
    };
    set({ stopListening });

    try {
      const transportStateSequenceFloor = get().transportStateSequence;
      const snapshot = await getPlaybackSnapshot();
      if (!shouldApply()) {
        return;
      }
      get().bootstrapFromSnapshot(snapshot, {
        preserveNewerTransport: true,
        transportStateSequenceFloor,
      });
    } catch (error) {
      if (!shouldApply()) {
        return;
      }
      set({ error: errorMessage(error) });
    }
  },

  teardown: () => {
    const stopListening = get().stopListening;
    set({
      started: false,
      generation: get().generation + 1,
      stopListening: undefined,
    });
    stopListening?.();
  },

  togglePlayback: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      togglePlayback,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  next: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      nextTrack,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  previous: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      previousTrack,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  seekTo: async (positionMs, expectedEntryId, debugRequestId) => {
    const generation = get().generation;
    const shouldApply = () => get().started && get().generation === generation;
    if (
      expectedEntryId &&
      get().transport?.currentEntry?.entryId !== expectedEntryId
    ) {
      recordPlaybackDebugTrace({
        kind: "store:seekTo:blocked",
        seekRequestId: debugRequestId,
        currentEntryId: get().transport?.currentEntry?.entryId ?? "",
        message: "expected entry mismatch",
        positionMs,
      });
      return null;
    }
    recordPlaybackDebugTrace({
      kind: "store:seekTo:start",
      seekRequestId: debugRequestId,
      currentEntryId: get().transport?.currentEntry?.entryId ?? "",
      positionMs,
    });
    try {
      const snapshot = await seekTo(Math.max(0, Math.trunc(positionMs)));
      if (!shouldApply()) {
        return null;
      }
      set({ error: "" });
      recordPlaybackDebugTrace({
        kind: "store:seekTo:resolved",
        seekRequestId: debugRequestId,
        currentEntryId: snapshot.currentEntry?.entryId ?? "",
        status: snapshot.status ?? "",
        positionMs: snapshot.positionMs,
        positionCapturedAtMs: snapshot.positionCapturedAtMs ?? 0,
      });
      return snapshot;
    } catch (error) {
      const recoveredSnapshot = await recoverPlaybackSnapshot(
        shouldApply,
        () => get().transportStateSequence,
        get().bootstrapFromSnapshot,
      );

      if (!shouldApply()) {
        return null;
      }

      if (isPendingPlaybackSnapshot(recoveredSnapshot)) {
        set({ error: "" });
        recordPlaybackDebugTrace({
          kind: "store:seekTo:recovered_pending",
          seekRequestId: debugRequestId,
          message: errorMessage(error),
          positionMs,
        });
        return null;
      }

      set({ error: errorMessage(error) });
      recordPlaybackDebugTrace({
        kind: "store:seekTo:rejected",
        seekRequestId: debugRequestId,
        message: errorMessage(error),
        positionMs,
      });
      return null;
    }
  },
  setVolume: async (volume) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => setVolume(Math.max(0, Math.min(100, Math.trunc(volume)))),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  setShuffle: async (enabled) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => setShuffle(enabled),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  setRepeatMode: async (mode) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => setRepeatMode(mode),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbum: async (albumId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playAlbum(albumId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbumTrack: async (albumId, recordingId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playAlbumTrack(albumId, recordingId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  queueAlbum: async (albumId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => queueAlbum(albumId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylist: async (playlistId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playPlaylist(playlistId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylistTrack: async (playlistId, itemId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playPlaylistTrack(playlistId, itemId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  queuePlaylist: async (playlistId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => queuePlaylist(playlistId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  queuePlaylistTrack: async (playlistId, itemId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => queuePlaylistTrack(playlistId, itemId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playRecording: async (recordingId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playRecording(recordingId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  queueRecording: async (recordingId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => queueRecording(recordingId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playLiked: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      playLiked,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playLikedTrack: async (recordingId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playLikedTrack(recordingId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  queueLikedTrack: async (recordingId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => queueLikedTrack(recordingId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playTracks: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      playTracks,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  shuffleTracks: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      shuffleTracks,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  playTracksFrom: async (recordingId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => playTracksFrom(recordingId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  selectEntry: async (entryId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => selectQueueEntry(entryId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  removeQueuedEntry: async (entryId) => {
    const generation = get().generation;
    await runPlaybackAction(
      () => removeQueuedEntry(entryId),
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
  clearQueue: async () => {
    const generation = get().generation;
    await runPlaybackAction(
      clearQueue,
      () => get().started && get().generation === generation,
      () => get().transportStateSequence,
      get().bootstrapFromSnapshot,
      (error) => set({ error }),
    );
  },
}));
