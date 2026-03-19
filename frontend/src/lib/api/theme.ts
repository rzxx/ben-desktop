import * as ThemeFacade from "../../../bindings/ben/desktop/themefacade";
import type { ThemePalette } from "./models";

export function generateRecordingTheme(
  recordingId: string,
): Promise<ThemePalette> {
  return ThemeFacade.GenerateRecordingTheme(recordingId);
}
