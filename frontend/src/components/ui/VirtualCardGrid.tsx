import { type ReactNode, useEffect, useRef, useState } from "react";
import { useElementScrollRestoration } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useNearEndScroll } from "@/hooks/app/useNearEndScroll";

type VirtualCardGridProps<T> = {
  items: T[];
  loading: boolean;
  loadingMore?: boolean;
  hasMore?: boolean;
  onEndReached?: () => void;
  minColumnWidth: number;
  rowHeight: number;
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
  rowHeight,
  getItemKey,
  renderCard,
  emptyState,
  className = "",
  viewportClassName = "",
  scrollRestorationId,
}: VirtualCardGridProps<T>) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const [viewportWidth, setViewportWidth] = useState(0);
  const gap = 32;
  const scrollEntry = useElementScrollRestoration(
    scrollRestorationId
      ? { id: scrollRestorationId }
      : { getElement: () => parentRef.current },
  );

  useEffect(() => {
    const syncViewportWidth = () => {
      setViewportWidth(window.innerWidth);
    };

    syncViewportWidth();
    window.addEventListener("resize", syncViewportWidth);

    return () => {
      window.removeEventListener("resize", syncViewportWidth);
    };
  }, []);

  let columnCount = 2;
  if (viewportWidth >= 1536) {
    columnCount = 6;
  } else if (viewportWidth >= 1280) {
    columnCount = 4;
  } else if (viewportWidth >= 768) {
    columnCount = 3;
  }

  void minColumnWidth;
  const rowCount = Math.ceil(items.length / columnCount);
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => parentRef.current,
    initialOffset: scrollEntry?.scrollY,
    estimateSize: () => rowHeight,
    overscan: 4,
  });

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
                  key={virtualRow.key}
                  style={{
                    gap: `${gap}px`,
                    gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
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
