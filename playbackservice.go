package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/platform"
	"ben/desktop/internal/playback"
	"ben/desktop/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type hostBridge interface {
	playback.CorePlaybackBridge
	ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error)
	ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error)
	CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error)
	SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error)
	RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error)
	LeaveLibrary(ctx context.Context, libraryID string) error
	DeleteLibrary(ctx context.Context, libraryID string) error
	ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error)
	UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error
	RemoveLibraryMember(ctx context.Context, deviceID string) error
	ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error)
	GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error)
	ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error)
	ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error)
	GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error)
	ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error)
	ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error)
	SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error
	SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error
	ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error)
	ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error)
	GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error)
	ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error)
	ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error)
	CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error)
	RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error)
	DeletePlaylist(ctx context.Context, playlistID string) error
	AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error)
	MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error)
	RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error
	LikeRecording(ctx context.Context, recordingID string) error
	UnlikeRecording(ctx context.Context, recordingID string) error
	IsRecordingLiked(ctx context.Context, recordingID string) (bool, error)
	GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error)
	ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error)
	CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error)
	ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error)
}

type PlaybackService struct {
	mu sync.RWMutex

	app      *application.App
	bridge   hostBridge
	session  *playback.Session
	platform playback.PlatformController
	store    interface{ Close() error }
	blobRoot string
}

func NewPlaybackService() *PlaybackService {
	return &PlaybackService{}
}

func (s *PlaybackService) ServiceName() string {
	return "PlaybackService"
}

func (s *PlaybackService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("application is not available")
	}

	storePath, err := playback.DefaultStorePath("ben-desktop")
	if err != nil {
		return err
	}
	store, err := playback.NewSQLiteStore(storePath)
	if err != nil {
		return err
	}

	coreSettings := settings.CoreRuntimeSettings{}
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		log.Printf("playback: resolve settings path: %v", err)
	} else {
		settingsStore, openErr := settings.NewStore(settingsPath)
		if openErr != nil {
			log.Printf("playback: open settings store: %v", openErr)
		} else {
			defer func() {
				if closeErr := settingsStore.Close(); closeErr != nil {
					log.Printf("playback: close settings store: %v", closeErr)
				}
			}()
			state, loadErr := settingsStore.Load()
			if loadErr != nil {
				log.Printf("playback: load settings: %v", loadErr)
			} else {
				coreSettings = state.Core
			}
		}
	}

	bridge, err := desktopcore.OpenFromSettings(ctx, coreSettings)
	if err != nil {
		log.Printf("playback: core bridge unavailable: %v", err)
		bridge = nil
	}

	var playbackBridge hostBridge
	if bridge != nil {
		playbackBridge = bridge
	} else {
		playbackBridge = desktopcore.NewUnavailableCore(err)
	}

	session := playback.NewSession(
		playbackBridge,
		playback.NewBackend(),
		store,
		preferredProfile(coreSettings),
		serviceLogger{},
	)
	session.SetSnapshotEmitter(s.handlePlaybackSnapshot)
	if err := session.Start(ctx); err != nil {
		_ = store.Close()
		_ = playbackBridge.Close()
		return err
	}

	controller := platform.NewController(app, session, playbackBridge)
	if err := controller.Start(); err != nil {
		_ = session.Close()
		_ = store.Close()
		_ = playbackBridge.Close()
		return err
	}

	s.mu.Lock()
	s.app = app
	s.bridge = playbackBridge
	s.session = session
	s.platform = controller
	s.store = store
	s.blobRoot = resolvedBlobRoot(coreSettings)
	s.mu.Unlock()

	s.handlePlaybackSnapshot(session.Snapshot())
	return nil
}

func (s *PlaybackService) ServiceShutdown() error {
	s.mu.Lock()
	controller := s.platform
	session := s.session
	bridge := s.bridge
	store := s.store
	s.platform = nil
	s.session = nil
	s.bridge = nil
	s.store = nil
	s.app = nil
	s.mu.Unlock()

	var shutdownErr error
	if controller != nil {
		if err := controller.Stop(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	if session != nil {
		if err := session.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	if store != nil {
		if err := store.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	if bridge != nil {
		if err := bridge.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	return shutdownErr
}

func (s *PlaybackService) GetPlaybackSnapshot() (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.Snapshot(), nil
}

func (s *PlaybackService) SubscribePlaybackEvents() string {
	return playback.EventSnapshotChanged
}

func (s *PlaybackService) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	return s.requireBridge().ListArtists(ctx, req)
}

func (s *PlaybackService) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return s.requireBridge().ListLibraries(ctx)
}

func (s *PlaybackService) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return s.requireBridge().ActiveLibrary(ctx)
}

func (s *PlaybackService) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return s.requireBridge().CreateLibrary(ctx, name)
}

func (s *PlaybackService) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return s.requireBridge().SelectLibrary(ctx, libraryID)
}

