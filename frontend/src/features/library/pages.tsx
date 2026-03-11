import { type ReactNode, useCallback, useEffect, useState } from "react";
import { Link } from "wouter";
import {
  Clock3,
  Disc3,
  ListMusic,
  Play,
  Plus,
  Shapes,
  UsersRound,
} from "lucide-react";
import {
  type AlbumListItem,
  type AlbumTrackItem,
  type AlbumVariantItem,
  type ArtistListItem,
  type LikedRecordingItem,
  type PlaylistListItem,
  type PlaylistTrackItem,
  type RecordingListItem,
  getAlbum,
  getArtist,
  getPlaylistSummary,
  listAlbumTracksPage,
  listAlbumVariants,
  listAlbumsPage,
  listArtistAlbumsPage,
  listArtistsPage,
  listLikedRecordingsPage,
  listPlaylistTracksPage,
  listPlaylistsPage,
  listTracksPage,
} from "../../shared/lib/desktop";
import {
  artistLetter,
  availabilityLabel,
  formatCount,
  formatDuration,
  formatRelativeDate,
  joinArtists,
} from "../../shared/lib/format";
import { useBlobUrl } from "../../shared/lib/use-blob-url";
import { usePagedQuery } from "../../shared/lib/use-paged-query";
import { ArtworkTile } from "../../shared/ui/ArtworkTile";
import { VirtualCardGrid } from "../../shared/ui/VirtualCardGrid";
import { VirtualRows } from "../../shared/ui/VirtualRows";
import { routes, playlistRoute } from "../../app/router/routes";
import { usePlaybackStore } from "../playback/store";

