export type EnsureOptions = {
  force?: boolean;
};

const inFlightRequests = new Map<string, Promise<void>>();

type ChunkedBulkLoadOptions<TItem> = {
  ids: string[];
  chunkSize: number;
  requestKeyPrefix: string;
  loadChunk: (chunk: string[]) => Promise<TItem[]>;
  onChunkLoaded?: (items: TItem[]) => void;
};

export function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export function dedupeRequest<T>(key: string, work: () => Promise<T>) {
  const existing = inFlightRequests.get(key) as Promise<T> | undefined;
  if (existing) {
    return existing;
  }

  const request = work().finally(() => {
    inFlightRequests.delete(key);
  }) as Promise<T>;
  inFlightRequests.set(key, request as Promise<void>);
  return request;
}

export function compactIds(values: string[]) {
  return Array.from(
    new Set(values.map((value) => value.trim()).filter(Boolean)),
  );
}

function chunkIds(values: string[], size: number) {
  const nextSize = Math.max(1, size);
  const chunks: string[][] = [];
  for (let index = 0; index < values.length; index += nextSize) {
    chunks.push(values.slice(index, index + nextSize));
  }
  return chunks;
}

function waitForNextPaint() {
  return new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
}

export async function loadChunkedBulk<TItem>({
  ids,
  chunkSize,
  requestKeyPrefix,
  loadChunk,
  onChunkLoaded,
}: ChunkedBulkLoadOptions<TItem>) {
  const chunks = chunkIds(ids, chunkSize);
  const results: TItem[] = [];

  for (const [index, chunk] of chunks.entries()) {
    const items = await dedupeRequest(
      `${requestKeyPrefix}:${chunk.slice().sort().join(",")}`,
      () => loadChunk(chunk),
    );
    results.push(...items);
    onChunkLoaded?.(items);

    if (index < chunks.length - 1) {
      await waitForNextPaint();
    }
  }

  return results;
}
