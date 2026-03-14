export type EnsureOptions = {
  force?: boolean;
};

const inFlightRequests = new Map<string, Promise<void>>();

export function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export function dedupeRequest(key: string, work: () => Promise<void>) {
  const existing = inFlightRequests.get(key);
  if (existing) {
    return existing;
  }

  const request = work().finally(() => {
    inFlightRequests.delete(key);
  });
  inFlightRequests.set(key, request);
  return request;
}
