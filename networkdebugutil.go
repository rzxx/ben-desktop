package main

import (
	"encoding/json"
	"fmt"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/settings"
)

var (
	loadNetworkDebugSettingsState = loadSettingsState
	saveNetworkDebugSettingsState = saveSettingsState
)

func loadNetworkTraceEnabledSetting() (bool, error) {
	state, err := loadNetworkDebugSettingsState()
	if err != nil {
		return false, err
	}
	return state.NetworkTrace.Enabled, nil
}

func setNetworkTraceEnabledSetting(enabled bool) error {
	state, err := loadNetworkDebugSettingsState()
	if err != nil {
		return fmt.Errorf("load network trace settings: %w", err)
	}
	state.NetworkTrace = settings.NetworkTraceSettings{Enabled: enabled}
	if err := saveNetworkDebugSettingsState(state); err != nil {
		return fmt.Errorf("save network trace settings: %w", err)
	}
	desktopcore.SetNetworkDebugTraceEnabled(enabled)
	return nil
}

func buildNetworkDebugDump(status apitypes.NetworkStatus) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"generatedAtMs": time.Now().UTC().UnixMilli(),
		"enabled":       desktopcore.NetworkDebugTraceEnabled(),
		"status":        status,
		"entries":       desktopcore.SnapshotNetworkDebugTrace(),
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
