import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { ImageUp, Pencil, Play, Plus, Trash2 } from "lucide-react";
import type { PlaylistTrackItem } from "@/lib/api/models";
import {
  ConfirmPlaylistDeleteDialog,
  PlaylistNameDialog,
} from "@/components/catalog/PlaylistDialogs";
import { ManagedTrackListRow } from "@/components/catalog/ManagedTrackListRow";
import { Button } from "@/components/ui/Button";
import { ArtworkTile } from "@/components/ui/ArtworkTile";
import { MetricPill } from "@/components/catalog/MetricPill";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { TracksEmptyState } from "@/components/catalog/EmptyState";
import { VirtualRows } from "@/components/ui/VirtualRows";
import {
  useStoreInfiniteQuery,
  useStoreQuery,
} from "@/hooks/catalog/useCatalogQuery";
import { useThumbnailUrl } from "@/hooks/media/useThumbnailUrl";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import {
  clearPlaylistCover,
  deletePlaylist,
  pickPlaylistCoverSourcePath,
  removePlaylistItem,
  renamePlaylist,
  setPlaylistCover,
} from "@/lib/api/catalog";
import { formatCount, formatRelativeDate, joinArtists } from "@/lib/format";
import {
  getDetailRecord,
  getValueQuery,
  useCatalogStore,
} from "@/stores/catalog/store";
import { usePlaybackStore } from "@/stores/playback/store";
import { selectDetail, selectValueQuery } from "@/stores/catalog/query-state";

const playlistDetailRouteApi = getRouteApi("/playlists_/$playlistId");

