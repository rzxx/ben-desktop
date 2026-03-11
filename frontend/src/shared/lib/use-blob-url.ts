import { useEffect, useState } from "react";
import { resolveBlobURL, resolveRecordingArtworkURL } from "./desktop";

const blobUrlCache = new Map<string, string>();
const blobPendingCache = new Map<string, Promise<string>>();
const recordingArtworkCache = new Map<string, string>();
const recordingArtworkPending = new Map<string, Promise<string>>();

async function resolveCached(
  key: string,
  cache: Map<string, string>,
  pending: Map<string, Promise<string>>,
  load: () => Promise<string>,
) {
  if (cache.has(key)) {
    return cache.get(key) ?? "";
  }
  if (!pending.has(key)) {
    pending.set(
      key,
      load()
        .then((value) => {
          cache.set(key, value);
          pending.delete(key);
          return value;
        })
        .catch((error) => {
          pending.delete(key);
          throw error;
        }),
    );
  }
  return pending.get(key) ?? Promise.resolve("");
}

export function useBlobUrl(blobId?: string | null) {
  const [state, setState] = useState<{ key: string; url: string }>({
    key: "",
    url: "",
  });

  const trimmedBlobId = blobId?.trim() ?? "";
  const cachedUrl = trimmedBlobId ? (blobUrlCache.get(trimmedBlobId) ?? "") : "";

  useEffect(() => {
    if (!trimmedBlobId) {
      return;
    }
    let active = true;
    void resolveCached(trimmedBlobId, blobUrlCache, blobPendingCache, () =>
      resolveBlobURL(trimmedBlobId),
    )
      .then((value) => {
        if (active) {
          setState({
            key: trimmedBlobId,
            url: value,
          });
        }
      })
      .catch(() => {
        if (active) {
          setState({
            key: trimmedBlobId,
            url: "",
          });
        }
      });
    return () => {
      active = false;
    };
  }, [trimmedBlobId]);

  if (!trimmedBlobId) {
    return "";
  }

  if (state.key === trimmedBlobId) {
    return state.url;
  }

  return cachedUrl;
}

export function useRecordingArtworkUrl(recordingId?: string | null) {
  const [state, setState] = useState<{ key: string; url: string }>({
    key: "",
    url: "",
  });

  const trimmedRecordingId = recordingId?.trim() ?? "";
  const cachedUrl = trimmedRecordingId
    ? (recordingArtworkCache.get(trimmedRecordingId) ?? "")
    : "";

  useEffect(() => {
    if (!trimmedRecordingId) {
      return;
    }
    let active = true;
    void resolveCached(
      trimmedRecordingId,
      recordingArtworkCache,
      recordingArtworkPending,
      () => resolveRecordingArtworkURL(trimmedRecordingId),
    )
      .then((value) => {
        if (active) {
          setState({
            key: trimmedRecordingId,
            url: value,
          });
        }
      })
      .catch(() => {
        if (active) {
          setState({
            key: trimmedRecordingId,
            url: "",
          });
        }
      });
    return () => {
      active = false;
    };
  }, [trimmedRecordingId]);

  if (!trimmedRecordingId) {
    return "";
  }

  if (state.key === trimmedRecordingId) {
    return state.url;
  }

  return cachedUrl;
}
