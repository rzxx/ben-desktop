import type {
  DetailRecord,
  IdQueryRecord,
  ValueQueryRecord,
} from "./catalog-store-types";

const EMPTY_PAGE_INFO = null;
const EMPTY_OFFSETS: number[] = [];
const EMPTY_IDS: string[] = [];
const EMPTY_VALUES: unknown[] = [];

export const EMPTY_ID_QUERY: IdQueryRecord = {
  error: "",
  ids: EMPTY_IDS,
  inFlightOffsets: EMPTY_OFFSETS,
  isRefreshing: false,
  lastFetchedAt: null,
  loadedOffsets: EMPTY_OFFSETS,
  pageInfo: EMPTY_PAGE_INFO,
  status: "idle",
};

export const EMPTY_VALUE_QUERY: ValueQueryRecord<unknown> = {
  error: "",
  inFlightOffsets: EMPTY_OFFSETS,
  isRefreshing: false,
  items: EMPTY_VALUES,
  lastFetchedAt: null,
  loadedOffsets: EMPTY_OFFSETS,
  pageInfo: EMPTY_PAGE_INFO,
  status: "idle",
};

export const EMPTY_DETAIL_RECORD: DetailRecord<unknown> = {
  data: null,
  error: "",
  inFlight: false,
  isRefreshing: false,
  lastFetchedAt: null,
  status: "idle",
};

export function createIdQueryRecord(): IdQueryRecord {
  return {
    ...EMPTY_ID_QUERY,
    ids: [],
    inFlightOffsets: [],
    loadedOffsets: [],
  };
}

export function createValueQueryRecord<T>(): ValueQueryRecord<T> {
  return {
    ...EMPTY_VALUE_QUERY,
    inFlightOffsets: [],
    items: [],
    loadedOffsets: [],
  } as ValueQueryRecord<T>;
}

export function createDetailRecord<T>(): DetailRecord<T> {
  return {
    ...EMPTY_DETAIL_RECORD,
  } as DetailRecord<T>;
}

export function appendUniqueNumber(values: number[], value: number) {
  if (!values.includes(value)) {
    values.push(value);
  }
}

export function removeNumber(values: number[], value: number) {
  const index = values.indexOf(value);
  if (index >= 0) {
    values.splice(index, 1);
  }
}

export function mergeFrontUnique(existing: string[], incoming: string[]) {
  const seen = new Set(incoming);
  return [...incoming, ...existing.filter((value) => !seen.has(value))];
}

export function mergeAppendUnique(existing: string[], incoming: string[]) {
  const existingSet = new Set(existing);
  return [...existing, ...incoming.filter((value) => !existingSet.has(value))];
}

export function mergeItemsByKey<TItem>(
  existing: TItem[],
  incoming: TItem[],
  getItemKey: (item: TItem) => string,
  mode: "append" | "replace-front",
) {
  if (mode === "replace-front") {
    const incomingKeys = new Set(incoming.map(getItemKey));
    return [
      ...incoming,
      ...existing.filter((item) => !incomingKeys.has(getItemKey(item))),
    ];
  }

  const existingKeys = new Set(existing.map(getItemKey));
  return [
    ...existing,
    ...incoming.filter((item) => !existingKeys.has(getItemKey(item))),
  ];
}
