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

func TestScanJobAndActivityCollapseIntoSingleNotification(t *testing.T) {
	_jsii := &NotificationsFacade{
		notifications:    make(map[string]apitypes.NotificationSnapshot),
		activeTranscodes: make(map[string]apitypes.NotificationSnapshot),
	}

	_jsii.handleJobSnapshot(desktopcore.JobSnapshot{
		JobID:     "scan-job-1",
		Kind:      "scan-library",
		LibraryID: "lib-1",
		Phase:     desktopcore.JobPhaseQueued,
		Progress:  0.05,
		Message:   "queued library scan",
		CreatedAt: time.Now().UTC().Add(-time.Second),
		UpdatedAt: time.Now().UTC().Add(-time.Second),
	})

	if len(_jsii.notifications) != 1 {
		t.Fatalf("notification count after queued scan = %d, want 1", len(_jsii.notifications))
	}

	var scanID string
	for id, notification := range _jsii.notifications {
		scanID = id
		if notification.Kind != "scan-library" {
			t.Fatalf("notification kind = %q, want %q", notification.Kind, "scan-library")
		}
		if notification.Audience != apitypes.NotificationAudienceUser {
			t.Fatalf("notification audience = %q, want %q", notification.Audience, apitypes.NotificationAudienceUser)
		}
		if notification.Importance != apitypes.NotificationImportanceNormal {
			t.Fatalf("notification importance = %q, want %q", notification.Importance, apitypes.NotificationImportanceNormal)
		}
		if notification.Phase != apitypes.NotificationPhaseQueued {
			t.Fatalf("notification phase = %q, want %q", notification.Phase, apitypes.NotificationPhaseQueued)
		}
	}
	if scanID == "" {
		t.Fatalf("expected scan notification id")
	}

	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "enumerating",
		RootsTotal:  1,
		CurrentRoot: "G:\\Music",
	})

	if len(_jsii.notifications) != 1 {
		t.Fatalf("notification count after scan activity = %d, want 1", len(_jsii.notifications))
	}
	updated := _jsii.notifications[scanID]
	if updated.Phase != apitypes.NotificationPhaseRunning {
		t.Fatalf("running phase = %q, want %q", updated.Phase, apitypes.NotificationPhaseRunning)
	}
	if updated.Kind != "scan-library" {
		t.Fatalf("running kind = %q, want %q", updated.Kind, "scan-library")
	}
	if updated.Audience != apitypes.NotificationAudienceUser {
		t.Fatalf("running audience = %q, want %q", updated.Audience, apitypes.NotificationAudienceUser)
	}
}

func TestArtworkActivityReusesSingleNotificationAcrossSequentialAlbums(t *testing.T) {
	_jsii := &NotificationsFacade{
		notifications:    make(map[string]apitypes.NotificationSnapshot),
		activeTranscodes: make(map[string]apitypes.NotificationSnapshot),
	}

	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    1,
		CurrentAlbumID: "album-1",
	})

	if len(_jsii.notifications) != 1 {
		t.Fatalf("notification count after first artwork run = %d, want 1", len(_jsii.notifications))
	}

	notification, ok := _jsii.notifications["artwork:activity"]
	if !ok {
		t.Fatalf("expected stable artwork notification id")
	}
	if notification.Message != "Generating artwork for album-1" {
		t.Fatalf("first artwork message = %q", notification.Message)
	}

	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:       "completed",
		AlbumsTotal: 1,
		AlbumsDone:  1,
	})
	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    1,
		CurrentAlbumID: "album-2",
	})

	if len(_jsii.notifications) != 1 {
		t.Fatalf("notification count after second artwork run = %d, want 1", len(_jsii.notifications))
	}

	notification = _jsii.notifications["artwork:activity"]
	if notification.Phase != apitypes.NotificationPhaseRunning {
		t.Fatalf("artwork phase = %q, want %q", notification.Phase, apitypes.NotificationPhaseRunning)
	}
	if notification.Message != "Generating artwork for album-2" {
		t.Fatalf("second artwork message = %q", notification.Message)
	}
}
