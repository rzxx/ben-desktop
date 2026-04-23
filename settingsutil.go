package main

import "ben/desktop/internal/settings"

func loadSettingsState() (settings.State, error) {
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		return settings.State{}, err
	}
	store, err := settings.NewStore(settingsPath)
	if err != nil {
		return settings.State{}, err
	}
	defer func() { _ = store.Close() }()
	return store.Load()
}

func saveSettingsState(state settings.State) error {
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		return err
	}
	store, err := settings.NewStore(settingsPath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return store.Save(state)
}
