package desktopcore

import (
	"context"
	"fmt"
	"os"

	apitypes "ben/core/api/types"
	"gorm.io/gorm"
)

type App struct {
	cfg Config
	db  *gorm.DB

	jobs     *JobsService
	library  *LibraryService
	ingest   *IngestService
	catalog  *CatalogService
	cache    *CacheService
	playlist *PlaylistService
	playback *PlaybackService
}

func Open(ctx context.Context, cfg Config) (*App, error) {
	resolved, err := ResolveConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved.BlobRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}

	db, err := openSQLite(resolved.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	openOK := false
	defer func() {
		if !openOK {
			_ = closeSQL(db)
		}
	}()
	if err := autoMigrate(db); err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	app := &App{
		cfg:  resolved,
		db:   db,
		jobs: NewJobsService(),
	}
	app.library = &LibraryService{app: app}
	app.ingest = &IngestService{app: app}
	app.catalog = &CatalogService{app: app}
	app.cache = &CacheService{app: app}
	app.playlist = &PlaylistService{app: app}
	app.playback = newPlaybackService(app)

	if _, err := app.ensureCurrentDevice(ctx); err != nil {
		return nil, fmt.Errorf("ensure current device: %w", err)
	}

	openOK = true
	return app, nil
}

func (a *App) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return closeSQL(a.db)
}

func (a *App) BlobRoot() string {
	if a == nil {
		return ""
	}
	return a.cfg.BlobRoot
}

func (a *App) Jobs() *JobsService {
	if a == nil {
		return nil
	}
	return a.jobs
}

func (a *App) Config() Config {
	if a == nil {
		return Config{}
	}
	return a.cfg
}

func (a *App) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return a.library.ListLibraries(ctx)
}

func (a *App) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return a.library.ActiveLibrary(ctx)
}

func (a *App) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return a.library.CreateLibrary(ctx, name)
}

func (a *App) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return a.library.SelectLibrary(ctx, libraryID)
}

func (a *App) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return a.library.RenameLibrary(ctx, libraryID, name)
}

func (a *App) LeaveLibrary(ctx context.Context, libraryID string) error {
	return a.library.LeaveLibrary(ctx, libraryID)
}

func (a *App) DeleteLibrary(ctx context.Context, libraryID string) error {
	return a.library.DeleteLibrary(ctx, libraryID)
}

func (a *App) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return a.library.ListLibraryMembers(ctx)
}

func (a *App) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return a.library.UpdateLibraryMemberRole(ctx, deviceID, role)
}

func (a *App) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return a.library.RemoveLibraryMember(ctx, deviceID)
}

func (a *App) SetScanRoots(ctx context.Context, roots []string) error {
	return a.ingest.SetScanRoots(ctx, roots)
}

func (a *App) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return a.ingest.AddScanRoots(ctx, roots)
}

func (a *App) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return a.ingest.RemoveScanRoots(ctx, roots)
}

func (a *App) ScanRoots(ctx context.Context) ([]string, error) {
	return a.ingest.ScanRoots(ctx)
}

func (a *App) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return a.catalog.ListArtists(ctx, req)
}

func (a *App) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return a.catalog.GetArtist(ctx, artistID)
}

func (a *App) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return a.catalog.ListArtistAlbums(ctx, req)
}

func (a *App) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return a.catalog.ListAlbums(ctx, req)
}

func (a *App) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return a.catalog.GetAlbum(ctx, albumID)
}

func (a *App) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return a.catalog.ListRecordings(ctx, req)
}

func (a *App) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return a.catalog.GetRecording(ctx, recordingID)
}

func (a *App) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return a.catalog.ListRecordingVariants(ctx, req)
}

func (a *App) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return a.catalog.ListAlbumVariants(ctx, req)
}

func (a *App) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return a.catalog.SetPreferredRecordingVariant(ctx, recordingID, variantRecordingID)
}

func (a *App) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return a.catalog.SetPreferredAlbumVariant(ctx, albumID, variantAlbumID)
}

func (a *App) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return a.catalog.ListAlbumTracks(ctx, req)
}

func (a *App) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return a.catalog.ListPlaylists(ctx, req)
}

func (a *App) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return a.catalog.GetPlaylistSummary(ctx, playlistID)
}

func (a *App) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return a.catalog.ListPlaylistTracks(ctx, req)
}

func (a *App) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return a.catalog.ListLikedRecordings(ctx, req)
}

func (a *App) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return a.playlist.CreatePlaylist(ctx, name, kind)
}

func (a *App) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return a.cache.GetCacheOverview(ctx)
}

func (a *App) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return a.cache.ListCacheEntries(ctx, req)
}

func (a *App) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return a.cache.CleanupCache(ctx, req)
}

func (a *App) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return a.playlist.RenamePlaylist(ctx, playlistID, name)
}

func (a *App) DeletePlaylist(ctx context.Context, playlistID string) error {
	return a.playlist.DeletePlaylist(ctx, playlistID)
}

func (a *App) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return a.playlist.AddPlaylistItem(ctx, req)
}

func (a *App) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return a.playlist.MovePlaylistItem(ctx, req)
}

func (a *App) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return a.playlist.RemovePlaylistItem(ctx, playlistID, itemID)
}

func (a *App) LikeRecording(ctx context.Context, recordingID string) error {
	return a.playlist.LikeRecording(ctx, recordingID)
}

func (a *App) UnlikeRecording(ctx context.Context, recordingID string) error {
	return a.playlist.UnlikeRecording(ctx, recordingID)
}

func (a *App) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return a.playlist.IsRecordingLiked(ctx, recordingID)
}

func (a *App) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (a *App) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.PreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (a *App) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return a.playback.GetPlaybackPreparation(ctx, recordingID, preferredProfile)
}

func (a *App) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return a.playback.ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (a *App) ResolveArtworkRef(ctx context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	return a.playback.ResolveArtworkRef(ctx, artwork)
}

func (a *App) ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
	return a.playback.ResolveRecordingArtwork(ctx, recordingID, variant)
}

func (a *App) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return a.playback.PinRecordingOffline(ctx, recordingID, preferredProfile)
}

func (a *App) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	return a.playback.UnpinRecordingOffline(ctx, recordingID)
}

func (a *App) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return a.playback.PinAlbumOffline(ctx, albumID, preferredProfile)
}

func (a *App) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	return a.playback.UnpinAlbumOffline(ctx, albumID)
}

func (a *App) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return a.playback.PinPlaylistOffline(ctx, playlistID, preferredProfile)
}

func (a *App) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	return a.playback.UnpinPlaylistOffline(ctx, playlistID)
}

func (a *App) PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return a.playback.PinLikedOffline(ctx, preferredProfile)
}

func (a *App) UnpinLikedOffline(ctx context.Context) error {
	return a.playback.UnpinLikedOffline(ctx)
}

func (a *App) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return a.playback.ListRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (a *App) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return a.playback.GetRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (a *App) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return a.playback.GetRecordingAvailabilityOverview(ctx, recordingID, preferredProfile)
}

func (a *App) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return a.playback.GetAlbumAvailabilityOverview(ctx, albumID, preferredProfile)
}
