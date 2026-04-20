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
  pinStateLabel,
} from "@/lib/format";
import { resolveAlbumArtworkURL } from "@/lib/api/playback";
import { startPin, unpin } from "@/lib/api/pin";
import { Types } from "@/lib/api/models";
import { router } from "@/app/router/router-instance";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const albumDetailRouteApi = getRouteApi("/albums_/$albumId");

type AlbumPinUiState = {
  albumId: string;
  busy: boolean;
  error: string;
  job: JobSnapshot | null;
};

type AlbumVariantSelectionState = {
  albumId: string;
  selectedVariantId: string;
};

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
  const [variantSelectionState, setVariantSelectionState] =
    useState<AlbumVariantSelectionState>({
      albumId,
      selectedVariantId: albumId,
    });
  const [pinUiState, setPinUiState] = useState<AlbumPinUiState>({
    albumId,
    busy: false,
    error: "",
    job: null,
  });
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
  const scopedPinUiState =
    pinUiState.albumId === albumId
      ? pinUiState
      : {
          albumId,
          busy: false,
          error: "",
          job: null,
        };
  const trackedPinJob = useJobSnapshot(scopedPinUiState.job);
  const routeSelectedVariantId =
    variantSelectionState.albumId === albumId
      ? variantSelectionState.selectedVariantId
      : albumId;
  const albumPinState = usePinState(
    routeSelectedVariantId
      ? new Types.PinSubjectRef({
          ID: routeSelectedVariantId,
          Kind: Types.PinSubjectKind.PinSubjectAlbumVariant,
        })
      : null,
  );

  const detail = useStoreQuery(
    (state) => selectDetail(getDetailRecord(state.albumDetails, albumId)),
    () => catalogLoaderClient.refetchAlbum(albumId),
  );
  const variants = useStoreQuery(
    (state) => selectDetail(getDetailRecord(state.albumVariants, albumId)),
    () => catalogLoaderClient.refetchAlbum(albumId),
  );
  const albumVariants = variants.data ?? [];
  const selectedVariantId = albumVariants.some(
    (variant) => variant.AlbumID === routeSelectedVariantId,
  )
    ? routeSelectedVariantId
    : (albumVariants[0]?.AlbumID ?? routeSelectedVariantId);
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
  const trackPinStates = usePinStates(
    trackQuery.items.map(
      (track) =>
        new Types.PinSubjectRef({
          ID: track.RecordingID,
          Kind: Types.PinSubjectKind.PinSubjectRecordingVariant,
        }),
    ),
  );
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
  const albumScopePinnedDirect = Boolean(albumPinState?.Direct);
  const canShowAlbumPinAction = Boolean(selectedVariantId);
  const pinBusy = scopedPinUiState.busy || isJobActive(trackedPinJob);
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

    setPinUiState({
      albumId,
      busy: true,
      error: "",
      job: scopedPinUiState.job,
    });

    try {
      if (albumScopePinnedDirect) {
        await unpin({
          ID: selectedVariantId,
          Kind: Types.PinSubjectKind.PinSubjectAlbumVariant,
        });
        setPinUiState({
          albumId,
          busy: false,
          error: "",
          job: null,
        });
      } else {
        const job = await startPin({
          ID: selectedVariantId,
          Kind: Types.PinSubjectKind.PinSubjectAlbumVariant,
        });
        setPinUiState({
          albumId,
          busy: false,
          error: "",
          job,
        });
      }
    } catch (error) {
      setPinUiState({
        albumId,
        busy: false,
        error: error instanceof Error ? error.message : String(error),
        job: scopedPinUiState.job,
      });
    }
  }

  return (
    <div className="flex h-full min-h-0 gap-8 max-xl:flex-col">
      <aside className="max-xl:w-full xl:sticky xl:top-4 xl:h-fit xl:w-2/5 xl:shrink-0">
        <div className="space-y-4">
          <button
            className="text-theme-600 hover:text-theme-900 dark:text-theme-400 dark:hover:text-theme-100 inline-flex w-fit items-center gap-2 rounded-md py-1 text-sm transition-colors"
            onClick={handleBackToAlbums}
            type="button"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to albums
          </button>

          <ArtworkTile
            alt={heroTitle}
            className="border-theme-300/70 shadow-theme-900/16 w-full rounded-2xl shadow-[0_24px_65px_rgba(15,23,42,0.18)] dark:border-black/10 dark:shadow-[0_24px_65px_rgba(0,0,0,0.3)]"
            src={highResArtworkUrl || lowResArtworkUrl}
            title={heroTitle}
          />

          <div className="space-y-4">
            <div className="space-y-1">
              <h1 className="text-theme-900 dark:text-theme-100 text-xl font-bold lg:text-2xl">
                {heroTitle}
              </h1>
              <p className="text-theme-600 dark:text-theme-300">
                {joinArtists(heroArtists)}
              </p>
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
                  tone={albumScopePinnedDirect ? "quiet" : "default"}
                >
                  {pinBusy
                    ? "Pinning album..."
                    : albumScopePinnedDirect
                      ? "Unpin album"
                      : albumPinState?.Covered
                        ? "Pin album directly"
                        : "Pin album"}
                </Button>
              ) : null}
            </div>
            {pinFeedback ? (
              <p className="text-theme-500 text-xs">{pinFeedback}</p>
            ) : null}
            {!pinFeedback && !scopedPinUiState.error && pinStateLabel(albumPinState) ? (
              <p className="text-theme-500 text-xs">
                {pinStateLabel(albumPinState)}
              </p>
            ) : null}
            {!pinFeedback && scopedPinUiState.error ? (
              <p className="text-xs text-red-600 dark:text-red-300">
                {scopedPinUiState.error}
              </p>
            ) : null}

            <dl className="grid grid-cols-2 gap-3 rounded-xl">
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Length
                </dt>
                <dd className="text-theme-900 dark:text-theme-100 text-sm font-medium">
                  {formatDuration(totalDurationMs)}
                </dd>
              </div>
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Discs
                </dt>
                <dd className="text-theme-900 dark:text-theme-100 text-sm font-medium">
                  {discCount}
                </dd>
              </div>
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Variants
                </dt>
                <dd className="text-theme-900 dark:text-theme-100 text-sm font-medium">
                  {detail.data?.VariantCount ?? variants.data?.length ?? 1}
                </dd>
              </div>
              <div>
                <dt className="text-theme-500 text-xs tracking-wide uppercase">
                  Availability
                </dt>
                <dd className="text-theme-900 dark:text-theme-100 text-sm font-medium">
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
                      ? "border-theme-900 bg-theme-900 text-theme-50 dark:text-theme-100 dark:border-white/18 dark:bg-white/8"
                      : "border-theme-300/75 text-theme-700 hover:border-theme-400/75 hover:bg-theme-100 dark:text-theme-300 bg-white/82 dark:border-white/8 dark:bg-white/3 dark:hover:border-white/14 dark:hover:bg-white/5",
                  ].join(" ")}
                  key={variant.AlbumID}
                  onClick={() => {
                    setVariantSelectionState({
                      albumId,
                      selectedVariantId: variant.AlbumID,
                    });
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
                pinState={
                  trackPinStates[
                    pinSubjectKey({
                      ID: track.RecordingID,
                      Kind: Types.PinSubjectKind.PinSubjectRecordingVariant,
                    })
                  ] ?? null
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
