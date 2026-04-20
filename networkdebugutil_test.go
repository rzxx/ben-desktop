package main

import (
	"errors"
	"testing"

	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/settings"
)

func TestLoadNetworkTraceEnabledSetting(t *testing.T) {
	originalLoader := loadNetworkDebugSettingsState
	loadNetworkDebugSettingsState = func() (settings.State, error) {
		return settings.State{
			NetworkTrace: settings.NetworkTraceSettings{Enabled: true},
		}, nil
	}
	t.Cleanup(func() {
		loadNetworkDebugSettingsState = originalLoader
	})

	enabled, err := loadNetworkTraceEnabledSetting()
	if err != nil {
		t.Fatalf("load network trace enabled setting: %v", err)
	}
	if !enabled {
		t.Fatal("expected network trace to load as enabled")
	}
}

func TestLoadNetworkTraceEnabledSettingReturnsError(t *testing.T) {
	originalLoader := loadNetworkDebugSettingsState
	loadNetworkDebugSettingsState = func() (settings.State, error) {
		return settings.State{}, errors.New("boom")
	}
	t.Cleanup(func() {
		loadNetworkDebugSettingsState = originalLoader
	})

	if _, err := loadNetworkTraceEnabledSetting(); err == nil {
		t.Fatal("expected load network trace enabled setting to fail")
	}
}

func TestSetNetworkTraceEnabledSettingPersistsAndUpdatesRuntime(t *testing.T) {
	originalLoader := loadNetworkDebugSettingsState
	originalSaver := saveNetworkDebugSettingsState
	desktopcore.SetNetworkDebugTraceEnabled(false)

	saved := settings.State{}
	loadNetworkDebugSettingsState = func() (settings.State, error) {
		return settings.State{}, nil
	}
	saveNetworkDebugSettingsState = func(state settings.State) error {
		saved = state
		return nil
	}
	t.Cleanup(func() {
		loadNetworkDebugSettingsState = originalLoader
		saveNetworkDebugSettingsState = originalSaver
		desktopcore.SetNetworkDebugTraceEnabled(false)
	})

	if err := setNetworkTraceEnabledSetting(true); err != nil {
		t.Fatalf("set network trace enabled: %v", err)
	}
	if !saved.NetworkTrace.Enabled {
		t.Fatalf("saved state = %+v, want network trace enabled", saved)
	}
	if !desktopcore.NetworkDebugTraceEnabled() {
		t.Fatal("expected runtime network trace toggle to be enabled")
	}
}
