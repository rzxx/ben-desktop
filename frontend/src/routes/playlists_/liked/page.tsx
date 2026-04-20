import { Download, LoaderCircle, Play } from "lucide-react";
import { useState } from "react";
import type { JobSnapshot, LikedRecordingItem, PinSubjectRef } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { ManagedTrackListRow } from "@/components/catalog/ManagedTrackListRow";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import {
  pinSubjectKey,
  usePinState,
  usePinStates,
} from "@/hooks/pins/usePinStates";
import { VirtualRows } from "@/components/ui/VirtualRows";
import {
  isJobActive,
  isJobFailed,
  useJobSnapshot,
} from "@/hooks/jobs/useJobSnapshot";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  formatCount,
  pinStateLabel,
  formatRelativeDate,
  isCatalogTrackActionable,
  isTrackCollectionPlayable,
  joinArtists,
} from "@/lib/format";
import { startPin, unpin } from "@/lib/api/pin";
import { Types } from "@/lib/api/models";
import { getValueQuery, useCatalogStore } from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectValueQuery } from "@/stores/catalog/query-state";
import { likedPlaybackRecordingId } from "./-playback-id";

export function LikedPlaylistPage() {
  const [pinActionBusy, setPinActionBusy] = useState(false);
  const [pinError, setPinError] = useState("");
  const [pinJob, setPinJob] = useState<JobSnapshot | null>(null);
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const playLikedTrack = usePlaybackStore((state) => state.playLikedTrack);
  const queueLikedTrack = usePlaybackStore((state) => state.queueLikedTrack);
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const likedPlaylist = useCatalogStore(
    (state) => state.playlistsById.liked ?? null,
  );
  const trackedPinJob = useJobSnapshot(pinJob);
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
  const likedPinSubject: PinSubjectRef | null = likedPlaylist?.PlaylistID
    ? new Types.PinSubjectRef({
        ID: likedPlaylist.PlaylistID,
        Kind: Types.PinSubjectKind.PinSubjectLikedPlaylist,
      })
    : null;
  const likedPinState = usePinState(likedPinSubject);
  const trackPinStates = usePinStates(
    query.items.map(
      (track) =>
        new Types.PinSubjectRef({
          ID: track.LibraryRecordingID || track.RecordingID,
          Kind: Types.PinSubjectKind.PinSubjectRecordingCluster,
        }),
    ),
  );
  const canPlayLiked = isTrackCollectionPlayable({
    trackCount: likedTrackCount,
    fullyLoaded: likedTracksFullyLoaded,
    hasPlayableLoadedTrack,
  });
  const likedScopePinnedDirect = Boolean(likedPinState?.Direct);
  const pinBusy = pinActionBusy || isJobActive(trackedPinJob);
  const pinFeedback = isJobActive(trackedPinJob)
    ? trackedPinJob?.message?.trim() || "Pinning liked songs..."
    : isJobFailed(trackedPinJob)
      ? trackedPinJob?.error?.trim() ||
        trackedPinJob?.message?.trim() ||
        "Liked songs pin failed."
      : "";

  async function handleLikedPinToggle() {
    if (!likedPinSubject) {
      return;
    }

    setPinActionBusy(true);
    setPinError("");

    try {
      if (likedScopePinnedDirect) {
        await unpin(likedPinSubject);
        setPinJob(null);
      } else {
        const job = await startPin(likedPinSubject);
        setPinJob(job);
      }
    } catch (error) {
      setPinError(error instanceof Error ? error.message : String(error));
    } finally {
      setPinActionBusy(false);
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <section className="flex flex-wrap items-end gap-5">
        <ArtworkTile
          alt="Liked songs"
          className="border-theme-300/70 h-40 w-40 shrink-0 dark:border-black/10"
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
          <Button
            disabled={pinBusy || !likedPinSubject}
            icon={
              pinBusy ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Download className="h-4 w-4" />
              )
            }
            onClick={() => {
              void handleLikedPinToggle();
            }}
            tone={likedScopePinnedDirect ? "quiet" : "default"}
          >
            {pinBusy
              ? "Pinning liked..."
              : likedScopePinnedDirect
                ? "Unpin liked"
                : likedPinState?.Covered
                  ? "Pin liked directly"
                  : "Pin liked"}
          </Button>
          {pinFeedback ? (
            <p className="text-theme-500 text-xs">{pinFeedback}</p>
          ) : null}
          {!pinFeedback && !pinError && pinStateLabel(likedPinState) ? (
            <p className="text-theme-500 text-xs">
              {pinStateLabel(likedPinState)}
            </p>
          ) : null}
          {!pinFeedback && pinError ? (
            <p className="text-xs text-red-600 dark:text-red-300">{pinError}</p>
          ) : null}
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
                void playLikedTrack(likedPlaybackRecordingId(track));
              }}
              onQueue={() => {
                void queueLikedTrack(likedPlaybackRecordingId(track));
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
