import { Route, Switch } from "wouter";
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
import { OperationsPage } from "../../features/operations/page";

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
      <Route component={OperationsPage} path="/operations" />
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
