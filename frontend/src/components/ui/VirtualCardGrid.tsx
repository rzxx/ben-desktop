import { type ReactNode, useEffect, useRef, useState } from "react";
import { useElementScrollRestoration } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useNearEndScroll } from "@/hooks/app/useNearEndScroll";
import {
  DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
  type VirtualCardGridBreakpoint,
  resolveVirtualCardGridColumnWidth,
  resolveVirtualCardGridColumns,
} from "./virtualCardGridLayout";

type VirtualCardGridProps<T> = {
  items: T[];
  loading: boolean;
  loadingMore?: boolean;
  hasMore?: boolean;
  onEndReached?: () => void;
  minColumnWidth: number;
  estimateCardHeight: (columnWidth: number) => number;
  gap?: number;
  breakpoints?: readonly VirtualCardGridBreakpoint[];
  getItemKey: (item: T, index: number) => string;
  renderCard: (item: T, index: number) => ReactNode;
  emptyState?: ReactNode;
  className?: string;
  viewportClassName?: string;
  scrollRestorationId?: string;
};

export function VirtualCardGrid<T>({
  items,
  loading,
  loadingMore = false,
  hasMore = false,
  onEndReached,
  minColumnWidth,
  estimateCardHeight,
  gap = 32,
  breakpoints = DEFAULT_VIRTUAL_CARD_GRID_BREAKPOINTS,
  getItemKey,
  renderCard,
  emptyState,
  className = "",
  viewportClassName = "",
  scrollRestorationId,
}: VirtualCardGridProps<T>) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const layoutRef = useRef<HTMLDivElement | null>(null);
  const [layoutWidth, setLayoutWidth] = useState(0);
  const scrollEntry = useElementScrollRestoration(
    scrollRestorationId
      ? { id: scrollRestorationId }
      : { getElement: () => parentRef.current },
  );

  useEffect(() => {
    const layout = layoutRef.current;
    if (!layout) {
      return;
    }

    const syncLayoutWidth = (width: number) => {
      setLayoutWidth(Math.round(width));
    };

    syncLayoutWidth(layout.getBoundingClientRect().width);

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      syncLayoutWidth(
        entry?.contentRect.width ?? layout.getBoundingClientRect().width,
      );
    });

    observer.observe(layout);

    return () => {
      observer.disconnect();
    };
  }, []);

  const columnCount = resolveVirtualCardGridColumns({
    breakpoints,
    containerWidth: layoutWidth,
    gap,
    minColumnWidth,
  });
  const columnWidth = resolveVirtualCardGridColumnWidth({
    containerWidth: layoutWidth,
    columnCount,
    gap,
    fallbackWidth: minColumnWidth,
  });
  const rowHeight = Math.max(0, estimateCardHeight(columnWidth));
  const rowCount = Math.ceil(items.length / columnCount);
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => parentRef.current,
    initialOffset: scrollEntry?.scrollY,
    estimateSize: () => rowHeight,
    gap,
    overscan: 4,
  });

  useEffect(() => {
    if (layoutWidth <= 0) {
      return;
    }

    rowVirtualizer.measure();
  }, [columnCount, gap, layoutWidth, rowHeight, rowVirtualizer]);

  useNearEndScroll(parentRef, {
    enabled: Boolean(hasMore && !loading && !loadingMore),
    onNearEnd: onEndReached,
  });

  if (!loading && items.length === 0) {
    return (
      <div className="flex h-full items-center justify-center">
        {emptyState}
      </div>
    );
  }

  return (
    <div
      className={["relative h-full min-h-0 overflow-hidden", className].join(
        " ",
      )}
    >
      <div
        className="ben-scrollbar h-full overflow-y-auto"
        data-scroll-restoration-id={scrollRestorationId}
        ref={parentRef}
        style={{
          scrollPaddingBottom: "var(--shell-player-clearance, 0px)",
        }}
      >
        <div className={["px-1 py-3", viewportClassName].join(" ")}>
          <div
            className="relative w-full"
            ref={layoutRef}
            style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
          >
            {rowVirtualizer.getVirtualItems().map((virtualRow) => {
              const rowItems = items.slice(
                virtualRow.index * columnCount,
                virtualRow.index * columnCount + columnCount,
              );

              return (
                <div
                  className="absolute top-0 left-0 grid w-full"
                  data-index={virtualRow.index}
                  key={virtualRow.key}
                  style={{
                    columnGap: `${gap}px`,
                    gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
                    height: `${rowHeight}px`,
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                >
                  {rowItems.map((item, index) => {
                    const itemIndex = virtualRow.index * columnCount + index;
                    return (
                      <div key={getItemKey(item, itemIndex)}>
                        {renderCard(item, itemIndex)}
                      </div>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </div>
        <div
          aria-hidden="true"
          style={{ height: "var(--shell-player-clearance, 0px)" }}
        />
        {(loading || loadingMore) && (
          <div className="text-theme-500 px-2 py-4 text-sm">Loading...</div>
        )}
      </div>
    </div>
  );
}
