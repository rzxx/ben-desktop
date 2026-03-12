import { Events } from "@wailsio/runtime";
import { create } from "zustand";
import * as PlaybackService from "../../../bindings/ben/desktop/playbackservice";
import {
  PlaybackModels,
  type SessionSnapshot,
  resolveRecordingArtworkURL,
} from "../../shared/lib/desktop";

type PlaybackStore = {
  artworkUrl: string;
  artworkRecordingId: string;
  error: string;
  started: boolean;
  snapshot: SessionSnapshot | null;
  stopListening?: () => void;
  bootstrap: () => Promise<void>;
  teardown: () => void;
  setSnapshot: (snapshot: SessionSnapshot) => void;
  refreshArtwork: (recordingId?: string) => Promise<void>;
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

async function applySnapshot(
  runner: () => Promise<SessionSnapshot>,
  setSnapshot: (snapshot: SessionSnapshot) => void,
  setError: (message: string) => void,
) {
  try {
    const snapshot = await runner();
    setSnapshot(snapshot);
    setError("");
  } catch (error) {
    setError(error instanceof Error ? error.message : String(error));
  }
}

export const usePlaybackStore = create<PlaybackStore>((set, get) => ({
  artworkUrl: "",
  artworkRecordingId: "",
  error: "",
  started: false,
  snapshot: null,
  stopListening: undefined,

  setSnapshot: (snapshot) => {
    set({
      snapshot,
      error: snapshot.lastError ?? "",
    });
    void get().refreshArtwork(snapshot.currentItem?.artworkRef);
  },

  refreshArtwork: async (recordingId) => {
    const trimmed = recordingId?.trim() ?? "";
    if (!trimmed) {
      set({
        artworkRecordingId: "",
        artworkUrl: "",
      });
      return;
    }
    if (get().artworkRecordingId === trimmed && get().artworkUrl) {
      return;
    }
    try {
      const artworkUrl = await resolveRecordingArtworkURL(trimmed);
      if (get().snapshot?.currentItem?.artworkRef !== trimmed) {
        return;
      }
      set({
        artworkRecordingId: trimmed,
        artworkUrl,
      });
    } catch {
      if (get().snapshot?.currentItem?.artworkRef !== trimmed) {
        return;
      }
      set({
        artworkRecordingId: trimmed,
        artworkUrl: "",
      });
    }
  },

  bootstrap: async () => {
    if (get().started) {
      return;
    }
    set({ started: true });

    const eventName = await PlaybackService.SubscribePlaybackEvents();
    const stopListening = Events.On(eventName, (event) => {
      get().setSnapshot(PlaybackModels.SessionSnapshot.createFrom(event.data));
    });
    set({ stopListening });

    await applySnapshot(
      () => PlaybackService.GetPlaybackSnapshot(),
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
      () => PlaybackService.TogglePlayback(),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  next: async () => {
    await applySnapshot(
      () => PlaybackService.Next(),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  previous: async () => {
    await applySnapshot(
      () => PlaybackService.Previous(),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  seekTo: async (positionMs) => {
    await applySnapshot(
      () => PlaybackService.SeekTo(Math.max(0, Math.trunc(positionMs))),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setVolume: async (volume) => {
    await applySnapshot(
      () => PlaybackService.SetVolume(Math.max(0, Math.min(100, Math.trunc(volume)))),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setShuffle: async (enabled) => {
    await applySnapshot(
      () => PlaybackService.SetShuffle(enabled),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  setRepeatMode: async (mode) => {
    await applySnapshot(
      () => PlaybackService.SetRepeatMode(mode),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbum: async (albumId) => {
    await applySnapshot(
      () => PlaybackService.PlayAlbum(albumId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playAlbumTrack: async (albumId, recordingId) => {
    await applySnapshot(
      () => PlaybackService.PlayAlbumTrack(albumId, recordingId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queueAlbum: async (albumId) => {
    await applySnapshot(
      () => PlaybackService.QueueAlbum(albumId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylist: async (playlistId) => {
    await applySnapshot(
      () => PlaybackService.PlayPlaylist(playlistId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playPlaylistTrack: async (playlistId, itemId) => {
    await applySnapshot(
      () => PlaybackService.PlayPlaylistTrack(playlistId, itemId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queuePlaylist: async (playlistId) => {
    await applySnapshot(
      () => PlaybackService.QueuePlaylist(playlistId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playRecording: async (recordingId) => {
    await applySnapshot(
      () => PlaybackService.PlayRecording(recordingId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  queueRecording: async (recordingId) => {
    await applySnapshot(
      () => PlaybackService.QueueRecording(recordingId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playLiked: async () => {
    await applySnapshot(
      () => PlaybackService.PlayLiked(),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  playLikedTrack: async (recordingId) => {
    await applySnapshot(
      () => PlaybackService.PlayLikedTrack(recordingId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  selectEntry: async (entryId) => {
    await applySnapshot(
      () => PlaybackService.SelectEntry(entryId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  removeQueuedEntry: async (entryId) => {
    await applySnapshot(
      () => PlaybackService.RemoveQueuedEntry(entryId),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
  clearQueue: async () => {
    await applySnapshot(
      () => PlaybackService.ClearQueue(),
      get().setSnapshot,
      (error) => set({ error }),
    );
  },
}));
