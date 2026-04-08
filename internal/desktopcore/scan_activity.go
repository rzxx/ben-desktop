package desktopcore

import (
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
)

func newActivityStatus() apitypes.ActivityStatus {
	now := time.Now().UTC()
	return apitypes.ActivityStatus{
		Scan: apitypes.ScanActivityStatus{
			Phase:     "idle",
			UpdatedAt: now,
		},
		Maintenance: apitypes.ScanMaintenanceStatus{
			UpdatedAt: now,
		},
		Artwork: apitypes.ArtworkActivityStatus{
			Phase:     "idle",
			UpdatedAt: now,
		},
		UpdatedAt: now,
	}
}

func (a *App) ActivityStatusSnapshot() apitypes.ActivityStatus {
	if a == nil {
		return newActivityStatus()
	}
	a.activityMu.RLock()
	defer a.activityMu.RUnlock()

	out := a.activity
	if out.Transcodes != nil {
		out.Transcodes = append([]apitypes.TranscodeActivityStatus(nil), out.Transcodes...)
	}
	return out
}

func (a *App) SubscribeActivitySnapshots(listener func(apitypes.ActivityStatus)) func() {
	if a == nil || listener == nil {
		return func() {}
	}

	a.activityMu.Lock()
	id := a.nextActivitySubscriber
	a.nextActivitySubscriber++
	a.activitySubscribers[id] = listener
	a.activityMu.Unlock()

	return func() {
		a.activityMu.Lock()
		delete(a.activitySubscribers, id)
		a.activityMu.Unlock()
	}
}

func (a *App) setScanActivity(status apitypes.ScanActivityStatus) {
	if a == nil {
		return
	}
	a.activityMu.Lock()
	if strings.TrimSpace(status.Phase) == "" {
		status.Phase = "idle"
	}
	status.UpdatedAt = time.Now().UTC()
	a.activity.Scan = status
	a.activity.UpdatedAt = status.UpdatedAt
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) updateScanActivity(apply func(*apitypes.ScanActivityStatus)) {
	if a == nil || apply == nil {
		return
	}
	a.activityMu.Lock()
	apply(&a.activity.Scan)
	if strings.TrimSpace(a.activity.Scan.Phase) == "" {
		a.activity.Scan.Phase = "idle"
	}
	a.activity.Scan.UpdatedAt = time.Now().UTC()
	a.activity.UpdatedAt = a.activity.Scan.UpdatedAt
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) setScanMaintenanceStatus(status apitypes.ScanMaintenanceStatus) {
	if a == nil {
		return
	}
	a.activityMu.Lock()
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	a.activity.Maintenance = status
	a.activity.UpdatedAt = status.UpdatedAt
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) setArtworkActivity(status apitypes.ArtworkActivityStatus) {
	if a == nil {
		return
	}
	a.activityMu.Lock()
	if strings.TrimSpace(status.Phase) == "" {
		status.Phase = "idle"
	}
	status.UpdatedAt = time.Now().UTC()
	a.activity.Artwork = status
	a.activity.UpdatedAt = status.UpdatedAt
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) updateArtworkActivity(apply func(*apitypes.ArtworkActivityStatus)) {
	if a == nil || apply == nil {
		return
	}
	a.activityMu.Lock()
	apply(&a.activity.Artwork)
	if strings.TrimSpace(a.activity.Artwork.Phase) == "" {
		a.activity.Artwork.Phase = "idle"
	}
	a.activity.Artwork.UpdatedAt = time.Now().UTC()
	a.activity.UpdatedAt = a.activity.Artwork.UpdatedAt
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) setTranscodeActivity(key string, status apitypes.TranscodeActivityStatus) {
	if a == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if strings.TrimSpace(status.Phase) == "" {
		status.Phase = "running"
	}
	a.activityMu.Lock()
	if a.transcodeActivity == nil {
		a.transcodeActivity = make(map[string]apitypes.TranscodeActivityStatus)
	}
	a.transcodeActivity[key] = status
	a.rebuildTranscodeActivityLocked()
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) clearTranscodeActivity(key string) {
	if a == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	a.activityMu.Lock()
	if len(a.transcodeActivity) == 0 {
		a.activityMu.Unlock()
		return
	}
	delete(a.transcodeActivity, key)
	a.rebuildTranscodeActivityLocked()
	snapshot := a.activity
	subscribers := a.snapshotActivitySubscribersLocked()
	a.activityMu.Unlock()

	notifyActivitySubscribers(subscribers, snapshot)
}

func (a *App) rebuildTranscodeActivityLocked() {
	if len(a.transcodeActivity) == 0 {
		a.activity.Transcodes = nil
		a.activity.UpdatedAt = time.Now().UTC()
		return
	}
	items := make([]apitypes.TranscodeActivityStatus, 0, len(a.transcodeActivity))
	for _, status := range a.transcodeActivity {
		items = append(items, status)
	}
	a.activity.Transcodes = items
	a.activity.UpdatedAt = time.Now().UTC()
}

func (a *App) snapshotActivitySubscribersLocked() []func(apitypes.ActivityStatus) {
	if len(a.activitySubscribers) == 0 {
		return nil
	}

	out := make([]func(apitypes.ActivityStatus), 0, len(a.activitySubscribers))
	for _, subscriber := range a.activitySubscribers {
		out = append(out, subscriber)
	}
	return out
}

func notifyActivitySubscribers(subscribers []func(apitypes.ActivityStatus), snapshot apitypes.ActivityStatus) {
	if snapshot.Transcodes != nil {
		snapshot.Transcodes = append([]apitypes.TranscodeActivityStatus(nil), snapshot.Transcodes...)
	}
	for _, subscriber := range subscribers {
		subscriber(snapshot)
	}
}
