import type {
  AlbumListItem,
  AlbumTrackItem,
  AlbumVariantItem,
  ArtistListItem,
  LikedRecordingItem,
  PageInfo,
  PlaylistListItem,
  PlaylistTrackItem,
  RecordingListItem,
} from "@/lib/api/models";

export type QueryStatus = "idle" | "loading" | "success" | "error";

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
};

export type ValueQueryRecord<T> = BaseQueryRecord & {
  items: T[];
};

export type DetailRecord<T> = {
  data: T | null;
  error: string;
  inFlight: boolean;
  isRefreshing: boolean;
  lastFetchedAt: number | null;
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

export type CatalogStoreState = {
  albumsById: Record<string, AlbumListItem>;
  albumDetails: Record<string, DetailRecord<AlbumListItem>>;
  albumVariants: Record<string, DetailRecord<AlbumVariantItem[]>>;
  artistDetails: Record<string, DetailRecord<ArtistListItem>>;
  artistsById: Record<string, ArtistListItem>;
  idQueries: Record<string, IdQueryRecord>;
  playlistSummaries: Record<string, DetailRecord<PlaylistListItem>>;
  playlistsById: Record<string, PlaylistListItem>;
  valueQueries: Record<string, ValueQueryRecord<CatalogValueQueryItem>>;
};

export type CatalogStoreActions = {
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
  markValueQueryError: (key: string, message: string, offset: number) => void;
  markValueQueryLoading: (
    key: string,
    offset: number,
    options?: { refreshing?: boolean },
  ) => void;
  removeIdQueryInFlight: (key: string, offset: number) => void;
  removeValueQueryInFlight: (key: string, offset: number) => void;
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
  setValueQueryPage: <TItem>(
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
