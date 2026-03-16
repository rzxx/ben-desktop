import type { AlbumListItem } from "@/lib/api/models";
import { formatCount } from "@/lib/format";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { getIdQuery, useCatalogStore } from "@/stores/catalog/store";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { AlbumCard } from "@/components/catalog/Cards";
import { AlbumsEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { PageHeader } from "@/components/catalog/SurfaceHeader";
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
      <PageHeader
        description="Default view. Browse the library by release, then jump into album detail pages and playback."
        eyebrow="Albums"
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "album",
            )}
          />
        }
        title="Albums"
      />
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
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
          renderCard={(album) => <AlbumCard album={album} />}
          rowHeight={320}
        />
      </div>
    </div>
  );
}
