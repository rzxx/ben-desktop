import { getRouteApi } from "@tanstack/react-router";
import { Play, Plus } from "lucide-react";
import type { PlaylistTrackItem } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TrackListRow } from "@/components/catalog/TrackListRow";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { VirtualRows } from "@/components/ui/VirtualRows";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "@/hooks/catalog/useCatalogQuery";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { formatCount, formatRelativeDate, joinArtists } from "@/lib/format";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const playlistDetailRouteApi = getRouteApi("/playlists_/$playlistId");

export function PlaylistDetailPage() {
  const { playlistId } = playlistDetailRouteApi.useParams();
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playPlaylistTrack = usePlaybackStore(
    (state) => state.playPlaylistTrack,
  );
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const detail = useStoreQuery(
    (state) =>
      selectDetail(getDetailRecord(state.playlistSummaries, playlistId)),
    () => catalogLoaderClient.refetchPlaylist(playlistId),
  );
  const artworkUrl = useThumbnailUrl(detail.data?.Thumb);
  const trackQuery = useStoreInfiniteQuery<PlaylistTrackItem>(
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
      <section className="flex flex-wrap items-end gap-5">
        <ArtworkTile
          alt={detail.data?.Name ?? "Playlist"}
          className="h-40 w-40 shrink-0 border-black/10"
          src={artworkUrl}
          subtitle="Playlist"
          title={detail.data?.Name ?? "Playlist"}
        />
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <SectionHeading
            meta={
              <>
                <MetricPill
                  label={formatCount(
                    detail.data?.ItemCount ?? trackQuery.pageInfo?.Total ?? 0,
                    "track",
                  )}
                />
                <MetricPill
                  label={formatRelativeDate(detail.data?.UpdatedAt)}
                />
              </>
            }
            title={detail.data?.Name ?? "Playlist"}
          />
          <div className="flex flex-wrap gap-2">
            <Button
              icon={<Play className="h-4 w-4" />}
              onClick={() => {
                void playPlaylist(playlistId);
              }}
              tone="primary"
            >
              Play playlist
            </Button>
            <Button
              icon={<Plus className="h-4 w-4" />}
              onClick={() => {
                void queuePlaylist(playlistId);
              }}
            >
              Queue playlist
            </Button>
          </div>
        </div>
      </section>
      <div className="min-h-0 flex-1">
        <VirtualRows
          className="min-h-0 flex-1"
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
            <TrackListRow
              availabilityState={
                trackAvailabilityByRecordingId[track.RecordingID]?.data?.State
              }
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              mode="list"
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
          viewportClassName="pr-2"
        />
      </div>
    </div>
  );
}
