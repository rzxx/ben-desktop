package main

import (
	"strings"

	"ben/desktop/internal/playback"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const appWindowBaseTitle = "ben"

func playbackWindowTitle(snapshot playback.SessionSnapshot) string {
	title := playbackSnapshotTitle(snapshot)
	if title == "" {
		return appWindowBaseTitle
	}
	return appWindowBaseTitle + " • " + title
}

func playbackSnapshotTitle(snapshot playback.SessionSnapshot) string {
	if snapshot.CurrentEntry != nil {
		if title := strings.TrimSpace(snapshot.CurrentEntry.Item.Title); title != "" {
			return title
		}
	}
	if snapshot.CurrentItem != nil {
		if title := strings.TrimSpace(snapshot.CurrentItem.Title); title != "" {
			return title
		}
	}
	if snapshot.LoadingEntry != nil {
		if title := strings.TrimSpace(snapshot.LoadingEntry.Item.Title); title != "" {
			return title
		}
	}
	if snapshot.LoadingItem != nil {
		if title := strings.TrimSpace(snapshot.LoadingItem.Title); title != "" {
			return title
		}
	}
	return ""
}

func applyPlaybackWindowTitle(app *application.App, snapshot playback.SessionSnapshot) {
	if app == nil || app.Window == nil {
		return
	}

	title := playbackWindowTitle(snapshot)
	for _, window := range app.Window.GetAll() {
		if window == nil {
			continue
		}
		window.SetTitle(title)
	}
}
