import type { DynamicTheme } from "@/lib/api/models";

const compatibilityTones = [
  50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950,
] as const;

export const semanticColorRoles = [
  "surface-canvas",
  "surface-recessed",
  "surface-default",
  "surface-raised",
  "surface-overlay",
  "content-primary",
  "content-secondary",
  "content-disabled",
  "content-inverse",
  "border-subtle",
  "border-default",
  "border-strong",
  "focus-ring",
  "accent",
  "accent-hover",
  "accent-pressed",
  "accent-subtle",
  "on-accent",
  "status-danger",
  "status-danger-subtle",
  "on-status-danger",
  "status-warning",
  "status-warning-subtle",
  "on-status-warning",
  "status-success",
  "status-success-subtle",
  "on-status-success",
] as const;

const transitioningRoles = semanticColorRoles.slice(0, 18);
if (typeof CSS !== "undefined" && "registerProperty" in CSS) {
  for (const role of transitioningRoles) {
    try {
      CSS.registerProperty({
        name: `--color-${role}`,
        syntax: "<color>",
        inherits: true,
        initialValue: "transparent",
      });
    } catch {
      // The property may already be registered during development hot reload.
    }
  }
}

type SemanticColorRole = (typeof semanticColorRoles)[number];
type ThemeScheme = DynamicTheme["light"];

export function applyDynamicThemeVariables(
  theme: DynamicTheme | null,
  mode: unknown = getDocumentMode(),
  root: HTMLElement = document.documentElement,
) {
  if (theme == null || theme.version !== 1) {
    clearVariables(root);
    return;
  }

  const scheme = mode === "dark" ? theme.dark : theme.light;
  for (const [role, hex] of semanticEntries(scheme)) {
    setOrRemove(root, `--color-${role}`, hex);
  }

  applyCompatibilityScale(root, "theme", theme.compatibility?.themeScale ?? []);
  applyCompatibilityScale(
    root,
    "accent",
    theme.compatibility?.accentScale ?? [],
  );
  root.dataset.artworkThemeClass = theme.artwork.class;
}

function semanticEntries(
  scheme: ThemeScheme,
): Array<[SemanticColorRole, string]> {
  return [
    ["surface-canvas", scheme.surface.canvas.hex],
    ["surface-recessed", scheme.surface.recessed.hex],
    ["surface-default", scheme.surface.default.hex],
    ["surface-raised", scheme.surface.raised.hex],
    ["surface-overlay", scheme.surface.overlay.hex],
    ["content-primary", scheme.content.primary.hex],
    ["content-secondary", scheme.content.secondary.hex],
    ["content-disabled", scheme.content.disabled.hex],
    ["content-inverse", scheme.content.inverse.hex],
    ["border-subtle", scheme.border.subtle.hex],
    ["border-default", scheme.border.default.hex],
    ["border-strong", scheme.border.strong.hex],
    ["focus-ring", scheme.border.focus.hex],
    ["accent", scheme.accent.default.hex],
    ["accent-hover", scheme.accent.hover.hex],
    ["accent-pressed", scheme.accent.pressed.hex],
    ["accent-subtle", scheme.accent.subtle.hex],
    ["on-accent", scheme.accent.onAccent.hex],
    ["status-danger", scheme.status.danger.default.hex],
    ["status-danger-subtle", scheme.status.danger.subtle.hex],
    ["on-status-danger", scheme.status.danger.on.hex],
    ["status-warning", scheme.status.warning.default.hex],
    ["status-warning-subtle", scheme.status.warning.subtle.hex],
    ["on-status-warning", scheme.status.warning.on.hex],
    ["status-success", scheme.status.success.default.hex],
    ["status-success-subtle", scheme.status.success.subtle.hex],
    ["on-status-success", scheme.status.success.on.hex],
  ];
}

function applyCompatibilityScale(
  root: HTMLElement,
  scale: "theme" | "accent",
  tones: DynamicTheme["compatibility"]["themeScale"],
) {
  const toneByValue = new Map(
    tones.map((tone) => [tone.tone, tone.color.hex.trim()]),
  );
  for (const tone of compatibilityTones) {
    setOrRemove(root, `--color-${scale}-${tone}`, toneByValue.get(tone));
  }
}

function clearVariables(root: HTMLElement) {
  for (const role of semanticColorRoles) {
    root.style.removeProperty(`--color-${role}`);
  }
  for (const scale of ["theme", "accent"] as const) {
    for (const tone of compatibilityTones) {
      root.style.removeProperty(`--color-${scale}-${tone}`);
    }
  }
  delete root.dataset.artworkThemeClass;
}

function setOrRemove(
  root: HTMLElement,
  variable: string,
  value: string | undefined,
) {
  const normalized = value?.trim();
  if (normalized) root.style.setProperty(variable, normalized);
  else root.style.removeProperty(variable);
}

function getDocumentMode(): "light" | "dark" {
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}
