import { useEffect, useRef, useState } from "react";
import type { SessionSnapshot } from "@/lib/api/models";
import {
  nextPlaybackDebugSeekRequestId,
  recordPlaybackDebugTrace,
  updatePlaybackDebugLiveState,
} from "@/lib/playback/debugTrace";

const pendingSeekMatchToleranceMs = 750;

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

type UsePlayerProgressInput = {
  currentEntryId: string;
  durationMs: number;
  hasCurrent: boolean;
  isPlaying: boolean;
  positionMs: number;
  positionCapturedAtMs: number;
  seekTo: (
    positionMs: number,
    expectedEntryId?: string,
    debugRequestId?: string,
  ) => Promise<SessionSnapshot | null>;
};

type TimelineState = {
  entryId: string;
  draftPositionMs: number | null;
  isDragging: boolean;
  pendingSeek: {
    baselinePositionMs: number;
    baselineCapturedAtMs: number;
    requestId: string;
    positionMs: number;
    positionCapturedAtMs: number;
    isPlaying: boolean;
  } | null;
};

type SliderCommitDetails = {
  reason?: string;
};

function hasAuthoritativeCaughtPendingSeek(
  positionMs: number,
  positionCapturedAtMs: number,
  pendingSeek: NonNullable<TimelineState["pendingSeek"]>,
) {
  return (
    positionCapturedAtMs > pendingSeek.baselineCapturedAtMs &&
    isWithinPendingSeekTolerance(positionMs, pendingSeek.positionMs)
  );
}

function hasAuthoritativeDivergedFromPendingSeek(
  positionMs: number,
  positionCapturedAtMs: number,
  pendingSeek: NonNullable<TimelineState["pendingSeek"]>,
) {
  if (positionCapturedAtMs <= pendingSeek.baselineCapturedAtMs) {
    return false;
  }
  if (isWithinPendingSeekTolerance(positionMs, pendingSeek.positionMs)) {
    return false;
  }
  if (pendingSeek.positionMs > pendingSeek.baselinePositionMs) {
    return (
      positionMs <=
      pendingSeek.baselinePositionMs - pendingSeekMatchToleranceMs
    );
  }
  if (pendingSeek.positionMs < pendingSeek.baselinePositionMs) {
    return (
      positionMs >=
      pendingSeek.baselinePositionMs + pendingSeekMatchToleranceMs
    );
  }
  return true;
}

function isWithinPendingSeekTolerance(
  positionMs: number,
  targetPositionMs: number,
) {
  return Math.abs(positionMs - targetPositionMs) <= pendingSeekMatchToleranceMs;
}

function deriveShownPositionMs(
  positionMs: number,
  positionCapturedAtMs: number,
  isPlaying: boolean,
  nowMs: number,
) {
  if (isPlaying && positionCapturedAtMs > 0) {
    return positionMs + Math.max(0, nowMs - positionCapturedAtMs);
  }
  return positionMs;
}

