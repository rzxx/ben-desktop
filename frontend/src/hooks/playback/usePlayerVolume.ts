import { useEffect, useRef, useState } from "react";

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

const volumeThrottleMs = 75;

type UsePlayerVolumeInput = {
  setVolume: (volume: number) => Promise<void>;
  volume: number;
};

export function usePlayerVolume({ setVolume, volume }: UsePlayerVolumeInput) {
  const [volumeDraft, setVolumeDraft] = useState<number | null>(null);
  const [pendingVolume, setPendingVolume] = useState<number | null>(null);
  const volumeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestScheduledVolumeRef = useRef<number | null>(null);
  const lastVolumeSentAtRef = useRef(0);

  useEffect(() => {
    return () => {
      if (volumeTimerRef.current !== null) {
        clearTimeout(volumeTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    if (pendingVolume === null || volume !== pendingVolume) {
      return;
    }
    const frame = requestAnimationFrame(() => {
      setPendingVolume(null);
      setVolumeDraft(null);
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [pendingVolume, volume]);

  const sendVolume = (nextVolume: number) => {
    lastVolumeSentAtRef.current = Date.now();
    void setVolume(nextVolume).catch(() => {
      setPendingVolume((currentPendingVolume) =>
        currentPendingVolume === nextVolume ? null : currentPendingVolume,
      );
      setVolumeDraft(null);
    });
  };

  const scheduleVolume = (nextVolume: number, immediate: boolean) => {
    latestScheduledVolumeRef.current = nextVolume;

    if (volumeTimerRef.current !== null) {
      clearTimeout(volumeTimerRef.current);
      volumeTimerRef.current = null;
    }

    if (immediate) {
      sendVolume(nextVolume);
      return;
    }

    const now = Date.now();
    const remaining = Math.max(
      0,
      volumeThrottleMs - (now - lastVolumeSentAtRef.current),
    );

    if (remaining === 0) {
      sendVolume(nextVolume);
      return;
    }

    volumeTimerRef.current = setTimeout(() => {
      volumeTimerRef.current = null;
      if (latestScheduledVolumeRef.current != null) {
        sendVolume(latestScheduledVolumeRef.current);
      }
    }, remaining);
  };

  const commitVolume = (nextValue: number, immediate: boolean) => {
    const clampedVolume = clamp(Math.round(nextValue), 0, 100);
    setPendingVolume(clampedVolume);
    setVolumeDraft(clampedVolume);
    scheduleVolume(clampedVolume, immediate);
  };

  return {
    displayedVolume:
      pendingVolume !== null ? (volumeDraft ?? pendingVolume) : volume,
    onValueChange: (nextValue: number) => {
      commitVolume(nextValue, false);
    },
    onValueCommitted: (nextValue: number) => {
      commitVolume(nextValue, true);
    },
  };
}