func (s *PlaybackService) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return s.requireBridge().RenameLibrary(ctx, libraryID, name)
}

func (s *PlaybackService) LeaveLibrary(ctx context.Context, libraryID string) error {
	return s.requireBridge().LeaveLibrary(ctx, libraryID)
}

func (s *PlaybackService) DeleteLibrary(ctx context.Context, libraryID string) error {
	return s.requireBridge().DeleteLibrary(ctx, libraryID)
}

func (s *PlaybackService) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return s.requireBridge().ListLibraryMembers(ctx)
}

func (s *PlaybackService) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return s.requireBridge().UpdateLibraryMemberRole(ctx, deviceID, role)
}

func (s *PlaybackService) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return s.requireBridge().RemoveLibraryMember(ctx, deviceID)
}

func (s *PlaybackService) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	return s.requireBridge().GetArtist(ctx, artistID)
}

func (s *PlaybackService) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return s.requireBridge().ListArtistAlbums(ctx, req)
}

func (s *PlaybackService) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	return s.requireBridge().ListAlbums(ctx, req)
}

func (s *PlaybackService) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	return s.requireBridge().GetAlbum(ctx, albumID)
}

func (s *PlaybackService) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return s.requireBridge().ListRecordings(ctx, req)
}

func (s *PlaybackService) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return s.requireBridge().GetRecording(ctx, recordingID)
}

func (s *PlaybackService) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return s.requireBridge().ListRecordingVariants(ctx, req)
}

func (s *PlaybackService) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	return s.requireBridge().ListAlbumVariants(ctx, req)
}

func (s *PlaybackService) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return s.requireBridge().SetPreferredRecordingVariant(ctx, recordingID, variantRecordingID)
}

func (s *PlaybackService) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return s.requireBridge().SetPreferredAlbumVariant(ctx, albumID, variantAlbumID)
}

func (s *PlaybackService) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return s.requireBridge().ListAlbumTracks(ctx, req)
}

func (s *PlaybackService) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	return s.requireBridge().ListPlaylists(ctx, req)
}

func (s *PlaybackService) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	return s.requireBridge().GetPlaylistSummary(ctx, playlistID)
}

func (s *PlaybackService) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return s.requireBridge().ListPlaylistTracks(ctx, req)
}

func (s *PlaybackService) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return s.requireBridge().ListLikedRecordings(ctx, req)
}

func (s *PlaybackService) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return s.requireBridge().CreatePlaylist(ctx, name, kind)
}

func (s *PlaybackService) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return s.requireBridge().RenamePlaylist(ctx, playlistID, name)
}

func (s *PlaybackService) DeletePlaylist(ctx context.Context, playlistID string) error {
	return s.requireBridge().DeletePlaylist(ctx, playlistID)
}

func (s *PlaybackService) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return s.requireBridge().AddPlaylistItem(ctx, req)
}

func (s *PlaybackService) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return s.requireBridge().MovePlaylistItem(ctx, req)
}

func (s *PlaybackService) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return s.requireBridge().RemovePlaylistItem(ctx, playlistID, itemID)
}

func (s *PlaybackService) LikeRecording(ctx context.Context, recordingID string) error {
	return s.requireBridge().LikeRecording(ctx, recordingID)
}

func (s *PlaybackService) UnlikeRecording(ctx context.Context, recordingID string) error {
	return s.requireBridge().UnlikeRecording(ctx, recordingID)
}

func (s *PlaybackService) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return s.requireBridge().IsRecordingLiked(ctx, recordingID)
}

func (s *PlaybackService) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return s.requireBridge().GetCacheOverview(ctx)
}

func (s *PlaybackService) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return s.requireBridge().ListCacheEntries(ctx, req)
}

func (s *PlaybackService) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return s.requireBridge().CleanupCache(ctx, req)
}

func (s *PlaybackService) ResolveBlobURL(blobID string) (string, error) {
	s.mu.RLock()
	blobRoot := s.blobRoot
	s.mu.RUnlock()

	path, ok, err := blobPathForID(blobRoot, blobID)
	if err != nil || !ok {
		return "", err
	}
	return fileURLFromPath(path)
}

