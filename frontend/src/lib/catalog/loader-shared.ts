export type EnsureOptions = {
  force?: boolean;
};

const inFlightRequests = new Map<string, Promise<void>>();

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
