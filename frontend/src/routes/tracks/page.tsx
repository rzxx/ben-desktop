import type { RecordingListItem } from "@/lib/api";
import { formatCount, joinArtists } from "@/lib/format";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { usePlaybackStore } from "@/stores/playback/usePlaybackStore";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { PageHeader } from "@/components/catalog/SurfaceHeader";
import { TrackRow } from "@/components/catalog/TrackRow";
import { selectValueQuery } from "@/stores/catalog/query-state";

export function TracksPage() {
  const playRecording = usePlaybackStore((state) => state.playRecording);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const query = useStoreInfiniteQuery<RecordingListItem>(
    (state) => selectValueQuery<RecordingListItem>(state, "tracks"),
    {
      fetchNextPage: () => {
        const record = getValueQuery<RecordingListItem>(
          useCatalogStore.getState(),
          "tracks",
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureTracksPage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchTracks(),
    },
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <PageHeader
        description="Flat track browser with virtualized rows and direct play or queue actions."
        eyebrow="Tracks"
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "track",
            )}
          />
        }
        title="Tracks"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <TracksEmptyState body="Track rows appear here after the core runtime exposes recordings." />
          }
          estimateSize={72}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.isLoading}
          loadingMore={query.isRefreshing}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          renderRow={(track, index) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              onPlay={() => {
                void playRecording(track.RecordingID);
              }}
              onQueue={() => {
                void queueRecording(track.RecordingID);
              }}
              subtitle={joinArtists(track.Artists)}
              title={track.Title}
            />
          )}
        />
      </div>
    </div>
  );
}


