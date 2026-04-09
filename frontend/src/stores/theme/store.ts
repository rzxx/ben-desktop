import { Events } from "@wailsio/runtime";
import { create } from "zustand";
import type { ThemePreferences } from "@/lib/api/models";
import { Types } from "@/lib/api/models";
import {
  getThemePreferences,
  setThemeMode,
  subscribeThemeEvents,
} from "@/lib/api/theme";

type ThemeStore = {
  started: boolean;
  preferences: ThemePreferences;
  stopListening?: () => void;
  bootstrap: () => Promise<void>;
  teardown: () => void;
  setMode: (mode: string) => Promise<void>;
};

const defaultThemePreferences = new Types.ThemePreferences({
  mode: Types.AppThemeMode.AppThemeModeSystem,
  system: Types.ResolvedTheme.ResolvedThemeDark,
  effective: Types.ResolvedTheme.ResolvedThemeDark,
});

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

    const stopListening = Events.On(eventName, (event) => {
      set({
        preferences: Types.ThemePreferences.createFrom(event.data),
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
    set({ preferences });
  },
}));
