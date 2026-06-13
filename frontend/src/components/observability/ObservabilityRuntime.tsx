import { useEffect } from "react";
import { getObservabilityStatus } from "@/lib/api/observability";
import {
  installObservabilityWindow,
  isFrontendTracingActive,
  recordFrontendEvent,
} from "@/lib/observability/trace";

export function ObservabilityRuntime() {
  useEffect(() => {
    installObservabilityWindow();
    void getObservabilityStatus();
    const timer = window.setInterval(() => {
      void getObservabilityStatus();
    }, 10_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const onError = (event: ErrorEvent) => {
      recordFrontendEvent("frontend.unhandled_error", {
        message: event.message,
        filename: event.filename,
        lineno: event.lineno,
        colno: event.colno,
      });
    };
    const onRejection = (event: PromiseRejectionEvent) => {
      recordFrontendEvent("frontend.unhandled_rejection", {
        message:
          event.reason instanceof Error
            ? event.reason.message
            : String(event.reason),
      });
    };
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    return () => {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
    };
  }, []);

  useEffect(() => {
    if (!("PerformanceObserver" in window)) {
      return;
    }
    const observer = new PerformanceObserver((list) => {
      if (!isFrontendTracingActive()) {
        return;
      }
      for (const entry of list.getEntries()) {
        recordFrontendEvent(
          "frontend.performance_entry",
          {
            entryType: entry.entryType,
            name: entry.name,
            startTime: Math.round(entry.startTime),
            duration: Math.round(entry.duration),
          },
          "frontend",
        );
      }
    });
    try {
      observer.observe({ entryTypes: ["measure", "longtask"] });
    } catch {
      observer.observe({ entryTypes: ["measure"] });
    }
    return () => observer.disconnect();
  }, []);

  return null;
}
