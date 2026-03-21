import { type ReactNode, useRef } from "react";
import { useElementScrollRestoration } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useNearEndScroll } from "@/hooks/app/useNearEndScroll";

type VirtualRowsProps<T> = {
  items: T[];
  estimateSize: number;
  gap?: number;
  overscan?: number;
  loading: boolean;
  loadingMore?: boolean;
  hasMore?: boolean;
  onEndReached?: () => void;
  emptyState?: ReactNode;
  renderRow: (item: T, index: number) => ReactNode;
  className?: string;
  viewportClassName?: string;
  scrollRestorationId?: string;
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
  className = "",
  viewportClassName = "",
  scrollRestorationId,
  gap = 0,
}: VirtualRowsProps<T>) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const scrollEntry = useElementScrollRestoration(
    scrollRestorationId
      ? { id: scrollRestorationId }
      : { getElement: () => parentRef.current },
  );
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    initialOffset: scrollEntry?.scrollY,
    estimateSize: () => estimateSize,
    gap: gap,
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
    <div
      className={["relative h-full min-h-0 overflow-hidden", className].join(
        " ",
      )}
    >
      <div
        className={[
          "ben-scrollbar h-full overflow-y-auto",
          viewportClassName,
        ].join(" ")}
        data-scroll-restoration-id={scrollRestorationId}
        ref={parentRef}
        style={{
          scrollPaddingBottom: "var(--shell-player-clearance, 0px)",
        }}
      >
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
        <div
          aria-hidden="true"
          style={{ height: "var(--shell-player-clearance, 0px)" }}
        />
        {(loading || loadingMore) && (
          <div className="text-theme-500 px-4 py-5 text-sm">Loading...</div>
        )}
      </div>
    </div>
  );
}
