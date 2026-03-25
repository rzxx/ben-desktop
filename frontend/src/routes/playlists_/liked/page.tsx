import { Play } from "lucide-react";
import type { LikedRecordingItem } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { ManagedTrackListRow } from "@/components/catalog/ManagedTrackListRow";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  formatCount,
  formatRelativeDate,
  isCatalogTrackActionable,
  isTrackCollectionPlayable,
  joinArtists,
} from "@/lib/format";
import { getValueQuery, useCatalogStore } from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectValueQuery } from "@/stores/catalog/query-state";

export function LikedPlaylistPage() {
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const playLikedTrack = usePlaybackStore((state) => state.playLikedTrack);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const query = useStoreInfiniteQuery<LikedRecordingItem>(
    (state) => selectValueQuery<LikedRecordingItem>(state, "liked"),
    {
      fetchNextPage: () => {
        const record = getValueQuery<LikedRecordingItem>(
          useCatalogStore.getState(),
          "liked",
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureLikedPage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchLiked(),
    },
  );
  const likedTrackCount = query.pageInfo?.Total ?? query.items.length;
  const hasPlayableLoadedTrack = query.items.some((track) =>
    isCatalogTrackActionable(
      trackAvailabilityByRecordingId[track.RecordingID]?.data?.State,
    ),
  );
  const likedTracksFullyLoaded =
    !query.isLoading &&
    !query.hasMore &&
    likedTrackCount === query.items.length;
  const canPlayLiked = isTrackCollectionPlayable({
    trackCount: likedTrackCount,
    fullyLoaded: likedTracksFullyLoaded,
    hasPlayableLoadedTrack,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <section className="flex flex-wrap items-end gap-5">
        <ArtworkTile
          alt="Liked songs"
          className="h-40 w-40 shrink-0 border-black/10"
          subtitle="Liked"
          title="Liked songs"
        />
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <SectionHeading
            meta={
              <MetricPill
                label={formatCount(
                  query.pageInfo?.Total ?? query.items.length,
                  "track",
                )}
              />
            }
            title="Liked songs"
          />
          <Button
            disabled={!canPlayLiked}
            icon={<Play className="h-4 w-4" />}
            onClick={() => {
              void playLiked();
            }}
            tone="primary"
          >
            Play liked
          </Button>
        </div>
      </section>
      <div className="min-h-0 flex-1">
        <VirtualRows
          className="min-h-0 flex-1"
          emptyState={
            <TracksEmptyState body="Liked recordings will appear here when tracks are liked in other surfaces." />
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
              initialLiked
              indexLabel={String(index + 1).padStart(2, "0")}
              libraryRecordingId={track.LibraryRecordingID}
              mode="list"
              onPlay={() => {
                void playLikedTrack(track.RecordingID);
              }}
              onQueue={() => {
                void queueRecording(track.RecordingID);
              }}
              recordingId={track.RecordingID}
              subtitle={`${joinArtists(track.Artists)} • added ${formatRelativeDate(track.AddedAt)}`}
              title={track.Title}
            />
          )}
          scrollRestorationId="liked-tracks-list"
          viewportClassName="pr-2"
        />
      </div>
    </div>
  );
}
