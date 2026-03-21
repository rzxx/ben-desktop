export type VirtualCardGridBreakpoint = {
  minWidth: number;
  columns: number;
};

const DEFAULT_COLUMN_WIDTH_SLACK = 16;

export const DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS = [
  { minWidth: 0, columns: 2 },
  { minWidth: 640, columns: 3 },
  { minWidth: 864, columns: 4 },
  { minWidth: 1088, columns: 5 },
  { minWidth: 1312, columns: 6 },
] satisfies readonly VirtualCardGridBreakpoint[];

export function resolveVirtualCardGridColumns({
  breakpoints = DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
  containerWidth,
  gap,
  minColumnWidth,
  columnWidthSlack = DEFAULT_COLUMN_WIDTH_SLACK,
}: {
  breakpoints?: readonly VirtualCardGridBreakpoint[];
  containerWidth: number;
  gap: number;
  minColumnWidth: number;
  columnWidthSlack?: number;
}) {
  const responsiveColumns =
    breakpoints.length > 0
      ? [...breakpoints]
          .sort((left, right) => left.minWidth - right.minWidth)
          .reduce((resolved, breakpoint) => {
            if (containerWidth < breakpoint.minWidth) {
              return resolved;
            }

            return breakpoint.columns;
          }, breakpoints[0]!.columns)
      : 1;

  if (containerWidth <= 0 || minColumnWidth <= 0) {
    return responsiveColumns;
  }

  const softMinColumnWidth = Math.max(
    1,
    minColumnWidth - Math.max(0, columnWidthSlack),
  );
  const fittedColumns = Math.max(
    1,
    Math.floor((containerWidth + gap) / (softMinColumnWidth + gap)),
  );

  return Math.max(responsiveColumns, fittedColumns);
}

export function resolveVirtualCardGridColumnWidth({
  containerWidth,
  columnCount,
  gap,
  fallbackWidth,
}: {
  containerWidth: number;
  columnCount: number;
  gap: number;
  fallbackWidth: number;
}) {
  if (containerWidth <= 0 || columnCount <= 0) {
    return fallbackWidth;
  }

  return Math.max(0, (containerWidth - gap * (columnCount - 1)) / columnCount);
}
