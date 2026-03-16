import type { ArtistListItem } from "@/lib/api";
import { formatCount } from "@/lib/format";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  getIdQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { ArtistCard } from "@/components/catalog/Cards";
import { ArtistsEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { PageHeader } from "@/components/catalog/SurfaceHeader";
import { selectEntityQuery } from "@/stores/catalog/query-state";

export function ArtistsPage() {
  const query = useStoreInfiniteQuery<ArtistListItem>(
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


