import { act, createElement, useLayoutEffect, useRef } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { PlaybackModels } from "@/lib/api/models";
import { usePlayerProgress } from "./usePlayerProgress";

type HookInput = Parameters<typeof usePlayerProgress>[0];
type HookState = ReturnType<typeof usePlayerProgress>;

function HookHarness({
  hookStateRef,
  input,
}: {
  hookStateRef: { current: HookState | null };
  input: HookInput;
}) {
  const state = usePlayerProgress(input);
  const latestStateRef = useRef(state);

  useLayoutEffect(() => {
    latestStateRef.current = state;
    hookStateRef.current = latestStateRef.current;
  }, [hookStateRef, state]);

  return null;
}

function renderUsePlayerProgress(props: HookInput) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  const hookStateRef: { current: HookState | null } = { current: null };

  const render = (nextProps: HookInput) => {
    act(() => {
      root.render(
        createElement(HookHarness, { hookStateRef, input: nextProps }),
      );
    });
  };

  render(props);

  return {
    latest() {
      if (hookStateRef.current == null) {
        throw new Error("hook state is unavailable");
      }
      return hookStateRef.current;
    },
    rerender(nextProps: HookInput) {
      render(nextProps);
    },
    unmount() {
      act(() => {
        root.unmount();
      });
      container.remove();
      hookStateRef.current = null;
    },
  };
}

