import { getRouteApi } from "@tanstack/react-router";
import type { AlbumListItem } from "@/lib/api/models";
import { AlbumGridTile } from "@/components/catalog/AlbumGridTile";
import { AlbumsEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { aggregateAvailabilityLabel, formatCount } from "@/lib/format";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const artistDetailRouteApi = getRouteApi("/artists_/$artistId");

export function ArtistDetailPage() {
  const { artistId } = artistDetailRouteApi.useParams();
  const albumAvailabilityByAlbumId = useCatalogStore(
    (state) => state.albumAvailabilityByAlbumId,
  );
  const detail = useStoreQuery(
    (state) => selectDetail(getDetailRecord(state.artistDetails, artistId)),
    () => catalogLoaderClient.refetchArtist(artistId),
  );
  const albumQuery = useStoreInfiniteQuery<AlbumListItem>(
    (state) =>
      selectValueQuery<AlbumListItem>(state, `artistAlbums:${artistId}`),
    {
      fetchNextPage: () => {
        const record = getValueQuery<AlbumListItem>(
          useCatalogStore.getState(),
          `artistAlbums:${artistId}`,
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureArtistAlbumsPage(
            artistId,
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () =>
        catalogLoaderClient.ensureArtistAlbumsPage(artistId, 0, {
          force: true,
        }),
    },
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-5">
      <section className="flex flex-wrap items-end gap-5">
        <div className="text-theme-100 flex h-28 w-28 items-center justify-center rounded-full border border-white/10 bg-white/[0.06] text-4xl font-semibold">
          {detail.data?.Name?.slice(0, 1).toUpperCase() || "A"}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <SectionHeading
            meta={
              <>
                <MetricPill
                  label={formatCount(detail.data?.AlbumCount ?? 0, "album")}
                />
                <MetricPill
                  label={formatCount(detail.data?.TrackCount ?? 0, "track")}
                />
              </>
            }
            title={detail.data?.Name ?? "Artist"}
          />
          {detail.error ? (
            <p className="text-sm text-amber-300">{detail.error}</p>
          ) : null}
        </div>
      </section>

      <div className="min-h-0 flex-1">
        <VirtualCardGrid
          className="min-h-0 flex-1"
          emptyState={
            <AlbumsEmptyState body="Artist albums will appear here when the artist has catalog entries." />
          }
          getItemKey={(album) => album.AlbumID}
          hasMore={albumQuery.hasMore}
          items={albumQuery.items}
          loading={albumQuery.isLoading}
          loadingMore={albumQuery.isRefreshing}
          minColumnWidth={210}
          onEndReached={() => {
            void albumQuery.fetchNextPage();
          }}
          renderCard={(album) => (
            <AlbumGridTile
              album={album}
              availabilityLabel={aggregateAvailabilityLabel(
                albumAvailabilityByAlbumId[album.AlbumID]?.data,
              )}
            />
          )}
          rowHeight={298}
          viewportClassName="px-1 py-3"
        />
      </div>
    </div>
  );
}
