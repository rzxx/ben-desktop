package main

import (
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
)

func TestNotificationFromJobMapsClassificationAndPhase(t *testing.T) {
	job := desktopcore.JobSnapshot{
		JobID:     "job-1",
		Kind:      "sync-now",
		LibraryID: "lib-1",
		Phase:     desktopcore.JobPhaseRunning,
		Progress:  0.4,
		Message:   "syncing peers",
		CreatedAt: time.Now().UTC().Add(-time.Minute),
		UpdatedAt: time.Now().UTC(),
	}

	got := notificationFromJob(job)
	if got.ID != "job:job-1" {
		t.Fatalf("notification id = %q, want %q", got.ID, "job:job-1")
	}
	if got.Audience != apitypes.NotificationAudienceUser {
		t.Fatalf("notification audience = %q, want %q", got.Audience, apitypes.NotificationAudienceUser)
	}
	if got.Importance != apitypes.NotificationImportanceNormal {
		t.Fatalf("notification importance = %q, want %q", got.Importance, apitypes.NotificationImportanceNormal)
	}
	if got.Phase != apitypes.NotificationPhaseRunning {
		t.Fatalf("notification phase = %q, want %q", got.Phase, apitypes.NotificationPhaseRunning)
	}
}

func TestNormalizeNotificationSnapshotPreservesFinishedFailures(t *testing.T) {
	notification := normalizeNotificationSnapshot(apitypes.NotificationSnapshot{
		ID:    "scan:1",
		Kind:  "scan-activity",
		Phase: apitypes.NotificationPhaseError,
	})

	if !notification.Sticky {
		t.Fatalf("error notification should be sticky")
	}
	if notification.FinishedAt.IsZero() {
		t.Fatalf("error notification should have finished timestamp")
	}
}

func TestNotificationFromTranscodeUsesLocalRequesterAsUserImportant(t *testing.T) {
	item := apitypes.TranscodeActivityStatus{
		RecordingID:       "rec-1",
		SourceFileID:      "src-1",
		Profile:           "aac_lc_vbr_high",
		RequesterDeviceID: "device-local",
		Phase:             "running",
		StartedAt:         time.Now().UTC(),
	}

	got := notificationFromTranscode(item, "device-local")
	if got.Audience != apitypes.NotificationAudienceUser {
		t.Fatalf("notification audience = %q, want %q", got.Audience, apitypes.NotificationAudienceUser)
	}
	if got.Importance != apitypes.NotificationImportanceImportant {
		t.Fatalf("notification importance = %q, want %q", got.Importance, apitypes.NotificationImportanceImportant)
	}
}

func TestNotificationFromTranscodeUsesOtherRequesterAsSystemDebug(t *testing.T) {
	item := apitypes.TranscodeActivityStatus{
		RecordingID:       "rec-1",
		SourceFileID:      "src-1",
		Profile:           "aac_lc_vbr_high",
		RequesterDeviceID: "device-remote",
		Phase:             "running",
		StartedAt:         time.Now().UTC(),
	}

	got := notificationFromTranscode(item, "device-local")
	if got.Audience != apitypes.NotificationAudienceSystem {
		t.Fatalf("notification audience = %q, want %q", got.Audience, apitypes.NotificationAudienceSystem)
	}
	if got.Importance != apitypes.NotificationImportanceDebug {
		t.Fatalf("notification importance = %q, want %q", got.Importance, apitypes.NotificationImportanceDebug)
	}
}

func TestUpsertNotificationMutatesSingleItem(t *testing.T) {
	_jsii := &NotificationsFacade{
		notifications: make(map[string]apitypes.NotificationSnapshot),
	}

	_jsii.upsertNotification(apitypes.NotificationSnapshot{
		ID:    "job:1",
		Kind:  "sync-now",
		Phase: apitypes.NotificationPhaseQueued,
	})
	first, ok := _jsii.notifications["job:1"]
	if !ok {
		t.Fatalf("expected first notification")
	}

	_jsii.upsertNotification(apitypes.NotificationSnapshot{
		ID:    "job:1",
		Kind:  "sync-now",
		Phase: apitypes.NotificationPhaseRunning,
	})

	if len(_jsii.notifications) != 1 {
		t.Fatalf("notification count = %d, want 1", len(_jsii.notifications))
	}
	updated := _jsii.notifications["job:1"]
	if updated.Phase != apitypes.NotificationPhaseRunning {
		t.Fatalf("updated phase = %q, want %q", updated.Phase, apitypes.NotificationPhaseRunning)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created at changed from %v to %v", first.CreatedAt, updated.CreatedAt)
	}
}
