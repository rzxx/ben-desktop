import type { ReactNode } from "react";
import { Clock3, Disc3, ListMusic, Shapes, UsersRound } from "lucide-react";

function EmptyState({
  body,
  icon,
  title,
}: {
  body: string;
  icon: ReactNode;
  title: string;
}) {
  return (
    <div className="rounded-lg border border-dashed border-zinc-800 bg-zinc-950 px-8 py-10 text-center">
      <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full border border-zinc-800 bg-zinc-900 text-zinc-400">
        {icon}
      </div>
      <h2 className="text-lg font-semibold text-zinc-100">{title}</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-zinc-400">{body}</p>
    </div>
  );
}

export function AlbumsEmptyState({ body }: { body: string }) {
  return (
    <EmptyState
      body={body}
      icon={<Disc3 className="h-5 w-5" />}
      title="No albums yet"
    />
  );
}

export function ArtistsEmptyState({ body }: { body: string }) {
  return (
    <EmptyState
      body={body}
      icon={<UsersRound className="h-5 w-5" />}
      title="No artists yet"
    />
  );
}

export function TracksEmptyState({ body }: { body: string }) {
  return (
    <EmptyState
      body={body}
      icon={<ListMusic className="h-5 w-5" />}
      title="No tracks yet"
    />
  );
}

export function PlaylistEmptyState({ body }: { body: string }) {
  return (
    <EmptyState
      body={body}
      icon={<Shapes className="h-5 w-5" />}
      title="No playlists yet"
    />
  );
}

export function AlbumTracksEmptyState({ body }: { body: string }) {
  return (
    <EmptyState
      body={body}
      icon={<Clock3 className="h-5 w-5" />}
      title="No album tracks"
    />
  );
}
