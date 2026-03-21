import type { ArtistListItem } from "@/lib/api/models";
import { ArtistGridTile } from "@/components/catalog/ArtistGridTile";
import { ArtistsEmptyState } from "@/components/catalog/EmptyState";
import { estimateArtistGridTileHeight } from "@/components/catalog/gridTileEstimates";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { formatCount } from "@/lib/format";
import { getIdQuery, useCatalogStore } from "@/stores/catalog/store";
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
      <SectionHeading
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
          className="min-h-0 flex-1"
          emptyState={
            <ArtistsEmptyState body="Artist entries will appear here once library metadata is available." />
          }
          getItemKey={(artist) => artist.ArtistID}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.isLoading}
          loadingMore={query.isRefreshing}
          minColumnWidth={192}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          estimateCardHeight={estimateArtistGridTileHeight}
          renderCard={(artist) => <ArtistGridTile artist={artist} />}
          scrollRestorationId="artists-grid"
          viewportClassName="px-1 py-3"
        />
      </div>
    </div>
  );
}
