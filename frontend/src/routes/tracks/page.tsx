import type { RecordingListItem } from "@/lib/api/models";
import { ManagedTrackListRow } from "@/components/catalog/ManagedTrackListRow";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { Button } from "@/components/ui/Button";
import { pinSubjectKey, usePinStates } from "@/hooks/pins/usePinStates";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { Types } from "@/lib/api/models";
import {
  formatCount,
  isTrackCollectionPlayable,
  joinArtists,
} from "@/lib/format";
import { getValueQuery, useCatalogStore } from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectValueQuery } from "@/stores/catalog/query-state";

export function TracksPage() {
  const playTracks = usePlaybackStore((state) => state.playTracks);
  const shuffleTracks = usePlaybackStore((state) => state.shuffleTracks);
  const playTracksFrom = usePlaybackStore((state) => state.playTracksFrom);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
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
  const trackPinStates = usePinStates(
    query.items.map(
      (track) =>
        new Types.PinSubjectRef({
          ID: track.LibraryRecordingID || track.RecordingID,
          Kind: Types.PinSubjectKind.PinSubjectRecordingCluster,
      }),
    ),
  );
  const totalTrackCount = query.pageInfo?.Total ?? query.items.length;
  const canPlayTracks = isTrackCollectionPlayable({
    trackCount: totalTrackCount,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <SectionHeading
        meta={
          <MetricPill label={formatCount(totalTrackCount, "track")} />
        }
        actions={
          <>
            <Button
              disabled={!canPlayTracks}
              onClick={() => {
                void playTracks();
              }}
            >
              Play All
            </Button>
            <Button
              disabled={!canPlayTracks}
              onClick={() => {
                void shuffleTracks();
              }}
              tone="quiet"
            >
              Shuffle All
            </Button>
          </>
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
            <ManagedTrackListRow
              availabilityState={
                trackAvailabilityByRecordingId[track.RecordingID]?.data?.State
              }
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              libraryRecordingId={track.LibraryRecordingID}
              mode="list"
              onPlay={() => {
                void playTracksFrom(track.RecordingID);
              }}
              onQueue={() => {
                void queueRecording(track.RecordingID);
              }}
              pinState={
                trackPinStates[
                  pinSubjectKey({
                    ID: track.LibraryRecordingID || track.RecordingID,
                    Kind: Types.PinSubjectKind.PinSubjectRecordingCluster,
                  })
                ] ?? null
              }
              recordingId={track.RecordingID}
              subtitle={joinArtists(track.Artists)}
              title={track.Title}
            />
          )}
          scrollRestorationId="tracks-list"
          viewportClassName="pr-2"
        />
      </div>
    </div>
  );
}
