import type { AlbumListItem } from "@/lib/api/models";
import { AlbumGridTile } from "@/components/catalog/AlbumGridTile";
import { AlbumsEmptyState } from "@/components/catalog/EmptyState";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { getIdQuery, useCatalogStore } from "@/stores/catalog/store";
import { selectEntityQuery } from "@/stores/catalog/query-state";

export function AlbumsPage() {
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
          minColumnWidth={210}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          renderCard={(album) => <AlbumGridTile album={album} />}
          rowHeight={298}
          viewportClassName="px-1 py-3"
        />
      </div>
    </div>
  );
}
