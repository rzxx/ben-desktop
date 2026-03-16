import type { ThemePalette } from "@/lib/api/models";

const tailwindThemeTones = [
  50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950,
] as const;

type TailwindThemeScale = "theme" | "accent";

export function applyThemePaletteVariables(
  palette: ThemePalette | null,
  root: HTMLElement = document.documentElement,
) {
  applyPaletteScaleVariables(root, "theme", palette?.themeScale ?? []);
  applyPaletteScaleVariables(root, "accent", palette?.accentScale ?? []);
}

function applyPaletteScaleVariables(
  root: HTMLElement,
  scale: TailwindThemeScale,
  tones: ThemePalette["themeScale"],
) {
  const toneByValue = new Map<number, string>();
  for (const tone of tones) {
    const hex = tone.color?.hex?.trim();
    if (hex) {
      toneByValue.set(tone.tone, hex);
    }
  }

  for (const tone of tailwindThemeTones) {
    const cssVariable = `--color-${scale}-${tone}`;
    const hex = toneByValue.get(tone);
    if (hex) {
      root.style.setProperty(cssVariable, hex);
      continue;
    }

    root.style.removeProperty(cssVariable);
  }
}