export function usePlayerProgress({
  currentEntryId,
  durationMs,
  hasCurrent,
  isPlaying,
  positionMs,
  positionCapturedAtMs,
  seekTo,
}: UsePlayerProgressInput) {
  const hasActiveEntry = hasCurrent && currentEntryId !== "";
  const maxDuration = Math.max(durationMs, 1);
  const [timelineState, setTimelineState] = useState<TimelineState>(() => ({
    entryId: currentEntryId,
    draftPositionMs: null as number | null,
    isDragging: false,
    pendingSeek: null,
  }));
  const dragStateRef = useRef({
    entryId: currentEntryId,
    draftPositionMs: null as number | null,
    isDragging: false,
  });
  const clearedPendingSeekRequestIdRef = useRef("");
  const [nowMs, setNowMs] = useState(() => Date.now());
  const hasMatchingDragState =
    hasActiveEntry && timelineState.entryId === currentEntryId;
  const draftPositionMs = hasMatchingDragState
    ? timelineState.draftPositionMs
    : null;
  const pendingSeek = hasMatchingDragState ? timelineState.pendingSeek : null;
  const isDragging = hasMatchingDragState ? timelineState.isDragging : false;
  const shouldDiscardPendingSeek =
    pendingSeek != null &&
    hasAuthoritativeDivergedFromPendingSeek(
      positionMs,
      positionCapturedAtMs,
      pendingSeek,
    );
  const shouldUsePendingSeek =
    pendingSeek != null &&
    !shouldDiscardPendingSeek &&
    !hasAuthoritativeCaughtPendingSeek(
      positionMs,
      positionCapturedAtMs,
      pendingSeek,
    );

  useEffect(() => {
    if (!hasMatchingDragState || pendingSeek == null || !shouldDiscardPendingSeek) {
      return;
    }
    if (clearedPendingSeekRequestIdRef.current === pendingSeek.requestId) {
      return;
    }
    clearedPendingSeekRequestIdRef.current = pendingSeek.requestId;
    // This effect syncs local pending-seek state with newer authoritative
    // transport updates; the extra render is intentional and bounded.
    setTimelineState((currentState) => {
      if (
        currentState.entryId !== currentEntryId ||
        currentState.pendingSeek?.requestId !== pendingSeek.requestId
      ) {
        return currentState;
      }
      return {
        ...currentState,
        pendingSeek: null,
      };
    });
    recordPlaybackDebugTrace({
      kind: "hook:pendingSeek:cleared",
      currentEntryId,
      seekRequestId: pendingSeek.requestId,
      positionMs,
      positionCapturedAtMs,
      message: "authoritative transport diverged from pending seek",
    });
  }, [
    currentEntryId,
    hasMatchingDragState,
    pendingSeek,
    positionCapturedAtMs,
    positionMs,
    shouldDiscardPendingSeek,
  ]);

  useEffect(() => {
    if (!hasActiveEntry || !isPlaying || isDragging) {
      return;
    }

    let frame = 0;
    const tick = () => {
      setNowMs(Date.now());
      frame = requestAnimationFrame(tick);
    };

    frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [hasActiveEntry, isDragging, isPlaying]);

  const onValueChange = (nextValue: number) => {
    const nextPositionMs = clamp(Math.round(nextValue), 0, maxDuration);
    dragStateRef.current = {
      entryId: currentEntryId,
      draftPositionMs: nextPositionMs,
      isDragging: true,
    };
    setTimelineState({
      entryId: currentEntryId,
      draftPositionMs: nextPositionMs,
      isDragging: true,
      pendingSeek: null,
    });
    recordPlaybackDebugTrace({
      kind: "slider:valueChange",
      currentEntryId,
      positionMs: nextPositionMs,
      draftPositionMs: nextPositionMs,
      shownPositionMs: nextPositionMs,
      isDragging: true,
    });
  };

  const onInteractionStart = () => {
    if (!hasActiveEntry) {
      return;
    }
    const interactionPositionMs = clamp(
      Math.round(
        draftPositionMs ??
          (shouldUsePendingSeek && pendingSeek != null
            ? deriveShownPositionMs(
                pendingSeek.positionMs,
                pendingSeek.positionCapturedAtMs,
                pendingSeek.isPlaying,
                nowMs,
              )
            : deriveShownPositionMs(
                positionMs,
                positionCapturedAtMs,
                isPlaying,
                nowMs,
              )),
      ),
      0,
      maxDuration,
    );
    setTimelineState((currentState) => {
      if (
        currentState.entryId === currentEntryId &&
        currentState.isDragging &&
        currentState.draftPositionMs === interactionPositionMs
      ) {
        return currentState;
      }
      dragStateRef.current = {
        entryId: currentEntryId,
        draftPositionMs: interactionPositionMs,
        isDragging: true,
      };
      return {
        entryId: currentEntryId,
        draftPositionMs: interactionPositionMs,
        isDragging: true,
        pendingSeek: null,
      };
    });
    recordPlaybackDebugTrace({
      kind: "slider:interactionStart",
      currentEntryId,
      positionMs: interactionPositionMs,
      draftPositionMs: interactionPositionMs,
      shownPositionMs: interactionPositionMs,
      isDragging: true,
    });
  };

  const onValueCommitted = (
    nextValue: number,
    details?: SliderCommitDetails,
  ) => {
    if (details?.reason === "input-change") {
      recordPlaybackDebugTrace({
        kind: "slider:valueCommitted",
        currentEntryId,
        positionMs: clamp(Math.round(nextValue), 0, maxDuration),
        message: "ignored input-change commit",
      });
      return;
    }
    const hasLiveDrag =
      dragStateRef.current.entryId === currentEntryId &&
      dragStateRef.current.isDragging &&
      dragStateRef.current.draftPositionMs != null;
    if (
      (details?.reason === "drag" || details?.reason === "track-press") &&
      !hasLiveDrag
    ) {
      recordPlaybackDebugTrace({
        kind: "slider:valueCommitted",
        currentEntryId,
        positionMs: clamp(Math.round(nextValue), 0, maxDuration),
        message: "ignored stale drag commit",
      });
      return;
    }

    const nextSeekMs = clamp(Math.round(nextValue), 0, maxDuration);
    const optimisticCapturedAtMs = Date.now();
    const requestId = nextPlaybackDebugSeekRequestId();

    dragStateRef.current = {
      entryId: currentEntryId,
      draftPositionMs: null,
      isDragging: false,
    };
    clearedPendingSeekRequestIdRef.current = "";
    setTimelineState({
      entryId: currentEntryId,
      draftPositionMs: null,
      isDragging: false,
      pendingSeek: {
        baselinePositionMs: positionMs,
        baselineCapturedAtMs: positionCapturedAtMs,
        requestId,
        positionMs: nextSeekMs,
        positionCapturedAtMs: optimisticCapturedAtMs,
        isPlaying,
      },
    });
    recordPlaybackDebugTrace({
      kind: "slider:valueCommitted",
      currentEntryId,
      seekRequestId: requestId,
      positionMs: nextSeekMs,
      pendingSeekMs: nextSeekMs,
      message: details?.reason,
    });
    recordPlaybackDebugTrace({
      kind: "hook:pendingSeek:set",
      currentEntryId,
      seekRequestId: requestId,
      pendingSeekMs: nextSeekMs,
      positionCapturedAtMs: optimisticCapturedAtMs,
    });

    void seekTo(nextSeekMs, currentEntryId, requestId)
      .then((snapshot) => {
        if (snapshot == null) {
          setTimelineState((currentState) =>
            currentState.entryId !== currentEntryId
              ? currentState
              : {
                  ...currentState,
                  pendingSeek: null,
                },
          );
          recordPlaybackDebugTrace({
            kind: "hook:pendingSeek:cleared",
            currentEntryId,
            seekRequestId: requestId,
            message: "seek returned null",
          });
          return;
        }

        const resolvedEntryId = snapshot.currentEntry?.entryId ?? currentEntryId;
        const resolvedPositionMs = snapshot.positionMs ?? 0;
        const resolvedMatchesRequestedSeek =
          resolvedEntryId === currentEntryId &&
          isWithinPendingSeekTolerance(resolvedPositionMs, nextSeekMs);
        if (!resolvedMatchesRequestedSeek) {
          setTimelineState((currentState) =>
            currentState.entryId !== currentEntryId
              ? currentState
              : {
                  ...currentState,
                  pendingSeek: null,
                },
          );
          recordPlaybackDebugTrace({
            kind: "hook:pendingSeek:cleared",
            currentEntryId,
            seekRequestId: requestId,
            message: `seek resolved without authoritative match position=${resolvedPositionMs}`,
          });
          return;
        }

        recordPlaybackDebugTrace({
          kind: "hook:pendingSeek:updated",
          currentEntryId,
          seekRequestId: requestId,
          pendingSeekMs: nextSeekMs,
          positionCapturedAtMs: optimisticCapturedAtMs,
          message: "awaiting authoritative transport event",
        });
      })
      .catch(() => {
        setTimelineState((currentState) =>
          currentState.entryId !== currentEntryId
            ? currentState
            : {
                ...currentState,
                pendingSeek: null,
              },
        );
        recordPlaybackDebugTrace({
          kind: "hook:pendingSeek:cleared",
          currentEntryId,
          seekRequestId: requestId,
          message: "seek rejected",
        });
      });
  };

  let shownPositionMs = 0;
  if (hasActiveEntry) {
    if (draftPositionMs !== null) {
      shownPositionMs = draftPositionMs;
    } else if (shouldUsePendingSeek && pendingSeek != null) {
      shownPositionMs = deriveShownPositionMs(
        pendingSeek.positionMs,
        pendingSeek.positionCapturedAtMs,
        pendingSeek.isPlaying,
        nowMs,
      );
    } else {
      shownPositionMs = deriveShownPositionMs(
        positionMs,
        positionCapturedAtMs,
        isPlaying,
        nowMs,
      );
    }
  }

  useEffect(() => {
    updatePlaybackDebugLiveState({
      currentEntryId,
      shownPositionMs: hasActiveEntry
        ? clamp(shownPositionMs, 0, maxDuration)
        : 0,
      draftPositionMs,
      pendingSeekMs:
        shouldUsePendingSeek && pendingSeek != null
          ? pendingSeek.positionMs
          : null,
      pendingSeekCapturedAtMs:
        shouldUsePendingSeek && pendingSeek != null
          ? pendingSeek.positionCapturedAtMs
          : null,
      pendingSeekRequestId:
        shouldUsePendingSeek && pendingSeek != null
          ? pendingSeek.requestId
          : "",
      isDragging,
    });
  }, [
    currentEntryId,
    draftPositionMs,
    hasActiveEntry,
    isDragging,
    maxDuration,
    pendingSeek,
    shouldUsePendingSeek,
    shownPositionMs,
  ]);

  return {
    shownPositionMs: hasActiveEntry
      ? clamp(shownPositionMs, 0, maxDuration)
      : 0,
    isDragging,
    onInteractionStart,
    onValueChange,
    onValueCommitted,
  };
}
