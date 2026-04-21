import { Play } from "lucide-react";
import type { OfflineRecordingItem } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { ManagedTrackListRow } from "@/components/catalog/ManagedTrackListRow";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { pinSubjectKey, usePinStates } from "@/hooks/pins/usePinStates";
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
import { Types } from "@/lib/api/models";
import { getValueQuery, useCatalogStore } from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectValueQuery } from "@/stores/catalog/query-state";
import { offlinePlaybackRecordingId } from "./-playback-id";

function offlineReasonLabel(track: OfflineRecordingItem) {
  if (track.HasLocalSource && track.HasLocalCached) {
    return "Local file + Cached";
  }
  if (track.HasLocalSource) {
    return "Local file";
  }
  return "Cached";
}

function offlineSubtitle(track: OfflineRecordingItem) {
  const parts = [joinArtists(track.Artists), offlineReasonLabel(track)].filter(
    Boolean,
  );
  if (track.OfflineSince) {
    parts.push(`offline ${formatRelativeDate(track.OfflineSince)}`);
  }
  return parts.join(" • ");
}

export function OfflinePlaylistPage() {
  const playOffline = usePlaybackStore((state) => state.playOffline);
  const playOfflineTrack = usePlaybackStore((state) => state.playOfflineTrack);
  const queueOfflineTrack = usePlaybackStore(
    (state) => state.queueOfflineTrack,
  );
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const offlinePlaylist = useCatalogStore(
    (state) => state.playlistsById.offline ?? null,
  );
  const query = useStoreInfiniteQuery<OfflineRecordingItem>(
    (state) => selectValueQuery<OfflineRecordingItem>(state, "offline"),
    {
      fetchNextPage: () => {
        const record = getValueQuery<OfflineRecordingItem>(
          useCatalogStore.getState(),
          "offline",
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureOfflinePage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchOffline(),
    },
  );
  const offlineTrackCount = query.pageInfo?.Total ?? query.items.length;
  const hasPlayableLoadedTrack = query.items.some((track) =>
    isCatalogTrackActionable(
      trackAvailabilityByRecordingId[track.RecordingID]?.data?.State,
    ),
  );
  const offlineTracksFullyLoaded =
    !query.isLoading &&
    !query.hasMore &&
    offlineTrackCount === query.items.length;
  const trackPinStates = usePinStates(
    query.items.map(
      (track) =>
        new Types.PinSubjectRef({
          ID: track.LibraryRecordingID || track.RecordingID,
          Kind: Types.PinSubjectKind.PinSubjectRecordingCluster,
        }),
    ),
  );
  const canPlayOffline = isTrackCollectionPlayable({
    trackCount: offlineTrackCount,
    fullyLoaded: offlineTracksFullyLoaded,
    hasPlayableLoadedTrack,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <section className="flex flex-wrap items-end gap-5">
        <ArtworkTile
          alt="Offline"
          className="border-theme-300/70 h-40 w-40 shrink-0 dark:border-black/10"
          subtitle="Reserved"
          title="Offline"
        />
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <SectionHeading
            meta={
              <MetricPill
                label={formatCount(
                  offlinePlaylist?.ItemCount ?? query.pageInfo?.Total ?? 0,
                  "track",
                )}
              />
            }
            title="Offline"
          />
          <Button
            disabled={!canPlayOffline}
            icon={<Play className="h-4 w-4" />}
            onClick={() => {
              void playOffline();
            }}
            tone="primary"
          >
            Play offline
          </Button>
        </div>
      </section>
      <div className="min-h-0 flex-1">
        <VirtualRows
          className="min-h-0 flex-1"
          emptyState={
            <TracksEmptyState body="Offline tracks and local files on this device will appear here." />
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
                void playOfflineTrack(offlinePlaybackRecordingId(track));
              }}
              onQueue={() => {
                void queueOfflineTrack(offlinePlaybackRecordingId(track));
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
              subtitle={offlineSubtitle(track)}
              title={track.Title}
            />
          )}
          scrollRestorationId="offline-tracks-list"
          viewportClassName="pr-2"
        />
      </div>
    </div>
  );
}
