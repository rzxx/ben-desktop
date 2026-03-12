import { Route, Switch } from "wouter";
import { CachePage } from "../../features/cache/page";
import {
  AlbumDetailPage,
  AlbumsPage,
  ArtistDetailPage,
  ArtistsPage,
  LikedPlaylistPage,
  PlaylistDetailPage,
  PlaylistsPage,
  TracksPage,
} from "../../features/library/pages";
import { LibrariesPage } from "../../features/library/libraries-page";
import { OperationsPage } from "../../features/operations/page";
import { SharingPage } from "../../features/sharing/page";

export function AppRouter() {
  return (
    <Switch>
      <Route component={AlbumsPage} path="/" />
      <Route component={AlbumsPage} path="/albums" />
      <Route path="/albums/:albumId">
        {(params) => <AlbumDetailPage albumId={params.albumId} />}
      </Route>
      <Route component={ArtistsPage} path="/artists" />
      <Route path="/artists/:artistId">
        {(params) => <ArtistDetailPage artistId={params.artistId} />}
      </Route>
      <Route component={CachePage} path="/cache" />
      <Route component={LibrariesPage} path="/libraries" />
      <Route component={OperationsPage} path="/operations" />
      <Route component={SharingPage} path="/sharing" />
      <Route component={TracksPage} path="/tracks" />
      <Route component={PlaylistsPage} path="/playlists" />
      <Route component={LikedPlaylistPage} path="/playlists/liked" />
      <Route path="/playlists/:playlistId">
        {(params) => <PlaylistDetailPage playlistId={params.playlistId} />}
      </Route>
      <Route component={AlbumsPage} />
    </Switch>
  );
}
