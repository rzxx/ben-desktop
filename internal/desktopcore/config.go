package desktopcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/settings"
)

const defaultCacheBytes = int64(10 * 1024 * 1024 * 1024)

type Config struct {
	DBPath           string
	BlobRoot         string
	IdentityKeyPath  string
	FFmpegPath       string
	TranscodeProfile string
	CacheBytes       int64
	TagReader        TagReader
	Logger           apitypes.Logger
}

func DefaultDataRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "ben", "v2"), nil
}

func DefaultDBPath() (string, error) {
	root, err := DefaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "library.db"), nil
}

func DefaultBlobRoot() (string, error) {
	root, err := DefaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "blobs"), nil
}

func DefaultIdentityPath() (string, error) {
	root, err := DefaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "identity.key"), nil
}

func OpenFromSettings(ctx context.Context, stored settings.CoreRuntimeSettings) (*App, error) {
	return Open(ctx, ConfigFromSettings(stored))
}

func ConfigFromSettings(stored settings.CoreRuntimeSettings) Config {
	return Config{
		DBPath:           strings.TrimSpace(stored.DBPath),
		BlobRoot:         strings.TrimSpace(stored.BlobRoot),
		IdentityKeyPath:  strings.TrimSpace(stored.IdentityKeyPath),
		FFmpegPath:       strings.TrimSpace(stored.FFmpegPath),
		TranscodeProfile: settings.EffectiveTranscodeProfile(stored.TranscodeProfile),
	}
}

func ResolveConfigFromSettings(stored settings.CoreRuntimeSettings) (Config, error) {
	return ResolveConfig(ConfigFromSettings(stored))
}

func ResolveConfig(cfg Config) (Config, error) {
	cfg.DBPath = strings.TrimSpace(cfg.DBPath)
	cfg.BlobRoot = strings.TrimSpace(cfg.BlobRoot)
	cfg.IdentityKeyPath = strings.TrimSpace(cfg.IdentityKeyPath)
	cfg.FFmpegPath = strings.TrimSpace(cfg.FFmpegPath)
	cfg.TranscodeProfile = settings.EffectiveTranscodeProfile(cfg.TranscodeProfile)

	if cfg.DBPath == "" {
		path, err := DefaultDBPath()
		if err != nil {
			return Config{}, err
		}
		cfg.DBPath = path
	}
	if cfg.BlobRoot == "" {
		path, err := DefaultBlobRoot()
		if err != nil {
			return Config{}, err
		}
		cfg.BlobRoot = path
	}
	if cfg.IdentityKeyPath == "" {
		path, err := DefaultIdentityPath()
		if err != nil {
			return Config{}, err
		}
		cfg.IdentityKeyPath = path
	}
	if cfg.FFmpegPath == "" {
		cfg.FFmpegPath = "ffmpeg"
	}
	if cfg.TranscodeProfile == "" {
		cfg.TranscodeProfile = settings.DefaultTranscodeProfile
	}
	if cfg.CacheBytes <= 0 {
		cfg.CacheBytes = defaultCacheBytes
	}
	if cfg.Logger == nil {
		cfg.Logger = noopLogger{}
	}
	return cfg, nil
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}

func (noopLogger) Errorf(string, ...any) {}
