import type { AlbumListItem } from "@/lib/api/models";
import { AlbumGridTile } from "@/components/catalog/AlbumGridTile";
import { AlbumsEmptyState } from "@/components/catalog/EmptyState";
import { estimateAlbumGridTileHeight } from "@/components/catalog/gridTileEstimates";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import { pinSubjectKey, usePinStates } from "@/hooks/pins/usePinStates";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { Types } from "@/lib/api/models";
import { aggregateAvailabilityLabel } from "@/lib/format";
import { getIdQuery, useCatalogStore } from "@/stores/catalog/store";
import { selectEntityQuery } from "@/stores/catalog/query-state";

export function AlbumsPage() {
  const albumAvailabilityByAlbumId = useCatalogStore(
    (state) => state.albumAvailabilityByAlbumId,
  );
  const query = useStoreInfiniteQuery<AlbumListItem>(
    (state) =>
      selectEntityQuery<AlbumListItem>(
        state,
        "albums",
        (current, id) => current.albumsById[id],
      ),
    {
      fetchNextPage: () => {
        const record = getIdQuery(useCatalogStore.getState(), "albums");
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureAlbumsPage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchAlbums(),
    },
  );
  const albumPinStates = usePinStates(
    query.items.map(
      (album) =>
        new Types.PinSubjectRef({
          ID: album.AlbumID,
          Kind: Types.PinSubjectKind.PinSubjectAlbumVariant,
        }),
    ),
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <SectionHeading title="Albums" />
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
          className="min-h-0 flex-1"
          emptyState={
            <AlbumsEmptyState body="Albums will appear here when the core catalog has materialized media." />
          }
          getItemKey={(album) => album.AlbumID}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.isLoading}
          loadingMore={query.isRefreshing}
          minColumnWidth={192}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          estimateCardHeight={estimateAlbumGridTileHeight}
          renderCard={(album) => (
            <AlbumGridTile
              album={album}
              availabilityState={
                albumAvailabilityByAlbumId[album.AlbumID]?.data?.State
              }
              availabilityLabel={aggregateAvailabilityLabel(
                albumAvailabilityByAlbumId[album.AlbumID]?.data,
              )}
              pinState={
                albumPinStates[
                  pinSubjectKey({
                    ID: album.AlbumID,
                    Kind: Types.PinSubjectKind.PinSubjectAlbumVariant,
                  })
                ] ?? null
              }
              state={{ __benSource: "albums" }}
            />
          )}
          scrollRestorationId="albums-grid"
          viewportClassName="px-1 py-3"
        />
      </div>
    </div>
  );
}
