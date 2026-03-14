import type { ArtistListItem } from "../../shared/lib/desktop";
import { formatCount } from "../../shared/lib/format";
import { VirtualCardGrid } from "../../shared/ui/VirtualCardGrid";
import { catalogLoaderClient } from "../../features/library/catalog-loader-client";
import {
  getIdQuery,
  useCatalogStore,
} from "../../features/library/catalog-store";
import { useStoreInfiniteQuery } from "../../features/library/use-store-query";
import { ArtistCard } from "../catalog/components/Cards";
import { ArtistsEmptyState } from "../catalog/components/EmptyState";
import { MetricPill } from "../catalog/components/MetricPill";
import { PageHeader } from "../catalog/components/SurfaceHeader";
import { selectEntityQuery } from "../catalog/query-state";

export function ArtistsPage() {
  const query = useStoreInfiniteQuery(
    (state) =>
      selectEntityQuery<ArtistListItem>(
        state,
        "artists",
        (current, id) => current.artistsById[id],
      ),
    {
      fetchNextPage: () => {
        const record = getIdQuery(useCatalogStore.getState(), "artists");
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureArtistsPage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchArtists(),
    },
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <PageHeader
        description="Artist directory with album and track counts. Open an artist to inspect their album catalog."
        eyebrow="Artists"
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "artist",
            )}
          />
        }
        title="Artists"
      />
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
          emptyState={
            <ArtistsEmptyState body="Artist entries will appear here once library metadata is available." />
          }
          getItemKey={(artist) => artist.ArtistID}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.isLoading}
          loadingMore={query.isRefreshing}
          minColumnWidth={220}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          renderCard={(artist) => <ArtistCard artist={artist} />}
          rowHeight={250}
        />
      </div>
    </div>
  );
}
