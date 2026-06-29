import * as ThemeFacade from "../../../bindings/ben/desktop/themefacade";
import { Types, type DynamicTheme } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export function generateRecordingTheme(
  recordingId: string,
): Promise<DynamicTheme> {
  return traceWailsCall(
    "theme",
    "generate_recording_theme",
    { recordingId },
    () => ThemeFacade.GenerateRecordingTheme(recordingId),
  );
}

export function subscribeThemeEvents() {
  return ThemeFacade.SubscribeThemeEvents();
}

export function getThemePreferences() {
  return traceWailsCall("theme", "get_theme_preferences", undefined, () =>
    ThemeFacade.GetThemePreferences(),
  );
}

export function setThemeMode(mode: string) {
  return traceWailsCall("theme", "set_theme_mode", { mode }, () =>
    ThemeFacade.SetThemeMode(mode as Types.AppThemeMode),
  );
}
