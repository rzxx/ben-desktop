import { getRouteApi, useLocation, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Download, LoaderCircle, Play } from "lucide-react";
import { useEffect, useState } from "react";
import type {
  AlbumTrackItem,
  AlbumVariantItem,
  JobSnapshot,
} from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { AlbumTracksEmptyState } from "@/components/catalog/EmptyState";
import { ManagedTrackListRow } from "@/components/catalog/ManagedTrackListRow";
import { VirtualRows } from "@/components/ui/VirtualRows";
import {
  isJobActive,
  isJobFailed,
  useJobSnapshot,
} from "@/hooks/jobs/useJobSnapshot";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "@/hooks/catalog/useCatalogQuery";
import { useResolvedUrl } from "@/hooks/media/useResolvedUrl";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  aggregateAvailabilityLabel,
  formatCount,
  formatDuration,
  isAggregateAvailabilityPlayable,
  joinArtists,
} from "@/lib/format";
import {
  resolveAlbumArtworkURL,
  startPinAlbumOffline,
  unpinAlbumOffline,
} from "@/lib/api/playback";
import { router } from "@/app/router/router-instance";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const albumDetailRouteApi = getRouteApi("/albums_/$albumId");

function albumVariantLocationBucket(variant: AlbumVariantItem) {
  const trackCount = Math.max(0, variant.TrackCount);
  const localTrackCount = Math.max(0, variant.LocalTrackCount);

  if (trackCount > 0 && localTrackCount >= trackCount) {
    return "local";
  }
  if (localTrackCount > 0) {
    return "partial";
  }
  return "remote";
}

function albumVariantLocationLabel(
  variant: AlbumVariantItem,
  variants: AlbumVariantItem[],
) {
  const bucket = albumVariantLocationBucket(variant);
  const bucketVariants = variants.filter(
    (candidate) => albumVariantLocationBucket(candidate) === bucket,
  );
  const bucketIndex =
    bucketVariants.findIndex(
      (candidate) => candidate.AlbumID === variant.AlbumID,
    ) + 1;

  const baseLabel =
    bucket === "local"
      ? "Local variant"
      : bucket === "partial"
        ? "Partial local variant"
        : "Non-local variant";

  return bucketVariants.length > 1 ? `${baseLabel} ${bucketIndex}` : baseLabel;
}

