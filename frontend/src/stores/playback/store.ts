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

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function isPendingPlaybackSnapshot(snapshot: SessionSnapshot | null) {
  return snapshot?.status === "pending" && Boolean(snapshot.loadingItem);
}

async function applySnapshot(
  runner: () => Promise<SessionSnapshot>,
  recoverSnapshot: () => Promise<SessionSnapshot>,
  setSnapshot: (snapshot: SessionSnapshot) => void,
  setError: (message: string) => void,
) {
  try {
    const snapshot = await runner();
    setSnapshot(snapshot);
    setError("");
  } catch (error) {
    const recoveredSnapshot = await recoverSnapshot()
      .then((snapshot) => {
        setSnapshot(snapshot);
        return snapshot;
      })
      .catch(() => null);

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

    await applySnapshot(
      getPlaybackSnapshot,
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },

  teardown: () => {
    get().stopListening?.();
    set({
      started: false,
      stopListening: undefined,
    });
  },

  togglePlayback: async () => {
    await applySnapshot(
      togglePlayback,
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  next: async () => {
    await applySnapshot(
      nextTrack,
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  previous: async () => {
    await applySnapshot(
      previousTrack,
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  seekTo: async (positionMs) => {
    await applySnapshot(
      () => seekTo(Math.max(0, Math.trunc(positionMs))),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setVolume: async (volume) => {
    await applySnapshot(
      () => setVolume(Math.max(0, Math.min(100, Math.trunc(volume)))),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setShuffle: async (enabled) => {
    await applySnapshot(
      () => setShuffle(enabled),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setRepeatMode: async (mode) => {
    await applySnapshot(
      () => setRepeatMode(mode),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbum: async (albumId) => {
    await applySnapshot(
      () => playAlbum(albumId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbumTrack: async (albumId, recordingId) => {
    await applySnapshot(
      () => playAlbumTrack(albumId, recordingId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queueAlbum: async (albumId) => {
    await applySnapshot(
      () => queueAlbum(albumId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylist: async (playlistId) => {
    await applySnapshot(
      () => playPlaylist(playlistId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylistTrack: async (playlistId, itemId) => {
    await applySnapshot(
      () => playPlaylistTrack(playlistId, itemId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queuePlaylist: async (playlistId) => {
    await applySnapshot(
      () => queuePlaylist(playlistId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playRecording: async (recordingId) => {
    await applySnapshot(
      () => playRecording(recordingId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queueRecording: async (recordingId) => {
    await applySnapshot(
      () => queueRecording(recordingId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playLiked: async () => {
    await applySnapshot(
      playLiked,
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playLikedTrack: async (recordingId) => {
    await applySnapshot(
      () => playLikedTrack(recordingId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  selectEntry: async (entryId) => {
    await applySnapshot(
      () => selectQueueEntry(entryId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  removeQueuedEntry: async (entryId) => {
    await applySnapshot(
      () => removeQueuedEntry(entryId),
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  clearQueue: async () => {
    await applySnapshot(
      clearQueue,
      getPlaybackSnapshot,
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
}));
