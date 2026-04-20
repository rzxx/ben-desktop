package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type State struct {
	Core          CoreRuntimeSettings    `json:"core,omitempty"`
	NetworkTrace  NetworkTraceSettings   `json:"networkTrace,omitempty"`
	Notifications NotificationUISettings `json:"notifications,omitempty"`
	PlaybackTrace PlaybackTraceSettings  `json:"playbackTrace,omitempty"`
	Theme         ThemeUISettings        `json:"theme,omitempty"`
}

type CoreRuntimeSettings struct {
	DBPath           string `json:"dbPath,omitempty"`
	BlobRoot         string `json:"blobRoot,omitempty"`
	IdentityKeyPath  string `json:"identityKeyPath,omitempty"`
	FFmpegPath       string `json:"ffmpegPath,omitempty"`
	TranscodeProfile string `json:"transcodeProfile,omitempty"`
}

type NotificationUISettings struct {
	Verbosity string `json:"verbosity,omitempty"`
}

type NetworkTraceSettings struct {
	Enabled bool `json:"enabled,omitempty"`
}

type PlaybackTraceSettings struct {
	Enabled bool `json:"enabled,omitempty"`
}

type ThemeUISettings struct {
	Mode string `json:"mode,omitempty"`
}

const (
	DefaultTranscodeProfile = "aac_lc_vbr_high"
	legacyDesktopProfile    = "desktop"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func DefaultPath(appName string) (string, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configRoot, appName, "settings.json"), nil
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir settings dir: %w", err)
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) Load() (State, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return State{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}

	if strings.TrimSpace(string(payload)) == "" {
		return State{}, nil
	}

	var state State
	if err := json.Unmarshal(payload, &state); err != nil {
		return State{}, err
	}
	return normalizeState(state), nil
}

func (s *Store) Save(state State) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.MarshalIndent(normalizeState(state), "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(s.path, payload, 0o644); err != nil {
		return err
	}
	return nil
}

func (s *Store) Close() error {
	return nil
}

func normalizeState(state State) State {
	state.Core = normalizeCoreRuntimeSettings(state.Core)
	state.NetworkTrace = normalizeNetworkTraceSettings(state.NetworkTrace)
	state.Notifications = normalizeNotificationUISettings(state.Notifications)
	state.PlaybackTrace = normalizePlaybackTraceSettings(state.PlaybackTrace)
	state.Theme = normalizeThemeUISettings(state.Theme)
	return state
}

func normalizeCoreRuntimeSettings(settings CoreRuntimeSettings) CoreRuntimeSettings {
	settings.DBPath = strings.TrimSpace(settings.DBPath)
	settings.BlobRoot = strings.TrimSpace(settings.BlobRoot)
	settings.IdentityKeyPath = strings.TrimSpace(settings.IdentityKeyPath)
	settings.FFmpegPath = strings.TrimSpace(settings.FFmpegPath)
	settings.TranscodeProfile = NormalizeTranscodeProfile(settings.TranscodeProfile)
	return settings
}

func normalizeNotificationUISettings(settings NotificationUISettings) NotificationUISettings {
	settings.Verbosity = NormalizeNotificationVerbosity(settings.Verbosity)
	return settings
}

func normalizeNetworkTraceSettings(settings NetworkTraceSettings) NetworkTraceSettings {
	return settings
}

func normalizePlaybackTraceSettings(settings PlaybackTraceSettings) PlaybackTraceSettings {
	return settings
}

func normalizeThemeUISettings(settings ThemeUISettings) ThemeUISettings {
	settings.Mode = NormalizeThemeMode(settings.Mode)
	return settings
}

func NormalizeTranscodeProfile(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, legacyDesktopProfile) {
		return DefaultTranscodeProfile
	}
	return value
}

func EffectiveTranscodeProfile(value string) string {
	value = NormalizeTranscodeProfile(value)
	if value == "" {
		return DefaultTranscodeProfile
	}
	return value
}

func NormalizeNotificationVerbosity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "important":
		return "important"
	case "everything":
		return "everything"
	default:
		return "user_activity"
	}
}

func NormalizeThemeMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "light":
		return "light"
	case "dark":
		return "dark"
	default:
		return "system"
	}
}
