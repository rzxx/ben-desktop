package main

import (
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/playback"
)

func newNotificationFacadeForTest(now *time.Time, emitted *[]apitypes.NotificationSnapshot) *NotificationsFacade {
	return &NotificationsFacade{
		notifications:     make(map[string]apitypes.NotificationSnapshot),
		activeTranscodes:  make(map[string]apitypes.NotificationSnapshot),
		scanEmitStates:    make(map[string]notificationEmitState),
		artworkEmitStates: make(map[string]notificationEmitState),
		now: func() time.Time {
			if now == nil {
				return time.Now().UTC()
			}
			return *now
		},
		emitNotification: func(notification apitypes.NotificationSnapshot) {
			if emitted != nil {
				*emitted = append(*emitted, notification)
			}
		},
	}
}

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

func TestScanRunningNotificationsAreCoalesced(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	_jsii := newNotificationFacadeForTest(&now, &emitted)
	_jsii.setScanNotificationMetadata(
		"scan-library",
		apitypes.NotificationAudienceUser,
		apitypes.NotificationImportanceNormal,
	)

	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:         "ingesting",
		TracksDone:    1,
		TracksTotal:   100,
		WorkersActive: 1,
	})

	now = now.Add(100 * time.Millisecond)
	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "ingesting",
		TracksDone:  2,
		TracksTotal: 100,
	})

	now = now.Add(100 * time.Millisecond)
	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "ingesting",
		TracksDone:  3,
		TracksTotal: 100,
	})

	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if emitted[0].Phase != apitypes.NotificationPhaseRunning {
		t.Fatalf("emitted phase = %q, want %q", emitted[0].Phase, apitypes.NotificationPhaseRunning)
	}
}

func TestScanQueuedAndTerminalNotificationsBypassThrottle(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handleScanJobSnapshot(desktopcore.JobSnapshot{
		JobID:     "scan-job-1",
		Kind:      "scan-library",
		LibraryID: "lib-1",
		Phase:     desktopcore.JobPhaseQueued,
		Message:   "queued library scan",
	})

	now = now.Add(10 * time.Millisecond)
	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:         "ingesting",
		TracksDone:    1,
		TracksTotal:   100,
		WorkersActive: 1,
	})

	now = now.Add(10 * time.Millisecond)
	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "completed",
		TracksDone:  100,
		TracksTotal: 100,
	})

	if len(emitted) != 3 {
		t.Fatalf("emitted notifications = %d, want 3", len(emitted))
	}
	if emitted[0].Phase != apitypes.NotificationPhaseQueued {
		t.Fatalf("queued phase = %q, want %q", emitted[0].Phase, apitypes.NotificationPhaseQueued)
	}
	if emitted[2].Phase != apitypes.NotificationPhaseSuccess {
		t.Fatalf("terminal phase = %q, want %q", emitted[2].Phase, apitypes.NotificationPhaseSuccess)
	}
}

func TestScanRunningJobSnapshotsOnlyUpdateMetadata(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 2)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handleScanJobSnapshot(desktopcore.JobSnapshot{
		JobID:     "scan-job-1",
		Kind:      "scan-library",
		LibraryID: "lib-1",
		Phase:     desktopcore.JobPhaseQueued,
		Message:   "queued library scan",
	})

	now = now.Add(5 * time.Millisecond)
	_jsii.handleScanJobSnapshot(desktopcore.JobSnapshot{
		JobID:     "scan-job-1",
		Kind:      "scan-library",
		LibraryID: "lib-1",
		Phase:     desktopcore.JobPhaseRunning,
		Message:   "ingesting track",
		Progress:  0.15,
	})

	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if got := _jsii.scanNotificationKind; got != "scan-library" {
		t.Fatalf("scan notification kind = %q, want %q", got, "scan-library")
	}
}

func TestSuppressedScanNotificationStillUpdatesStoredSnapshot(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 2)
	_jsii := newNotificationFacadeForTest(&now, &emitted)
	_jsii.setScanNotificationMetadata(
		"scan-library",
		apitypes.NotificationAudienceUser,
		apitypes.NotificationImportanceNormal,
	)

	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:         "ingesting",
		TracksDone:    1,
		TracksTotal:   100,
		WorkersActive: 1,
	})

	now = now.Add(100 * time.Millisecond)
	_jsii.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "ingesting",
		TracksDone:  2,
		TracksTotal: 100,
	})

	notifications := _jsii.ListNotifications()
	if len(notifications) != 1 {
		t.Fatalf("notification count = %d, want 1", len(notifications))
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if got := notifications[0].Progress; got != 0.02 {
		t.Fatalf("stored progress = %v, want %v", got, 0.02)
	}
	if notifications[0].UpdatedAt != now {
		t.Fatalf("stored updated at = %v, want %v", notifications[0].UpdatedAt, now)
	}
}

