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
	catalog  *CatalogService
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
	app.catalog = &CatalogService{app: app}
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

func (a *App) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return a.playback.ListRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (a *App) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return a.playback.GetRecordingAvailability(ctx, recordingID, preferredProfile)
}
