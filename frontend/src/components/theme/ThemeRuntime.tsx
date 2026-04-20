import { useEffect, useLayoutEffect } from "react";
import { generateRecordingTheme } from "@/lib/api/theme";
import { applyThemeToDocument } from "@/lib/theme/bootstrap";
import { applyThemePaletteVariables } from "@/lib/theme/palette";
import { usePlaybackStore } from "@/stores/playback/store";
import { useThemeStore } from "@/stores/theme/store";

export function ThemeRuntime() {
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
  }, [
    themePreferences.effective,
    themePreferences.mode,
    themePreferences.system,
  ]);

  useEffect(() => {
    let cancelled = false;

    if (!themeRecordingId) {
      applyThemePaletteVariables(null);
      return () => {
        cancelled = true;
      };
    }

    void generateRecordingTheme(themeRecordingId)
      .then((palette) => {
        if (cancelled) {
          return;
        }
        applyThemePaletteVariables(palette);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        applyThemePaletteVariables(null);
      });

    return () => {
      cancelled = true;
    };
  }, [themeRecordingId]);

  return null;
}
