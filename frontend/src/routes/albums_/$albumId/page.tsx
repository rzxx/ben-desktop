import { getRouteApi } from "@tanstack/react-router";
import { Play, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import type {
  AlbumListItem,
  AlbumTrackItem,
  AlbumVariantItem,
} from "@/lib/api/models";
import { resolveAlbumArtworkURL } from "@/lib/api/playback";
import { formatCount, joinArtists } from "@/lib/format";
import { useResolvedUrl } from "@/hooks/media/useResolvedUrl";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "@/hooks/catalog/useCatalogQuery";
import { usePlaybackStore } from "@/stores/playback/store";
import { buildAlbumSubtitle, EMPTY_THUMB } from "@/lib/catalog/album";
import { AlbumTracksEmptyState } from "@/components/catalog/EmptyState";
import { MetricPill } from "@/components/catalog/MetricPill";
import { ActionButton, DetailHero } from "@/components/catalog/SurfaceHeader";
import { TrackRow } from "@/components/catalog/TrackRow";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const albumDetailRouteApi = getRouteApi("/albums_/$albumId");

export function AlbumDetailPage() {
  const { albumId } = albumDetailRouteApi.useParams();
  const [selectedVariantId, setSelectedVariantId] = useState(albumId);
  const playAlbum = usePlaybackStore((state) => state.playAlbum);
  const playAlbumTrack = usePlaybackStore((state) => state.playAlbumTrack);
  const queueAlbum = usePlaybackStore((state) => state.queueAlbum);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);

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
    void catalogLoaderClient.ensureAlbumTracksPage(selectedVariantId, 0);
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
  const heroThumb = activeVariant?.Thumb ?? detail.data?.Thumb ?? EMPTY_THUMB;
  const heroAlbum =
    detail.data ??
    ({
      AlbumID: selectedVariantId,
      AlbumClusterID: selectedVariantId,
      Artists: heroArtists,
      Availability: activeVariant?.Availability ?? {
        AvailableTrackCount: 0,
        CachedTrackCount: 0,
        LocalTrackCount: 0,
        ProviderOfflineTrackCount: 0,
        ProviderOnlineTrackCount: 0,
        UnavailableTrackCount: 0,
      },
      HasVariants: Boolean(variants.data?.length && variants.data.length > 1),
      Thumb: heroThumb,
      Title: heroTitle,
      TrackCount: activeVariant?.TrackCount ?? 0,
      VariantCount: variants.data?.length ?? 0,
      Year: activeVariant?.Year ?? null,
    } as AlbumListItem);
  const error =
    detail.error ||
    variants.error ||
    trackQuery.error ||
    "Album tracks will render here when the selected variant contains recordings.";

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <DetailHero
        actions={
          <>
            <ActionButton
              icon={<Play className="h-4 w-4" />}
              label="Play album"
              onClick={() => {
                void playAlbum(selectedVariantId);
              }}
              priority="primary"
            />
            <ActionButton
              icon={<Plus className="h-4 w-4" />}
              label="Queue album"
              onClick={() => {
                void queueAlbum(selectedVariantId);
              }}
            />
          </>
        }
        artworkUrl={highResArtworkUrl || lowResArtworkUrl}
        eyebrow="Album detail"
        meta={
          <>
            <MetricPill label={joinArtists(heroArtists)} />
            <MetricPill
              label={formatCount(
                activeVariant?.TrackCount ?? detail.data?.TrackCount ?? 0,
                "track",
              )}
            />
            {activeVariant?.Year && (
              <MetricPill label={String(activeVariant.Year)} />
            )}
          </>
        }
        subtitle={buildAlbumSubtitle(heroAlbum)}
        thumb={heroThumb}
        title={heroTitle}
      />

      {variants.data && variants.data.length > 0 && (
        <div className="flex flex-wrap gap-2 rounded-lg border border-zinc-800 bg-zinc-950 p-3">
          {variants.data.map((variant: AlbumVariantItem) => (
            <button
              className={[
                "inline-flex items-center gap-2 rounded-full border px-3 py-2 text-sm transition",
                variant.AlbumID === selectedVariantId
                  ? "border-zinc-500 bg-zinc-800 text-zinc-50"
                  : "border-zinc-700 bg-zinc-900 text-zinc-300 hover:border-zinc-600 hover:bg-zinc-800",
              ].join(" ")}
              key={variant.AlbumID}
              onClick={() => {
                setSelectedVariantId(variant.AlbumID);
              }}
              type="button"
            >
              <span>{variant.Edition || variant.Title}</span>
              {variant.Year && (
                <small className="text-xs text-zinc-500">{variant.Year}</small>
              )}
            </button>
          ))}
        </div>
      )}

      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={<AlbumTracksEmptyState body={error} />}
          estimateSize={72}
          hasMore={trackQuery.hasMore}
          items={trackQuery.items}
          loading={trackQuery.isLoading}
          loadingMore={trackQuery.isRefreshing}
          onEndReached={() => {
            void trackQuery.fetchNextPage();
          }}
          renderRow={(track) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={`${track.DiscNo}.${track.TrackNo}`}
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
        />
      </div>
    </div>
  );
}
