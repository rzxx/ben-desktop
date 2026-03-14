import { type ReactNode, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useNearEndScroll } from "./use-near-end-scroll";

type VirtualRowsProps<T> = {
  items: T[];
  estimateSize: number;
  overscan?: number;
  loading: boolean;
  loadingMore?: boolean;
  hasMore?: boolean;
  onEndReached?: () => void;
  emptyState?: ReactNode;
  renderRow: (item: T, index: number) => ReactNode;
};

export function VirtualRows<T>({
  items,
  estimateSize,
  overscan = 8,
  loading,
  loadingMore = false,
  hasMore = false,
  onEndReached,
  emptyState,
  renderRow,
}: VirtualRowsProps<T>) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimateSize,
    overscan,
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
    <div className="relative h-full min-h-0 overflow-hidden rounded-[1.6rem] border border-white/8 bg-black/10">
      <div className="h-full overflow-y-auto px-2" ref={parentRef}>
        <div
          className="relative w-full"
          style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
        >
          {rowVirtualizer.getVirtualItems().map((virtualRow) => (
            <div
              className="absolute top-0 left-0 w-full"
              key={virtualRow.key}
              style={{
                height: `${virtualRow.size}px`,
                transform: `translateY(${virtualRow.start}px)`,
              }}
            >
              {renderRow(items[virtualRow.index]!, virtualRow.index)}
            </div>
          ))}
        </div>
        {(loading || loadingMore) && (
          <div className="px-4 py-5 text-sm text-white/45">Loading…</div>
        )}
      </div>
    </div>
  );
}
