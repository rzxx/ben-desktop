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
