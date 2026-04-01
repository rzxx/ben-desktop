import { Events } from "@wailsio/runtime";
import { create } from "zustand";
import {
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
  previousTrack,
  queueAlbum,
  queuePlaylist,
  queueRecording,
  removeQueuedEntry,
  selectQueueEntry,
  setRepeatMode,
  setShuffle,
  setVolume,
  seekTo,
  subscribePlaybackEvents,
  togglePlayback,
} from "@/lib/api/playback";
import { PlaybackModels, type SessionSnapshot } from "@/lib/api/models";

type PlaybackStore = {
  error: string;
  started: boolean;
  snapshot: SessionSnapshot | null;
  stopListening?: () => void;
  bootstrap: () => Promise<void>;
  teardown: () => void;
  setSnapshot: (snapshot: SessionSnapshot) => void;
  togglePlayback: () => Promise<void>;
  next: () => Promise<void>;
  previous: () => Promise<void>;
  seekTo: (positionMs: number) => Promise<void>;
  setVolume: (volume: number) => Promise<void>;
  setShuffle: (enabled: boolean) => Promise<void>;
  setRepeatMode: (mode: string) => Promise<void>;
  playAlbum: (albumId: string) => Promise<void>;
  playAlbumTrack: (albumId: string, recordingId: string) => Promise<void>;
  queueAlbum: (albumId: string) => Promise<void>;
  playPlaylist: (playlistId: string) => Promise<void>;
  playPlaylistTrack: (playlistId: string, itemId: string) => Promise<void>;
  queuePlaylist: (playlistId: string) => Promise<void>;
  playRecording: (recordingId: string) => Promise<void>;
  queueRecording: (recordingId: string) => Promise<void>;
  playLiked: () => Promise<void>;
  playLikedTrack: (recordingId: string) => Promise<void>;
  selectEntry: (entryId: string) => Promise<void>;
  removeQueuedEntry: (entryId: string) => Promise<void>;
  clearQueue: () => Promise<void>;
};

let snapshotRequestSequence = 0;

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function isPendingPlaybackSnapshot(snapshot: SessionSnapshot | null) {
  return snapshot?.status === "pending" && Boolean(snapshot.loadingItem);
}

async function applySnapshot(
  runner: () => Promise<SessionSnapshot>,
  recoverSnapshot: () => Promise<SessionSnapshot>,
  shouldApply: () => boolean,
  setSnapshot: (snapshot: SessionSnapshot) => void,
  setError: (message: string) => void,
) {
  try {
    const snapshot = await runner();
    if (!shouldApply()) {
      return;
    }
    setSnapshot(snapshot);
    setError("");
  } catch (error) {
    const recoveredSnapshot = await recoverSnapshot()
      .then((snapshot) => {
        if (shouldApply()) {
          setSnapshot(snapshot);
        }
        return snapshot;
      })
      .catch(() => null);

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
  snapshot: null,
  stopListening: undefined,

  setSnapshot: (snapshot) => {
    set({
      snapshot,
      error: snapshot.lastError ?? "",
    });
  },

  bootstrap: async () => {
    if (get().started) {
      return;
    }
    set({ started: true });

    const eventName = await subscribePlaybackEvents();
    const stopListening = Events.On(eventName, (event) => {
      get().setSnapshot(PlaybackModels.SessionSnapshot.createFrom(event.data));
    });
    set({ stopListening });

    const token = ++snapshotRequestSequence;
    await applySnapshot(
      getPlaybackSnapshot,
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },

  teardown: () => {
    get().stopListening?.();
    snapshotRequestSequence++;
    set({
      started: false,
      stopListening: undefined,
    });
  },

  togglePlayback: async () => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      togglePlayback,
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  next: async () => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      nextTrack,
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  previous: async () => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      previousTrack,
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  seekTo: async (positionMs) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => seekTo(Math.max(0, Math.trunc(positionMs))),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setVolume: async (volume) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => setVolume(Math.max(0, Math.min(100, Math.trunc(volume)))),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setShuffle: async (enabled) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => setShuffle(enabled),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setRepeatMode: async (mode) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => setRepeatMode(mode),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbum: async (albumId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => playAlbum(albumId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbumTrack: async (albumId, recordingId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => playAlbumTrack(albumId, recordingId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queueAlbum: async (albumId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => queueAlbum(albumId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylist: async (playlistId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => playPlaylist(playlistId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylistTrack: async (playlistId, itemId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => playPlaylistTrack(playlistId, itemId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queuePlaylist: async (playlistId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => queuePlaylist(playlistId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playRecording: async (recordingId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => playRecording(recordingId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queueRecording: async (recordingId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => queueRecording(recordingId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playLiked: async () => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      playLiked,
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playLikedTrack: async (recordingId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => playLikedTrack(recordingId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  selectEntry: async (entryId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => selectQueueEntry(entryId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  removeQueuedEntry: async (entryId) => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      () => removeQueuedEntry(entryId),
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  clearQueue: async () => {
    const token = ++snapshotRequestSequence;
    await applySnapshot(
      clearQueue,
      getPlaybackSnapshot,
      () => token === snapshotRequestSequence,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
}));
