package main

import (
	"context"
	"strings"
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

func TestNotificationFromJobClassifiesManualAndAutoPinJobs(t *testing.T) {
	manual := notificationFromJob(desktopcore.JobSnapshot{
		JobID: "pin-1",
		Kind:  "pin-album-offline",
		Phase: desktopcore.JobPhaseQueued,
	})
	if manual.Audience != apitypes.NotificationAudienceUser || manual.Importance != apitypes.NotificationImportanceNormal {
		t.Fatalf("manual pin classification = %q/%q, want %q/%q", manual.Audience, manual.Importance, apitypes.NotificationAudienceUser, apitypes.NotificationImportanceNormal)
	}

	auto := notificationFromJob(desktopcore.JobSnapshot{
		JobID: "refresh-1",
		Kind:  "refresh-pinned-playlist",
		Phase: desktopcore.JobPhaseQueued,
	})
	if auto.Audience != apitypes.NotificationAudienceSystem || auto.Importance != apitypes.NotificationImportanceDebug {
		t.Fatalf("auto refresh classification = %q/%q, want %q/%q", auto.Audience, auto.Importance, apitypes.NotificationAudienceSystem, apitypes.NotificationImportanceDebug)
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

func TestUpsertNotificationSkipsSemanticDuplicates(t *testing.T) {
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 2)
	facade := newNotificationFacadeForTest(&now, &emitted)

	facade.upsertNotification(apitypes.NotificationSnapshot{
		ID:         "job:1",
		Kind:       "join-session",
		Audience:   apitypes.NotificationAudienceUser,
		Importance: apitypes.NotificationImportanceNormal,
		Phase:      apitypes.NotificationPhaseSuccess,
		Message:    "join session completed",
		Progress:   1,
	})
	first := facade.notifications["job:1"]

	now = now.Add(time.Second)
	facade.upsertNotification(apitypes.NotificationSnapshot{
		ID:         "job:1",
		Kind:       "join-session",
		Audience:   apitypes.NotificationAudienceUser,
		Importance: apitypes.NotificationImportanceNormal,
		Phase:      apitypes.NotificationPhaseSuccess,
		Message:    "join session completed",
		Progress:   1,
	})

	if len(emitted) != 1 {
		t.Fatalf("emitted notifications = %d, want 1", len(emitted))
	}
	if got := facade.notifications["job:1"].UpdatedAt; !got.Equal(first.UpdatedAt) {
		t.Fatalf("updated at = %v, want %v", got, first.UpdatedAt)
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

func TestPlaybackLoadingFailureUsesLastError(t *testing.T) {
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	facade := newNotificationFacadeForTest(&now, &emitted)

	facade.handlePlaybackSnapshot(playback.SessionSnapshot{
		LoadingItem: &playback.SessionItem{
			RecordingID: "rec-1",
			Title:       "Track 1",
		},
		LoadingPreparation: &playback.EntryPreparation{
			EntryID: "loading-1",
			Status: apitypes.PlaybackPreparationStatus{
				RecordingID: "rec-1",
				Purpose:     apitypes.PlaybackPreparationPlayNow,
				Phase:       apitypes.PlaybackPreparationPreparingFetch,
			},
		},
	})

	now = now.Add(time.Second)
	facade.handlePlaybackSnapshot(playback.SessionSnapshot{
		LastError: "remote fetch failed",
	})

	if len(emitted) != 2 {
		t.Fatalf("emitted notifications = %d, want 2", len(emitted))
	}
	if emitted[1].Phase != apitypes.NotificationPhaseError {
		t.Fatalf("phase = %q, want %q", emitted[1].Phase, apitypes.NotificationPhaseError)
	}
	if emitted[1].Error != "remote fetch failed" {
		t.Fatalf("error = %q, want remote fetch failed", emitted[1].Error)
	}
}

func TestPlaybackLoadingSuccessUsesPlaybackStartedMessage(t *testing.T) {
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	facade := newNotificationFacadeForTest(&now, &emitted)

	facade.handlePlaybackSnapshot(playback.SessionSnapshot{
		LoadingItem: &playback.SessionItem{
			RecordingID: "rec-1",
			Title:       "Track 1",
		},
		LoadingPreparation: &playback.EntryPreparation{
			EntryID: "loading-1",
			Status: apitypes.PlaybackPreparationStatus{
				RecordingID: "rec-1",
				Purpose:     apitypes.PlaybackPreparationPlayNow,
				Phase:       apitypes.PlaybackPreparationPreparingTranscode,
			},
		},
	})

	now = now.Add(time.Second)
	facade.handlePlaybackSnapshot(playback.SessionSnapshot{
		CurrentItem: &playback.SessionItem{
			RecordingID: "rec-1",
			Title:       "Track 1",
		},
	})

	if len(emitted) != 2 {
		t.Fatalf("emitted notifications = %d, want 2", len(emitted))
	}
	if emitted[1].Phase != apitypes.NotificationPhaseSuccess {
		t.Fatalf("phase = %q, want %q", emitted[1].Phase, apitypes.NotificationPhaseSuccess)
	}
	if emitted[1].Message != "Playback started." {
		t.Fatalf("message = %q, want %q", emitted[1].Message, "Playback started.")
	}
}

func TestPlaybackPreloadFetchNotificationsUseSystemRunningAndSuccess(t *testing.T) {
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 4)
	facade := newNotificationFacadeForTest(&now, &emitted)

	facade.handlePlaybackSnapshot(playback.SessionSnapshot{
		UpcomingEntries: []playback.SessionEntry{{
			EntryID: "next-1",
			Item: playback.SessionItem{
				RecordingID: "rec-next",
				Title:       "Next Track",
			},
		}},
		NextPreparation: &playback.EntryPreparation{
			EntryID: "next-1",
			Status: apitypes.PlaybackPreparationStatus{
				RecordingID: "rec-next",
				Purpose:     apitypes.PlaybackPreparationPreloadNext,
				Phase:       apitypes.PlaybackPreparationPreparingFetch,
			},
		},
	})

	now = now.Add(time.Second)
	facade.handlePlaybackSnapshot(playback.SessionSnapshot{
		UpcomingEntries: []playback.SessionEntry{{
			EntryID: "next-1",
			Item: playback.SessionItem{
				RecordingID: "rec-next",
				Title:       "Next Track",
			},
		}},
		NextPreparation: &playback.EntryPreparation{
			EntryID: "next-1",
			Status: apitypes.PlaybackPreparationStatus{
				RecordingID: "rec-next",
				Purpose:     apitypes.PlaybackPreparationPreloadNext,
				Phase:       apitypes.PlaybackPreparationReady,
			},
		},
	})

	if len(emitted) != 2 {
		t.Fatalf("emitted notifications = %d, want 2", len(emitted))
	}
	if emitted[0].Kind != "playback-preload" || emitted[0].Audience != apitypes.NotificationAudienceSystem {
		t.Fatalf("running preload notification = %+v", emitted[0])
	}
	if emitted[1].Phase != apitypes.NotificationPhaseSuccess {
		t.Fatalf("terminal phase = %q, want %q", emitted[1].Phase, apitypes.NotificationPhaseSuccess)
	}
}

func TestScanPipelineMergesArtworkStageIntoScanNotification(t *testing.T) {
	facade := &NotificationsFacade{
		notifications:    make(map[string]apitypes.NotificationSnapshot),
		activeTranscodes: make(map[string]apitypes.NotificationSnapshot),
	}

	facade.handleJobSnapshot(desktopcore.JobSnapshot{
		JobID:   "scan-job-1",
		Kind:    "scan-library",
		Phase:   desktopcore.JobPhaseQueued,
		Message: "queued library scan",
	})
	var scanID string
	for id := range facade.notifications {
		scanID = id
	}

	facade.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "ingesting",
		TracksDone:  1,
		TracksTotal: 10,
	})
	facade.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    2,
		CurrentAlbumID: "album-1",
	})
	if got := facade.notifications[scanID].Message; !strings.Contains(got, "Generating artwork") {
		t.Fatalf("scan pipeline message = %q", got)
	}

	facade.handleArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:       "failed",
		Errors:      2,
		AlbumsTotal: 2,
	})
	facade.handleScanActivity(apitypes.ScanActivityStatus{
		Phase:       "completed",
		TracksDone:  10,
		TracksTotal: 10,
	})

	if got := facade.notifications[scanID]; got.Phase != apitypes.NotificationPhaseSuccess || !strings.Contains(got.Message, "2 artwork error") {
		t.Fatalf("scan completion notification = %+v", got)
	}
}

