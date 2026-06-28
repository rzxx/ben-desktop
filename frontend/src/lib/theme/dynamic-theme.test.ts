import { beforeEach, describe, expect, it } from "vitest";
import { ArtworkClass } from "../../../bindings/ben/desktop/internal/dynamictheme/models";
import type { DynamicTheme } from "@/lib/api/models";
import {
  applyDynamicThemeVariables,
  semanticColorRoles,
} from "./dynamic-theme";

const color = (hex: string) => ({
  hex,
  r: 0,
  g: 0,
  b: 0,
  population: 0,
  lightness: 0,
  chroma: 0,
  hue: 0,
});

function scheme(prefix: string): DynamicTheme["light"] {
  return {
    surface: {
      canvas: color(`${prefix}01`),
      recessed: color(`${prefix}02`),
      default: color(`${prefix}03`),
      raised: color(`${prefix}04`),
      overlay: color(`${prefix}05`),
    },
    content: {
      primary: color(`${prefix}06`),
      secondary: color(`${prefix}07`),
      disabled: color(`${prefix}08`),
      inverse: color(`${prefix}09`),
    },
    border: {
      subtle: color(`${prefix}0A`),
      default: color(`${prefix}0B`),
      strong: color(`${prefix}0C`),
      focus: color(`${prefix}0D`),
    },
    accent: {
      default: color(`${prefix}0E`),
      hover: color(`${prefix}0F`),
      pressed: color(`${prefix}10`),
      subtle: color(`${prefix}11`),
      onAccent: color(`${prefix}12`),
    },
    status: {
      danger: {
        default: color(`${prefix}13`),
        subtle: color(`${prefix}14`),
        on: color(`${prefix}15`),
      },
      warning: {
        default: color(`${prefix}16`),
        subtle: color(`${prefix}17`),
        on: color(`${prefix}18`),
      },
      success: {
        default: color(`${prefix}19`),
        subtle: color(`${prefix}1A`),
        on: color(`${prefix}1B`),
      },
    },
  };
}

function theme(): DynamicTheme {
  return {
    version: 1,
    artwork: {
      class: ArtworkClass.ArtworkClassMulticolor,
      atmosphere: color("#111111"),
      candidates: [],
    },
    light: scheme("#11"),
    dark: scheme("#22"),
    compatibility: {
      themeScale: [{ tone: 50, color: color("#EEEEEE") }],
      accentScale: [{ tone: 500, color: color("#ABCDEF") }],
    },
  };
}

describe("dynamic theme variables", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.artworkThemeClass;
  });

  it("publishes semantic roles from the selected scheme", () => {
    applyDynamicThemeVariables(theme(), "dark");
    expect(
      document.documentElement.style.getPropertyValue("--color-surface-canvas"),
    ).toBe("#2201");
    expect(
      document.documentElement.style.getPropertyValue("--color-accent"),
    ).toBe("#220E");
    expect(document.documentElement.dataset.artworkThemeClass).toBe(
      "multicolor",
    );
  });

  it("keeps the numeric palette isolated as a compatibility bridge", () => {
    applyDynamicThemeVariables(theme(), "light");
    expect(
      document.documentElement.style.getPropertyValue("--color-theme-50"),
    ).toBe("#EEEEEE");
    expect(
      document.documentElement.style.getPropertyValue("--color-accent-500"),
    ).toBe("#ABCDEF");
    expect(
      document.documentElement.style.getPropertyValue("--color-theme-100"),
    ).toBe("");
  });

  it("clears every dynamic role on fallback", () => {
    applyDynamicThemeVariables(theme(), "light");
    applyDynamicThemeVariables(null, "light");
    for (const role of semanticColorRoles) {
      expect(
        document.documentElement.style.getPropertyValue(`--color-${role}`),
      ).toBe("");
    }
    expect(document.documentElement.dataset.artworkThemeClass).toBeUndefined();
  });
});
