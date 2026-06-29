import { useEffect, useLayoutEffect, useRef } from "react";
import type { DynamicTheme } from "@/lib/api/models";
import { generateRecordingTheme } from "@/lib/api/theme";
import { applyThemeToDocument } from "@/lib/theme/bootstrap";
import { applyDynamicThemeVariables } from "@/lib/theme/dynamic-theme";
import { usePlaybackStore } from "@/stores/playback/store";
import { useThemeStore } from "@/stores/theme/store";

export function ThemeRuntime() {
  const dynamicTheme = useRef<DynamicTheme | null>(null);
  const bootstrapTheme = useThemeStore((state) => state.bootstrap);
  const teardownTheme = useThemeStore((state) => state.teardown);
  const themePreferences = useThemeStore((state) => state.preferences);
  const themeRecordingId = usePlaybackStore((state) => {
    const item =
      state.transport?.currentEntry?.item ??
      state.transport?.loadingEntry?.item ??
      null;
    return item?.artworkRef?.trim() ?? "";
  });

  useEffect(() => {
    void bootstrapTheme();
    return () => {
      teardownTheme();
    };
  }, [bootstrapTheme, teardownTheme]);

  useLayoutEffect(() => {
    applyThemeToDocument({
      mode: themePreferences.mode,
      system: themePreferences.system,
      effective: themePreferences.effective,
    });
    applyDynamicThemeVariables(
      dynamicTheme.current,
      themePreferences.effective,
    );
  }, [
    themePreferences.effective,
    themePreferences.mode,
    themePreferences.system,
  ]);

  useEffect(() => {
    let cancelled = false;

    if (!themeRecordingId) {
      dynamicTheme.current = null;
      applyDynamicThemeVariables(null);
      return () => {
        cancelled = true;
      };
    }

    void generateRecordingTheme(themeRecordingId)
      .then((theme) => {
        if (cancelled) {
          return;
        }
        dynamicTheme.current = theme;
        applyDynamicThemeVariables(theme);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        dynamicTheme.current = null;
        applyDynamicThemeVariables(null);
      });

    return () => {
      cancelled = true;
    };
  }, [themeRecordingId]);

  return null;
}