func TestPollNetworkSyncStatusSynthesizesManualAndBackgroundUpdates(t *testing.T) {
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 6)
	network := &stubNotificationNetworkRuntime{
		status: apitypes.NetworkStatus{
			LibraryID: "lib-1",
			NetworkSyncState: apitypes.NetworkSyncState{
				Mode:             apitypes.NetworkSyncModeCatchup,
				Reason:           apitypes.NetworkSyncReasonManual,
				ActivePeerID:     "peer-1",
				BacklogEstimate:  40,
				LastBatchApplied: 5,
			},
		},
	}
	facade := newNotificationFacadeForTest(&now, &emitted)
	facade.host = &coreHost{network: network}
	facade.activeManualSyncID = "sync:lib-1"

	facade.pollNetworkSyncStatus()
	if len(emitted) != 1 || emitted[0].ID != "job:sync:lib-1" || emitted[0].Phase != apitypes.NotificationPhaseRunning {
		t.Fatalf("manual sync notification = %+v", emitted)
	}

	network.status = apitypes.NetworkStatus{
		LibraryID: "lib-1",
		NetworkSyncState: apitypes.NetworkSyncState{
			Mode:             apitypes.NetworkSyncModeCatchup,
			Reason:           apitypes.NetworkSyncReasonStartup,
			ActivePeerID:     "peer-2",
			BacklogEstimate:  12,
			LastBatchApplied: 3,
		},
	}
	now = now.Add(time.Second)
	facade.pollNetworkSyncStatus()
	if len(emitted) != 2 || emitted[1].Kind != "sync-activity" || emitted[1].Audience != apitypes.NotificationAudienceSystem {
		t.Fatalf("background sync notification = %+v", emitted)
	}

	network.status = apitypes.NetworkStatus{
		LibraryID: "lib-1",
		NetworkSyncState: apitypes.NetworkSyncState{
			Mode:             apitypes.NetworkSyncModeIdle,
			LastBatchApplied: 3,
		},
	}
	now = now.Add(time.Second)
	facade.pollNetworkSyncStatus()
	if len(emitted) != 3 || emitted[2].Phase != apitypes.NotificationPhaseSuccess {
		t.Fatalf("background sync completion notification = %+v", emitted)
	}
}

