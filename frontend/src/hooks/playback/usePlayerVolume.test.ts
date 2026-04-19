import { act, createElement, useLayoutEffect, useRef } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { usePlayerVolume } from "./usePlayerVolume";

type HookInput = Parameters<typeof usePlayerVolume>[0];
type HookState = ReturnType<typeof usePlayerVolume>;

function HookHarness({
  hookStateRef,
  input,
}: {
  hookStateRef: { current: HookState | null };
  input: HookInput;
}) {
  const state = usePlayerVolume(input);
  const latestStateRef = useRef(state);

  useLayoutEffect(() => {
    latestStateRef.current = state;
    hookStateRef.current = latestStateRef.current;
  }, [hookStateRef, state]);

  return null;
}

function renderUsePlayerVolume(props: HookInput) {
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

describe("usePlayerVolume", () => {
  let activeRoot: ReturnType<typeof renderUsePlayerVolume> | null = null;

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

  test("sends the final committed volume immediately and cancels throttled intermediate updates", () => {
    const setVolume = vi.fn().mockResolvedValue(undefined);

    activeRoot = renderUsePlayerVolume({
      setVolume,
      volume: 25,
    });

    act(() => {
      activeRoot?.latest().onValueChange(30);
      activeRoot?.latest().onValueChange(35);
    });

    expect(setVolume).toHaveBeenCalledTimes(1);
    expect(setVolume).toHaveBeenNthCalledWith(1, 30);

    act(() => {
      activeRoot?.latest().onValueCommitted(40);
    });

    expect(setVolume).toHaveBeenCalledTimes(2);
    expect(setVolume).toHaveBeenNthCalledWith(2, 40);

    act(() => {
      vi.advanceTimersByTime(100);
    });

    expect(setVolume).toHaveBeenCalledTimes(2);
    expect(activeRoot.latest().displayedVolume).toBe(40);
  });

  test("clears pending state after the authoritative volume catches up", () => {
    const setVolume = vi.fn().mockResolvedValue(undefined);

    activeRoot = renderUsePlayerVolume({
      setVolume,
      volume: 25,
    });

    act(() => {
      activeRoot?.latest().onValueCommitted(60);
    });

    expect(activeRoot.latest().displayedVolume).toBe(60);

    activeRoot.rerender({
      setVolume,
      volume: 60,
    });

    act(() => {
      vi.advanceTimersByTime(20);
    });

    expect(activeRoot.latest().displayedVolume).toBe(60);

    act(() => {
      vi.advanceTimersByTime(20);
    });

    expect(activeRoot.latest().displayedVolume).toBe(60);
    expect(setVolume).toHaveBeenCalledTimes(1);
  });
});
