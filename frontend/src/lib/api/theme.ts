import * as ThemeFacade from "../../../bindings/ben/desktop/themefacade";
import { Types, type ThemePalette } from "./models";

export function generateRecordingTheme(
  recordingId: string,
): Promise<ThemePalette> {
  return ThemeFacade.GenerateRecordingTheme(recordingId);
}

export function subscribeThemeEvents() {
  return ThemeFacade.SubscribeThemeEvents();
}

export function getThemePreferences() {
  return ThemeFacade.GetThemePreferences();
}

export function setThemeMode(mode: string) {
  return ThemeFacade.SetThemeMode(mode as Types.AppThemeMode);
}
