import { useEffect, useRef } from "react";

type Options = {
  enabled: boolean;
  onNearEnd?: () => void;
  thresholdPx?: number;
};

export function useNearEndScroll(
  containerRef: React.RefObject<HTMLElement | null>,
  { enabled, onNearEnd, thresholdPx = 640 }: Options,
) {
  const hasScrolledRef = useRef(false);

  useEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }

    let animationFrame = 0;

    const checkPosition = () => {
      if (!enabled || !hasScrolledRef.current) {
        return;
      }

      if (element.scrollHeight <= element.clientHeight) {
        return;
      }

      const nearEnd =
        element.scrollTop + element.clientHeight >=
        element.scrollHeight - thresholdPx;

      if (nearEnd) {
        onNearEnd?.();
      }
    };

    const handleScroll = () => {
      hasScrolledRef.current = true;
      if (animationFrame) {
        window.cancelAnimationFrame(animationFrame);
      }
      animationFrame = window.requestAnimationFrame(checkPosition);
    };

    element.addEventListener("scroll", handleScroll, { passive: true });

    return () => {
      element.removeEventListener("scroll", handleScroll);
      if (animationFrame) {
        window.cancelAnimationFrame(animationFrame);
      }
    };
  }, [containerRef, enabled, onNearEnd, thresholdPx]);

  useEffect(() => {
    const element = containerRef.current;
    if (!element || !enabled || !hasScrolledRef.current) {
      return;
    }

    if (element.scrollHeight <= element.clientHeight) {
      return;
    }

    const nearEnd =
      element.scrollTop + element.clientHeight >=
      element.scrollHeight - thresholdPx;

    if (nearEnd) {
      onNearEnd?.();
    }
  }, [containerRef, enabled, onNearEnd, thresholdPx]);
}
