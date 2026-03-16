import { type ReactNode, useEffect, useRef, useState } from "react";
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
}: VirtualCardGridProps<T>) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(0);
  const gap = 18;

  useEffect(() => {
    const element = parentRef.current;
    if (!element) {
      return;
    }
    const observer = new ResizeObserver(([entry]) => {
      setWidth(entry?.contentRect.width ?? 0);
    });
    observer.observe(element);
    setWidth(element.clientWidth);
    return () => observer.disconnect();
  }, []);

  const columnCount = Math.max(
    1,
    Math.floor(
      (Math.max(width, minColumnWidth) + gap) / (minColumnWidth + gap),
    ),
  );
  const rowCount = Math.ceil(items.length / columnCount);
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => parentRef.current,
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
    <div className="relative h-full min-h-0 overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950">
      <div className="h-full overflow-y-auto px-4 py-4" ref={parentRef}>
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
        {(loading || loadingMore) && (
          <div className="px-2 py-4 text-sm text-zinc-400">Loading...</div>
        )}
      </div>
    </div>
  );
}