func TestArtworkRunningNotificationsAreCoalesced(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    100,
		CurrentAlbumID: "album-1",
	})

	now = now.Add(100 * time.Millisecond)
	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    100,
		AlbumsDone:     1,
		CurrentAlbumID: "album-2",
	})

	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
}

func TestArtworkTerminalNotificationsBypassThrottle(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    10,
		CurrentAlbumID: "album-1",
	})

	now = now.Add(10 * time.Millisecond)
	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:       "completed",
		AlbumsTotal: 10,
		AlbumsDone:  10,
	})

	if len(emitted) != 2 {
		t.Fatalf("emitted notifications = %d, want 2", len(emitted))
	}
	if emitted[1].Phase != apitypes.NotificationPhaseSuccess {
		t.Fatalf("terminal phase = %q, want %q", emitted[1].Phase, apitypes.NotificationPhaseSuccess)
	}
}

func TestSuppressedArtworkNotificationStillUpdatesStoredSnapshot(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 2)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    100,
		CurrentAlbumID: "album-1",
	})

	now = now.Add(100 * time.Millisecond)
	_jsii.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    100,
		AlbumsDone:     1,
		CurrentAlbumID: "album-2",
	})

	notifications := _jsii.ListNotifications()
	if len(notifications) != 1 {
		t.Fatalf("notification count = %d, want 1", len(notifications))
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if got := notifications[0].Progress; got != 0.01 {
		t.Fatalf("stored progress = %v, want %v", got, 0.01)
	}
	if notifications[0].Message != "Generating artwork for album-2" {
		t.Fatalf("stored message = %q, want %q", notifications[0].Message, "Generating artwork for album-2")
	}
}

func TestPlaybackSkipNotificationsEmitAfterInitialPrime(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handlePlaybackSnapshot(playback.SessionSnapshot{
		LastSkipEvent: &playback.PlaybackSkipEvent{
			EventID: "skip-primed",
			Message: "Primed skip event.",
			Count:   1,
		},
	})

	if len(emitted) != 0 {
		t.Fatalf("initial playback snapshot should not emit skip toast, got %d", len(emitted))
	}

	now = now.Add(time.Second)
	_jsii.handlePlaybackSnapshot(playback.SessionSnapshot{
		LastSkipEvent: &playback.PlaybackSkipEvent{
			EventID: "skip-2",
			Message: "Skipped unavailable track: Track 2.",
			Count:   1,
			FirstEntry: &playback.SessionEntry{
				Item: playback.SessionItem{
					RecordingID: "rec-2",
					Title:       "Track 2",
					Subtitle:    "Artist",
				},
			},
		},
	})

	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if emitted[0].Kind != "playback-skip" {
		t.Fatalf("kind = %q, want %q", emitted[0].Kind, "playback-skip")
	}
	if emitted[0].Phase != apitypes.NotificationPhaseSuccess {
		t.Fatalf("phase = %q, want %q", emitted[0].Phase, apitypes.NotificationPhaseSuccess)
	}
	if emitted[0].Subject == nil || emitted[0].Subject.Title != "Track 2" {
		t.Fatalf("subject = %+v, want track title", emitted[0].Subject)
	}
}

func TestPlaybackSkipNotificationsUseErrorPhaseWhenPlaybackStops(t *testing.T) {
	now := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	_jsii := newNotificationFacadeForTest(&now, &emitted)

	_jsii.handlePlaybackSnapshot(playback.SessionSnapshot{})
	now = now.Add(time.Second)
	_jsii.handlePlaybackSnapshot(playback.SessionSnapshot{
		LastSkipEvent: &playback.PlaybackSkipEvent{
			EventID: "skip-stop",
			Message: "Skipped 3 unavailable tracks. No playable tracks remain.",
			Count:   3,
			Stopped: true,
		},
	})

	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if emitted[0].Phase != apitypes.NotificationPhaseError {
		t.Fatalf("phase = %q, want %q", emitted[0].Phase, apitypes.NotificationPhaseError)
	}
	if !emitted[0].Sticky {
		t.Fatalf("stopped skip notification should be sticky")
	}
}
