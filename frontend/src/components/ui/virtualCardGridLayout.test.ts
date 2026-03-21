import { describe, expect, test } from "vitest";
import {
  DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
  resolveVirtualCardGridColumnWidth,
  resolveVirtualCardGridColumns,
} from "./virtualCardGridLayout";

describe("resolveVirtualCardGridColumns", () => {
  test("treats the configured width as a target and keeps filling the grid", () => {
    expect(
      resolveVirtualCardGridColumns({
        breakpoints: DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
        containerWidth: 639,
        gap: 32,
        minColumnWidth: 192,
      }),
    ).toBe(3);

    expect(
      resolveVirtualCardGridColumns({
        breakpoints: DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
        containerWidth: 864,
        gap: 32,
        minColumnWidth: 192,
      }),
    ).toBe(4);

    expect(
      resolveVirtualCardGridColumns({
        breakpoints: DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
        containerWidth: 1088,
        gap: 32,
        minColumnWidth: 192,
      }),
    ).toBe(5);

    expect(
      resolveVirtualCardGridColumns({
        breakpoints: DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
        containerWidth: 1304,
        gap: 32,
        minColumnWidth: 192,
      }),
    ).toBe(6);

    expect(
      resolveVirtualCardGridColumns({
        breakpoints: DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
        containerWidth: 1800,
        gap: 32,
        minColumnWidth: 192,
      }),
    ).toBe(8);
  });

  test("still honors custom breakpoint floors", () => {
    expect(
      resolveVirtualCardGridColumns({
        breakpoints: [
          { minWidth: 0, columns: 2 },
          { minWidth: 900, columns: 5 },
        ],
        containerWidth: 950,
        gap: 32,
        minColumnWidth: 320,
      }),
    ).toBe(5);
  });

  test("falls back to auto-fit sizing when no breakpoints are provided", () => {
    expect(
      resolveVirtualCardGridColumns({
        breakpoints: [],
        containerWidth: 1000,
        gap: 32,
        minColumnWidth: 192,
      }),
    ).toBe(4);
  });
});

describe("resolveVirtualCardGridColumnWidth", () => {
  test("derives the real tile width from the resolved column count", () => {
    expect(
      resolveVirtualCardGridColumnWidth({
        containerWidth: 1304,
        columnCount: 6,
        gap: 32,
        fallbackWidth: 192,
      }),
    ).toBeCloseTo(190.67, 1);
  });
});
