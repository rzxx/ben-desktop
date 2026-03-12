package desktopcore

import (
	"strings"
	"time"

	apitypes "ben/core/api/types"
)

type scanFlight struct {
	roots map[string]string
	stats apitypes.ScanStats
	err   error
	done  chan struct{}
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
