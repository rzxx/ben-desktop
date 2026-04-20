const THEME_MODE_STORAGE_KEY = "ben.theme.mode";
const THEME_DARK_CLASS = "dark";
const SYSTEM_THEME_QUERY = "(prefers-color-scheme: dark)";

export type ThemeMode = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

export type InitialThemeState = {
  mode: ThemeMode;
  system: ResolvedTheme;
  effective: ResolvedTheme;
};

function normalizeThemeMode(value: unknown): ThemeMode {
  return value === "light" || value === "dark" || value === "system"
    ? value
    : "system";
}

function normalizeResolvedTheme(value: unknown): ResolvedTheme {
  return value === "dark" ? "dark" : "light";
}

export function getSystemTheme(): ResolvedTheme {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return "light";
  }
  return window.matchMedia(SYSTEM_THEME_QUERY).matches ? "dark" : "light";
}

export function getStoredThemeMode(): ThemeMode {
  if (typeof window === "undefined") {
    return "system";
  }
  try {
    return normalizeThemeMode(window.localStorage.getItem(THEME_MODE_STORAGE_KEY));
  } catch {
    return "system";
  }
}

export function resolveEffectiveTheme(
  mode: ThemeMode,
  systemTheme: ResolvedTheme,
): ResolvedTheme {
  return mode === "system" ? systemTheme : mode;
}

function getSeededThemeState(): InitialThemeState | null {
  if (typeof document === "undefined") {
    return null;
  }

  const root = document.documentElement;
  const mode = normalizeThemeMode(root.dataset.themeMode);
  const system = normalizeResolvedTheme(root.dataset.systemTheme);
  const effective = normalizeResolvedTheme(root.dataset.theme);

  if (
    root.dataset.themeMode == null &&
    root.dataset.systemTheme == null &&
    root.dataset.theme == null
  ) {
    return null;
  }

  return {
    mode,
    system,
    effective,
  };
}

export function getInitialThemeState(): InitialThemeState {
  const seededThemeState = getSeededThemeState();
  if (seededThemeState != null) {
    return seededThemeState;
  }

  const mode = getStoredThemeMode();
  const system = getSystemTheme();
  return {
    mode,
    system,
    effective: resolveEffectiveTheme(mode, system),
  };
}

export function persistThemeMode(mode: unknown) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(
      THEME_MODE_STORAGE_KEY,
      normalizeThemeMode(mode),
    );
  } catch {
    // Ignore storage failures and fall back to backend preferences.
  }
}

export function applyThemeToDocument(input: {
  mode?: unknown;
  system?: unknown;
  effective?: unknown;
}) {
  if (typeof document === "undefined") {
    return;
  }

  const mode = normalizeThemeMode(input.mode);
  const system = normalizeResolvedTheme(input.system);
  const effective = normalizeResolvedTheme(input.effective);
  const root = document.documentElement;

  root.classList.toggle(THEME_DARK_CLASS, effective === "dark");
  root.dataset.theme = effective;
  root.dataset.themeMode = mode;
  root.dataset.systemTheme = system;
  root.style.colorScheme = effective;
}
