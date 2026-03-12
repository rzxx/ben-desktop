package corebridge

import (
	"context"
	"path/filepath"
	"strings"

	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/settings"
)

type Config struct {
	Core desktopcore.Config
}

type RuntimeBridge struct {
	*desktopcore.App
}

type UnavailableBridge = desktopcore.UnavailableCore

func Open(ctx context.Context, cfg Config) (*RuntimeBridge, error) {
	app, err := desktopcore.Open(ctx, cfg.Core)
	if err != nil {
		return nil, err
	}
	return &RuntimeBridge{App: app}, nil
}

func OpenFromSettings(ctx context.Context, stored settings.CoreRuntimeSettings) (*RuntimeBridge, error) {
	return Open(ctx, configFromSettings(stored))
}

func ResolveConfigFromSettings(stored settings.CoreRuntimeSettings) (Config, error) {
	cfg, err := desktopcore.ResolveConfigFromSettings(stored)
	if err != nil {
		return Config{}, err
	}
	return Config{Core: cfg}, nil
}

func configFromSettings(stored settings.CoreRuntimeSettings) Config {
	cfg := desktopcore.ConfigFromSettings(stored)
	if strings.TrimSpace(cfg.BlobRoot) == "" && strings.TrimSpace(cfg.DBPath) != "" {
		cfg.BlobRoot = filepath.Join(filepath.Dir(cfg.DBPath), "blobs")
	}
	if strings.TrimSpace(cfg.IdentityKeyPath) == "" && strings.TrimSpace(cfg.DBPath) != "" {
		cfg.IdentityKeyPath = filepath.Join(filepath.Dir(cfg.DBPath), "identity.key")
	}
	return Config{Core: cfg}
}

func NewUnavailableBridge(err error) *UnavailableBridge {
	return desktopcore.NewUnavailableCore(err)
}
