import type { LikedRecordingItem } from "@/lib/api";
import {
  formatCount,
  formatRelativeDate,
  joinArtists,
} from "@/lib/format";
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
import {
  ActionButton,
  DetailHero,
} from "@/components/catalog/SurfaceHeader";
import { TrackRow } from "@/components/catalog/TrackRow";
import { selectValueQuery } from "@/stores/catalog/query-state";
import { Play } from "lucide-react";

export function LikedPlaylistPage() {
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const playLikedTrack = usePlaybackStore((state) => state.playLikedTrack);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
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