function PageHeader({
  eyebrow,
  title,
  description,
  meta,
  actions,
}: {
  eyebrow: string;
  title: string;
  description: string;
  meta?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(135deg,rgba(249,115,22,0.12),transparent_45%),linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-[0.68rem] uppercase tracking-[0.35em] text-white/35">
            {eyebrow}
          </p>
          <h1 className="mt-3 text-3xl font-semibold text-white">{title}</h1>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-white/55">
            {description}
          </p>
          {meta && <div className="mt-4 flex flex-wrap gap-2">{meta}</div>}
        </div>
        {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      </div>
    </section>
  );
}

function ActionButton({
  icon,
  label,
  onClick,
  priority = "secondary",
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  priority?: "primary" | "secondary";
}) {
  return (
    <button
      className={`action-button ${priority === "primary" ? "is-primary" : ""}`}
      onClick={onClick}
      type="button"
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

function EmptyState({
  icon,
  title,
  body,
}: {
  icon: ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="rounded-[1.6rem] border border-dashed border-white/10 bg-black/10 px-8 py-10 text-center">
      <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white/40">
        {icon}
      </div>
      <h2 className="text-lg font-semibold text-white/90">{title}</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-white/50">{body}</p>
    </div>
  );
}

function DetailHero({
  eyebrow,
  title,
  subtitle,
  blobId,
  meta,
  actions,
}: {
  eyebrow: string;
  title: string;
  subtitle: string;
  blobId?: string;
  meta?: ReactNode;
  actions?: ReactNode;
}) {
  const artworkUrl = useBlobUrl(blobId);

  return (
    <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(135deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
      <div className="flex flex-col gap-6 xl:flex-row">
        <ArtworkTile
          alt={title}
          className="h-52 w-52 shrink-0"
          src={artworkUrl}
          title={title}
        />
        <div className="flex min-w-0 flex-1 flex-col justify-end">
          <p className="text-[0.68rem] uppercase tracking-[0.35em] text-white/35">
            {eyebrow}
          </p>
          <h1 className="mt-3 text-4xl font-semibold text-white">{title}</h1>
          <p className="mt-4 max-w-3xl text-sm leading-6 text-white/55">
            {subtitle}
          </p>
          {meta && <div className="mt-4 flex flex-wrap gap-2">{meta}</div>}
          {actions && <div className="mt-5 flex flex-wrap gap-2">{actions}</div>}
        </div>
      </div>
    </section>
  );
}

function MetricPill({ label }: { label: string }) {
  return (
    <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs uppercase tracking-[0.2em] text-white/52">
      {label}
    </span>
  );
}

function AlbumCard({ album }: { album: AlbumListItem }) {
  const artworkUrl = useBlobUrl(album.ThumbBlobID);

  return (
    <Link
      className="album-card group block rounded-[1.6rem] border border-white/8 bg-black/10 p-3 transition duration-200 hover:-translate-y-1 hover:border-white/18 hover:bg-white/8"
      href={routes.album(album.AlbumID)}
    >
      <ArtworkTile
        alt={album.Title}
        src={artworkUrl}
        title={album.Title}
      />
      <div className="mt-4">
        <h2 className="truncate text-base font-semibold text-white">
          {album.Title}
        </h2>
        <p className="mt-1 truncate text-sm text-white/50">
          {joinArtists(album.Artists)}
        </p>
        <div className="mt-3 flex items-center justify-between text-xs uppercase tracking-[0.2em] text-white/35">
          <span>{formatCount(album.TrackCount, "track")}</span>
          <span>
            {availabilityLabel(
              album.Availability?.LocalTrackCount ? "LOCAL" : undefined,
            )}
          </span>
        </div>
      </div>
    </Link>
  );
}

function ArtistCard({ artist }: { artist: ArtistListItem }) {
  return (
    <Link
      className="artist-card group flex h-full flex-col rounded-[1.6rem] border border-white/8 bg-black/10 p-4 transition duration-200 hover:-translate-y-1 hover:border-white/18 hover:bg-white/8"
      href={routes.artist(artist.ArtistID)}
    >
      <div className="mb-5 flex h-28 w-28 items-center justify-center rounded-full border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.35),transparent_60%),rgba(255,255,255,0.05)] text-5xl font-semibold text-white/85">
        {artistLetter(artist.Name)}
      </div>
      <h2 className="truncate text-lg font-semibold text-white">{artist.Name}</h2>
      <p className="mt-2 text-sm text-white/50">
        {formatCount(artist.AlbumCount, "album")} •{" "}
        {formatCount(artist.TrackCount, "track")}
      </p>
    </Link>
  );
}

function PlaylistCard({ playlist }: { playlist: PlaylistListItem }) {
  const artworkUrl = useBlobUrl(playlist.ThumbBlobID);
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playLiked = usePlaybackStore((state) => state.playLiked);

  const isLiked = playlist.Kind === "liked";

  return (
    <div className="playlist-row flex items-center gap-4 rounded-[1.35rem] border border-white/8 bg-black/10 px-4 py-3">
      <Link
        className="flex min-w-0 flex-1 items-center gap-4"
        href={playlistRoute(playlist)}
      >
        <ArtworkTile
          alt={playlist.Name}
          className="h-18 w-18 shrink-0"
          src={artworkUrl}
          title={playlist.Name}
        />
        <div className="min-w-0">
          <p className="text-[0.7rem] uppercase tracking-[0.28em] text-white/35">
            {isLiked ? "Reserved" : "Playlist"}
          </p>
          <h2 className="truncate text-base font-semibold text-white">
            {playlist.Name}
          </h2>
          <p className="truncate text-sm text-white/50">
            {formatCount(playlist.ItemCount, "track")} • Updated{" "}
            {formatRelativeDate(playlist.UpdatedAt)}
          </p>
        </div>
      </Link>
      <div className="flex gap-2">
        <button
          className="row-action"
          onClick={() => {
            if (isLiked) {
              void playLiked();
            } else {
              void playPlaylist(playlist.PlaylistID);
            }
          }}
          type="button"
        >
          <Play className="h-4 w-4" />
        </button>
        {!isLiked && (
          <button
            className="row-action"
            onClick={() => {
              void queuePlaylist(playlist.PlaylistID);
            }}
            type="button"
          >
            <Plus className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
}

function TrackRow({
  indexLabel,
  title,
  subtitle,
  durationMs,
  availabilityState,
  onPlay,
  onQueue,
}: {
  indexLabel: string;
  title: string;
  subtitle: string;
  durationMs: number;
  availabilityState?: string;
  onPlay: () => void;
  onQueue: () => void;
}) {
  return (
    <div className="track-row flex h-full items-center gap-4 rounded-[1.1rem] px-3">
      <div className="flex w-16 shrink-0 justify-center text-xs uppercase tracking-[0.25em] text-white/30">
        {indexLabel}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-white">{title}</div>
        <div className="truncate text-xs text-white/45">{subtitle}</div>
      </div>
      <div className="hidden w-28 shrink-0 justify-center text-xs text-white/38 xl:flex">
        {availabilityLabel(availabilityState)}
      </div>
      <div className="w-16 shrink-0 text-right text-xs tabular-nums text-white/38">
        {formatDuration(durationMs)}
      </div>
      <div className="flex shrink-0 gap-2">
        <button className="row-action" onClick={onPlay} type="button">
          <Play className="h-4 w-4" />
        </button>
        <button className="row-action" onClick={onQueue} type="button">
          <Plus className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

function buildAlbumSubtitle(album: AlbumListItem) {
  const artists = joinArtists(album.Artists);
  const year = album.Year ? ` • ${album.Year}` : "";
  return `${artists}${year}`;
}

export function AlbumsPage() {
  const query = usePagedQuery({
    key: "albums",
    fetchPage: listAlbumsPage,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <PageHeader
        description="Default view. Browse the library by release, then jump into album detail pages and playback."
        eyebrow="Albums"
        meta={
          <MetricPill
            label={formatCount(query.pageInfo?.Total ?? query.items.length, "album")}
          />
        }
        title="Albums"
      />
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
          emptyState={
            <EmptyState
              body="Albums will appear here when the core catalog has materialized media."
              icon={<Disc3 className="h-5 w-5" />}
              title="No albums yet"
            />
          }
          getItemKey={(album) => album.AlbumID}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.loading}
          loadingMore={query.loadingMore}
          minColumnWidth={210}
          onEndReached={query.loadMore}
          renderCard={(album) => <AlbumCard album={album} />}
          rowHeight={320}
        />
      </div>
    </div>
  );
}

export function ArtistsPage() {
  const query = usePagedQuery({
    key: "artists",
    fetchPage: listArtistsPage,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <PageHeader
        description="Artist directory with album and track counts. Open an artist to inspect their album catalog."
        eyebrow="Artists"
        meta={
          <MetricPill
            label={formatCount(query.pageInfo?.Total ?? query.items.length, "artist")}
          />
        }
        title="Artists"
      />
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
          emptyState={
            <EmptyState
              body="Artist entries will appear here once library metadata is available."
              icon={<UsersRound className="h-5 w-5" />}
              title="No artists yet"
            />
          }
          getItemKey={(artist) => artist.ArtistID}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.loading}
          loadingMore={query.loadingMore}
          minColumnWidth={220}
          onEndReached={query.loadMore}
          renderCard={(artist) => <ArtistCard artist={artist} />}
          rowHeight={250}
        />
      </div>
    </div>
  );
}

export function TracksPage() {
  const query = usePagedQuery({
    key: "tracks",
    fetchPage: listTracksPage,
  });
  const playRecording = usePlaybackStore((state) => state.playRecording);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <PageHeader
        description="Flat track browser with virtualized rows and direct play or queue actions."
        eyebrow="Tracks"
        meta={
          <MetricPill
            label={formatCount(query.pageInfo?.Total ?? query.items.length, "track")}
          />
        }
        title="Tracks"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <EmptyState
              body="Track rows appear here after the core runtime exposes recordings."
              icon={<ListMusic className="h-5 w-5" />}
              title="No tracks yet"
            />
          }
          estimateSize={72}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.loading}
          loadingMore={query.loadingMore}
          onEndReached={query.loadMore}
          renderRow={(track: RecordingListItem, index) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              onPlay={() => {
                void playRecording(track.RecordingID);
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

export function PlaylistsPage() {
  const query = usePagedQuery({
    key: "playlists",
    fetchPage: listPlaylistsPage,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <PageHeader
        description="Playlists, including the reserved liked view, with direct navigation into each playlist detail screen."
        eyebrow="Playlists"
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "playlist",
            )}
          />
        }
        title="Playlists"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <EmptyState
              body="Playlist records will appear here once the library contains playlists."
              icon={<Shapes className="h-5 w-5" />}
              title="No playlists yet"
            />
          }
          estimateSize={98}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.loading}
          loadingMore={query.loadingMore}
          onEndReached={query.loadMore}
          renderRow={(playlist: PlaylistListItem) => (
            <PlaylistCard playlist={playlist} />
          )}
        />
      </div>
    </div>
  );
}

export function AlbumDetailPage({ albumId }: { albumId: string }) {
  const [detail, setDetail] = useState<AlbumListItem | null>(null);
  const [variants, setVariants] = useState<AlbumVariantItem[]>([]);
  const [selectedVariantId, setSelectedVariantId] = useState(albumId);
  const [error, setError] = useState("");
  const playAlbum = usePlaybackStore((state) => state.playAlbum);
  const queueAlbum = usePlaybackStore((state) => state.queueAlbum);
  const playRecording = usePlaybackStore((state) => state.playRecording);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);

  useEffect(() => {
    let active = true;
    void Promise.all([getAlbum(albumId), listAlbumVariants(albumId)])
      .then(([album, page]) => {
        if (!active) {
          return;
        }
        setDetail(album);
        setVariants(page.Items);
        setSelectedVariantId(page.Items[0]?.AlbumID ?? album.AlbumID);
        setError("");
      })
      .catch((fetchError: unknown) => {
        if (!active) {
          return;
        }
        setError(
          fetchError instanceof Error ? fetchError.message : String(fetchError),
        );
      });
    return () => {
      active = false;
    };
  }, [albumId]);

  const activeVariant =
    variants.find((variant) => variant.AlbumID === selectedVariantId) ?? null;
  const fetchAlbumTracksPage = useCallback(
    (offset: number) => listAlbumTracksPage(selectedVariantId, offset),
    [selectedVariantId],
  );
  const trackQuery = usePagedQuery({
    key: `album:${selectedVariantId}`,
    fetchPage: fetchAlbumTracksPage,
  });

  const heroTitle = activeVariant?.Title ?? detail?.Title ?? "Album";
  const heroArtists = activeVariant?.Artists ?? detail?.Artists ?? [];
  const heroBlobId = activeVariant?.ThumbBlobID || detail?.ThumbBlobID;
  const heroAvailability = activeVariant?.Availability ??
    detail?.Availability ?? {
      LocalTrackCount: 0,
      ProviderOnlineTrackCount: 0,
      ProviderOfflineTrackCount: 0,
      AvailableTrackCount: 0,
      UnavailableTrackCount: 0,
    };
  const heroAlbum =
    detail ??
    ({
      AlbumID: selectedVariantId,
      AlbumClusterID: selectedVariantId,
      Title: heroTitle,
      Artists: heroArtists,
      Year: activeVariant?.Year ?? null,
      TrackCount: activeVariant?.TrackCount ?? 0,
      ThumbBlobID: heroBlobId ?? "",
      VariantCount: variants.length,
      HasVariants: variants.length > 1,
      Availability: heroAvailability,
    } as AlbumListItem);

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
        blobId={heroBlobId}
        eyebrow="Album detail"
        meta={
          <>
            <MetricPill label={joinArtists(heroArtists)} />
            <MetricPill
              label={formatCount(activeVariant?.TrackCount ?? detail?.TrackCount ?? 0, "track")}
            />
            {activeVariant?.Year && <MetricPill label={String(activeVariant.Year)} />}
          </>
        }
        subtitle={buildAlbumSubtitle(heroAlbum)}
        title={heroTitle}
      />

      {variants.length > 0 && (
        <div className="flex flex-wrap gap-2 rounded-[1.4rem] border border-white/8 bg-black/10 p-3">
          {variants.map((variant) => (
            <button
              className={`variant-chip ${variant.AlbumID === selectedVariantId ? "is-active" : ""}`}
              key={variant.AlbumID}
              onClick={() => {
                setSelectedVariantId(variant.AlbumID);
              }}
              type="button"
            >
              <span>{variant.Edition || variant.Title}</span>
              {variant.Year && <small>{variant.Year}</small>}
            </button>
          ))}
        </div>
      )}

      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <EmptyState
              body={error || "Album tracks will render here when the selected variant contains recordings."}
              icon={<Clock3 className="h-5 w-5" />}
              title="No album tracks"
            />
          }
          estimateSize={72}
          hasMore={trackQuery.hasMore}
          items={trackQuery.items}
          loading={trackQuery.loading}
          loadingMore={trackQuery.loadingMore}
          onEndReached={trackQuery.loadMore}
          renderRow={(track: AlbumTrackItem) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={`${track.DiscNo}.${track.TrackNo}`}
              onPlay={() => {
                void playRecording(track.RecordingID);
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

export function ArtistDetailPage({ artistId }: { artistId: string }) {
  const [artist, setArtist] = useState<ArtistListItem | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    void getArtist(artistId)
      .then((item) => {
        if (!active) {
          return;
        }
        setArtist(item);
        setError("");
      })
      .catch((fetchError: unknown) => {
        if (!active) {
          return;
        }
        setError(
          fetchError instanceof Error ? fetchError.message : String(fetchError),
        );
      });
    return () => {
      active = false;
    };
  }, [artistId]);

  const fetchArtistAlbumsPage = useCallback(
    (offset: number) => listArtistAlbumsPage(artistId, offset),
    [artistId],
  );
  const albumQuery = usePagedQuery({
    key: `artist-albums:${artistId}`,
    fetchPage: fetchArtistAlbumsPage,
  });

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <section className="rounded-[1.6rem] border border-white/8 bg-[linear-gradient(180deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-6">
        <div className="flex flex-col gap-5 xl:flex-row xl:items-end">
          <div className="flex h-36 w-36 items-center justify-center rounded-full border border-white/10 bg-[radial-gradient(circle_at_top_left,rgba(249,115,22,0.35),transparent_60%),rgba(255,255,255,0.05)] text-6xl font-semibold text-white/85">
            {artistLetter(artist?.Name ?? "Artist")}
          </div>
          <div>
            <p className="text-[0.68rem] uppercase tracking-[0.35em] text-white/35">
              Artist detail
            </p>
            <h1 className="mt-3 text-4xl font-semibold text-white">
              {artist?.Name ?? "Artist"}
            </h1>
            <div className="mt-4 flex flex-wrap gap-2">
              <MetricPill
                label={formatCount(artist?.AlbumCount ?? 0, "album")}
              />
              <MetricPill
                label={formatCount(artist?.TrackCount ?? 0, "track")}
              />
            </div>
            {error && <p className="mt-4 text-sm text-amber-300">{error}</p>}
          </div>
        </div>
      </section>
      <div className="min-h-0 flex-1">
        <VirtualCardGrid
          emptyState={
            <EmptyState
              body="Artist albums will appear here when the artist has catalog entries."
              icon={<Disc3 className="h-5 w-5" />}
              title="No albums for this artist"
            />
          }
          getItemKey={(album) => album.AlbumID}
          hasMore={albumQuery.hasMore}
          items={albumQuery.items}
          loading={albumQuery.loading}
          loadingMore={albumQuery.loadingMore}
          minColumnWidth={210}
          onEndReached={albumQuery.loadMore}
          renderCard={(album) => <AlbumCard album={album} />}
          rowHeight={320}
        />
      </div>
    </div>
  );
}

export function PlaylistDetailPage({ playlistId }: { playlistId: string }) {
  const [playlist, setPlaylist] = useState<PlaylistListItem | null>(null);
  const [error, setError] = useState("");
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playRecording = usePlaybackStore((state) => state.playRecording);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);

  useEffect(() => {
    let active = true;
    void getPlaylistSummary(playlistId)
      .then((item) => {
        if (!active) {
          return;
        }
        setPlaylist(item);
        setError("");
      })
      .catch((fetchError: unknown) => {
        if (!active) {
          return;
        }
        setError(
          fetchError instanceof Error ? fetchError.message : String(fetchError),
        );
      });
    return () => {
      active = false;
    };
  }, [playlistId]);

  const fetchPlaylistTracksPage = useCallback(
    (offset: number) => listPlaylistTracksPage(playlistId, offset),
    [playlistId],
  );
  const trackQuery = usePagedQuery({
    key: `playlist:${playlistId}`,
    fetchPage: fetchPlaylistTracksPage,
  });

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
        blobId={playlist?.ThumbBlobID}
        eyebrow="Playlist detail"
        meta={
          <>
            <MetricPill
              label={formatCount(
                playlist?.ItemCount ?? trackQuery.pageInfo?.Total ?? 0,
                "track",
              )}
            />
            <MetricPill label={formatRelativeDate(playlist?.UpdatedAt)} />
          </>
        }
        subtitle="Playlist header with track list below. This view stays read-only in this slice."
        title={playlist?.Name ?? "Playlist"}
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <EmptyState
              body={error || "Playlist tracks will render here when items exist."}
              icon={<ListMusic className="h-5 w-5" />}
              title="No playlist tracks"
            />
          }
          estimateSize={72}
          hasMore={trackQuery.hasMore}
          items={trackQuery.items}
          loading={trackQuery.loading}
          loadingMore={trackQuery.loadingMore}
          onEndReached={trackQuery.loadMore}
          renderRow={(track: PlaylistTrackItem, index) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              onPlay={() => {
                void playRecording(track.RecordingID);
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

export function LikedPlaylistPage() {
  const playLiked = usePlaybackStore((state) => state.playLiked);
  const playRecording = usePlaybackStore((state) => state.playRecording);
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const query = usePagedQuery({
    key: "liked",
    fetchPage: listLikedRecordingsPage,
  });

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
            label={formatCount(query.pageInfo?.Total ?? query.items.length, "track")}
          />
        }
        subtitle="Special liked songs view backed by the reserved playlist in core."
        title="Liked songs"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          emptyState={
            <EmptyState
              body="Liked recordings will appear here when tracks are liked in other surfaces."
              icon={<ListMusic className="h-5 w-5" />}
              title="No liked songs"
            />
          }
          estimateSize={72}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.loading}
          loadingMore={query.loadingMore}
          onEndReached={query.loadMore}
          renderRow={(track: LikedRecordingItem, index) => (
            <TrackRow
              availabilityState={track.Availability.State}
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              onPlay={() => {
                void playRecording(track.RecordingID);
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
