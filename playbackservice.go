package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"ben/desktop/internal/corebridge"
	"ben/desktop/internal/platform"
	"ben/desktop/internal/playback"
	"ben/desktop/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type PlaybackService struct {
	mu sync.RWMutex

	app      *application.App
	bridge   playback.CorePlaybackBridge
	session  *playback.Session
	platform playback.PlatformController
	store    interface{ Close() error }
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

	bridge, err := corebridge.OpenFromSettings(ctx, coreSettings)
	if err != nil {
		log.Printf("playback: core bridge unavailable: %v", err)
		bridge = nil
	}

	var playbackBridge playback.CorePlaybackBridge
	if bridge != nil {
		playbackBridge = bridge
	} else {
		playbackBridge = corebridge.NewUnavailableBridge(err)
	}

	session := playback.NewSession(
		playbackBridge,
		playback.NewBackend(),
		store,
		strings.TrimSpace(getPreferredProfile()),
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

func (s *PlaybackService) requireSession() (*playback.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session == nil {
		return nil, fmt.Errorf("playback session is not available")
	}
	return s.session, nil
}

func getPreferredProfile() string {
	if value := strings.TrimSpace(os.Getenv("BEN_CORE_TRANSCODE_PROFILE")); value != "" {
		return value
	}
	return "desktop"
}

type serviceLogger struct{}

func (serviceLogger) Printf(format string, args ...any) {
	log.Printf(format, args...)
}

func (serviceLogger) Errorf(format string, args ...any) {
	log.Printf(format, args...)
}