func (s *PlaybackService) ResolveThumbnailURL(artwork apitypes.ArtworkRef) (string, error) {
	artwork.BlobID = strings.TrimSpace(artwork.BlobID)
	if artwork.BlobID == "" {
		return "", nil
	}
	resolved, err := s.requireBridge().ResolveArtworkRef(context.Background(), artwork)
	if err != nil {
		return "", err
	}
	if !resolved.Available || strings.TrimSpace(resolved.LocalPath) == "" {
		return "", nil
	}
	fileExt := normalizeArtworkFileExt(strings.TrimSpace(resolved.Artwork.FileExt), strings.TrimSpace(resolved.Artwork.MIME))
	if fileExt == "" {
		return "", fmt.Errorf("thumbnail file extension is required")
	}
	aliasPath, err := ensureTypedBlobAlias(resolved.LocalPath, fileExt)
	if err != nil {
		return "", err
	}
	return fileURLFromPath(aliasPath)
}

func (s *PlaybackService) ResolveRecordingArtworkURL(ctx context.Context, recordingID string, variant string) (string, error) {
	bridge := s.requireBridge()
	result, err := bridge.ResolveRecordingArtwork(ctx, recordingID, variant)
	if err != nil {
		return "", err
	}
	return s.ResolveThumbnailURL(result.Artwork)
}

func (s *PlaybackService) SetPlaybackContext(input playback.PlaybackContextInput) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SetContext(input)
}

func (s *PlaybackService) QueueItems(items []playback.SessionItem, mode string) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.QueueItems(items, playback.ParseQueueInsertMode(mode))
}

func (s *PlaybackService) RemoveQueuedEntry(entryID string) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.RemoveQueuedEntry(entryID)
}

func (s *PlaybackService) MoveQueuedEntry(entryID string, toIndex int) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.MoveQueuedEntry(entryID, toIndex)
}

func (s *PlaybackService) SelectEntry(ctx context.Context, entryID string) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SelectEntry(ctx, entryID)
}

func (s *PlaybackService) ReplaceQueue(items []playback.SessionItem, startIndex int) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.ReplaceQueue(items, startIndex)
}

func (s *PlaybackService) AppendToQueue(items []playback.SessionItem) (playback.SessionSnapshot, error) {
	return s.QueueItems(items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) RemoveQueueItem(index int) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.RemoveQueueItem(index)
}

func (s *PlaybackService) MoveQueueItem(fromIndex int, toIndex int) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.MoveQueueItem(fromIndex, toIndex)
}

func (s *PlaybackService) SelectQueueIndex(ctx context.Context, index int) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SelectQueueIndex(ctx, index)
}

func (s *PlaybackService) ClearQueue() (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.ClearQueue()
}

func (s *PlaybackService) Play(ctx context.Context) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.Play(ctx)
}

func (s *PlaybackService) Pause(ctx context.Context) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.Pause(ctx)
}

func (s *PlaybackService) TogglePlayback(ctx context.Context) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.TogglePlayback(ctx)
}

func (s *PlaybackService) Next(ctx context.Context) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.Next(ctx)
}

func (s *PlaybackService) Previous(ctx context.Context) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.Previous(ctx)
}

func (s *PlaybackService) SeekTo(ctx context.Context, positionMS int64) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SeekTo(ctx, positionMS)
}

func (s *PlaybackService) SetVolume(ctx context.Context, volume int) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SetVolume(ctx, volume)
}

func (s *PlaybackService) SetRepeatMode(mode string) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SetRepeatMode(mode)
}

func (s *PlaybackService) SetShuffle(enabled bool) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.SetShuffle(enabled)
}

