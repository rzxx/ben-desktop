import type { PageInfo } from "@/lib/api/models";
import type {
  DetailRecord,
  IdQueryRecord,
  QueryPageRecord,
  QueryStatus,
  ValueQueryRecord,
} from "./types";

const EMPTY_PAGE_INFO = null;
const EMPTY_OFFSETS: number[] = [];
const EMPTY_IDS: string[] = [];
const EMPTY_VALUES: unknown[] = [];
const EMPTY_ID_PAGES: Record<string, QueryPageRecord<string>> = {};
const EMPTY_VALUE_PAGES: Record<string, QueryPageRecord<unknown>> = {};

export const EMPTY_ID_QUERY: IdQueryRecord = {
  error: "",
  ids: EMPTY_IDS,
  inFlightOffsets: EMPTY_OFFSETS,
  isRefreshing: false,
  lastFetchedAt: null,
  loadedOffsets: EMPTY_OFFSETS,
  pageInfo: EMPTY_PAGE_INFO,
  pages: EMPTY_ID_PAGES,
  status: "idle",
};

export const EMPTY_VALUE_QUERY: ValueQueryRecord<unknown> = {
  error: "",
  getItemKey: null,
  inFlightOffsets: EMPTY_OFFSETS,
  isRefreshing: false,
  items: EMPTY_VALUES,
  lastFetchedAt: null,
  loadedOffsets: EMPTY_OFFSETS,
  pageInfo: EMPTY_PAGE_INFO,
  pages: EMPTY_VALUE_PAGES,
  status: "idle",
};

export const EMPTY_DETAIL_RECORD: DetailRecord<unknown> = {
  data: null,
  error: "",
  inFlight: false,
  isRefreshing: false,
  lastFetchedAt: null,
  stale: false,
  status: "idle",
};

export function createIdQueryRecord(): IdQueryRecord {
  return {
    ...EMPTY_ID_QUERY,
    ids: [],
    inFlightOffsets: [],
    loadedOffsets: [],
    pages: {},
  };
}

export function createValueQueryRecord<T>(): ValueQueryRecord<T> {
  return {
    ...EMPTY_VALUE_QUERY,
    inFlightOffsets: [],
    items: [],
    loadedOffsets: [],
    pages: {},
  } as ValueQueryRecord<T>;
}

export function createDetailRecord<T>(): DetailRecord<T> {
  return {
    ...EMPTY_DETAIL_RECORD,
  } as DetailRecord<T>;
}

export function createQueryPageRecord<T>(): QueryPageRecord<T> {
  return {
    error: "",
    fetchedAt: null,
    inFlight: false,
    items: [],
    pageInfo: null,
    stale: false,
  };
}

function sortOffsets<T>(pages: Record<string, QueryPageRecord<T>>) {
  return Object.keys(pages)
    .map((offset) => Number(offset))
    .filter((offset) => Number.isFinite(offset))
    .sort((left, right) => left - right);
}

function resolveStatus(
  hasData: boolean,
  inFlightOffsets: number[],
  error: string,
): QueryStatus {
  if (inFlightOffsets.length > 0) {
    return hasData ? "success" : "loading";
  }
  if (error) {
    return hasData ? "success" : "error";
  }
  if (hasData) {
    return "success";
  }
  return "idle";
}

export function rebuildIdQueryRecord(record: IdQueryRecord) {
  const offsets = sortOffsets(record.pages);
  const ids: string[] = [];
  const seen = new Set<string>();
  let pageInfo: PageInfo | null = null;
  let lastFetchedAt: number | null = null;
  let error = "";
  const inFlightOffsets: number[] = [];
  const loadedOffsets: number[] = [];

  for (const offset of offsets) {
    const page = record.pages[String(offset)];
    if (!page) {
      continue;
    }
    for (const id of page.items) {
      if (seen.has(id)) {
        continue;
      }
      seen.add(id);
      ids.push(id);
    }
    if (page.pageInfo) {
      pageInfo = page.pageInfo;
    }
    if (page.inFlight) {
      inFlightOffsets.push(offset);
    }
    if (page.fetchedAt !== null) {
      if (!page.stale) {
        loadedOffsets.push(offset);
      }
      lastFetchedAt = Math.max(lastFetchedAt ?? 0, page.fetchedAt);
    }
    if (!error && page.error) {
      error = page.error;
    }
  }

  record.ids = ids;
  record.pageInfo = pageInfo;
  record.inFlightOffsets = inFlightOffsets;
  record.loadedOffsets = loadedOffsets;
  record.lastFetchedAt = lastFetchedAt;
  record.error = error;
  record.isRefreshing = inFlightOffsets.length > 0 && ids.length > 0;
  record.status = resolveStatus(ids.length > 0, inFlightOffsets, error);
}

export function rebuildValueQueryRecord<T>(record: ValueQueryRecord<T>) {
  const offsets = sortOffsets(record.pages);
  const items: T[] = [];
  const seen = new Set<string>();
  let pageInfo: PageInfo | null = null;
  let lastFetchedAt: number | null = null;
  let error = "";
  const inFlightOffsets: number[] = [];
  const loadedOffsets: number[] = [];

  for (const offset of offsets) {
    const page = record.pages[String(offset)];
    if (!page) {
      continue;
    }
    for (const item of page.items) {
      const key = record.getItemKey?.(item);
      if (key) {
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
      }
      items.push(item);
    }
    if (page.pageInfo) {
      pageInfo = page.pageInfo;
    }
    if (page.inFlight) {
      inFlightOffsets.push(offset);
    }
    if (page.fetchedAt !== null) {
      if (!page.stale) {
        loadedOffsets.push(offset);
      }
      lastFetchedAt = Math.max(lastFetchedAt ?? 0, page.fetchedAt);
    }
    if (!error && page.error) {
      error = page.error;
    }
  }

  record.items = items;
  record.pageInfo = pageInfo;
  record.inFlightOffsets = inFlightOffsets;
  record.loadedOffsets = loadedOffsets;
  record.lastFetchedAt = lastFetchedAt;
  record.error = error;
  record.isRefreshing = inFlightOffsets.length > 0 && items.length > 0;
  record.status = resolveStatus(items.length > 0, inFlightOffsets, error);
}

export function pageHeadChanged<T>(
  existing: QueryPageRecord<T> | undefined,
  incoming: T[],
  getItemKey: (item: T) => string,
  nextTotal?: number | null,
) {
  if (!existing) {
    return false;
  }
  if ((existing.pageInfo?.Total ?? null) !== (nextTotal ?? null)) {
    return true;
  }
  if (existing.items.length !== incoming.length) {
    return true;
  }
  for (let index = 0; index < incoming.length; index += 1) {
    const current = existing.items[index];
    const next = incoming[index];
    if (!current || getItemKey(current) !== getItemKey(next)) {
      return true;
    }
  }
  return false;
}

export function pageHeadChangedIds(
  existing: QueryPageRecord<string> | undefined,
  incoming: string[],
  nextTotal?: number | null,
) {
  if (!existing) {
    return false;
  }
  if ((existing.pageInfo?.Total ?? null) !== (nextTotal ?? null)) {
    return true;
  }
  if (existing.items.length !== incoming.length) {
    return true;
  }
  for (let index = 0; index < incoming.length; index += 1) {
    if (existing.items[index] !== incoming[index]) {
      return true;
    }
  }
  return false;
}

export function dropPagesAfterOffset<T>(
  pages: Record<string, QueryPageRecord<T>>,
  offset: number,
) {
  for (const key of Object.keys(pages)) {
    if (Number(key) > offset) {
      delete pages[key];
    }
  }
}
