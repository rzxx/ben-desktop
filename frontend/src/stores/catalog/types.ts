import type {
  AggregateAvailabilitySummary,
  AlbumAvailabilitySummaryItem,
  AlbumListItem,
  AlbumTrackItem,
  AlbumVariantItem,
  ArtistListItem,
  LikedRecordingItem,
  PageInfo,
  PlaylistListItem,
  PlaylistTrackItem,
  RecordingListItem,
  RecordingPlaybackAvailability,
} from "@/lib/api/models";

export type QueryStatus = "idle" | "loading" | "success" | "error";

export type QueryPageRecord<T> = {
  error: string;
  fetchedAt: number | null;
  inFlight: boolean;
  items: T[];
  pageInfo: PageInfo | null;
  stale: boolean;
};

export type BaseQueryRecord = {
  error: string;
  inFlightOffsets: number[];
  isRefreshing: boolean;
  lastFetchedAt: number | null;
  loadedOffsets: number[];
  pageInfo: PageInfo | null;
  status: QueryStatus;
};

export type IdQueryRecord = BaseQueryRecord & {
  ids: string[];
  pages: Record<string, QueryPageRecord<string>>;
};

export type ValueQueryRecord<T> = BaseQueryRecord & {
  getItemKey?: ((item: T) => string) | null;
  items: T[];
  pages: Record<string, QueryPageRecord<T>>;
};

export type DetailRecord<T> = {
  data: T | null;
  error: string;
  inFlight: boolean;
  isRefreshing: boolean;
  lastFetchedAt: number | null;
  stale: boolean;
  status: QueryStatus;
};

export type DetailKind =
  | "album"
  | "albumVariants"
  | "artist"
  | "playlistSummary";

export type CatalogValueQueryItem =
  | AlbumListItem
  | AlbumTrackItem
  | LikedRecordingItem
  | PlaylistTrackItem
  | RecordingListItem;

export type CatalogTrackLookupItem =
  | LikedRecordingItem
  | PlaylistTrackItem
  | RecordingListItem;

export type CatalogStoreState = {
  albumsById: Record<string, AlbumListItem>;
  albumAvailabilityByAlbumId: Record<
    string,
    DetailRecord<AggregateAvailabilitySummary>
  >;
  albumDetails: Record<string, DetailRecord<AlbumListItem>>;
  albumVariants: Record<string, DetailRecord<AlbumVariantItem[]>>;
  artistDetails: Record<string, DetailRecord<ArtistListItem>>;
  artistsById: Record<string, ArtistListItem>;
  idQueries: Record<string, IdQueryRecord>;
  playlistSummaries: Record<string, DetailRecord<PlaylistListItem>>;
  playlistTrackItemsByItemId: Record<string, PlaylistTrackItem>;
  playlistsById: Record<string, PlaylistListItem>;
  trackItemsByLibraryRecordingId: Record<string, CatalogTrackLookupItem>;
  trackItemsByRecordingId: Record<string, CatalogTrackLookupItem>;
  trackAvailabilityByRecordingId: Record<
    string,
    DetailRecord<RecordingPlaybackAvailability>
  >;
  valueQueries: Record<string, ValueQueryRecord<CatalogValueQueryItem>>;
};

export type CatalogStoreActions = {
  invalidateAlbumAvailability: (albumIds: string[]) => void;
  invalidateDetail: (kind: DetailKind, id: string) => void;
  invalidateIdQuery: (
    key: string,
    options?: { clear?: boolean; dropAfterOffset?: number | null },
  ) => void;
  invalidateTrackAvailability: (recordingIds: string[]) => void;
  invalidateValueQuery: (
    key: string,
    options?: { clear?: boolean; dropAfterOffset?: number | null },
  ) => void;
  markAlbumAvailabilityError: (albumIds: string[], message: string) => void;
  markAlbumAvailabilityLoading: (
    albumIds: string[],
    options?: { refreshing?: boolean },
  ) => void;
  markDetailError: (kind: DetailKind, id: string, message: string) => void;
  markDetailLoading: (
    kind: DetailKind,
    id: string,
    options?: { refreshing?: boolean },
  ) => void;
  markIdQueryError: (key: string, message: string, offset: number) => void;
  markIdQueryLoading: (
    key: string,
    offset: number,
    options?: { refreshing?: boolean },
  ) => void;
  markTrackAvailabilityError: (recordingIds: string[], message: string) => void;
  markTrackAvailabilityLoading: (
    recordingIds: string[],
    options?: { refreshing?: boolean },
  ) => void;
  markValueQueryError: (key: string, message: string, offset: number) => void;
  markValueQueryLoading: (
    key: string,
    offset: number,
    options?: { refreshing?: boolean },
  ) => void;
  removeIdQueryInFlight: (key: string, offset: number) => void;
  removeValueQueryInFlight: (key: string, offset: number) => void;
  setAlbumAvailability: (
    items: AlbumAvailabilitySummaryItem[],
    fetchedAt?: number,
  ) => void;
  setAlbumDetail: (
    albumId: string,
    album: AlbumListItem,
    fetchedAt?: number,
  ) => void;
  setAlbumVariants: (
    albumId: string,
    variants: AlbumVariantItem[],
    fetchedAt?: number,
  ) => void;
  setArtistDetail: (
    artistId: string,
    artist: ArtistListItem,
    fetchedAt?: number,
  ) => void;
  setIdQueryPage: (
    key: string,
    ids: string[],
    pageInfo: PageInfo,
    offset: number,
    mode?: "append" | "replace-front",
    fetchedAt?: number,
  ) => void;
  setPlaylistSummary: (
    playlistId: string,
    playlist: PlaylistListItem,
    fetchedAt?: number,
  ) => void;
  setTrackAvailability: (
    items: RecordingPlaybackAvailability[],
    fetchedAt?: number,
  ) => void;
  setValueQueryPage: <TItem extends CatalogValueQueryItem>(
    key: string,
    items: TItem[],
    pageInfo: PageInfo,
    offset: number,
    getItemKey: (item: TItem) => string,
    mode?: "append" | "replace-front",
    fetchedAt?: number,
  ) => void;
  upsertAlbums: (albums: AlbumListItem[]) => void;
  upsertArtists: (artists: ArtistListItem[]) => void;
  upsertPlaylists: (playlists: PlaylistListItem[]) => void;
};

export type CatalogStore = CatalogStoreState & CatalogStoreActions;
