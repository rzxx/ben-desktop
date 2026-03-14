package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/platform"
	"ben/desktop/internal/playback"
	"ben/desktop/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type PlaybackService struct {
	mu sync.RWMutex

	app      *application.App
	host     *coreHost
	bridge   playback.CorePlaybackBridge
	session  *playback.Session
	platform playback.PlatformController
	store    interface{ Close() error }
}

func NewPlaybackService() *PlaybackService {
	return NewPlaybackServiceWithHost(newCoreHost())
}

func NewPlaybackServiceWithHost(host *coreHost) *PlaybackService {
	return &PlaybackService{host: host}
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

	host := s.requireHost()
	if err := host.Start(ctx); err != nil {
		_ = store.Close()
		return err
	}
	playbackRuntime := host.Runtime()

	session := playback.NewSession(
		playbackRuntime,
		playback.NewBackend(),
		store,
		host.PreferredProfile(),
		serviceLogger{},
	)
	session.SetSnapshotEmitter(s.handlePlaybackSnapshot)
	if err := session.Start(ctx); err != nil {
		_ = store.Close()
		_ = playbackRuntime.Close()
		return err
	}

	controller := platform.NewController(app, session, playbackRuntime)
	if err := controller.Start(); err != nil {
		_ = session.Close()
		_ = store.Close()
		_ = playbackRuntime.Close()
		return err
	}

	s.mu.Lock()
	s.app = app
	s.host = host
	s.bridge = playbackRuntime
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
	host := s.host
	store := s.store
	s.platform = nil
	s.session = nil
	s.bridge = nil
	s.host = nil
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
	if host != nil {
		if err := host.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	} else if bridge != nil {
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
	return playback.NewCatalogLoader(s.requirePlaybackBridge())
}

func (s *PlaybackService) requirePlaybackBridge() playback.CorePlaybackBridge {
	s.mu.RLock()
	bridge := s.bridge
	host := s.host
	s.mu.RUnlock()
	if bridge == nil {
		if host != nil {
			return host.Runtime()
		}
		return desktopcore.NewUnavailableCore(fmt.Errorf("core playback bridge is not available"))
	}
	return bridge
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

func typedBlobStorePath(path string, fileExt string) (string, error) {
	path = strings.TrimSpace(path)
	fileExt = normalizeArtworkFileExt(fileExt, "")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if fileExt == "" {
		return "", fmt.Errorf("file extension is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(absPath + "|" + fileExt))
	typedDir := filepath.Join(filepath.Dir(absPath), ".typed")
	typedPath := filepath.Join(typedDir, hex.EncodeToString(sum[:])+fileExt)
	if _, err := os.Stat(typedPath); err == nil {
		return typedPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(typedDir, 0o755); err != nil {
		return "", err
	}
	if err := os.Link(absPath, typedPath); err == nil {
		return typedPath, nil
	}

	src, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	tmpPath := typedPath + ".tmp"
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
	if err := os.Rename(tmpPath, typedPath); err != nil {
		if _, statErr := os.Stat(typedPath); statErr == nil {
			_ = os.Remove(tmpPath)
			return typedPath, nil
		}
		_ = os.Remove(tmpPath)
		return "", err
	}
	return typedPath, nil
}

type serviceLogger struct{}

func (serviceLogger) Printf(format string, args ...any) {
	log.Printf(format, args...)
}

func (serviceLogger) Errorf(format string, args ...any) {
	log.Printf(format, args...)
}
