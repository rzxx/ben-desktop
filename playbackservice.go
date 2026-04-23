package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"ben/desktop/internal/platform"
	"ben/desktop/internal/playback"
	"ben/desktop/internal/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type PlaybackService struct {
	mu sync.RWMutex

	app      *application.App
	host     *coreHost
	core     playback.PlaybackCore
	session  *playback.Session
	platform playback.PlatformController
	store    interface{ Close() error }

	subscribers    map[uint64]func(playback.SessionSnapshot)
	nextSubscriber uint64

	lastQueueVersion int64
	queueVersionSet  bool
}

var (
	loadPlaybackSettingsState = loadSettingsState
	savePlaybackSettingsState = saveSettingsState
)

func NewPlaybackService() *PlaybackService {
	return NewPlaybackServiceWithHost(newCoreHost())
}

func NewPlaybackServiceWithHost(host *coreHost) *PlaybackService {
	return &PlaybackService{
		host:        host,
		subscribers: make(map[uint64]func(playback.SessionSnapshot)),
	}
}

func (s *PlaybackService) ServiceName() string {
	return "PlaybackService"
}

func (s *PlaybackService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("application is not available")
	}

	traceEnabled, err := loadPlaybackTraceEnabledSetting()
	if err != nil {
		return fmt.Errorf("load playback trace settings: %w", err)
	}
	playback.SetDebugTraceEnabled(traceEnabled)

	storePath, err := playback.DefaultStorePath("ben-desktop")
	if err != nil {
		return err
	}
	store, err := playback.NewSQLiteStore(storePath)
	if err != nil {
		return err
	}

	host := s.requireHost()
	if err := host.Start(ctx); err != nil {
		_ = store.Close()
		return err
	}
	playbackCore := host
	playback.ClearDebugTrace()

	session := playback.NewSession(
		playbackCore,
		playback.NewBackend(),
		store,
		host.PreferredProfile(),
		serviceLogger{},
	)
	session.SetSnapshotEmitter(s.handlePlaybackSnapshot)
	if err := session.Start(ctx); err != nil {
		_ = store.Close()
		_ = playbackCore.Close()
		return err
	}

	controller := platform.NewController(app, session, playbackCore)
	if err := controller.Start(); err != nil {
		_ = session.Close()
		_ = store.Close()
		_ = playbackCore.Close()
		return err
	}

	s.mu.Lock()
	s.app = app
	s.host = host
	s.core = playbackCore
	s.session = session
	s.platform = controller
	s.store = store
	s.mu.Unlock()

	if app.Window != nil {
		app.Window.OnCreate(func(_ application.Window) {
			applyPlaybackWindowTitle(app, session.Snapshot())
		})
	}

	s.handlePlaybackSnapshot(session.Snapshot())
	return nil
}

func (s *PlaybackService) ServiceShutdown() error {
	s.mu.Lock()
	controller := s.platform
	session := s.session
	core := s.core
	host := s.host
	store := s.store
	s.platform = nil
	s.session = nil
	s.core = nil
	s.host = nil
	s.store = nil
	s.app = nil
	s.lastQueueVersion = 0
	s.queueVersionSet = false
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
	if host != nil {
		if err := host.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	} else if core != nil {
		if err := core.Close(); err != nil && shutdownErr == nil {
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

func (s *PlaybackService) GetPlaybackDebugDump() (string, error) {
	var current *playback.DebugTraceEntry

	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()

	if session != nil {
		snapshot := session.Snapshot()
		entry := playback.NewDebugTraceEntry("service.current", &snapshot)
		current = &entry
	}

	payload, err := json.Marshal(map[string]any{
		"generatedAtMs": time.Now().UTC().UnixMilli(),
		"enabled":       playback.DebugTraceEnabled(),
		"current":       current,
		"entries":       playback.SnapshotDebugTrace(),
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (s *PlaybackService) ClearPlaybackDebugTrace() {
	playback.ClearDebugTrace()
}

func (s *PlaybackService) GetPlaybackTraceEnabled() bool {
	return playback.DebugTraceEnabled()
}

func (s *PlaybackService) SetPlaybackTraceEnabled(enabled bool) error {
	state, err := loadPlaybackSettingsState()
	if err != nil {
		return fmt.Errorf("load playback trace settings: %w", err)
	}
	state.PlaybackTrace.Enabled = enabled
	if err := savePlaybackSettingsState(state); err != nil {
		return fmt.Errorf("save playback trace settings: %w", err)
	}
	playback.SetDebugTraceEnabled(enabled)
	return nil
}

func (s *PlaybackService) subscribeSnapshots(listener func(playback.SessionSnapshot)) func() {
	if s == nil || listener == nil {
		return func() {}
	}

	s.mu.Lock()
	id := s.nextSubscriber
	s.nextSubscriber++
	s.subscribers[id] = listener
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
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
	before := session.Snapshot()
	start := playback.NewDebugTraceEntry("rpc.seek.start", &before)
	start.TargetPositionMS = &positionMS
	playback.RecordDebugTrace(start)

	snapshot, seekErr := session.SeekTo(ctx, positionMS)
	if seekErr != nil {
		failed := playback.NewDebugTraceEntry("rpc.seek.error", &before)
		failed.TargetPositionMS = &positionMS
		failed.Message = seekErr.Error()
		playback.RecordDebugTrace(failed)
		return playback.SessionSnapshot{}, seekErr
	}

	result := playback.NewDebugTraceEntry("rpc.seek.result", &snapshot)
	result.TargetPositionMS = &positionMS
	playback.RecordDebugTrace(result)
	return snapshot, nil
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
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildAlbumSource(albumID))
}

func (s *PlaybackService) PlayAlbumTrack(ctx context.Context, albumID string, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildAlbumTrackSource(albumID, recordingID))
}

func (s *PlaybackService) QueueAlbum(ctx context.Context, albumID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	contextInput, err := loader.MaterializeSource(ctx, loader.BuildAlbumSource(albumID))
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems(contextInput.Items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) PlayPlaylist(ctx context.Context, playlistID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildPlaylistSource(playlistID))
}

func (s *PlaybackService) PlayPlaylistTrack(ctx context.Context, playlistID string, itemID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildPlaylistTrackSource(playlistID, itemID))
}

func (s *PlaybackService) QueuePlaylist(ctx context.Context, playlistID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	contextInput, err := loader.MaterializeSource(ctx, loader.BuildPlaylistSource(playlistID))
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems(contextInput.Items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) QueuePlaylistTrack(ctx context.Context, playlistID string, itemID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	item, err := loader.ResolveSourceItem(ctx, loader.BuildPlaylistTrackSource(playlistID, itemID))
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems([]playback.SessionItem{item}, string(playback.QueueInsertLast))
}

func (s *PlaybackService) PlayRecording(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildRecordingSource(recordingID))
}

func (s *PlaybackService) QueueRecording(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	contextInput, err := loader.MaterializeSource(ctx, loader.BuildRecordingSource(recordingID))
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems(contextInput.Items, string(playback.QueueInsertLast))
}

func (s *PlaybackService) PlayLiked(ctx context.Context) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildLikedSource())
}

func (s *PlaybackService) PlayLikedTrack(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildLikedTrackSource(recordingID))
}

