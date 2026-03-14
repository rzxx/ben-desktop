package main

import (
	"context"
	"errors"
	"log"
	"sync"

	"ben/desktop/internal/desktopcore"
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

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return nil
	}

	coreSettings := loadCoreRuntimeSettings()
	runtime, err := openCoreRuntime(ctx, coreSettings)
	if err != nil {
		return err
	}
	h.App = runtime
	h.library = runtime.LibraryRuntime()
	h.network = runtime.NetworkRuntime()
	h.jobs = runtime.Jobs()
	h.catalog = runtime.CatalogRuntime()
	h.invite = runtime.InviteRuntime()
	h.cache = runtime.CacheRuntime()
	h.playback = runtime.PlaybackRuntime()
	h.preferredProfile = preferredProfile(coreSettings)
	h.started = true
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
		log.Printf("playback: resolve settings path: %v", err)
		return coreSettings
	}

	settingsStore, err := settings.NewStore(settingsPath)
	if err != nil {
		log.Printf("playback: open settings store: %v", err)
		return coreSettings
	}
	defer func() {
		if closeErr := settingsStore.Close(); closeErr != nil {
			log.Printf("playback: close settings store: %v", closeErr)
		}
	}()

	state, err := settingsStore.Load()
	if err != nil {
		log.Printf("playback: load settings: %v", err)
		return coreSettings
	}
	return state.Core
}

func openCoreRuntime(ctx context.Context, coreSettings settings.CoreRuntimeSettings) (*desktopcore.App, error) {
	runtime, err := desktopcore.OpenFromSettings(ctx, coreSettings)
	if err != nil {
		log.Printf("playback: desktop core runtime unavailable: %v", err)
		return nil, err
	}
	return runtime, nil
}