func (s *PlaybackService) PlayAlbum(ctx context.Context, albumID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadAlbumContext(ctx, albumID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) PlayAlbumTrack(ctx context.Context, albumID string, recordingID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadAlbumTrackContext(ctx, albumID, recordingID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) QueueAlbum(ctx context.Context, albumID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadAlbumContext(ctx, albumID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems(contextInput.Items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) PlayPlaylist(ctx context.Context, playlistID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadPlaylistContext(ctx, playlistID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) PlayPlaylistTrack(ctx context.Context, playlistID string, itemID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadPlaylistTrackContext(ctx, playlistID, itemID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) QueuePlaylist(ctx context.Context, playlistID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadPlaylistContext(ctx, playlistID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems(contextInput.Items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) PlayRecording(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadRecordingContext(ctx, recordingID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) QueueRecording(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadRecordingContext(ctx, recordingID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems(contextInput.Items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) PlayLiked(ctx context.Context) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadLikedContext(ctx)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) PlayLikedTrack(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	contextInput, err := s.requireLoader().LoadLikedTrackContext(ctx, recordingID)
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceContextAndPlay(ctx, contextInput)
}

func (s *PlaybackService) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.requireBridge().InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackService) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return s.requireBridge().PreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (s *PlaybackService) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return s.requireBridge().GetPlaybackPreparation(ctx, recordingID, preferredProfile)
}

func (s *PlaybackService) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return s.requireBridge().ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (s *PlaybackService) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return s.requireBridge().ListRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (s *PlaybackService) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return s.requireBridge().GetRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (s *PlaybackService) handlePlaybackSnapshot(snapshot playback.SessionSnapshot) {
	s.mu.RLock()
	app := s.app
	controller := s.platform
	s.mu.RUnlock()

	if controller != nil {
		controller.HandlePlaybackSnapshot(snapshot)
	}
	if app != nil && app.Event != nil {
		app.Event.Emit(playback.EventSnapshotChanged, snapshot)
	}
}

func (s *PlaybackService) replaceContextAndPlay(ctx context.Context, input playback.PlaybackContextInput) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	if _, err := session.SetContext(input); err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.Play(ctx)
}

func (s *PlaybackService) requireLoader() *playback.CatalogLoader {
	s.mu.RLock()
	bridge := s.bridge
	s.mu.RUnlock()
	return playback.NewCatalogLoader(bridge)
}

func (s *PlaybackService) requireBridge() hostBridge {
	s.mu.RLock()
	bridge := s.bridge
	s.mu.RUnlock()
	if bridge == nil {
		return desktopcore.NewUnavailableCore(fmt.Errorf("core bridge is not available"))
	}
	return bridge
}

func (s *PlaybackService) requireSession() (*playback.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session == nil {
		return nil, fmt.Errorf("playback session is not available")
	}
	return s.session, nil
}

func preferredProfile(coreSettings settings.CoreRuntimeSettings) string {
	return settings.EffectiveTranscodeProfile(coreSettings.TranscodeProfile)
}

func resolvedBlobRoot(coreSettings settings.CoreRuntimeSettings) string {
	cfg, err := desktopcore.ResolveConfigFromSettings(coreSettings)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.BlobRoot)
}

func blobPathForID(root string, blobID string) (string, bool, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", false, nil
	}

	parts := strings.SplitN(strings.TrimSpace(blobID), ":", 2)
	if len(parts) != 2 {
		return "", false, fmt.Errorf("invalid blob id format")
	}
	algo := strings.TrimSpace(parts[0])
	hashHex := strings.ToLower(strings.TrimSpace(parts[1]))
	if algo != "b3" {
		return "", false, fmt.Errorf("unsupported blob algo %q", algo)
	}
	if len(hashHex) != 64 {
		return "", false, fmt.Errorf("invalid blob hash length")
	}
	if _, err := hex.DecodeString(hashHex); err != nil {
		return "", false, fmt.Errorf("invalid blob hash: %w", err)
	}

	path := filepath.Join(root, algo, hashHex[:2], hashHex[2:4], hashHex)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return path, true, nil
}

func fileURLFromPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(absPath),
	}).String(), nil
}

func normalizeArtworkFileExt(fileExt string, mimeType string) string {
	fileExt = strings.TrimSpace(strings.ToLower(fileExt))
	if fileExt != "" {
		if !strings.HasPrefix(fileExt, ".") {
			fileExt = "." + fileExt
		}
		switch fileExt {
		case ".jpeg", ".jpe":
			return ".jpg"
		default:
			return fileExt
		}
	}

	switch strings.TrimSpace(strings.ToLower(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func ensureTypedBlobAlias(path string, fileExt string) (string, error) {
	path = strings.TrimSpace(path)
	fileExt = normalizeArtworkFileExt(fileExt, "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if fileExt == "" {
		return "", fmt.Errorf("file extension is required")
	}

	aliasPath := path + fileExt
	if _, err := os.Stat(aliasPath); err == nil {
		return aliasPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.Link(path, aliasPath); err == nil {
		return aliasPath, nil
	}

	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()

	tmpPath := aliasPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o755); err != nil {
		return "", err
	}
	dst, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	copyErr := error(nil)
	if _, err := io.Copy(dst, src); err != nil {
		copyErr = err
	}
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", closeErr
	}
	if err := os.Rename(tmpPath, aliasPath); err != nil {
		if _, statErr := os.Stat(aliasPath); statErr == nil {
			_ = os.Remove(tmpPath)
			return aliasPath, nil
		}
		_ = os.Remove(tmpPath)
		return "", err
	}
	return aliasPath, nil
}

type serviceLogger struct{}

func (serviceLogger) Printf(format string, args ...any) {
	log.Printf(format, args...)
}

func (serviceLogger) Errorf(format string, args ...any) {
	log.Printf(format, args...)
}
