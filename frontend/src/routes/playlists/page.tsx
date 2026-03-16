import type { PlaylistListItem } from "@/lib/api/models";
import { MetricPill } from "@/components/catalog/MetricPill";
import { PlaylistRow } from "@/components/catalog/PlaylistRow";
import { PlaylistEmptyState } from "@/components/catalog/EmptyState";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { formatCount } from "@/lib/format";
import { getIdQuery, useCatalogStore } from "@/stores/catalog/store";
import { selectEntityQuery } from "@/stores/catalog/query-state";

export function PlaylistsPage() {
  const query = useStoreInfiniteQuery<PlaylistListItem>(
    (state) =>
      selectEntityQuery<PlaylistListItem>(
        state,
        "playlists",
        (current, id) => current.playlistsById[id],
      ),
    {
      fetchNextPage: () => {
        const record = getIdQuery(useCatalogStore.getState(), "playlists");
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensurePlaylistsPage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchPlaylists(),
    },
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <SectionHeading
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "playlist",
            )}
          />
        }
        title="Playlists"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          className="min-h-0 flex-1"
          emptyState={
            <PlaylistEmptyState body="Playlist records will appear here once the library contains playlists." />
          }
          estimateSize={98}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.isLoading}
          loadingMore={query.isRefreshing}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          renderRow={(playlist) => <PlaylistRow playlist={playlist} />}
          viewportClassName="pr-2"
        />
      </div>
    </div>
  );
}
