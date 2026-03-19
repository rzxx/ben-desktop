import { getRouteApi, Link } from "@tanstack/react-router";
import { ArrowLeft, Play } from "lucide-react";
import { useEffect, useState } from "react";
import type { AlbumTrackItem, AlbumVariantItem } from "@/lib/api/models";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { AlbumTracksEmptyState } from "@/components/catalog/EmptyState";
import { TrackListRow } from "@/components/catalog/TrackListRow";
import { VirtualRows } from "@/components/ui/VirtualRows";
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
  joinArtists,
} from "@/lib/format";
import { resolveAlbumArtworkURL } from "@/lib/api/playback";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const albumDetailRouteApi = getRouteApi("/albums_/$albumId");

export function AlbumDetailPage() {
  const { albumId } = albumDetailRouteApi.useParams();
  const [selectedVariantId, setSelectedVariantId] = useState(albumId);
  const playAlbum = usePlaybackStore((state) => state.playAlbum);
  const playAlbumTrack = usePlaybackStore((state) => state.playAlbumTrack);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const albumAvailabilityByAlbumId = useCatalogStore(
    (state) => state.albumAvailabilityByAlbumId,
  );
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );

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

  return (
    <div className="flex h-full min-h-0 gap-8 max-xl:flex-col">
      <aside className="max-xl:w-full xl:sticky xl:top-4 xl:h-fit xl:w-2/5 xl:shrink-0">
        <div className="space-y-4">
          <Link
            className="text-theme-500 hover:text-theme-100 inline-flex w-fit items-center gap-2 rounded-md py-1 text-sm transition-colors"
            to="/albums"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to albums
          </Link>

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

            <div>
              <Button
                icon={<Play className="h-4 w-4" />}
                onClick={() => {
                  void playAlbum(selectedVariantId);
                }}
                tone="primary"
              >
                Play all tracks
              </Button>
            </div>

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

          {variants.data && variants.data.length > 1 ? (
            <div className="space-y-2">
              <p className="text-theme-500 text-[11px] tracking-[0.28em] uppercase">
                Variants
              </p>
              <div className="space-y-2">
                {variants.data.map((variant: AlbumVariantItem) => (
                  <button
                    className={[
                      "block w-full rounded-lg border px-3 py-3 text-left transition",
                      variant.AlbumID === selectedVariantId
                        ? "text-theme-100 border-white/18 bg-white/[0.08]"
                        : "text-theme-300 border-white/8 bg-white/[0.03] hover:border-white/14 hover:bg-white/[0.05]",
                    ].join(" ")}
                    key={variant.AlbumID}
                    onClick={() => {
                      setSelectedVariantId(variant.AlbumID);
                    }}
                    type="button"
                  >
                    <span className="block text-sm font-medium">
                      {variant.Edition || variant.Title}
                    </span>
                    <span className="text-theme-500 mt-1 block text-xs">
                      {[
                        variant.Year ? String(variant.Year) : null,
                        formatCount(variant.TrackCount, "track"),
                        aggregateAvailabilityLabel(
                          albumAvailabilityByAlbumId[variant.AlbumID]?.data,
                        ),
                      ]
                        .filter(Boolean)
                        .join(" • ")}
                    </span>
                  </button>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      </aside>

      <section className="mt-10 flex min-h-0 flex-1 flex-col gap-4 xl:w-3/5">
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
              <TrackListRow
                availabilityState={
                  trackAvailabilityByRecordingId[track.RecordingID]?.data?.State
                }
                durationMs={track.DurationMS}
                indexLabel={
                  track.DiscNo > 1
                    ? `${track.DiscNo}-${track.TrackNo}`
                    : String(track.TrackNo)
                }
                mode="album"
                onPlay={() => {
                  void playAlbumTrack(selectedVariantId, track.RecordingID);
                }}
                onQueue={() => {
                  void queueRecording(track.RecordingID);
                }}
                subtitle={joinArtists(track.Artists)}
                title={track.Title}
              />
            )}
            viewportClassName="pr-2"
          />
        </div>
      </section>
    </div>
  );
}
