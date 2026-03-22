import { useEffect } from "react";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { QUERY_KEYS } from "@/lib/catalog/query-keys";
import { getValueQuery, useCatalogStore } from "@/stores/catalog/store";
import { useRecordingLikesStore } from "@/stores/catalog/likes";

type RecordingLikeInput = {
  initialLiked?: boolean;
  libraryRecordingId?: string | null;
  recordingId?: string | null;
};

export function useRecordingLikeState(input: RecordingLikeInput) {
  const initialLiked = input.initialLiked;
  const libraryRecordingId = input.libraryRecordingId?.trim() ?? "";
  const recordingId = input.recordingId?.trim() ?? "";
  const key = useRecordingLikesStore(() => {
    if (libraryRecordingId) {
      return libraryRecordingId;
    }
    return recordingId;
  });
  const record = useRecordingLikesStore((state) =>
    key ? state.likesById[key] : undefined,
  );

  useEffect(() => {
    if (!key) {
      return;
    }
    const nextInput = {
      libraryRecordingId,
      recordingId,
    };
    if (initialLiked !== undefined && !record) {
      useRecordingLikesStore.getState().seedLikeState(nextInput, initialLiked);
    }
    if (record && !record.stale) {
      return;
    }
    void useRecordingLikesStore
      .getState()
      .ensureLikeState(nextInput)
      .catch(() => {});
  }, [initialLiked, key, libraryRecordingId, record, recordingId]);

  return {
    error: record?.error ?? "",
    hasIdentity: key.length > 0,
    inFlight: record?.inFlight ?? false,
    liked: record?.liked ?? false,
    toggleLike: async () => {
      if (!key) {
        return false;
      }
      const nextLiked = !(record?.liked ?? false);
      const result = await useRecordingLikesStore.getState().setLikeState(
        {
          libraryRecordingId,
          recordingId,
        },
        nextLiked,
      );
      if (
        result &&
        getValueQuery(useCatalogStore.getState(), QUERY_KEYS.liked)
          .loadedOffsets.length > 0
      ) {
        void catalogLoaderClient.refetchLiked();
      }
      return result;
    },
  };
}
