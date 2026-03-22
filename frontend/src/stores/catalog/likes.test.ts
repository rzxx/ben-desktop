import { afterEach, describe, expect, test, vi } from "vitest";

vi.mock("@/lib/api/catalog", () => ({
  isRecordingLiked: vi.fn(
    async (recordingId: string) => recordingId === "lib-rec-1",
  ),
  likeRecording: vi.fn(async () => undefined),
  unlikeRecording: vi.fn(async () => undefined),
}));

import { resolveRecordingLikeKey, useRecordingLikesStore } from "./likes";
import {
  isRecordingLiked,
  likeRecording,
  unlikeRecording,
} from "@/lib/api/catalog";

describe("recording likes store", () => {
  afterEach(() => {
    useRecordingLikesStore.setState({ likesById: {} });
    vi.clearAllMocks();
  });

  test("prefers library recording ids when resolving keys", () => {
    expect(
      resolveRecordingLikeKey({
        libraryRecordingId: " lib-rec-1 ",
        recordingId: "rec-1",
      }),
    ).toBe("lib-rec-1");
    expect(
      resolveRecordingLikeKey({
        recordingId: " rec-1 ",
      }),
    ).toBe("rec-1");
  });

  test("loads and caches liked status by resolved identity", async () => {
    const first = await useRecordingLikesStore.getState().ensureLikeState({
      libraryRecordingId: "lib-rec-1",
      recordingId: "rec-1",
    });

    const second = await useRecordingLikesStore.getState().ensureLikeState({
      libraryRecordingId: "lib-rec-1",
      recordingId: "rec-1",
    });

    expect(first).toBe(true);
    expect(second).toBe(true);
    expect(isRecordingLiked).toHaveBeenCalledTimes(1);
    expect(isRecordingLiked).toHaveBeenCalledWith("lib-rec-1");
  });

  test("routes like and unlike mutations through the resolved identity", async () => {
    await useRecordingLikesStore.getState().setLikeState(
      {
        libraryRecordingId: "lib-rec-2",
        recordingId: "rec-2",
      },
      true,
    );
    await useRecordingLikesStore.getState().setLikeState(
      {
        recordingId: "rec-2",
      },
      false,
    );

    expect(likeRecording).toHaveBeenCalledWith("lib-rec-2");
    expect(unlikeRecording).toHaveBeenCalledWith("rec-2");
  });

  test("marks cached likes stale when invalidated", () => {
    useRecordingLikesStore.getState().seedLikeState(
      {
        libraryRecordingId: "lib-rec-3",
      },
      true,
    );
    useRecordingLikesStore.setState((state) => ({
      likesById: {
        ...state.likesById,
        "lib-rec-3": {
          ...state.likesById["lib-rec-3"]!,
          fetchedAt: Date.now() - 5_000,
        },
      },
    }));

    useRecordingLikesStore.getState().invalidateRecordingLikes(["lib-rec-3"]);

    expect(
      useRecordingLikesStore.getState().likesById["lib-rec-3"]?.stale,
    ).toBe(true);
  });
});