describe("usePlayerProgress", () => {
  let activeRoot: ReturnType<typeof renderUsePlayerProgress> | null = null;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T00:00:00Z"));
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.stubGlobal("requestAnimationFrame", ((callback: FrameRequestCallback) =>
      setTimeout(
        () => callback(Date.now()),
        16,
      )) as typeof requestAnimationFrame);
    vi.stubGlobal("cancelAnimationFrame", ((
      handle: ReturnType<typeof setTimeout>,
    ) => clearTimeout(handle)) as typeof cancelAnimationFrame);
  });

  afterEach(() => {
    activeRoot?.unmount();
    activeRoot = null;
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  test("advances playback from the authoritative captured timestamp", () => {
    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: true,
      positionMs: 1_500,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    expect(activeRoot.latest().shownPositionMs).toBe(1_500);

    act(() => {
      vi.advanceTimersByTime(64);
    });

    expect(activeRoot.latest().shownPositionMs).toBeGreaterThan(1_500);
  });

  test("freezes to the authoritative paused position without preserving an old draft", () => {
    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: true,
      positionMs: 1_500,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    act(() => {
      vi.advanceTimersByTime(250);
    });

    const interpolatedPosition = activeRoot.latest().shownPositionMs;
    expect(interpolatedPosition).toBeGreaterThan(1_500);

    activeRoot.rerender({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 1_900,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    expect(activeRoot.latest().shownPositionMs).toBe(1_900);
  });

  test("updates only local drag state until seek commit", () => {
    const seekCalls: Array<{ positionMs: number; expectedEntryId?: string }> =
      [];

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: true,
      positionMs: 1_500,
      positionCapturedAtMs: Date.now(),
      seekTo: (positionMs, expectedEntryId) => {
        seekCalls.push({ positionMs, expectedEntryId });
        return new Promise<PlaybackModels.SessionSnapshot | null>(() => {});
      },
    });

    act(() => {
      activeRoot?.latest().onValueChange(3_200);
    });

    expect(activeRoot.latest().isDragging).toBe(true);
    expect(activeRoot.latest().shownPositionMs).toBe(3_200);
    expect(seekCalls).toEqual([]);

    act(() => {
      activeRoot?.latest().onValueCommitted(3_200);
    });

    expect(activeRoot.latest().isDragging).toBe(false);
    expect(activeRoot.latest().shownPositionMs).toBe(3_200);
    expect(seekCalls).toEqual([
      { positionMs: 3_200, expectedEntryId: "entry-a" },
    ]);
  });

  test("ignores slider commits caused by hidden input change events", () => {
    const seekCalls: Array<{ positionMs: number; expectedEntryId?: string }> =
      [];

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: true,
      positionMs: 1_500,
      positionCapturedAtMs: Date.now(),
      seekTo: async (positionMs, expectedEntryId) => {
        seekCalls.push({ positionMs, expectedEntryId });
        return null;
      },
    });

    act(() => {
      activeRoot?.latest().onValueCommitted(3_200, { reason: "input-change" });
    });

    expect(seekCalls).toEqual([]);
    expect(activeRoot.latest().shownPositionMs).toBeGreaterThanOrEqual(1_500);
  });

  test("ignores stale drag commits that arrive after the real drag commit", () => {
    const seekCalls: Array<{ positionMs: number; expectedEntryId?: string }> =
      [];

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 200_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 25_836,
      positionCapturedAtMs: Date.now(),
      seekTo: async (positionMs, expectedEntryId) => {
        seekCalls.push({ positionMs, expectedEntryId });
        return null;
      },
    });

    act(() => {
      activeRoot?.latest().onInteractionStart();
      activeRoot?.latest().onValueChange(29_695);
      activeRoot?.latest().onValueCommitted(29_695, { reason: "drag" });
    });

    expect(seekCalls).toEqual([
      { positionMs: 29_695, expectedEntryId: "entry-a" },
    ]);

    act(() => {
      activeRoot?.latest().onValueCommitted(29_695, { reason: "drag" });
    });

    expect(seekCalls).toEqual([
      { positionMs: 29_695, expectedEntryId: "entry-a" },
    ]);
  });

  test("freezes the moving timeline immediately when interaction starts", () => {
    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: true,
      positionMs: 1_500,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    act(() => {
      vi.advanceTimersByTime(96);
    });

    const movingPosition = activeRoot.latest().shownPositionMs;
    expect(movingPosition).toBeGreaterThan(1_500);

    act(() => {
      activeRoot?.latest().onInteractionStart();
    });

    const frozenPosition = activeRoot.latest().shownPositionMs;
    expect(frozenPosition).toBe(movingPosition);

    act(() => {
      vi.advanceTimersByTime(96);
    });

    expect(activeRoot.latest().shownPositionMs).toBe(frozenPosition);
    expect(activeRoot.latest().isDragging).toBe(true);
  });

  test("keeps the committed seek position visible until authoritative transport catches up", async () => {
    let resolveSeek:
      | ((value: PlaybackModels.SessionSnapshot | null) => void)
      | null = null;
    const baseTime = Date.now();

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 1_500,
      positionCapturedAtMs: baseTime,
      seekTo: () =>
        new Promise<PlaybackModels.SessionSnapshot | null>((resolve) => {
          resolveSeek = resolve;
        }),
    });

    act(() => {
      activeRoot?.latest().onValueChange(3_200);
      activeRoot?.latest().onValueCommitted(3_200);
    });

    expect(activeRoot.latest().shownPositionMs).toBe(3_200);

    activeRoot.rerender({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 1_500,
      positionCapturedAtMs: 1_000,
      seekTo: () =>
        new Promise<PlaybackModels.SessionSnapshot | null>((resolve) => {
          resolveSeek = resolve;
        }),
    });

    expect(activeRoot.latest().shownPositionMs).toBe(3_200);

    await act(async () => {
      resolveSeek?.(
        PlaybackModels.SessionSnapshot.createFrom({
          status: PlaybackModels.Status.StatusPaused,
          positionMs: 3_180,
          positionCapturedAtMs: baseTime + 100,
        }),
      );
      await Promise.resolve();
    });

    expect(activeRoot.latest().shownPositionMs).toBe(3_200);

    activeRoot.rerender({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 3_180,
      positionCapturedAtMs: baseTime + 120,
      seekTo: async () => null,
    });

    expect(activeRoot.latest().shownPositionMs).toBe(3_180);
  });

  test("clears a pending seek when the resolved snapshot does not match the requested position", async () => {
    let resolveSeek:
      | ((value: PlaybackModels.SessionSnapshot | null) => void)
      | null = null;
    const baseTime = Date.now();

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 1_500,
      positionCapturedAtMs: baseTime,
      seekTo: () =>
        new Promise<PlaybackModels.SessionSnapshot | null>((resolve) => {
          resolveSeek = resolve;
        }),
    });

    act(() => {
      activeRoot?.latest().onValueChange(3_200);
      activeRoot?.latest().onValueCommitted(3_200);
    });

    await act(async () => {
      resolveSeek?.(
        PlaybackModels.SessionSnapshot.createFrom({
          status: PlaybackModels.Status.StatusPaused,
          positionMs: 1_520,
          positionCapturedAtMs: baseTime - 100,
        }),
      );
      await Promise.resolve();
    });

    expect(activeRoot.latest().shownPositionMs).toBe(1_500);

    activeRoot.rerender({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 1_520,
      positionCapturedAtMs: baseTime + 120,
      seekTo: async () => null,
    });

    expect(activeRoot.latest().shownPositionMs).toBe(1_520);
  });

  test("clears pending seek when authoritative transport matches at the same capture timestamp", async () => {
    let resolveSeek:
      | ((value: PlaybackModels.SessionSnapshot | null) => void)
      | null = null;
    const baseTime = Date.now();

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 1_500,
      positionCapturedAtMs: baseTime,
      seekTo: () =>
        new Promise<PlaybackModels.SessionSnapshot | null>((resolve) => {
          resolveSeek = resolve;
        }),
    });

    act(() => {
      activeRoot?.latest().onValueChange(3_200);
      activeRoot?.latest().onValueCommitted(3_200);
    });

    await act(async () => {
      resolveSeek?.(
        PlaybackModels.SessionSnapshot.createFrom({
          status: PlaybackModels.Status.StatusPaused,
          positionMs: 3_200,
          positionCapturedAtMs: baseTime + 100,
        }),
      );
      await Promise.resolve();
    });

    activeRoot.rerender({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 3_200,
      positionCapturedAtMs: baseTime + 100,
      seekTo: async () => null,
    });

    expect(activeRoot.latest().shownPositionMs).toBe(3_200);
  });

  test("keeps a matched seek visible until transport confirms it", async () => {
    let resolveSeek:
      | ((value: PlaybackModels.SessionSnapshot | null) => void)
      | null = null;
    const baseTime = Date.now();

    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 200_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 55_000,
      positionCapturedAtMs: baseTime,
      seekTo: () =>
        new Promise<PlaybackModels.SessionSnapshot | null>((resolve) => {
          resolveSeek = resolve;
        }),
    });

    act(() => {
      activeRoot?.latest().onValueChange(49_000);
      activeRoot?.latest().onValueCommitted(49_000, { reason: "drag" });
    });

    await act(async () => {
      resolveSeek?.(
        PlaybackModels.SessionSnapshot.createFrom({
          status: PlaybackModels.Status.StatusPaused,
          positionMs: 49_000,
          positionCapturedAtMs: baseTime + 100,
        }),
      );
      await Promise.resolve();
    });

    activeRoot.rerender({
      currentEntryId: "entry-a",
      durationMs: 200_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 49_100,
      positionCapturedAtMs: baseTime + 5_100,
      seekTo: async () => null,
    });

    expect(activeRoot.latest().shownPositionMs).toBe(49_100);
  });

  test("clears local drag state when playback switches entries", () => {
    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 400,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    act(() => {
      activeRoot?.latest().onValueChange(700);
    });

    expect(activeRoot.latest().shownPositionMs).toBe(700);

    activeRoot.rerender({
      currentEntryId: "entry-b",
      durationMs: 10_000,
      hasCurrent: true,
      isPlaying: false,
      positionMs: 200,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    expect(activeRoot.latest().isDragging).toBe(false);
    expect(activeRoot.latest().shownPositionMs).toBe(200);
  });

  test("clamps playback to the track duration", () => {
    activeRoot = renderUsePlayerProgress({
      currentEntryId: "entry-a",
      durationMs: 2_000,
      hasCurrent: true,
      isPlaying: true,
      positionMs: 1_900,
      positionCapturedAtMs: Date.now(),
      seekTo: async () => null,
    });

    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(activeRoot.latest().shownPositionMs).toBe(2_000);
  });
});
