import type { RecordingListItem } from "@/lib/api/models";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TrackListRow } from "@/components/catalog/TrackListRow";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { formatCount, joinArtists } from "@/lib/format";
import { getValueQuery, useCatalogStore } from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
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
      <SectionHeading
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
          className="min-h-0 flex-1"
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
            <TrackListRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              mode="list"
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
          viewportClassName="pr-2"
        />
      </div>
    </div>
  );
}
