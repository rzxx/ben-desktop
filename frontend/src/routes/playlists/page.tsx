import type { PlaylistListItem } from "@/lib/api/models";
import { formatCount } from "@/lib/format";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  getIdQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { PlaylistCard } from "@/components/catalog/Cards";
import { PlaylistEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { PageHeader } from "@/components/catalog/SurfaceHeader";
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
      <PageHeader
        description="Playlists, including the reserved liked view, with direct navigation into each playlist detail screen."
        eyebrow="Playlists"
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
          renderRow={(playlist) => <PlaylistCard playlist={playlist} />}
        />
      </div>
    </div>
  );
}


