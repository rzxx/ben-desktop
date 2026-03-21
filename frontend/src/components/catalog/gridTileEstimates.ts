const ALBUM_GRID_TILE_METADATA_HEIGHT = 68;
const ARTIST_GRID_TILE_HEIGHT = 192;

export function estimateAlbumGridTileHeight(columnWidth: number) {
  return Math.round(columnWidth + ALBUM_GRID_TILE_METADATA_HEIGHT);
}

export function estimateArtistGridTileHeight() {
  return ARTIST_GRID_TILE_HEIGHT;
}
