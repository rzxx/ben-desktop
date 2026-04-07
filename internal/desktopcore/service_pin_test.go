package desktopcore

import (
	"context"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestStartPinAllowsLocalOnlyAlbumIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "pin-local-only-album"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCacheRecording(t, app, local.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-local-album",
		AlbumID:        "album-local-only",
		SourceFileID:   "src-local-album",
		EncodingID:     "enc-local-album",
		BlobID:         testBlobID("a"),
		Profile:        "desktop",
		LastVerifiedAt: now,
	})

	job, err := app.StartPin(ctx, apitypes.PinIntentRequest{
		Profile: "desktop",
		Subject: apitypes.PinSubjectRef{
			Kind: apitypes.PinSubjectAlbumVariant,
			ID:   "album-local-only",
		},
	})
	if err != nil {
		t.Fatalf("start pin local-only album: %v", err)
	}
	if job.JobID == "" {
		t.Fatalf("expected local-only album pin to return a job snapshot")
	}

	final := waitForJobPhase(
		t,
		ctx,
		app,
		pinJobID(local.LibraryID, "album", "album-local-only", "desktop"),
		JobPhaseCompleted,
	)
	if final.Kind != jobKindPinAlbum {
		t.Fatalf("job kind = %q, want %q", final.Kind, jobKindPinAlbum)
	}

	state, err := app.GetPinState(ctx, apitypes.PinStateRequest{
		Profile: "desktop",
		Subject: apitypes.PinSubjectRef{
			Kind: apitypes.PinSubjectAlbumVariant,
			ID:   "album-local-only",
		},
	})
	if err != nil {
		t.Fatalf("get album pin state: %v", err)
	}
	if !state.Pinned || !state.Direct {
		t.Fatalf("album pin state = %+v, want direct pinned", state)
	}
	if state.Pending {
		t.Fatalf("expected local-only album pin to be fully materialized: %+v", state)
	}
}

func TestGetPinStateMarksTrackCoveredByPinnedPlaylist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "pin-covered-track"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCacheRecording(t, app, local.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-covered-track",
		AlbumID:        "album-covered-track",
		SourceFileID:   "src-covered-track",
		EncodingID:     "enc-covered-track",
		BlobID:         testBlobID("b"),
		Profile:        "desktop",
		LastVerifiedAt: now,
	})

	playlist, err := app.CreatePlaylist(ctx, "Covered tracks", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:         playlist.PlaylistID,
		LibraryRecordingID: "rec-covered-track",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	if _, err := app.StartPin(ctx, apitypes.PinIntentRequest{
		Profile: "desktop",
		Subject: apitypes.PinSubjectRef{
			Kind: apitypes.PinSubjectPlaylist,
			ID:   playlist.PlaylistID,
		},
	}); err != nil {
		t.Fatalf("start pin playlist: %v", err)
	}

	waitForJobPhase(
		t,
		ctx,
		app,
		pinJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop"),
		JobPhaseCompleted,
	)

	state, err := app.GetPinState(ctx, apitypes.PinStateRequest{
		Profile: "desktop",
		Subject: apitypes.PinSubjectRef{
			Kind: apitypes.PinSubjectRecordingCluster,
			ID:   "rec-covered-track",
		},
	})
	if err != nil {
		t.Fatalf("get track pin state: %v", err)
	}
	if !state.Pinned || !state.Covered || state.Direct {
		t.Fatalf("track pin state = %+v, want covered-only pin", state)
	}
	if len(state.Sources) == 0 || state.Sources[0].Subject.Kind != apitypes.PinSubjectPlaylist {
		t.Fatalf("track pin sources = %+v, want playlist coverage", state.Sources)
	}
}

func TestRunPinScopeRefreshJobSchedulesRetryWhenTracksRemainPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.pin.refreshRetryDelay = 20 * time.Millisecond

	library, err := app.CreateLibrary(ctx, "pin-refresh-retry")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-pending-refresh", "Pending Refresh")

	if err := app.pin.upsertPinRoot(ctx, local, "recording", "rec-pending-refresh", "desktop"); err != nil {
		t.Fatalf("upsert pending pin root: %v", err)
	}

	app.pin.runPinScopeRefreshJob(ctx, library.LibraryID, "recording", "rec-pending-refresh", "desktop")

	jobID := refreshPinScopeJobID(library.LibraryID, "recording", "rec-pending-refresh", "desktop")
	app.pin.refreshMu.Lock()
	_, scheduled := app.pin.refreshTimers[jobID]
	app.pin.refreshMu.Unlock()
	if !scheduled {
		t.Fatalf("expected pending pin scope refresh retry for %s", jobID)
	}
}

func TestSyncActiveRuntimeServicesRefreshesPinRootsEvenWhenPendingCountIsZero(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-pin-startup-refresh"}, nil
	}

	library, err := app.CreateLibrary(ctx, "pin-startup-refresh")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-startup-refresh", "Startup Refresh")

	if err := app.pin.upsertPinRoot(ctx, local, "recording", "rec-startup-refresh", "desktop"); err != nil {
		t.Fatalf("upsert pin root: %v", err)
	}
	if err := app.db.WithContext(ctx).
		Model(&PinRoot{}).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", library.LibraryID, local.DeviceID, "recording", "rec-startup-refresh").
		Update("pending_count", 0).Error; err != nil {
		t.Fatalf("clear pending_count: %v", err)
	}

	app.pin.refreshMu.Lock()
	for key, timer := range app.pin.refreshTimers {
		if timer != nil {
			timer.Stop()
		}
		delete(app.pin.refreshTimers, key)
	}
	app.pin.refreshMu.Unlock()

	if err := app.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("sync runtime services: %v", err)
	}

	jobID := refreshPinScopeJobID(library.LibraryID, "recording", "rec-startup-refresh", "desktop")
	app.pin.refreshMu.Lock()
	_, scheduled := app.pin.refreshTimers[jobID]
	app.pin.refreshMu.Unlock()
	if !scheduled {
		t.Fatalf("expected startup refresh for %s even with pending_count = 0", jobID)
	}
}