func TestJoinSessionRefreshLoopRefreshesActiveSessions(t *testing.T) {
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	emitted := make([]apitypes.NotificationSnapshot, 0, 6)
	facade := newNotificationFacadeForTest(&now, &emitted)
	invite := &stubNotificationInviteRuntime{
		called: make(chan struct{}, 1),
		onGetJoinSession: func(sessionID string) {
			now = now.Add(time.Second)
			facade.handleJobSnapshot(desktopcore.JobSnapshot{
				JobID:   sessionID,
				Kind:    "join-session",
				Phase:   desktopcore.JobPhaseRunning,
				Message: "join request approved",
			})
		},
	}
	facade.host = &coreHost{invite: invite}
	facade.handleJobSnapshot(desktopcore.JobSnapshot{
		JobID:   "session-1",
		Kind:    "join-session",
		Phase:   desktopcore.JobPhaseRunning,
		Message: "join request pending approval",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		facade.runJoinSessionRefreshLoop(ctx)
		close(done)
	}()

	select {
	case <-invite.called:
	case <-time.After(3 * time.Second):
		t.Fatal("join session refresh loop did not call GetJoinSession")
	}
	cancel()
	<-done

	last := emitted[len(emitted)-1]
	if last.Kind != "join-session" || !strings.Contains(last.Message, "approved") {
		t.Fatalf("last join notification = %+v", last)
	}
}

type stubNotificationNetworkRuntime struct {
	status apitypes.NetworkStatus
	local  apitypes.LocalContext
}

func (s *stubNotificationNetworkRuntime) EnsureLocalContext(context.Context) (apitypes.LocalContext, error) {
	return s.local, nil
}
func (s *stubNotificationNetworkRuntime) Inspect(context.Context) (apitypes.InspectSummary, error) {
	return apitypes.InspectSummary{}, nil
}
func (s *stubNotificationNetworkRuntime) InspectLibraryOplog(context.Context, string) (apitypes.LibraryOplogDiagnostics, error) {
	return apitypes.LibraryOplogDiagnostics{}, nil
}
func (s *stubNotificationNetworkRuntime) ActivityStatus(context.Context) (apitypes.ActivityStatus, error) {
	return apitypes.ActivityStatus{}, nil
}
func (s *stubNotificationNetworkRuntime) NetworkStatus() apitypes.NetworkStatus { return s.status }
func (s *stubNotificationNetworkRuntime) StartSyncNow(context.Context) (desktopcore.JobSnapshot, error) {
	return desktopcore.JobSnapshot{}, nil
}
func (s *stubNotificationNetworkRuntime) StartConnectPeer(context.Context, string) (desktopcore.JobSnapshot, error) {
	return desktopcore.JobSnapshot{}, nil
}
func (s *stubNotificationNetworkRuntime) CheckpointStatus(context.Context) (apitypes.LibraryCheckpointStatus, error) {
	return apitypes.LibraryCheckpointStatus{}, nil
}
func (s *stubNotificationNetworkRuntime) StartPublishCheckpoint(context.Context) (desktopcore.JobSnapshot, error) {
	return desktopcore.JobSnapshot{}, nil
}
func (s *stubNotificationNetworkRuntime) StartCompactCheckpoint(context.Context, bool) (desktopcore.JobSnapshot, error) {
	return desktopcore.JobSnapshot{}, nil
}

type stubNotificationInviteRuntime struct {
	onGetJoinSession func(sessionID string)
	called           chan struct{}
}

func (s *stubNotificationInviteRuntime) CreateInviteCode(context.Context, apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	return apitypes.InviteCodeResult{}, nil
}
func (s *stubNotificationInviteRuntime) ListIssuedInvites(context.Context, string) ([]apitypes.IssuedInviteRecord, error) {
	return nil, nil
}
func (s *stubNotificationInviteRuntime) RevokeIssuedInvite(context.Context, string, string) error {
	return nil
}
func (s *stubNotificationInviteRuntime) StartJoinFromInvite(context.Context, apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	return apitypes.JoinSession{}, nil
}
func (s *stubNotificationInviteRuntime) GetJoinSession(_ context.Context, sessionID string) (apitypes.JoinSession, error) {
	if s.called == nil {
		s.called = make(chan struct{}, 1)
	}
	if s.onGetJoinSession != nil {
		s.onGetJoinSession(sessionID)
	}
	select {
	case s.called <- struct{}{}:
	default:
	}
	return apitypes.JoinSession{SessionID: sessionID}, nil
}
func (s *stubNotificationInviteRuntime) StartFinalizeJoinSession(context.Context, string) (desktopcore.JobSnapshot, error) {
	return desktopcore.JobSnapshot{}, nil
}
func (s *stubNotificationInviteRuntime) CancelJoinSession(context.Context, string) error { return nil }
func (s *stubNotificationInviteRuntime) ListJoinRequests(context.Context, string) ([]apitypes.InviteJoinRequestRecord, error) {
	return nil, nil
}
func (s *stubNotificationInviteRuntime) ApproveJoinRequest(context.Context, string, string) error {
	return nil
}
func (s *stubNotificationInviteRuntime) RejectJoinRequest(context.Context, string, string) error {
	return nil
}
