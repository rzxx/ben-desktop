package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/settings"
)

type coreHost struct {
	mu sync.RWMutex

	started          bool
	runtime          desktopcore.Runtime
	blobRoot         string
	preferredProfile string
}

func newCoreHost() *coreHost {
	return &coreHost{}
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
	h.runtime = openCoreRuntime(ctx, coreSettings)
	h.blobRoot = resolvedBlobRoot(coreSettings)
	h.preferredProfile = preferredProfile(coreSettings)
	h.started = true
	return nil
}

func (h *coreHost) Close() error {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	runtime := h.runtime
	h.runtime = nil
	h.blobRoot = ""
	h.preferredProfile = ""
	h.started = false
	h.mu.Unlock()

	if runtime == nil {
		return nil
	}
	return runtime.Close()
}

func (h *coreHost) Runtime() desktopcore.Runtime {
	if h == nil {
		return desktopcore.NewUnavailableCore(fmt.Errorf("core runtime is not available"))
	}

	h.mu.RLock()
	runtime := h.runtime
	h.mu.RUnlock()
	if runtime == nil {
		return desktopcore.NewUnavailableCore(fmt.Errorf("core runtime is not available"))
	}
	return runtime
}

func (h *coreHost) BlobRoot() string {
	if h == nil {
		return ""
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.blobRoot
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

func openCoreRuntime(ctx context.Context, coreSettings settings.CoreRuntimeSettings) desktopcore.Runtime {
	runtime, err := desktopcore.OpenFromSettings(ctx, coreSettings)
	if err != nil {
		log.Printf("playback: desktop core runtime unavailable: %v", err)
		return desktopcore.NewUnavailableCore(err)
	}
	return runtime
}
