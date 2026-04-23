package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/palette"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	EventThemePreferencesChanged = "theme:preferences"
	maxThemeCacheEntries         = 96
	themeArtworkVariant          = "320_webp"
	errThemeArtworkAbsent        = "theme artwork is not available"
)

type themeExtractor interface {
	ExtractFromPath(path string, options palette.ExtractOptions) (palette.ThemePalette, error)
}

type themeCacheEntry struct {
	palette           palette.ThemePalette
	sourceModUnixNano int64
	cachedAt          time.Time
}

type ThemeFacade struct {
	facadeBase
	extractor themeExtractor
	cacheMu   sync.RWMutex
	cache     map[string]themeCacheEntry

	mu                  sync.Mutex
	app                 *application.App
	systemTheme         apitypes.ResolvedTheme
	stopThemeMonitoring func()
}

func NewThemeFacade(host *coreHost) *ThemeFacade {
	return &ThemeFacade{
		facadeBase: facadeBase{host: host},
		extractor:  palette.NewExtractor(),
		cache:      make(map[string]themeCacheEntry),
	}
}

func (s *ThemeFacade) ServiceName() string { return "ThemeFacade" }

func (s *ThemeFacade) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.host == nil {
		return nil
	}
	if err := s.host.Start(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.app = application.Get()
	s.mu.Unlock()

	s.currentSystemTheme()
	s.startSystemThemeMonitor()
	return nil
}

func (s *ThemeFacade) ServiceShutdown() error {
	s.mu.Lock()
	stop := s.stopThemeMonitoring
	s.stopThemeMonitoring = nil
	s.app = nil
	s.mu.Unlock()

	if stop != nil {
		stop()
	}
	return nil
}

func (s *ThemeFacade) SubscribeThemeEvents() string {
	return EventThemePreferencesChanged
}

func (s *ThemeFacade) GetThemePreferences() (apitypes.ThemePreferences, error) {
	mode, err := s.loadThemeMode()
	if err != nil {
		return apitypes.ThemePreferences{}, err
	}
	return s.themePreferences(mode), nil
}

func (s *ThemeFacade) SetThemeMode(mode apitypes.AppThemeMode) (apitypes.ThemePreferences, error) {
	state, err := loadSettingsState()
	if err != nil {
		return apitypes.ThemePreferences{}, err
	}

	nextMode := apitypes.NormalizeAppThemeMode(mode)
	state.Theme.Mode = string(nextMode)
	if err := saveSettingsState(state); err != nil {
		return apitypes.ThemePreferences{}, err
	}

	preferences := s.themePreferences(nextMode)
	s.emitThemePreferences(preferences)
	return preferences, nil
}

func (s *ThemeFacade) GenerateRecordingTheme(ctx context.Context, recordingID string) (palette.ThemePalette, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return palette.ThemePalette{}, errors.New("recording id is required")
	}

	playbackRuntime := s.playback()
	if playbackRuntime == nil {
		return palette.ThemePalette{}, errors.New("playback runtime is not available")
	}

	resolved, err := playbackRuntime.ResolveRecordingArtwork(ctx, recordingID, themeArtworkVariant)
	if err != nil {
		return palette.ThemePalette{}, err
	}

	resolvedPath := strings.TrimSpace(resolved.LocalPath)
	if !resolved.Available || resolvedPath == "" {
		return palette.ThemePalette{}, errors.New(errThemeArtworkAbsent)
	}

	sourceInfo, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return palette.ThemePalette{}, errors.New(errThemeArtworkAbsent)
		}
		return palette.ThemePalette{}, fmt.Errorf("stat theme artwork: %w", err)
	}
	sourceModUnixNano := sourceInfo.ModTime().UnixNano()

	if cachedPalette, ok := s.loadCachedPalette(resolvedPath, sourceModUnixNano); ok {
		return cachedPalette, nil
	}

	themePalette, err := s.extractor.ExtractFromPath(resolvedPath, palette.DefaultExtractOptions())
	if err != nil {
		return palette.ThemePalette{}, fmt.Errorf("generate recording theme: %w", err)
	}

	s.storeCachedPalette(resolvedPath, sourceModUnixNano, themePalette)
	return themePalette, nil
}

func (s *ThemeFacade) loadCachedPalette(cacheKey string, sourceModUnixNano int64) (palette.ThemePalette, bool) {
	s.cacheMu.RLock()
	entry, ok := s.cache[cacheKey]
	s.cacheMu.RUnlock()
	if !ok || entry.sourceModUnixNano != sourceModUnixNano {
		return palette.ThemePalette{}, false
	}

	return entry.palette, true
}

func (s *ThemeFacade) storeCachedPalette(cacheKey string, sourceModUnixNano int64, themePalette palette.ThemePalette) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	s.cache[cacheKey] = themeCacheEntry{
		palette:           themePalette,
		sourceModUnixNano: sourceModUnixNano,
		cachedAt:          time.Now(),
	}

	if len(s.cache) <= maxThemeCacheEntries {
		return
	}

	oldestKey := ""
	oldestAt := time.Now()
	for key, entry := range s.cache {
		if oldestKey == "" || entry.cachedAt.Before(oldestAt) {
			oldestKey = key
			oldestAt = entry.cachedAt
		}
	}

	if oldestKey != "" {
		delete(s.cache, oldestKey)
	}
}

func (s *ThemeFacade) loadThemeMode() (apitypes.AppThemeMode, error) {
	state, err := loadSettingsState()
	if err != nil {
		return apitypes.AppThemeModeSystem, err
	}
	return apitypes.NormalizeAppThemeMode(apitypes.AppThemeMode(state.Theme.Mode)), nil
}

func (s *ThemeFacade) themePreferences(mode apitypes.AppThemeMode) apitypes.ThemePreferences {
	systemTheme := s.currentSystemTheme()
	return apitypes.ThemePreferences{
		Mode:      apitypes.NormalizeAppThemeMode(mode),
		System:    systemTheme,
		Effective: apitypes.ResolveTheme(mode, systemTheme),
	}
}

func (s *ThemeFacade) currentSystemTheme() apitypes.ResolvedTheme {
	s.mu.Lock()
	if s.systemTheme == "" {
		s.systemTheme = detectSystemTheme()
	}
	systemTheme := s.systemTheme
	s.mu.Unlock()
	return apitypes.NormalizeResolvedTheme(systemTheme)
}

func (s *ThemeFacade) refreshSystemTheme() {
	nextTheme := detectSystemTheme()

	s.mu.Lock()
	if nextTheme == "" {
		nextTheme = apitypes.ResolvedThemeLight
	}
	changed := s.systemTheme != nextTheme
	s.systemTheme = nextTheme
	s.mu.Unlock()

	if !changed {
		return
	}

	mode, err := s.loadThemeMode()
	if err != nil {
		return
	}
	s.emitThemePreferences(s.themePreferences(mode))
}

func (s *ThemeFacade) emitThemePreferences(preferences apitypes.ThemePreferences) {
	s.mu.Lock()
	app := s.app
	s.mu.Unlock()

	if app != nil && app.Event != nil {
		app.Event.Emit(EventThemePreferencesChanged, preferences)
	}
}