export function PlaylistDetailPage() {
  const navigate = useNavigate();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [coverError, setCoverError] = useState("");
  const [coverBusy, setCoverBusy] = useState(false);
  const { playlistId } = playlistDetailRouteApi.useParams();
  const playPlaylist = usePlaybackStore((state) => state.playPlaylist);
  const queuePlaylist = usePlaybackStore((state) => state.queuePlaylist);
  const playPlaylistTrack = usePlaybackStore(
    (state) => state.playPlaylistTrack,
  );
  const queueRecording = usePlaybackStore((state) => state.queueRecording);
  const trackAvailabilityByRecordingId = useCatalogStore(
    (state) => state.trackAvailabilityByRecordingId,
  );
  const detail = useStoreQuery(
    (state) =>
      selectDetail(getDetailRecord(state.playlistSummaries, playlistId)),
    () => catalogLoaderClient.refetchPlaylist(playlistId),
  );
  const artworkUrl = useThumbnailUrl(detail.data?.Thumb);
  const isLiked = detail.data?.Kind === "liked";
  const trackQuery = useStoreInfiniteQuery<PlaylistTrackItem>(
    (state) =>
      selectValueQuery<PlaylistTrackItem>(
        state,
        `playlistTracks:${playlistId}`,
      ),
    {
      fetchNextPage: () => {
        const record = getValueQuery<PlaylistTrackItem>(
          useCatalogStore.getState(),
          `playlistTracks:${playlistId}`,
        );
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensurePlaylistTracksPage(
            playlistId,
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () =>
        catalogLoaderClient.ensurePlaylistTracksPage(playlistId, 0, {
          force: true,
        }),
    },
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <section className="flex flex-wrap items-end gap-5">
        <ArtworkTile
          alt={detail.data?.Name ?? "Playlist"}
          className="h-40 w-40 shrink-0 border-black/10"
          src={artworkUrl}
          subtitle="Playlist"
          title={detail.data?.Name ?? "Playlist"}
        />
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <SectionHeading
            actions={
              !isLiked ? (
                <>
                  <Button
                    icon={<Pencil className="h-4 w-4" />}
                    onClick={() => {
                      setRenameOpen(true);
                    }}
                    tone="quiet"
                  >
                    Rename
                  </Button>
                  <Button
                    disabled={coverBusy}
                    icon={<ImageUp className="h-4 w-4" />}
                    onClick={() => {
                      setCoverBusy(true);
                      setCoverError("");
                      void pickPlaylistCoverSourcePath()
                        .then((sourcePath) => {
                          if (!sourcePath) {
                            return;
                          }
                          return setPlaylistCover(playlistId, sourcePath);
                        })
                        .catch((error) => {
                          setCoverError(
                            error instanceof Error
                              ? error.message
                              : String(error),
                          );
                        })
                        .finally(() => {
                          setCoverBusy(false);
                        });
                    }}
                    tone="quiet"
                  >
                    {detail.data?.HasCustomCover
                      ? "Replace cover"
                      : "Upload cover"}
                  </Button>
                  {detail.data?.HasCustomCover ? (
                    <Button
                      disabled={coverBusy}
                      icon={<Trash2 className="h-4 w-4" />}
                      onClick={() => {
                        setCoverBusy(true);
                        setCoverError("");
                        void clearPlaylistCover(playlistId)
                          .catch((error) => {
                            setCoverError(
                              error instanceof Error
                                ? error.message
                                : String(error),
                            );
                          })
                          .finally(() => {
                            setCoverBusy(false);
                          });
                      }}
                      tone="quiet"
                    >
                      Remove cover
                    </Button>
                  ) : null}
                  <Button
                    icon={<Trash2 className="h-4 w-4" />}
                    onClick={() => {
                      setDeleteOpen(true);
                    }}
                    tone="danger"
                  >
                    Delete
                  </Button>
                </>
              ) : null
            }
            meta={
              <>
                <MetricPill
                  label={formatCount(
                    detail.data?.ItemCount ?? trackQuery.pageInfo?.Total ?? 0,
                    "track",
                  )}
                />
                <MetricPill
                  label={formatRelativeDate(detail.data?.UpdatedAt)}
                />
              </>
            }
            title={detail.data?.Name ?? "Playlist"}
          />
          <div className="flex flex-wrap gap-2">
            <Button
              icon={<Play className="h-4 w-4" />}
              onClick={() => {
                void playPlaylist(playlistId);
              }}
              tone="primary"
            >
              Play playlist
            </Button>
            <Button
              icon={<Plus className="h-4 w-4" />}
              onClick={() => {
                void queuePlaylist(playlistId);
              }}
            >
              Queue playlist
            </Button>
          </div>
          {coverError ? (
            <p className="text-sm text-red-300">{coverError}</p>
          ) : null}
        </div>
      </section>
      <div className="min-h-0 flex-1">
        <VirtualRows
          className="min-h-0 flex-1"
          emptyState={
            <TracksEmptyState
              body={
                detail.error ||
                "Playlist tracks will render here when items exist."
              }
            />
          }
          estimateSize={72}
          hasMore={trackQuery.hasMore}
          items={trackQuery.items}
          loading={trackQuery.isLoading}
          loadingMore={trackQuery.isRefreshing}
          onEndReached={() => {
            void trackQuery.fetchNextPage();
          }}
          renderRow={(track, index) => (
            <ManagedTrackListRow
              availabilityState={
                trackAvailabilityByRecordingId[track.RecordingID]?.data?.State
              }
              durationMs={track.DurationMS}
              indexLabel={String(index + 1).padStart(2, "0")}
              libraryRecordingId={track.LibraryRecordingID}
              mode="list"
              onPlay={() => {
                void playPlaylistTrack(playlistId, track.ItemID);
              }}
              onQueue={() => {
                void queueRecording(track.RecordingID);
              }}
              onRemove={() => {
                void removePlaylistItem(playlistId, track.ItemID).catch(
                  () => {},
                );
              }}
              recordingId={track.RecordingID}
              removeLabel={`Remove ${track.Title} from playlist`}
              subtitle={`${joinArtists(track.Artists)} • added ${formatRelativeDate(track.AddedAt)}`}
              title={track.Title}
            />
          )}
          scrollRestorationId="playlist-tracks-list"
          viewportClassName="pr-2"
        />
      </div>
      <PlaylistNameDialog
        confirmLabel="Save name"
        description="Rename this playlist."
        initialValue={detail.data?.Name ?? ""}
        onClose={() => {
          setRenameOpen(false);
        }}
        onConfirm={async (name) => {
          await renamePlaylist(playlistId, name);
        }}
        open={renameOpen}
        title="Rename playlist"
      />
      <ConfirmPlaylistDeleteDialog
        description={`Delete "${detail.data?.Name ?? "this playlist"}" and remove its custom cover and track order.`}
        onClose={() => {
          setDeleteOpen(false);
        }}
        onConfirm={async () => {
          await deletePlaylist(playlistId);
          await navigate({ to: "/playlists" });
        }}
        open={deleteOpen}
        title="Delete playlist?"
      />
    </div>
  );
}
