import { useState } from "react";
import { Plus } from "lucide-react";
import type { PlaylistListItem } from "@/lib/api/models";
import { MetricPill } from "@/components/catalog/MetricPill";
import { PlaylistNameDialog } from "@/components/catalog/PlaylistDialogs";
import { PlaylistRow } from "@/components/catalog/PlaylistRow";
import { PlaylistEmptyState } from "@/components/catalog/EmptyState";
import { SectionHeading } from "@/components/catalog/SectionHeading";
import { Button } from "@/components/ui/Button";
import { VirtualRows } from "@/components/ui/VirtualRows";
import { pinSubjectKey, usePinStates } from "@/hooks/pins/usePinStates";
import { createPlaylist } from "@/lib/api/catalog";
import { useStoreInfiniteQuery } from "@/hooks/catalog/useCatalogQuery";
import { catalogLoaderClient } from "@/lib/catalog/loader-client";
import { Types } from "@/lib/api/models";
import { formatCount } from "@/lib/format";
import { getIdQuery, useCatalogStore } from "@/stores/catalog/store";
import { selectEntityQuery } from "@/stores/catalog/query-state";

function playlistPinSubject(playlist: PlaylistListItem) {
  if (playlist.Kind === "liked") {
    return new Types.PinSubjectRef({
      ID: playlist.PlaylistID,
      Kind: Types.PinSubjectKind.PinSubjectLikedPlaylist,
    });
  }
  if (playlist.IsReserved) {
    return null;
  }
  return new Types.PinSubjectRef({
    ID: playlist.PlaylistID,
    Kind: Types.PinSubjectKind.PinSubjectPlaylist,
  });
}

export function PlaylistsPage() {
  const [createOpen, setCreateOpen] = useState(false);
  const query = useStoreInfiniteQuery<PlaylistListItem>(
    (state) =>
      selectEntityQuery<PlaylistListItem>(
        state,
        "playlists",
        (current, id) => current.playlistsById[id],
      ),
    {
      fetchNextPage: () => {
        const record = getIdQuery(useCatalogStore.getState(), "playlists");
        if (record.pageInfo?.HasMore) {
          return catalogLoaderClient.ensurePlaylistsPage(
            record.pageInfo.NextOffset,
          );
        }
      },
      refetch: () => catalogLoaderClient.refetchPlaylists(),
    },
  );
  const playlistPinStates = usePinStates(
    query.items
      .map((playlist) => playlistPinSubject(playlist))
      .filter((subject): subject is Types.PinSubjectRef => subject !== null),
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <SectionHeading
        actions={
          <Button
            icon={<Plus className="h-4 w-4" />}
            onClick={() => {
              setCreateOpen(true);
            }}
            tone="primary"
          >
            Create playlist
          </Button>
        }
        meta={
          <MetricPill
            label={formatCount(
              query.pageInfo?.Total ?? query.items.length,
              "playlist",
            )}
          />
        }
        title="Playlists"
      />
      <div className="min-h-0 flex-1">
        <VirtualRows
          className="min-h-0 flex-1"
          emptyState={
            <PlaylistEmptyState body="Playlist records will appear here once the library contains playlists." />
          }
          estimateSize={98}
          hasMore={query.hasMore}
          items={query.items}
          loading={query.isLoading}
          loadingMore={query.isRefreshing}
          onEndReached={() => {
            void query.fetchNextPage();
          }}
          renderRow={(playlist) => (
            <PlaylistRow
              pinState={(() => {
                const subject = playlistPinSubject(playlist);
                if (!subject) {
                  return null;
                }
                return playlistPinStates[pinSubjectKey(subject)] ?? null;
              })()}
              playlist={playlist}
            />
          )}
          scrollRestorationId="playlists-list"
          viewportClassName="pr-2"
        />
      </div>
      <PlaylistNameDialog
        confirmLabel="Create playlist"
        description="Create a normal playlist in the active library."
        onClose={() => {
          setCreateOpen(false);
        }}
        onConfirm={async (name) => {
          await createPlaylist(name);
        }}
        open={createOpen}
        title="Create playlist"
      />
    </div>
  );
}