export function AlbumDetailPage() {
  const { albumId } = albumDetailRouteApi.useParams();
  const [selectedVariantId, setSelectedVariantId] = useState(albumId);
  const [pinActionBusy, setPinActionBusy] = useState(false);
  const [pinError, setPinError] = useState("");
  const [pinJob, setPinJob] = useState<JobSnapshot | null>(null);
  const location = useLocation();
  const navigate = useNavigate();
  const playAlbum = usePlaybackStore((state) => state.playAlbum);
  const playAlbumTrack = usePlaybackStore((state) => state.playAlbumTrack);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const albumAvailabilityByAlbumId = useCatalogStore(
    (state) => state.albumAvailabilityByAlbumId,
  );
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const trackedPinJob = useJobSnapshot(pinJob);

  const detail = useStoreQuery(
    (state) => selectDetail(getDetailRecord(state.albumDetails, albumId)),
    () => catalogLoaderClient.refetchAlbum(albumId),
  );
  const variants = useStoreQuery(
    (state) => selectDetail(getDetailRecord(state.albumVariants, albumId)),
    () => catalogLoaderClient.refetchAlbum(albumId),
  );
  const trackQuery = useStoreInfiniteQuery<AlbumTrackItem>(
    (state) =>
      selectValueQuery<AlbumTrackItem>(
        state,
        `albumTracks:${selectedVariantId}`,
      ),
    {
      fetchNextPage: () => {
        const record = getValueQuery<AlbumTrackItem>(
          useCatalogStore.getState(),
          `albumTracks:${selectedVariantId}`,
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensureAlbumTracksPage(
            selectedVariantId,
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () =>
        catalogLoaderClient.ensureAlbumTracksPage(selectedVariantId, 0, {
          force: true,
        }),
    },
  );

  useEffect(() => {
    setSelectedVariantId(albumId);
    setPinActionBusy(false);
    setPinError("");
    setPinJob(null);
  }, [albumId]);

  useEffect(() => {
    if (!variants.data?.length) {
      return;
    }
    if (
      !variants.data.some((variant) => variant.AlbumID === selectedVariantId)
    ) {
      setSelectedVariantId(variants.data[0]!.AlbumID);
    }
  }, [selectedVariantId, variants.data]);

  useEffect(() => {
    if (!selectedVariantId) {
      return;
    }
    void catalogLoaderClient.ensureAlbumTracksPage(selectedVariantId, 0, {
      force: true,
    });
  }, [selectedVariantId]);

  const activeVariant =
    variants.data?.find((variant) => variant.AlbumID === selectedVariantId) ??
    null;
  const albumVariants = variants.data ?? [];
  const lowResArtworkUrl = useResolvedUrl(
    selectedVariantId ? `album:${selectedVariantId}:96_jpeg` : "",
    selectedVariantId
      ? () => resolveAlbumArtworkURL(selectedVariantId, "96_jpeg")
      : undefined,
  );
  const highResArtworkUrl = useResolvedUrl(
    selectedVariantId ? `album:${selectedVariantId}:1024_avif` : "",
    selectedVariantId
      ? () => resolveAlbumArtworkURL(selectedVariantId, "1024_avif")
      : undefined,
  );
  const heroTitle = activeVariant?.Title ?? detail.data?.Title ?? "Album";
  const heroArtists = activeVariant?.Artists ?? detail.data?.Artists ?? [];
  const heroAvailability =
    albumAvailabilityByAlbumId[selectedVariantId]?.data ??
    (detail.data
      ? albumAvailabilityByAlbumId[detail.data.AlbumID]?.data
      : undefined);
  const canPlayAlbum = isAggregateAvailabilityPlayable(heroAvailability);
  const albumScopePinned = Boolean(heroAvailability?.ScopePinned);
  const canShowAlbumPinAction =
    albumScopePinned || heroAvailability?.State !== "LOCAL";
  const pinBusy = pinActionBusy || isJobActive(trackedPinJob);
  const pinFeedback = isJobActive(trackedPinJob)
    ? trackedPinJob?.message?.trim() || "Pinning album..."
    : isJobFailed(trackedPinJob)
      ? trackedPinJob?.error?.trim() ||
        trackedPinJob?.message?.trim() ||
        "Album pin failed."
      : "";
  const trackCount = activeVariant?.TrackCount ?? detail.data?.TrackCount ?? 0;
  const totalDurationMs = trackQuery.items.reduce(
    (total, track) => total + track.DurationMS,
    0,
  );
  const discCount = Math.max(
    trackQuery.items.reduce(
      (maxDiscNo, track) => Math.max(maxDiscNo, track.DiscNo || 1),
      1,
    ),
    1,
  );
  const releaseDateLabel = activeVariant?.Year
    ? String(activeVariant.Year)
    : detail.data?.Year
      ? String(detail.data.Year)
      : "Unknown release date";
  const error =
    detail.error ||
    variants.error ||
    trackQuery.error ||
    "Album tracks will render here when the selected variant contains recordings.";
  const navigationState = location.state as { __benSource?: string };
  const canReturnToAlbumsWithHistory =
    navigationState.__benSource === "albums" && router.history.canGoBack();

  function handleBackToAlbums() {
    if (canReturnToAlbumsWithHistory) {
      router.history.back();
      return;
    }

    void navigate({ to: "/albums" });
  }

  async function handleAlbumPinToggle() {
    if (!selectedVariantId) {
      return;
    }

    setPinActionBusy(true);
    setPinError("");

    try {
      if (albumScopePinned) {
        await unpinAlbumOffline(selectedVariantId);
        setPinJob(null);
      } else {
        const job = await startPinAlbumOffline(selectedVariantId);
        setPinJob(job);
      }
    } catch (error) {
      setPinError(error instanceof Error ? error.message : String(error));
    } finally {
      setPinActionBusy(false);
    }
  }

  return (
    <div className="flex h-full min-h-0 gap-8 max-xl:flex-col">
      <aside className="max-xl:w-full xl:sticky xl:top-4 xl:h-fit xl:w-2/5 xl:shrink-0">
        <div className="space-y-4">
          <button
            className="text-theme-500 hover:text-theme-100 inline-flex w-fit items-center gap-2 rounded-md py-1 text-sm transition-colors"
            onClick={handleBackToAlbums}
            type="button"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to albums
          </button>

          <ArtworkTile
            alt={heroTitle}
            className="w-full rounded-2xl border-black/10 shadow-[0_24px_65px_rgba(0,0,0,0.3)]"
            src={highResArtworkUrl || lowResArtworkUrl}
            title={heroTitle}
          />

          <div className="space-y-4">
            <div className="space-y-1">
              <h1 className="text-theme-100 text-xl font-bold lg:text-2xl">
                {heroTitle}
              </h1>
              <p className="text-theme-300">{joinArtists(heroArtists)}</p>
              <p className="text-theme-500 text-xs">
                {releaseDateLabel} • {formatCount(trackCount, "track")}
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              <Button
                disabled={!canPlayAlbum}
                icon={<Play className="h-4 w-4" />}
                onClick={() => {
                  void playAlbum(selectedVariantId);
                }}
                tone="primary"
              >
                Play all tracks
              </Button>
              {canShowAlbumPinAction ? (
                <Button
                  disabled={pinBusy || !selectedVariantId}
                  icon={
                    pinBusy ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      <Download className="h-4 w-4" />
                    )
                  }
                  onClick={() => {
                    void handleAlbumPinToggle();
                  }}
                  tone={albumScopePinned ? "quiet" : "default"}
                >
                  {pinBusy
                    ? "Pinning album..."
                    : albumScopePinned
                      ? "Unpin album"
                      : "Pin album"}
                </Button>
              ) : null}
            </div>
            {pinFeedback ? (
              <p className="text-theme-500 text-xs">{pinFeedback}</p>
            ) : null}
            {!pinFeedback && pinError ? (
              <p className="text-xs text-red-300">{pinError}</p>
            ) : null}

            <dl className="grid grid-cols-2 gap-3 rounded-xl">
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Length
                </dt>
                <dd className="text-theme-100 text-sm font-medium">
                  {formatDuration(totalDurationMs)}
                </dd>
              </div>
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Discs
                </dt>
                <dd className="text-theme-100 text-sm font-medium">
                  {discCount}
                </dd>
              </div>
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Variants
                </dt>
                <dd className="text-theme-100 text-sm font-medium">
                  {detail.data?.VariantCount ?? variants.data?.length ?? 1}
                </dd>
              </div>
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Availability
                </dt>
                <dd className="text-theme-100 text-sm font-medium">
                  {aggregateAvailabilityLabel(heroAvailability)}
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </aside>

      <section className="flex min-h-0 flex-1 flex-col xl:w-3/5">
        <div className="min-h-10 pr-2">
          {albumVariants.length > 1 ? (
            <div className="ben-scrollbar flex min-h-10 items-center gap-2 overflow-x-auto overflow-y-hidden whitespace-nowrap">
              {albumVariants.map((variant: AlbumVariantItem) => (
                <button
                  aria-pressed={variant.AlbumID === selectedVariantId}
                  className={[
                    "shrink-0 rounded-full border px-3 py-1.5 text-sm font-medium transition",
                    variant.AlbumID === selectedVariantId
                      ? "text-theme-100 border-white/18 bg-white/[0.08]"
                      : "text-theme-300 border-white/8 bg-white/[0.03] hover:border-white/14 hover:bg-white/[0.05]",
                  ].join(" ")}
                  key={variant.AlbumID}
                  onClick={() => {
                    setSelectedVariantId(variant.AlbumID);
                  }}
                  title={[
                    variant.Edition || variant.Title,
                    variant.Year ? String(variant.Year) : null,
                    formatCount(variant.TrackCount, "track"),
                    aggregateAvailabilityLabel(
                      albumAvailabilityByAlbumId[variant.AlbumID]?.data,
                    ),
                  ]
                    .filter(Boolean)
                    .join(" • ")}
                  type="button"
                >
                  {albumVariantLocationLabel(variant, albumVariants)}
                </button>
              ))}
            </div>
          ) : null}
        </div>

        <div className="min-h-0 flex-1">
          <VirtualRows
            className="min-h-0 flex-1"
            emptyState={<AlbumTracksEmptyState body={error} />}
            estimateSize={64}
            gap={8}
            hasMore={trackQuery.hasMore}
            items={trackQuery.items}
            loading={trackQuery.isLoading}
            loadingMore={trackQuery.isRefreshing}
            onEndReached={() => {
              void trackQuery.fetchNextPage();
            }}
            renderRow={(track) => (
              <ManagedTrackListRow
                availabilityState={
                  trackAvailabilityByRecordingId[track.RecordingID]?.data?.State
                }
                durationMs={track.DurationMS}
                indexLabel={
                  track.DiscNo > 1
                    ? `${track.DiscNo}-${track.TrackNo}`
                    : String(track.TrackNo)
                }
                libraryRecordingId={track.LibraryRecordingID}
                mode="album"
                onPlay={() => {
                  void playAlbumTrack(selectedVariantId, track.RecordingID);
                }}
                onQueue={() => {
                  void queueRecording(track.RecordingID);
                }}
                pinned={
                  trackAvailabilityByRecordingId[track.RecordingID]?.data
                    ?.Pinned
                }
                recordingId={track.RecordingID}
                subtitle={joinArtists(track.Artists)}
                title={track.Title}
              />
            )}
            scrollRestorationId="album-tracks-list"
            viewportClassName="pr-2"
          />
        </div>
      </section>
    </div>
  );
}
