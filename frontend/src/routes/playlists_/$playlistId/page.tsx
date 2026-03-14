import { getRouteApi } from "@tanstack/react-router";
import { Play, Plus } from "lucide-react";
import type { PlaylistTrackItem } from "../../../shared/lib/desktop";
import {
  formatCount,
  formatRelativeDate,
  joinArtists,
} from "../../../shared/lib/format";
import { VirtualRows } from "../../../shared/ui/VirtualRows";
import { catalogLoaderClient } from "../../../features/library/catalog-loader-client";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "../../../features/library/catalog-store";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "../../../features/library/use-store-query";
import { usePlaybackStore } from "../../../features/playback/store";
import { TracksEmptyState } from "../../catalog/components/EmptyState";
import { MetricPill } from "../../catalog/components/MetricPill";
import {
  ActionButton,
  DetailHero,
} from "../../catalog/components/SurfaceHeader";
import { TrackRow } from "../../catalog/components/TrackRow";
import { selectDetail, selectValueQuery } from "../../catalog/query-state";

const playlistDetailRouteApi = getRouteApi("/playlists_/$playlistId");

export function PlaylistDetailPage() {
  const { playlistId } = playlistDetailRouteApi.useParams();
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playPlaylistTrack = usePlaybackStore(
    (state) => state.playPlaylistTrack,
  );
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const detail = useStoreQuery(
    (state) =>
      selectDetail(getDetailRecord(state.playlistSummaries, playlistId)),
    () => catalogLoaderClient.refetchPlaylist(playlistId),
  );
  const trackQuery = useStoreInfiniteQuery(
    (state) =>
      selectValueQuery<PlaylistTrackItem>(
        state,
        `playlistTracks:${playlistId}`,
      ),
    {
      fetchNextPage: () => {
        const record = getValueQuery<PlaylistTrackItem>(
          useCatalogStore.getState(),
          `playlistTracks:${playlistId}`,
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensurePlaylistTracksPage(
            playlistId,
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () =>
        catalogLoaderClient.ensurePlaylistTracksPage(playlistId, 0, {
          force: true,
        }),
    },
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <DetailHero
        actions={
          <>
            <ActionButton
              icon={<Play className="h-4 w-4" />}
              label="Play playlist"
              onClick={() => {
                void playPlaylist(playlistId);
              }}
              priority="primary"
            />
            <ActionButton
              icon={<Plus className="h-4 w-4" />}
              label="Queue playlist"
              onClick={() => {
                void queuePlaylist(playlistId);
              }}
            />
          </>
        }
        eyebrow="Playlist detail"
        meta={
          <>
            <MetricPill
              label={formatCount(
                detail.data?.ItemCount ?? trackQuery.pageInfo?.Total ?? 0,
                "track",
              )}
            />
            <MetricPill label={formatRelativeDate(detail.data?.UpdatedAt)} />
          </>
        }
        subtitle="Playlist header with track list below. This view stays read-only in this slice."
        thumb={detail.data?.Thumb}
        title={detail.data?.Name ?? "Playlist"}
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <TracksEmptyState
              body={
                detail.error ||
                "Playlist tracks will render here when items exist."
              }
            />
          }
          estimateSize={72}
          hasMore={trackQuery.hasMore}
          items={trackQuery.items}
          loading={trackQuery.isLoading}
          loadingMore={trackQuery.isRefreshing}
          onEndReached={() => {
            void trackQuery.fetchNextPage();
          }}
          renderRow={(track, index) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              onPlay={() => {
                void playPlaylistTrack(playlistId, track.ItemID);
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
