import {
  EMPTY_DETAIL_RECORD,
  EMPTY_ID_QUERY,
  EMPTY_VALUE_QUERY,
} from "./records";
import type {
  CatalogStoreState,
  DetailRecord,
  DetailKind,
  ValueQueryRecord,
} from "./types";

export function getDetailContainer(state: CatalogStoreState, kind: DetailKind) {
  switch (kind) {
    case "album":
      return state.albumDetails;
    case "albumVariants":
      return state.albumVariants;
    case "artist":
      return state.artistDetails;
    case "playlistSummary":
      return state.playlistSummaries;
  }
}

export function getIdQuery(state: CatalogStoreState, key: string) {
  return state.idQueries[key] ?? EMPTY_ID_QUERY;
}

export function getValueQuery<T>(state: CatalogStoreState, key: string) {
  return (
    (state.valueQueries[key] as ValueQueryRecord<T> | undefined) ??
    (EMPTY_VALUE_QUERY as ValueQueryRecord<T>)
  );
}

export function getDetailRecord<T>(
  record: Record<string, DetailRecord<T>>,
  id: string,
) {
  return record[id] ?? (EMPTY_DETAIL_RECORD as DetailRecord<T>);
}

