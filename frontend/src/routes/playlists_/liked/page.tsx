import type { LikedRecordingItem } from "../../../shared/lib/desktop";
import {
  formatCount,
  formatRelativeDate,
  joinArtists,
} from "../../../shared/lib/format";
import { VirtualRows } from "../../../shared/ui/VirtualRows";
import { catalogLoaderClient } from "../../../features/library/catalog-loader-client";
import {
  getValueQuery,
  useCatalogStore,
} from "../../../features/library/catalog-store";
import { useStoreInfiniteQuery } from "../../../features/library/use-store-query";
import { usePlaybackStore } from "../../../features/playback/store";
import { TracksEmptyState } from "../../catalog/components/EmptyState";
import { MetricPill } from "../../catalog/components/MetricPill";
import {
  ActionButton,
  DetailHero,
} from "../../catalog/components/SurfaceHeader";
import { TrackRow } from "../../catalog/components/TrackRow";
import { selectValueQuery } from "../../catalog/query-state";
import { Play } from "lucide-react";

export function LikedPlaylistPage() {
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const playLikedTrack = usePlaybackStore((state) => state.playLikedTrack);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const query = useStoreInfiniteQuery(
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

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <DetailHero
        actions={
          <ActionButton
            icon={<Play className="h-4 w-4" />}
            label="Play liked"
            onClick={() => {
              void playLiked();
            }}
            priority="primary"
          />
        }
        eyebrow="Reserved playlist"
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "track",
            )}
          />
        }
        subtitle="Special liked songs view backed by the reserved playlist in core."
        title="Liked songs"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
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
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              onPlay={() => {
                void playLikedTrack(track.RecordingID);
              }}
              onQueue={() => {
                void queueRecording(track.RecordingID);
              }}
              subtitle={`${joinArtists(track.Artists)} • added ${formatRelativeDate(track.AddedAt)}`}
              title={track.Title}
            />
          )}
        />
      </div>
    </div>
  );
}
