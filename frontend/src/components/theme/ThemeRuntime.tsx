import { useEffect } from "react";
import { generateRecordingTheme } from "@/lib/api/theme";
import { applyThemePaletteVariables } from "@/lib/theme/palette";
import { usePlaybackStore } from "@/stores/playback/store";

export function ThemeRuntime() {
  const themeRecordingId = usePlaybackStore((state) => {
    const item = state.snapshot?.currentItem ?? state.snapshot?.loadingItem ?? null;
    return item?.artworkRef?.trim() ?? "";
  });

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