func (s *PlaybackService) PlayOffline(ctx context.Context) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildOfflineSource())
}

func (s *PlaybackService) PlayOfflineTrack(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildOfflineTrackSource(recordingID))
}

func (s *PlaybackService) PlayTracks(ctx context.Context) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildTracksSource())
}

func (s *PlaybackService) ShuffleTracks(ctx context.Context) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.ReplaceSourceAndPlay(ctx, loader.BuildTracksSource(), true)
}

func (s *PlaybackService) PlayTracksFrom(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.replaceSourceAndPlay(ctx, loader.BuildTracksTrackSource(recordingID))
}

func (s *PlaybackService) QueueLikedTrack(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	item, err := loader.ResolveSourceItem(ctx, loader.BuildLikedTrackSource(recordingID))
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems([]playback.SessionItem{item}, string(playback.QueueInsertLast))
}

func (s *PlaybackService) QueueOfflineTrack(ctx context.Context, recordingID string) (playback.SessionSnapshot, error) {
	loader, err := s.requireLoader()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	item, err := loader.ResolveSourceItem(ctx, loader.BuildOfflineTrackSource(recordingID))
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return s.QueueItems([]playback.SessionItem{item}, string(playback.QueueInsertLast))
}

func (s *PlaybackService) handlePlaybackSnapshot(snapshot playback.SessionSnapshot) {
	s.mu.Lock()
	app := s.app
	controller := s.platform
	subscribers := make([]func(playback.SessionSnapshot), 0, len(s.subscribers))
	for _, subscriber := range s.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	queueChanged := !s.queueVersionSet || snapshot.QueueVersion != s.lastQueueVersion
	s.lastQueueVersion = snapshot.QueueVersion
	s.queueVersionSet = true
	s.mu.Unlock()

	if controller != nil {
		controller.HandlePlaybackSnapshot(snapshot)
	}
	applyPlaybackWindowTitle(app, snapshot)
	for _, subscriber := range subscribers {
		subscriber(snapshot)
	}
	if app != nil && app.Event != nil {
		playback.RecordDebugTrace(playback.NewDebugTraceEntry("service.emit.transport", &snapshot))
		app.Event.Emit(playback.EventTransportChanged, playback.BuildTransportEventSnapshot(snapshot))
		if queueChanged {
			queueTrace := playback.NewDebugTraceEntry("service.emit.queue", &snapshot)
			queueTrace.Reason = "queue_changed"
			playback.RecordDebugTrace(queueTrace)
			app.Event.Emit(playback.EventQueueChanged, playback.BuildQueueEventSnapshot(snapshot))
		}
	}
}

func (s *PlaybackService) replaceSourceAndPlay(ctx context.Context, req playback.PlaybackSourceRequest) (playback.SessionSnapshot, error) {
	session, err := s.requireSession()
	if err != nil {
		return playback.SessionSnapshot{}, err
	}
	return session.ReplaceSourceAndPlay(ctx, req, session.Snapshot().Shuffle)
}

func (s *PlaybackService) requireLoader() (*playback.CatalogLoader, error) {
	core, err := s.requirePlaybackCore()
	if err != nil {
		return nil, err
	}
	return playback.NewCatalogLoader(core), nil
}

func (s *PlaybackService) requirePlaybackCore() (playback.PlaybackCore, error) {
	s.mu.RLock()
	core := s.core
	s.mu.RUnlock()
	if core == nil {
		return nil, fmt.Errorf("playback core is not available")
	}
	return core, nil
}

func (s *PlaybackService) requireHost() *coreHost {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.host == nil {
		s.host = newCoreHost()
	}
	return s.host
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

func loadPlaybackTraceEnabledSetting() (bool, error) {
	state, err := loadPlaybackSettingsState()
	if err != nil {
		return false, err
	}
	return state.PlaybackTrace.Enabled, nil
}

type serviceLogger struct{}

func (serviceLogger) Printf(format string, args ...any) {
	log.Printf(format, args...)
}

func (serviceLogger) Errorf(format string, args ...any) {
	log.Printf(format, args...)
}
