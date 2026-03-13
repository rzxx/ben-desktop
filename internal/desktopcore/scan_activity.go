package desktopcore

import (
	"strings"
	"time"

	apitypes "ben/core/api/types"
)

type scanFlight struct {
	libraryID string
	roots     map[string]string
	stats     apitypes.ScanStats
	err       error
	done      chan struct{}
}

func newActivityStatus() apitypes.ActivityStatus {
	now := time.Now().UTC()
	return apitypes.ActivityStatus{
		Scan: apitypes.ScanActivityStatus{
			Phase:     "idle",
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

func (a *App) setScanActivity(status apitypes.ScanActivityStatus) {
	if a == nil {
		return
	}
	a.activityMu.Lock()
	defer a.activityMu.Unlock()

	if strings.TrimSpace(status.Phase) == "" {
		status.Phase = "idle"
	}
	status.UpdatedAt = time.Now().UTC()
	a.activity.Scan = status
	a.activity.UpdatedAt = status.UpdatedAt
}

func (a *App) updateScanActivity(apply func(*apitypes.ScanActivityStatus)) {
	if a == nil || apply == nil {
		return
	}
	a.activityMu.Lock()
	defer a.activityMu.Unlock()

	apply(&a.activity.Scan)
	if strings.TrimSpace(a.activity.Scan.Phase) == "" {
		a.activity.Scan.Phase = "idle"
	}
	a.activity.Scan.UpdatedAt = time.Now().UTC()
	a.activity.UpdatedAt = a.activity.Scan.UpdatedAt
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
	defer a.activityMu.Unlock()
	if a.transcodeActivity == nil {
		a.transcodeActivity = make(map[string]apitypes.TranscodeActivityStatus)
	}
	a.transcodeActivity[key] = status
	a.rebuildTranscodeActivityLocked()
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
	defer a.activityMu.Unlock()
	if len(a.transcodeActivity) == 0 {
		return
	}
	delete(a.transcodeActivity, key)
	a.rebuildTranscodeActivityLocked()
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
