package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/observability"
	"ben/desktop/internal/settings"
)

type coreHost struct {
	mu sync.RWMutex

	started bool
	*desktopcore.App
	unavailable      *desktopcore.UnavailableCore
	library          desktopcore.LibraryRuntime
	network          desktopcore.NetworkRuntime
	jobs             desktopcore.JobsRuntime
	catalog          desktopcore.CatalogRuntime
	pin              desktopcore.PinRuntime
	invite           desktopcore.InviteRuntime
	cache            desktopcore.CacheRuntime
	playback         desktopcore.PlaybackRuntime
	preferredProfile string
}

func newCoreHost() *coreHost {
	return &coreHost{
		unavailable: desktopcore.NewUnavailableCore(errors.New("desktop core is not started")),
	}
}

func (h *coreHost) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}
	ctx, span := observability.Start(ctx, "corehost.start", observability.String("service", "corehost"))
	defer span.End()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		span.Event("corehost.already_started")
		return nil
	}

	coreSettings := loadCoreRuntimeSettings()
	span.SetInput(observability.RedactedSummary("core runtime settings", map[string]any{
		"db_path_set":         coreSettings.DBPath != "",
		"blob_root_set":       coreSettings.BlobRoot != "",
		"identity_key_set":    coreSettings.IdentityKeyPath != "",
		"transcode_profile":   coreSettings.TranscodeProfile,
		"relay_bootstrap":     len(coreSettings.RelayBootstrap),
		"registry_url_set":    coreSettings.RegistryURL != "",
		"lan_discovery_set":   coreSettings.EnableLANDiscovery != nil,
		"direct_transfer_set": coreSettings.RequireDirectForLargeTransfers != nil,
	}))
	runtime, err := openCoreRuntime(ctx, coreSettings)
	if err != nil {
		span.RecordError(err)
		return err
	}
	h.App = runtime
	h.library = runtime.LibraryRuntime()
	h.network = runtime.NetworkRuntime()
	h.jobs = runtime.Jobs()
	h.catalog = runtime.CatalogRuntime()
	h.pin = runtime.PinRuntime()
	h.invite = runtime.InviteRuntime()
	h.cache = runtime.CacheRuntime()
	h.playback = runtime.PlaybackRuntime()
	h.preferredProfile = preferredProfile(coreSettings)
	h.started = true
	span.SetOutput(observability.Summary("core runtime started", map[string]any{
		"preferred_profile": h.preferredProfile,
	}))
	return nil
}

func (h *coreHost) Close() error {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	runtime := h.App
	h.App = nil
	h.library = nil
	h.network = nil
	h.jobs = nil
	h.catalog = nil
	h.pin = nil
	h.invite = nil
	h.cache = nil
	h.playback = nil
	h.preferredProfile = ""
	h.started = false
	h.mu.Unlock()

	if runtime == nil {
		return nil
	}
	return runtime.Close()
}

func (h *coreHost) PreferredProfile() string {
	if h == nil {
		return settings.DefaultTranscodeProfile
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.preferredProfile == "" {
		return settings.DefaultTranscodeProfile
	}
	return h.preferredProfile
}

func (h *coreHost) LibraryRuntime() desktopcore.LibraryRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.library == nil {
		return h.unavailable
	}
	return h.library
}

func (h *coreHost) NetworkRuntime() desktopcore.NetworkRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.network == nil {
		return h.unavailable
	}
	return h.network
}

func (h *coreHost) JobsRuntime() desktopcore.JobsRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.jobs == nil {
		return h.unavailable
	}
	return h.jobs
}

func (h *coreHost) CatalogRuntime() desktopcore.CatalogRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.catalog == nil {
		return h.unavailable
	}
	return h.catalog
}

func (h *coreHost) PinRuntime() desktopcore.PinRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.pin == nil {
		return h.unavailable
	}
	return h.pin
}

func (h *coreHost) InviteRuntime() desktopcore.InviteRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.invite == nil {
		return h.unavailable
	}
	return h.invite
}

func (h *coreHost) CacheRuntime() desktopcore.CacheRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.cache == nil {
		return h.unavailable
	}
	return h.cache
}

func (h *coreHost) PlaybackRuntime() desktopcore.PlaybackRuntime {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.playback == nil {
		return h.unavailable
	}
	return h.playback
}

func loadCoreRuntimeSettings() settings.CoreRuntimeSettings {
	coreSettings := settings.CoreRuntimeSettings{}
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		slog.Error("resolve settings path failed", slog.Any("error", err), slog.String("service", "settings"))
		return coreSettings
	}

	settingsStore, err := settings.NewStore(settingsPath)
	if err != nil {
		slog.Error("open settings store failed", slog.Any("error", err), slog.String("service", "settings"))
		return coreSettings
	}
	defer func() {
		if closeErr := settingsStore.Close(); closeErr != nil {
			slog.Warn("close settings store failed", slog.Any("error", closeErr), slog.String("service", "settings"))
		}
	}()

	state, err := settingsStore.Load()
	if err != nil {
		slog.Error("load settings failed", slog.Any("error", err), slog.String("service", "settings"))
		return coreSettings
	}
	return state.Core
}

func openCoreRuntime(ctx context.Context, coreSettings settings.CoreRuntimeSettings) (*desktopcore.App, error) {
	ctx, span := observability.Start(ctx, "desktopcore.open", observability.String("service", "desktopcore"))
	defer span.End()
	cfg := desktopcore.ConfigFromSettings(coreSettings)
	cfg.Logger = observability.NewLegacyLogger(slog.Default(), "desktopcore")
	runtime, err := desktopcore.Open(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		slog.Error("desktop core runtime unavailable", slog.Any("error", err), slog.String("service", "desktopcore"))
		return nil, err
	}
	return runtime, nil
}
