import { Events } from "@wailsio/runtime";
import { create } from "zustand";
import type { ThemePreferences } from "@/lib/api/models";
import { Types } from "@/lib/api/models";
import {
  getThemePreferences,
  setThemeMode,
  subscribeThemeEvents,
} from "@/lib/api/theme";
import {
  getInitialThemeState,
  persistThemeMode,
  type InitialThemeState,
} from "@/lib/theme/bootstrap";

type ThemeStore = {
  started: boolean;
  preferences: ThemePreferences;
  stopListening?: () => void;
  bootstrap: () => Promise<void>;
  teardown: () => void;
  setMode: (mode: string) => Promise<void>;
};

function createThemePreferences(input: InitialThemeState): ThemePreferences {
  const mode =
    input.mode === "light"
      ? Types.AppThemeMode.AppThemeModeLight
      : input.mode === "dark"
        ? Types.AppThemeMode.AppThemeModeDark
        : Types.AppThemeMode.AppThemeModeSystem;
  const system =
    input.system === "dark"
      ? Types.ResolvedTheme.ResolvedThemeDark
      : Types.ResolvedTheme.ResolvedThemeLight;
  const effective =
    input.effective === "dark"
      ? Types.ResolvedTheme.ResolvedThemeDark
      : Types.ResolvedTheme.ResolvedThemeLight;

  return new Types.ThemePreferences({
    mode,
    system,
    effective,
  });
}

const defaultThemePreferences = createThemePreferences(getInitialThemeState());

export const useThemeStore = create<ThemeStore>((set, get) => ({
  started: false,
  preferences: defaultThemePreferences,
  stopListening: undefined,

  bootstrap: async () => {
    if (get().started) {
      return;
    }

    const [preferences, eventName] = await Promise.all([
      getThemePreferences().catch(() => defaultThemePreferences),
      subscribeThemeEvents(),
    ]);

    persistThemeMode(preferences.mode);

    const stopListening = Events.On(eventName, (event) => {
      const nextPreferences = Types.ThemePreferences.createFrom(event.data);
      persistThemeMode(nextPreferences.mode);
      set({
        preferences: nextPreferences,
      });
    });

    set({
      started: true,
      preferences,
      stopListening,
    });
  },

  teardown: () => {
    get().stopListening?.();
    set({
      started: false,
      stopListening: undefined,
    });
  },

  setMode: async (mode) => {
    const preferences = await setThemeMode(mode);
    persistThemeMode(preferences.mode);
    set({ preferences });
  },
}));
