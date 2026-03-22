import { create } from "zustand";
import {
  isRecordingLiked,
  likeRecording,
  unlikeRecording,
} from "@/lib/api/catalog";

type RecordingLikeInput = {
  libraryRecordingId?: string | null;
  recordingId?: string | null;
};

type RecordingLikeRecord = {
  error: string;
  fetchedAt: number | null;
  inFlight: boolean;
  liked: boolean;
  stale: boolean;
};

type RecordingLikesStore = {
  likesById: Record<string, RecordingLikeRecord>;
  ensureLikeState: (input: RecordingLikeInput) => Promise<boolean>;
  seedLikeState: (input: RecordingLikeInput, liked: boolean) => void;
  setLikeState: (input: RecordingLikeInput, liked: boolean) => Promise<boolean>;
  invalidateRecordingLikes: (recordingIds: string[]) => void;
  invalidateAllRecordingLikes: () => void;
};

const FRESH_INVALIDATION_GRACE_MS = 1000;

const EMPTY_LIKE_RECORD: RecordingLikeRecord = {
  error: "",
  fetchedAt: null,
  inFlight: false,
  liked: false,
  stale: true,
};

export function resolveRecordingLikeKey(input: RecordingLikeInput) {
  const libraryRecordingId = input.libraryRecordingId?.trim() ?? "";
  if (libraryRecordingId) {
    return libraryRecordingId;
  }
  return input.recordingId?.trim() ?? "";
}

export const useRecordingLikesStore = create<RecordingLikesStore>(
  (set, get) => ({
    likesById: {},

    async ensureLikeState(input) {
      const key = resolveRecordingLikeKey(input);
      if (!key) {
        return false;
      }
      const current = get().likesById[key];
      if (current && !current.stale && !current.inFlight) {
        return current.liked;
      }
      if (current?.inFlight) {
        return current.liked;
      }

      set((state) => ({
        likesById: {
          ...state.likesById,
          [key]: {
            ...(state.likesById[key] ?? EMPTY_LIKE_RECORD),
            error: "",
            inFlight: true,
          },
        },
      }));

      try {
        const liked = await isRecordingLiked(key);
        set((state) => ({
          likesById: {
            ...state.likesById,
            [key]: {
              error: "",
              fetchedAt: Date.now(),
              inFlight: false,
              liked,
              stale: false,
            },
          },
        }));
        return liked;
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        set((state) => ({
          likesById: {
            ...state.likesById,
            [key]: {
              ...(state.likesById[key] ?? EMPTY_LIKE_RECORD),
              error: message,
              inFlight: false,
              stale: true,
            },
          },
        }));
        throw error;
      }
    },

    seedLikeState(input, liked) {
      const key = resolveRecordingLikeKey(input);
      if (!key) {
        return;
      }
      const current = get().likesById[key];
      if (current && !current.stale && current.liked === liked) {
        return;
      }
      set((state) => ({
        likesById: {
          ...state.likesById,
          [key]: {
            error: "",
            fetchedAt: Date.now(),
            inFlight: false,
            liked,
            stale: false,
          },
        },
      }));
    },

    async setLikeState(input, liked) {
      const key = resolveRecordingLikeKey(input);
      if (!key) {
        return false;
      }
      set((state) => ({
        likesById: {
          ...state.likesById,
          [key]: {
            ...(state.likesById[key] ?? EMPTY_LIKE_RECORD),
            error: "",
            inFlight: true,
          },
        },
      }));

      try {
        if (liked) {
          await likeRecording(key);
        } else {
          await unlikeRecording(key);
        }
        set((state) => ({
          likesById: {
            ...state.likesById,
            [key]: {
              error: "",
              fetchedAt: Date.now(),
              inFlight: false,
              liked,
              stale: false,
            },
          },
        }));
        return liked;
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        set((state) => ({
          likesById: {
            ...state.likesById,
            [key]: {
              ...(state.likesById[key] ?? EMPTY_LIKE_RECORD),
              error: message,
              inFlight: false,
              stale: true,
            },
          },
        }));
        throw error;
      }
    },

    invalidateRecordingLikes(recordingIds) {
      set((state) => {
        if (recordingIds.length === 0) {
          return state;
        }
        const likesById = { ...state.likesById };
        let changed = false;
        for (const recordingId of recordingIds) {
          const key = recordingId.trim();
          const current = likesById[key];
          if (!key || !current) {
            continue;
          }
          if (
            current.fetchedAt !== null &&
            Date.now() - current.fetchedAt < FRESH_INVALIDATION_GRACE_MS
          ) {
            continue;
          }
          likesById[key] = {
            ...current,
            stale: true,
          };
          changed = true;
        }
        return changed ? { likesById } : state;
      });
    },

    invalidateAllRecordingLikes() {
      set((state) => {
        const keys = Object.keys(state.likesById);
        if (keys.length === 0) {
          return state;
        }
        const likesById = { ...state.likesById };
        for (const key of keys) {
          likesById[key] = {
            ...likesById[key],
            stale: true,
          };
        }
        return { likesById };
      });
    },
  }),
);
