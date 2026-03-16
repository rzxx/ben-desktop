import { getRouteApi } from "@tanstack/react-router";
import type { AlbumListItem } from "@/lib/api/models";
import { formatCount } from "@/lib/format";
import { VirtualCardGrid } from "@/components/ui/VirtualCardGrid";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "@/hooks/catalog/useCatalogQuery";
import { AlbumCard } from "@/components/catalog/Cards";
import { AlbumsEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const artistDetailRouteApi = getRouteApi("/artists_/$artistId");

export function ArtistDetailPage() {
  const { artistId } = artistDetailRouteApi.useParams();
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
    <div className="flex h-full min-h-0 flex-col gap-4">
      <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
        <div className="flex flex-col gap-5 xl:flex-row xl:items-end">
          <div className="flex h-36 w-36 items-center justify-center rounded-full border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.35),transparent_60%),rgba(255,255,255,0.05)] text-6xl font-semibold text-white/85">
            {detail.data?.Name?.slice(0, 1).toUpperCase() || "A"}
          </div>
          <div>
            <p className="text-[0.68rem] tracking-[0.35em] text-white/35 uppercase">
              Artist detail
            </p>
            <h1 className="mt-3 text-4xl font-semibold text-white">
              {detail.data?.Name ?? "Artist"}
            </h1>
            <div className="mt-4 flex flex-wrap gap-2">
              <MetricPill
                label={formatCount(detail.data?.AlbumCount ?? 0, "album")}
              />
              <MetricPill
                label={formatCount(detail.data?.TrackCount ?? 0, "track")}
              />
            </div>
            {detail.error && (
              <p className="mt-4 text-sm text-amber-300">{detail.error}</p>
            )}
          </div>
        </div>
      </section>
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
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
          renderCard={(album) => <AlbumCard album={album} />}
          rowHeight={320}
        />
      </div>
    </div>
  );
}


