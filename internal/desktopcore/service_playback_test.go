package desktopcore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
)

func startPinJob(t *testing.T, ctx context.Context, app *App, subject apitypes.PinSubjectRef, profile string) (JobSnapshot, error) {
	t.Helper()
	return app.StartPin(ctx, apitypes.PinIntentRequest{
		Profile: profile,
		Subject: subject,
	})
}

func startRecordingPinJob(t *testing.T, ctx context.Context, app *App, recordingID, profile string) (JobSnapshot, error) {
	t.Helper()
	return startPinJob(t, ctx, app, apitypes.PinSubjectRef{
		Kind: apitypes.PinSubjectRecordingCluster,
		ID:   recordingID,
	}, profile)
}

func startExactRecordingPinJob(t *testing.T, ctx context.Context, app *App, recordingID, profile string) (JobSnapshot, error) {
	t.Helper()
	return startPinJob(t, ctx, app, apitypes.PinSubjectRef{
		Kind: apitypes.PinSubjectRecordingVariant,
		ID:   recordingID,
	}, profile)
}

func startAlbumPinJob(t *testing.T, ctx context.Context, app *App, albumID, profile string) (JobSnapshot, error) {
	t.Helper()
	return startPinJob(t, ctx, app, apitypes.PinSubjectRef{
		Kind: apitypes.PinSubjectAlbumVariant,
		ID:   albumID,
	}, profile)
}

func startPlaylistPinJob(t *testing.T, ctx context.Context, app *App, playlistID, profile string) (JobSnapshot, error) {
	t.Helper()
	return startPinJob(t, ctx, app, apitypes.PinSubjectRef{
		Kind: apitypes.PinSubjectPlaylist,
		ID:   playlistID,
	}, profile)
}

func startLikedPinJob(t *testing.T, ctx context.Context, app *App, playlistID, profile string) (JobSnapshot, error) {
	t.Helper()
	return startPinJob(t, ctx, app, apitypes.PinSubjectRef{
		Kind: apitypes.PinSubjectLikedPlaylist,
		ID:   playlistID,
	}, profile)
}

func unpinRecording(t *testing.T, ctx context.Context, app *App, recordingID string) error {
	t.Helper()
	return app.Unpin(ctx, apitypes.PinIntentRequest{
		Profile: "desktop",
		Subject: apitypes.PinSubjectRef{
			Kind: apitypes.PinSubjectRecordingCluster,
			ID:   recordingID,
		},
	})
}

func loadPinRoot(t *testing.T, ctx context.Context, app *App, libraryID, deviceID, scope, scopeID string) PinRoot {
	t.Helper()

	var root PinRoot
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", libraryID, deviceID, scope, scopeID).
		Take(&root).Error; err != nil {
		t.Fatalf("load pin root: %v", err)
	}
	return root
}

func countPinRoots(t *testing.T, ctx context.Context, app *App, libraryID, deviceID, scope, scopeID string) int64 {
	t.Helper()

	var count int64
	if err := app.db.WithContext(ctx).
		Model(&PinRoot{}).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", libraryID, deviceID, scope, scopeID).
		Count(&count).Error; err != nil {
		t.Fatalf("count pin roots: %v", err)
	}
	return count
}

func pinRootBlobIDs(t *testing.T, ctx context.Context, app *App, libraryID, deviceID, scope, scopeID, profile string) []string {
	t.Helper()

	type row struct {
		BlobID string
	}
	var rows []row
	if err := app.db.WithContext(ctx).
		Model(&PinBlobRef{}).
		Select("blob_id").
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ? AND profile = ?", libraryID, deviceID, scope, scopeID, profile).
		Order("blob_id ASC").
		Scan(&rows).Error; err != nil {
		t.Fatalf("load pin root blob ids: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.BlobID))
	}
	return compactNonEmptyStrings(out)
}

func TestPinRecordingPersistsAndUnpinsCachedAsset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "pin-recording")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	blobID := testBlobID("1")
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pin",
		AlbumID:        "album-pin",
		SourceFileID:   "src-pin",
		EncodingID:     "enc-pin",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, blobID, 128)

	job, err := startRecordingPinJob(t, ctx, app, "rec-pin", "desktop")
	if err != nil {
		t.Fatalf("start recording pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, job.JobID, JobPhaseCompleted)

	pin := loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "recording", "rec-pin")
	if pin.Profile != "desktop" {
		t.Fatalf("pin profile = %q, want desktop", pin.Profile)
	}
	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "recording", "rec-pin", "desktop")
	if !reflect.DeepEqual(protectedBlobIDs, []string{blobID}) {
		t.Fatalf("pin root blob ids = %v, want [%s]", protectedBlobIDs, blobID)
	}

	if err := unpinRecording(t, ctx, app, "rec-pin"); err != nil {
		t.Fatalf("unpin recording: %v", err)
	}

	count := countPinRoots(t, ctx, app, local.LibraryID, local.DeviceID, "recording", "rec-pin")
	if count != 0 {
		t.Fatalf("recording pin count = %d, want 0", count)
	}
	blobPath, err := app.blobs.Path(blobID)
	if err != nil {
		t.Fatalf("resolve cached blob path: %v", err)
	}
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("expected cached blob to remain after unpin: %v", err)
	}
}

func TestPinRecordingSeparatesExactAndLogicalTrackScopes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("pin-track-scope")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "pin-track-scope")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-pin-scope"
		variantA  = "rec-pin-scope-a"
		variantB  = "rec-pin-scope-b"
	)

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-pin-scope-a",
		AlbumClusterID: "album-pin-scope-a",
		SourceFileID:   "src-pin-scope-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-pin-scope-a", []byte("variant-a"))
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-pin-scope-b",
		AlbumClusterID: "album-pin-scope-b",
		SourceFileID:   "src-pin-scope-b",
		QualityRank:    120,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-pin-scope-b", []byte("variant-b"))

	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}

	if _, err := startRecordingPinJob(t, ctx, app, clusterID, "desktop"); err != nil {
		t.Fatalf("start logical recording pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "recording", clusterID, "desktop"), JobPhaseCompleted)

	_ = loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "recording", clusterID)

	clusterAvailability, err := app.GetRecordingAvailability(ctx, clusterID, "desktop")
	if err != nil {
		t.Fatalf("get logical availability: %v", err)
	}
	if !clusterAvailability.Pinned {
		t.Fatalf("expected collapsed track to report pinned")
	}
	preferredAvailability, err := app.GetRecordingAvailability(ctx, variantB, "desktop")
	if err != nil {
		t.Fatalf("get preferred variant availability: %v", err)
	}
	if !preferredAvailability.Pinned {
		t.Fatalf("expected preferred exact variant to inherit logical pin state")
	}
	otherAvailability, err := app.GetRecordingAvailability(ctx, variantA, "desktop")
	if err != nil {
		t.Fatalf("get non-preferred variant availability: %v", err)
	}
	if otherAvailability.Pinned {
		t.Fatalf("expected non-preferred exact variant to remain unpinned for logical scope")
	}

	if err := unpinRecording(t, ctx, app, clusterID); err != nil {
		t.Fatalf("unpin logical recording: %v", err)
	}
	if _, err := startExactRecordingPinJob(t, ctx, app, variantA, "desktop"); err != nil {
		t.Fatalf("start exact recording pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "recording", variantA, "desktop"), JobPhaseCompleted)

	_ = loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "recording", variantA)

	exactAvailability, err := app.GetRecordingAvailability(ctx, variantA, "desktop")
	if err != nil {
		t.Fatalf("get exact pinned availability: %v", err)
	}
	if !exactAvailability.Pinned {
		t.Fatalf("expected exact variant to report pinned")
	}
	clusterAvailability, err = app.GetRecordingAvailability(ctx, clusterID, "desktop")
	if err != nil {
		t.Fatalf("get collapsed availability after exact pin: %v", err)
	}
	if clusterAvailability.Pinned {
		t.Fatalf("expected collapsed track to remain unpinned when only a non-preferred exact variant is pinned")
	}
	preferredAvailability, err = app.GetRecordingAvailability(ctx, variantB, "desktop")
	if err != nil {
		t.Fatalf("get preferred variant availability after exact pin: %v", err)
	}
	if preferredAvailability.Pinned {
		t.Fatalf("expected preferred variant to remain unpinned when another exact variant is pinned")
	}
}

func TestResolveArtworkRefReturnsTypedArtworkPathOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	blobID := testBlobID("f")
	writeArtworkBlob(t, app, blobID, ".webp", 32)

	result, err := app.ResolveArtworkRef(ctx, apitypes.ArtworkRef{
		BlobID:  blobID,
		MIME:    "image/webp",
		FileExt: ".webp",
		Variant: defaultArtworkVariant320,
	})
	if err != nil {
		t.Fatalf("resolve artwork ref: %v", err)
	}
	if !result.Available {
		t.Fatalf("expected artwork ref to resolve")
	}
	if !strings.HasSuffix(result.LocalPath, ".webp") {
		t.Fatalf("artwork local path = %q, want .webp suffix", result.LocalPath)
	}
	if _, err := os.Stat(result.LocalPath); err != nil {
		t.Fatalf("stat typed artwork path: %v", err)
	}
	basePath, err := app.blobs.Path(blobID)
	if err != nil {
		t.Fatalf("resolve legacy artwork path: %v", err)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Fatalf("expected no extensionless artwork blob at %q, err=%v", basePath, err)
	}
}

func TestResolveArtworkRefDoesNotBackfillLegacyArtworkBlob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	blobID := testBlobID("e")
	writeCacheBlob(t, app, blobID, 32)

	result, err := app.ResolveArtworkRef(ctx, apitypes.ArtworkRef{
		BlobID:  blobID,
		MIME:    "image/webp",
		FileExt: ".webp",
		Variant: defaultArtworkVariant320,
	})
	if err != nil {
		t.Fatalf("resolve legacy artwork ref: %v", err)
	}
	if result.Available {
		t.Fatalf("expected legacy extensionless artwork blob to stay unavailable, got %+v", result)
	}
	if strings.TrimSpace(result.LocalPath) != "" {
		t.Fatalf("expected no local path for legacy artwork blob, got %q", result.LocalPath)
	}
}

func TestResolveRecordingArtworkUsesExactVariantWithoutFallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "recording-artwork")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-art",
		TrackClusterID: "rec-art",
		AlbumID:        "album-art",
		AlbumClusterID: "album-art",
		SourceFileID:   "src-art",
		QualityRank:    100,
	})

	jpegBlobID := testBlobID("7")
	webpBlobID := testBlobID("8")
	writeArtworkBlob(t, app, jpegBlobID, ".jpg", 24)
	writeArtworkBlob(t, app, webpBlobID, ".webp", 48)

	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).Create(&ArtworkVariant{
		LibraryID: local.LibraryID,
		ScopeType: "album",
		ScopeID:   "album-art",
		Variant:   defaultArtworkVariant96,
		BlobID:    jpegBlobID,
		MIME:      "image/jpeg",
		FileExt:   ".jpg",
		W:         96,
		H:         96,
		Bytes:     24,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed 96 artwork: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&ArtworkVariant{
		LibraryID: local.LibraryID,
		ScopeType: "album",
		ScopeID:   "album-art",
		Variant:   defaultArtworkVariant320,
		BlobID:    webpBlobID,
		MIME:      "image/webp",
		FileExt:   ".webp",
		W:         320,
		H:         320,
		Bytes:     48,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed 320 artwork: %v", err)
	}

	got96, err := app.ResolveRecordingArtwork(ctx, "rec-art", defaultArtworkVariant96)
	if err != nil {
		t.Fatalf("resolve recording 96 artwork: %v", err)
	}
	if !got96.Available || got96.Artwork.Variant != defaultArtworkVariant96 {
		t.Fatalf("resolve recording 96 artwork = %+v", got96)
	}

	got1024, err := app.ResolveRecordingArtwork(ctx, "rec-art", defaultArtworkVariant1024)
	if err != nil {
		t.Fatalf("resolve recording 1024 artwork: %v", err)
	}
	if got1024.Available {
		t.Fatalf("expected missing 1024 artwork to stay unavailable, got %+v", got1024)
	}
	if strings.TrimSpace(got1024.Artwork.BlobID) != "" {
		t.Fatalf("expected no fallback artwork blob, got %+v", got1024.Artwork)
	}
}

func TestPreparePlaybackRecordingPublishesCompletedJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "prepare-playback-job")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	blobID := testBlobID("9")
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-job",
		AlbumID:        "album-job",
		SourceFileID:   "src-job",
		EncodingID:     "enc-job",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, blobID, 128)

	status, err := app.PreparePlaybackRecording(ctx, "rec-job", "desktop", apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		t.Fatalf("prepare playback recording: %v", err)
	}
	if status.Phase != apitypes.PlaybackPreparationReady {
		t.Fatalf("prepare playback phase = %q, want %q", status.Phase, apitypes.PlaybackPreparationReady)
	}

	jobID := playbackPreparationJobID(local.LibraryID, "rec-job", "desktop", apitypes.PlaybackPreparationPlayNow)
	job, ok, err := app.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get playback job: %v", err)
	}
	if !ok {
		t.Fatalf("expected playback preparation job %q", jobID)
	}
	if job.Kind != jobKindPreparePlayback {
		t.Fatalf("job kind = %q, want %q", job.Kind, jobKindPreparePlayback)
	}
	if job.Phase != JobPhaseCompleted {
		t.Fatalf("job phase = %q, want %q", job.Phase, JobPhaseCompleted)
	}
	if job.Message == "" {
		t.Fatalf("expected playback job to include a message")
	}
}

func TestStartEnsureRecordingEncodingQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("encoded-job")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "ensure-recording-job")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-encode-job",
		TrackClusterID: "rec-encode-job",
		AlbumID:        "album-encode-job",
		AlbumClusterID: "album-encode-job",
		SourceFileID:   "src-encode-job",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-encode-job", []byte("lossless"))

	job, err := app.StartEnsureRecordingEncoding(ctx, "rec-encode-job", "desktop")
	if err != nil {
		t.Fatalf("start ensure recording encoding: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindEnsureRecordingEncoding {
		t.Fatalf("unexpected queued recording encoding job: %+v", job)
	}

	jobID := playbackEnsureRecordingEncodingJobID(local.LibraryID, "rec-encode-job", "desktop")
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseCompleted)
	if final.Kind != jobKindEnsureRecordingEncoding || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final recording encoding job: %+v", final)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("transcode calls = %d, want 1", len(builder.calls))
	}
}

func TestStartEnsureAlbumEncodingsQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("album-encoded")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "ensure-album-job")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-album-job",
		TrackClusterID: "rec-album-job",
		AlbumID:        "album-job",
		AlbumClusterID: "album-job",
		SourceFileID:   "src-album-job",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-album-job", []byte("lossless"))

	job, err := app.StartEnsureAlbumEncodings(ctx, "album-job", "desktop")
	if err != nil {
		t.Fatalf("start ensure album encodings: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindEnsureAlbumEncodings {
		t.Fatalf("unexpected queued album encoding job: %+v", job)
	}

	jobID := playbackEnsureScopeEncodingsJobID(local.LibraryID, "album", "album-job", "desktop")
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseCompleted)
	if final.Kind != jobKindEnsureAlbumEncodings || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final album encoding job: %+v", final)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("transcode calls = %d, want 1", len(builder.calls))
	}
}

func TestStartEnsurePlaylistEncodingsQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("playlist-encoded")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "ensure-playlist-job")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-playlist-job",
		TrackClusterID: "rec-playlist-job",
		AlbumID:        "album-playlist-job",
		AlbumClusterID: "album-playlist-job",
		SourceFileID:   "src-playlist-job",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-playlist-job", []byte("lossless"))
	playlist, err := app.CreatePlaylist(ctx, "Playlist Job", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-playlist-job",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	job, err := app.StartEnsurePlaylistEncodings(ctx, playlist.PlaylistID, "desktop")
	if err != nil {
		t.Fatalf("start ensure playlist encodings: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindEnsurePlaylistEncodings {
		t.Fatalf("unexpected queued playlist encoding job: %+v", job)
	}

	jobID := playbackEnsureScopeEncodingsJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop")
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseCompleted)
	if final.Kind != jobKindEnsurePlaylistEncodings || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final playlist encoding job: %+v", final)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("transcode calls = %d, want 1", len(builder.calls))
	}
}

func TestStartPreparePlaybackRecordingQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "prepare-playback-async")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	blobID := testBlobID("a")
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-async",
		AlbumID:        "album-async",
		SourceFileID:   "src-async",
		EncodingID:     "enc-async",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, blobID, 256)

	job, err := app.StartPreparePlaybackRecording(ctx, "rec-async", "desktop", apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		t.Fatalf("start prepare playback recording: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindPreparePlayback {
		t.Fatalf("unexpected queued playback job: %+v", job)
	}

	jobID := playbackPreparationJobID(local.LibraryID, "rec-async", "desktop", apitypes.PlaybackPreparationPlayNow)
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseCompleted)
	if final.Kind != jobKindPreparePlayback || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final playback job: %+v", final)
	}

	status, err := app.GetPlaybackPreparation(ctx, "rec-async", "desktop")
	if err != nil {
		t.Fatalf("get playback preparation: %v", err)
	}
	if status.Phase != apitypes.PlaybackPreparationReady {
		t.Fatalf("playback preparation phase = %q, want %q", status.Phase, apitypes.PlaybackPreparationReady)
	}
}

func TestStartPinRecordingReusesActiveJobAndMarksPinnedImmediately(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	release := make(chan struct{})
	builder := &fakeAACBuilder{result: []byte("pin-recording-job"), waitFor: release}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "pin-recording-job")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-pin-job",
		TrackClusterID: "rec-pin-job",
		AlbumID:        "album-pin-job",
		AlbumClusterID: "album-pin-job",
		SourceFileID:   "src-pin-job",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-pin-job", []byte("lossless"))

	first, err := startRecordingPinJob(t, ctx, app, "rec-pin-job", "desktop")
	if err != nil {
		t.Fatalf("start pin recording offline: %v", err)
	}
	if first.Kind != jobKindPinRecording || first.Phase != JobPhaseQueued {
		t.Fatalf("unexpected first pin job: %+v", first)
	}

	second, err := startRecordingPinJob(t, ctx, app, "rec-pin-job", "desktop")
	if err != nil {
		t.Fatalf("start pin recording offline again: %v", err)
	}
	if second.JobID != first.JobID {
		t.Fatalf("second job id = %q, want %q", second.JobID, first.JobID)
	}

	_ = loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "recording", "rec-pin-job")

	availability, err := app.GetRecordingAvailability(ctx, "rec-pin-job", "desktop")
	if err != nil {
		t.Fatalf("get recording availability: %v", err)
	}
	if !availability.Pinned {
		t.Fatalf("expected recording availability to report pinned while job is active")
	}

	close(release)

	final := waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "recording", "rec-pin-job", "desktop"), JobPhaseCompleted)
	if final.Kind != jobKindPinRecording {
		t.Fatalf("final job kind = %q, want %q", final.Kind, jobKindPinRecording)
	}
	if strings.TrimSpace(final.Message) == "" {
		t.Fatalf("expected final pin job message")
	}
}

func TestStartPinAlbumProtectsCachedBlobsImmediately(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	release := make(chan struct{})
	builder := &fakeAACBuilder{result: []byte("pin-album-job"), waitFor: release}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "pin-album-job")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-pin-album-remote", time.Now().UTC())

	const albumID = "album-pin-async"
	blobID := testBlobID("a")
	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pin-album-a",
		AlbumID:        albumID,
		SourceFileID:   "src-pin-album-a",
		EncodingID:     "enc-pin-album-a",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, blobID, 96)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-pin-album-b",
		TrackClusterID: "rec-pin-album-b",
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   "src-pin-album-b",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-pin-album-b", []byte("lossless"))

	first, err := startAlbumPinJob(t, ctx, app, albumID, "desktop")
	if err != nil {
		t.Fatalf("start pin album offline: %v", err)
	}
	second, err := startAlbumPinJob(t, ctx, app, albumID, "desktop")
	if err != nil {
		t.Fatalf("start pin album offline again: %v", err)
	}
	if second.JobID != first.JobID {
		t.Fatalf("second job id = %q, want %q", second.JobID, first.JobID)
	}

	pin := loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "album", albumID)
	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "album", albumID, pin.Profile)
	if !reflect.DeepEqual(protectedBlobIDs, []string{blobID}) {
		t.Fatalf("protected blob ids = %v, want [%s]", protectedBlobIDs, blobID)
	}

	summary := mustAlbumAvailabilitySummary(t, app, ctx, albumID)
	if !summary.ScopePinned {
		t.Fatalf("expected album availability summary to report scope pinned while job is active")
	}

	close(release)

	final := waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "album", albumID, "desktop"), JobPhaseCompleted)
	if final.Kind != jobKindPinAlbum {
		t.Fatalf("final job kind = %q, want %q", final.Kind, jobKindPinAlbum)
	}
}

func TestPinnedAvailabilityFieldsReflectDirectScopePinState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("pinned-fields")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "pin-fields")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-pin-fields-remote", time.Now().UTC())

	const albumID = "album-pin-fields"
	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pin-fields-a",
		AlbumID:        albumID,
		SourceFileID:   "src-pin-fields-a",
		EncodingID:     "enc-pin-fields-a",
		BlobID:         testBlobID("c"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pin-fields-b",
		AlbumID:        albumID,
		SourceFileID:   "src-pin-fields-b",
		EncodingID:     "enc-pin-fields-b",
		BlobID:         testBlobID("d"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})

	if _, err := startAlbumPinJob(t, ctx, app, albumID, "desktop"); err != nil {
		t.Fatalf("start album pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "album", albumID, "desktop"), JobPhaseCompleted)

	trackAvailability, err := app.GetRecordingAvailability(ctx, "rec-pin-fields-a", "desktop")
	if err != nil {
		t.Fatalf("get recording availability: %v", err)
	}
	if trackAvailability.Pinned {
		t.Fatalf("expected recording pinned flag to remain false for album-only pin")
	}

	summary := mustAlbumAvailabilitySummary(t, app, ctx, albumID)
	if !summary.ScopePinned {
		t.Fatalf("expected album summary scope pinned")
	}

	if _, err := startRecordingPinJob(t, ctx, app, "rec-pin-fields-a", "desktop"); err != nil {
		t.Fatalf("start recording pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "recording", "rec-pin-fields-a", "desktop"), JobPhaseCompleted)

	items, err := app.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:     []string{"rec-pin-fields-a", "rec-pin-fields-b"},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list recording playback availability: %v", err)
	}
	if !items[0].Pinned {
		t.Fatalf("expected first recording to report pinned")
	}
	if items[1].Pinned {
		t.Fatalf("expected second recording to remain unpinned")
	}
}

func TestPinnedPlaylistAutoRefreshFetchesNewTrack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("playlist-refresh")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "playlist-refresh")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	playlist, err := app.CreatePlaylist(ctx, "Refresh playlist", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-refresh-a",
		TrackClusterID: "rec-refresh-a",
		AlbumID:        "album-refresh",
		AlbumClusterID: "album-refresh",
		SourceFileID:   "src-refresh-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-refresh-a", []byte("refresh-a"))
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-refresh-a",
	}); err != nil {
		t.Fatalf("add playlist item a: %v", err)
	}
	if _, err := startPlaylistPinJob(t, ctx, app, playlist.PlaylistID, "desktop"); err != nil {
		t.Fatalf("start playlist pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop"), JobPhaseCompleted)

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-refresh-b",
		TrackClusterID: "rec-refresh-b",
		AlbumID:        "album-refresh",
		AlbumClusterID: "album-refresh",
		SourceFileID:   "src-refresh-b",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-refresh-b", []byte("refresh-b"))
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-refresh-b", "desktop"); err != nil {
		t.Fatalf("pre-refresh cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected new playlist track to start uncached")
	}
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-refresh-b",
	}); err != nil {
		t.Fatalf("add playlist item b: %v", err)
	}

	final := waitForJobPhase(t, ctx, app, refreshPinScopeJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop"), JobPhaseCompleted)
	if final.Kind != jobKindRefreshPinnedPlaylist {
		t.Fatalf("refresh job kind = %q, want %q", final.Kind, jobKindRefreshPinnedPlaylist)
	}
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-refresh-b", "desktop"); err != nil {
		t.Fatalf("post-refresh cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected pinned local playlist track to remain uncached")
	}
	if len(builder.calls) != 0 {
		t.Fatalf("expected pinned local playlist refresh to avoid transcoding, got %d calls", len(builder.calls))
	}
}

func TestPinnedPlaylistProtectsResolvedPreferredVariant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buildIndex atomic.Int32
	builder := &fakeAACBuilder{}
	builder.before = func() {
		builder.result = []byte(fmt.Sprintf("playlist-preferred-%d", buildIndex.Add(1)))
	}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "playlist-preferred")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	playlist, err := app.CreatePlaylist(ctx, "Preferred playlist", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	const (
		clusterID = "cluster-playlist-preferred"
		variantA  = "rec-playlist-preferred-a"
		variantB  = "rec-playlist-preferred-b"
	)

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-playlist-preferred-a",
		AlbumClusterID: "album-playlist-preferred-a",
		SourceFileID:   "src-playlist-preferred-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-playlist-preferred-a", []byte("playlist-a"))
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-playlist-preferred-b",
		AlbumClusterID: "album-playlist-preferred-b",
		SourceFileID:   "src-playlist-preferred-b",
		QualityRank:    120,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-playlist-preferred-b", []byte("playlist-b"))

	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if _, err := app.EnsureRecordingEncoding(ctx, variantA, "desktop"); err != nil {
		t.Fatalf("ensure non-preferred variant encoding: %v", err)
	}
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: clusterID,
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	if _, err := startPlaylistPinJob(t, ctx, app, playlist.PlaylistID, "desktop"); err != nil {
		t.Fatalf("start playlist pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop"), JobPhaseCompleted)

	blobA, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, variantA, "desktop")
	if err != nil || !ok {
		t.Fatalf("cached encoding for non-preferred variant = %v, %v", ok, err)
	}
	if ok, err := app.pin.hasPinnedCachedEncoding(ctx, local, variantB, "desktop"); err != nil {
		t.Fatalf("pinned cached encoding for preferred variant: %v", err)
	} else if ok {
		t.Fatalf("expected preferred variant to stay uncached when only local source is available")
	}

	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "playlist", playlist.PlaylistID, "desktop")
	if slicesContains(protectedBlobIDs, blobA) {
		t.Fatalf("expected non-preferred variant blob %q to remain unprotected, got %v", blobA, protectedBlobIDs)
	}
	if len(protectedBlobIDs) != 0 {
		t.Fatalf("expected local-only logical playlist pin to avoid protecting transcoded blobs, got %v", protectedBlobIDs)
	}
}

func TestSyncedPlaylistPinningNormalizesLogicalRecordingIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buildIndex atomic.Int32
	builder := &fakeAACBuilder{}
	builder.before = func() {
		builder.result = []byte(fmt.Sprintf("playlist-sync-%d", buildIndex.Add(1)))
	}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "playlist-sync-logical")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	playlist, err := app.CreatePlaylist(ctx, "Synced playlist", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	const (
		clusterID = "cluster-playlist-sync"
		variantA  = "rec-playlist-sync-a"
		variantB  = "rec-playlist-sync-b"
		itemID    = "playlist-sync-item"
	)

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-playlist-sync-a",
		AlbumClusterID: "album-playlist-sync-a",
		SourceFileID:   "src-playlist-sync-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-playlist-sync-a", []byte("playlist-sync-a"))
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-playlist-sync-b",
		AlbumClusterID: "album-playlist-sync-b",
		SourceFileID:   "src-playlist-sync-b",
		QualityRank:    120,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-playlist-sync-b", []byte("playlist-sync-b"))

	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if _, err := app.EnsureRecordingEncoding(ctx, variantA, "desktop"); err != nil {
		t.Fatalf("ensure non-preferred variant encoding: %v", err)
	}

	applyPlaylistItemOplogForTest(t, app, playlistItemOplogApplyTestInput{
		LibraryID:   library.LibraryID,
		DeviceID:    "remote-playlist-sync",
		EntityID:    itemID,
		PlaylistID:  playlist.PlaylistID,
		ItemID:      itemID,
		RecordingID: variantA,
	})

	var item PlaylistItem
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND item_id = ?", library.LibraryID, playlist.PlaylistID, itemID).
		Take(&item).Error; err != nil {
		t.Fatalf("load synced playlist item: %v", err)
	}
	if item.TrackVariantID != clusterID {
		t.Fatalf("stored playlist recording id = %q, want %q", item.TrackVariantID, clusterID)
	}

	tracks, err := app.ListPlaylistTracks(ctx, apitypes.PlaylistTrackListRequest{PlaylistID: playlist.PlaylistID})
	if err != nil {
		t.Fatalf("list playlist tracks: %v", err)
	}
	if len(tracks.Items) != 1 {
		t.Fatalf("playlist track count = %d, want 1", len(tracks.Items))
	}
	if tracks.Items[0].LibraryRecordingID != clusterID {
		t.Fatalf("playlist library recording id = %q, want %q", tracks.Items[0].LibraryRecordingID, clusterID)
	}
	if tracks.Items[0].RecordingID != variantB {
		t.Fatalf("playlist playback recording id = %q, want %q", tracks.Items[0].RecordingID, variantB)
	}

	if _, err := startPlaylistPinJob(t, ctx, app, playlist.PlaylistID, "desktop"); err != nil {
		t.Fatalf("start playlist pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop"), JobPhaseCompleted)

	blobA, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, variantA, "desktop")
	if err != nil || !ok {
		t.Fatalf("cached encoding for non-preferred variant = %v, %v", ok, err)
	}
	if ok, err := app.pin.hasPinnedCachedEncoding(ctx, local, variantB, "desktop"); err != nil {
		t.Fatalf("pinned cached encoding for preferred variant: %v", err)
	} else if ok {
		t.Fatalf("expected preferred variant to stay uncached when only local source is available")
	}

	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "playlist", playlist.PlaylistID, "desktop")
	if slicesContains(protectedBlobIDs, blobA) {
		t.Fatalf("expected non-preferred variant blob %q to remain unprotected, got %v", blobA, protectedBlobIDs)
	}
	if len(protectedBlobIDs) != 0 {
		t.Fatalf("expected local-only logical playlist pin to avoid protecting transcoded blobs, got %v", protectedBlobIDs)
	}
}

func TestSyncedLikedPinningNormalizesLogicalRecordingIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buildIndex atomic.Int32
	builder := &fakeAACBuilder{}
	builder.before = func() {
		builder.result = []byte(fmt.Sprintf("liked-sync-%d", buildIndex.Add(1)))
	}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "liked-sync-logical")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-liked-sync"
		variantA  = "rec-liked-sync-a"
		variantB  = "rec-liked-sync-b"
	)

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-liked-sync-a",
		AlbumClusterID: "album-liked-sync-a",
		SourceFileID:   "src-liked-sync-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-liked-sync-a", []byte("liked-sync-a"))
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-liked-sync-b",
		AlbumClusterID: "album-liked-sync-b",
		SourceFileID:   "src-liked-sync-b",
		QualityRank:    120,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-liked-sync-b", []byte("liked-sync-b"))

	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if _, err := app.EnsureRecordingEncoding(ctx, variantA, "desktop"); err != nil {
		t.Fatalf("ensure non-preferred variant encoding: %v", err)
	}

	likedPlaylistID := likedPlaylistIDForLibrary(library.LibraryID)
	applyPlaylistItemOplogForTest(t, app, playlistItemOplogApplyTestInput{
		LibraryID:   library.LibraryID,
		DeviceID:    "remote-liked-sync",
		EntityID:    "legacy-liked-item",
		PlaylistID:  likedPlaylistID,
		ItemID:      "legacy-liked-item",
		RecordingID: variantA,
		Liked:       true,
	})

	var item PlaylistItem
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ?", library.LibraryID, likedPlaylistID).
		Take(&item).Error; err != nil {
		t.Fatalf("load synced liked item: %v", err)
	}
	if item.TrackVariantID != clusterID {
		t.Fatalf("stored liked recording id = %q, want %q", item.TrackVariantID, clusterID)
	}
	if item.ItemID != likedItemID(likedPlaylistID, clusterID) {
		t.Fatalf("liked item id = %q, want %q", item.ItemID, likedItemID(likedPlaylistID, clusterID))
	}

	liked, err := app.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{})
	if err != nil {
		t.Fatalf("list liked recordings: %v", err)
	}
	if len(liked.Items) != 1 {
		t.Fatalf("liked track count = %d, want 1", len(liked.Items))
	}
	if liked.Items[0].LibraryRecordingID != clusterID {
		t.Fatalf("liked library recording id = %q, want %q", liked.Items[0].LibraryRecordingID, clusterID)
	}
	if liked.Items[0].RecordingID != variantB {
		t.Fatalf("liked playback recording id = %q, want %q", liked.Items[0].RecordingID, variantB)
	}

	if _, err := startLikedPinJob(t, ctx, app, likedPlaylistID, "desktop"); err != nil {
		t.Fatalf("start liked pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)

	blobA, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, variantA, "desktop")
	if err != nil || !ok {
		t.Fatalf("cached encoding for non-preferred variant = %v, %v", ok, err)
	}
	if ok, err := app.pin.hasPinnedCachedEncoding(ctx, local, variantB, "desktop"); err != nil {
		t.Fatalf("pinned cached encoding for preferred variant: %v", err)
	} else if ok {
		t.Fatalf("expected preferred variant to stay uncached when only local source is available")
	}

	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "playlist", likedPlaylistID, "desktop")
	if slicesContains(protectedBlobIDs, blobA) {
		t.Fatalf("expected non-preferred variant blob %q to remain unprotected, got %v", blobA, protectedBlobIDs)
	}
	if len(protectedBlobIDs) != 0 {
		t.Fatalf("expected local-only logical liked pin to avoid protecting transcoded blobs, got %v", protectedBlobIDs)
	}
}

type playlistItemOplogApplyTestInput struct {
	LibraryID   string
	DeviceID    string
	EntityID    string
	PlaylistID  string
	ItemID      string
	RecordingID string
	Liked       bool
}

func applyPlaylistItemOplogForTest(t *testing.T, app *App, in playlistItemOplogApplyTestInput) {
	t.Helper()

	payloadJSON, err := json.Marshal(playlistItemOplogPayload{
		PlaylistID:  strings.TrimSpace(in.PlaylistID),
		ItemID:      strings.TrimSpace(in.ItemID),
		RecordingID: strings.TrimSpace(in.RecordingID),
		PositionKey: defaultPositionKey(),
		Liked:       in.Liked,
	})
	if err != nil {
		t.Fatalf("marshal playlist item oplog payload: %v", err)
	}

	entry := OplogEntry{
		LibraryID:   strings.TrimSpace(in.LibraryID),
		DeviceID:    strings.TrimSpace(in.DeviceID),
		Seq:         1,
		OpID:        strings.TrimSpace(in.DeviceID) + ":1",
		EntityType:  entityTypePlaylistItem,
		EntityID:    strings.TrimSpace(in.EntityID),
		OpKind:      "upsert",
		TSNS:        time.Now().UTC().UnixNano(),
		PayloadJSON: string(payloadJSON),
	}
	if entry.DeviceID == "" {
		entry.DeviceID = "remote-playlist-item"
		entry.OpID = entry.DeviceID + ":1"
	}
	if entry.EntityID == "" {
		entry.EntityID = entry.OpID
	}

	if err := app.db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		return applyPlaylistItemOplogEntryTx(tx, entry)
	}); err != nil {
		t.Fatalf("apply playlist item oplog entry: %v", err)
	}
}

func TestPinnedLikedAutoRefreshFetchesNewTrack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("liked-refresh")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "liked-refresh")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-liked-refresh-a",
		TrackClusterID: "rec-liked-refresh-a",
		AlbumID:        "album-liked-refresh",
		AlbumClusterID: "album-liked-refresh",
		SourceFileID:   "src-liked-refresh-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-liked-refresh-a", []byte("liked-a"))
	if err := app.LikeRecording(ctx, "rec-liked-refresh-a"); err != nil {
		t.Fatalf("like recording a: %v", err)
	}

	likedPlaylistID := likedPlaylistIDForLibrary(local.LibraryID)
	if _, err := startLikedPinJob(t, ctx, app, likedPlaylistID, "desktop"); err != nil {
		t.Fatalf("start liked pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-liked-refresh-b",
		TrackClusterID: "rec-liked-refresh-b",
		AlbumID:        "album-liked-refresh",
		AlbumClusterID: "album-liked-refresh",
		SourceFileID:   "src-liked-refresh-b",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-liked-refresh-b", []byte("liked-b"))
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-liked-refresh-b", "desktop"); err != nil {
		t.Fatalf("pre-refresh cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected new liked track to start uncached")
	}
	if err := app.LikeRecording(ctx, "rec-liked-refresh-b"); err != nil {
		t.Fatalf("like recording b: %v", err)
	}

	final := waitForJobPhase(t, ctx, app, refreshPinScopeJobID(local.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)
	if final.Kind != jobKindRefreshPinnedPlaylist {
		t.Fatalf("refresh job kind = %q, want %q", final.Kind, jobKindRefreshPinnedPlaylist)
	}
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-liked-refresh-b", "desktop"); err != nil {
		t.Fatalf("post-refresh cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected pinned local liked track to remain uncached")
	}
	if len(builder.calls) != 0 {
		t.Fatalf("expected pinned local liked refresh to avoid transcoding, got %d calls", len(builder.calls))
	}
}

func TestPinnedPlaylistWithLocalSourceDoesNotTranscode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("should-not-run")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "playlist-local-no-transcode")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	playlist, err := app.CreatePlaylist(ctx, "Local only playlist", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-local-only",
		TrackClusterID: "rec-local-only",
		AlbumID:        "album-local-only",
		AlbumClusterID: "album-local-only",
		SourceFileID:   "src-local-only",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-local-only", []byte("local-only"))
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-local-only",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	if _, err := startPlaylistPinJob(t, ctx, app, playlist.PlaylistID, "desktop"); err != nil {
		t.Fatalf("start playlist pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", playlist.PlaylistID, "desktop"), JobPhaseCompleted)

	if len(builder.calls) != 0 {
		t.Fatalf("expected local-only pin to avoid transcoding, got %d calls", len(builder.calls))
	}
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-local-only", "desktop"); err != nil {
		t.Fatalf("cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected local-only pin to avoid creating cached encodings")
	}

	pin := loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "playlist", playlist.PlaylistID)
	if pin.PendingCount != 0 {
		t.Fatalf("playlist pin pending count = %d, want 0", pin.PendingCount)
	}

	var member PinMember
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ? AND profile = ? AND variant_recording_id = ?",
			local.LibraryID,
			local.DeviceID,
			"playlist",
			playlist.PlaylistID,
			"desktop",
			"rec-local-only",
		).
		Take(&member).Error; err != nil {
		t.Fatalf("load pin member: %v", err)
	}
	if member.Pending {
		t.Fatalf("expected local-only pin member to be satisfied")
	}
}

func TestStartPinLikedKeepsPinWhenTrackIsInitiallyUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("liked-pending")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "liked-pending")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const recordingID = "rec-liked-pending"
	seedAlbumTrackWithoutSources(t, app, library.LibraryID, "album-liked-pending", recordingID)
	if err := app.LikeRecording(ctx, recordingID); err != nil {
		t.Fatalf("like unavailable recording: %v", err)
	}

	likedPlaylistID := likedPlaylistIDForLibrary(local.LibraryID)
	job, err := startLikedPinJob(t, ctx, app, likedPlaylistID, "desktop")
	if err != nil {
		t.Fatalf("start pin liked offline: %v", err)
	}
	if job.Kind != jobKindPinPlaylist {
		t.Fatalf("job kind = %q, want %q", job.Kind, jobKindPinPlaylist)
	}

	final := waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)
	if final.Kind != jobKindPinPlaylist {
		t.Fatalf("final job kind = %q, want %q", final.Kind, jobKindPinPlaylist)
	}

	_ = loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "playlist", likedPlaylistID)
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, recordingID, "desktop"); err != nil {
		t.Fatalf("initial cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected unavailable liked track to start uncached")
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    recordingID,
		TrackClusterID: recordingID,
		AlbumID:        "album-liked-pending",
		AlbumClusterID: "album-liked-pending",
		SourceFileID:   "src-liked-pending",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-liked-pending", []byte("liked-pending"))

	app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		InvalidateAll: true,
	})

	refresh := waitForJobPhase(t, ctx, app, refreshPinScopeJobID(local.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)
	if refresh.Kind != jobKindRefreshPinnedPlaylist {
		t.Fatalf("refresh job kind = %q, want %q", refresh.Kind, jobKindRefreshPinnedPlaylist)
	}
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, recordingID, "desktop"); err != nil {
		t.Fatalf("post-refresh cached encoding lookup: %v", err)
	} else if ok {
		t.Fatalf("expected pinned liked track to become locally available without background transcoding")
	}
	pin := loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "playlist", likedPlaylistID)
	if pin.PendingCount != 0 {
		t.Fatalf("pending pin count after local source appears = %d, want 0", pin.PendingCount)
	}
}

func TestSyncActiveRuntimeServicesRefreshesPendingPins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("runtime-refresh")}
	owner := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "runtime-pin-refresh")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	const (
		recordingID  = "rec-runtime-refresh"
		albumID      = "album-runtime-refresh"
		sourceFileID = "src-runtime-refresh"
	)
	seedInput := playbackSeedInput{
		RecordingID:    recordingID,
		TrackClusterID: recordingID,
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   sourceFileID,
		QualityRank:    100,
	}
	seedSourceOnlyRecording(t, owner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	seedSourceOnlyRecording(t, joiner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	writeSeedSourceFile(t, owner, library.LibraryID, ownerLocal.DeviceID, sourceFileID, []byte("runtime-refresh-source"))

	if err := joiner.LikeRecording(ctx, recordingID); err != nil {
		t.Fatalf("like recording on joiner: %v", err)
	}
	likedPlaylistID := likedPlaylistIDForLibrary(joinerLocal.LibraryID)
	if _, err := startLikedPinJob(t, ctx, joiner, likedPlaylistID, "desktop"); err != nil {
		t.Fatalf("start liked pin: %v", err)
	}
	waitForJobPhase(t, ctx, joiner, pinJobID(joinerLocal.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)

	pin := loadPinRoot(t, ctx, joiner, joinerLocal.LibraryID, joinerLocal.DeviceID, "playlist", likedPlaylistID)
	if pin.PendingCount != 1 {
		t.Fatalf("pending pin count before runtime refresh = %d, want 1", pin.PendingCount)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner-runtime", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner-runtime", joiner))

	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("sync active runtime services: %v", err)
	}

	refresh := waitForJobPhase(t, ctx, joiner, refreshPinScopeJobID(joinerLocal.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)
	if refresh.Kind != jobKindRefreshPinnedPlaylist {
		t.Fatalf("refresh job kind = %q, want %q", refresh.Kind, jobKindRefreshPinnedPlaylist)
	}
	if _, _, ok, err := joiner.playback.bestCachedEncoding(ctx, joinerLocal.LibraryID, joinerLocal.DeviceID, recordingID, "desktop"); err != nil {
		t.Fatalf("cached encoding lookup after runtime refresh: %v", err)
	} else if !ok {
		t.Fatalf("expected runtime refresh to fetch the pending liked recording")
	}
}

func TestAvailabilityInvalidationRefreshesPendingPins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("availability-refresh")}
	owner := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "availability-pin-refresh")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	const (
		recordingID  = "rec-availability-refresh"
		albumID      = "album-availability-refresh"
		sourceFileID = "src-availability-refresh"
	)
	seedInput := playbackSeedInput{
		RecordingID:    recordingID,
		TrackClusterID: recordingID,
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   sourceFileID,
		QualityRank:    100,
	}
	seedSourceOnlyRecording(t, owner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	seedSourceOnlyRecording(t, joiner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	writeSeedSourceFile(t, owner, library.LibraryID, ownerLocal.DeviceID, sourceFileID, []byte("availability-refresh-source"))

	if err := joiner.LikeRecording(ctx, recordingID); err != nil {
		t.Fatalf("like recording on joiner: %v", err)
	}
	likedPlaylistID := likedPlaylistIDForLibrary(joinerLocal.LibraryID)
	if _, err := startLikedPinJob(t, ctx, joiner, likedPlaylistID, "desktop"); err != nil {
		t.Fatalf("start liked pin: %v", err)
	}
	waitForJobPhase(t, ctx, joiner, pinJobID(joinerLocal.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner-availability", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner-availability", joiner))

	joiner.emitAvailabilityInvalidateAll()

	refresh := waitForJobPhase(t, ctx, joiner, refreshPinScopeJobID(joinerLocal.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)
	if refresh.Kind != jobKindRefreshPinnedPlaylist {
		t.Fatalf("refresh job kind = %q, want %q", refresh.Kind, jobKindRefreshPinnedPlaylist)
	}
	if _, _, ok, err := joiner.playback.bestCachedEncoding(ctx, joinerLocal.LibraryID, joinerLocal.DeviceID, recordingID, "desktop"); err != nil {
		t.Fatalf("cached encoding lookup after availability refresh: %v", err)
	} else if !ok {
		t.Fatalf("expected availability refresh to fetch the pending liked recording")
	}
}

func TestStartPinLikedFetchesRemoteTrackAcrossClusterVariantMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("liked-remote-cluster")}
	owner := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "liked-remote-cluster-pin")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	const (
		clusterID      = "liked-cluster-remote"
		requestedID    = "liked-requested-variant"
		providerOnlyID = "liked-provider-variant"
		albumID        = "liked-remote-album"
		sourceFileID   = "liked-remote-source"
	)
	seedInput := playbackSeedInput{
		RecordingID:    providerOnlyID,
		TrackClusterID: clusterID,
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   sourceFileID,
		QualityRank:    100,
	}
	seedSourceOnlyRecording(t, owner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	seedSourceOnlyRecording(t, joiner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	writeSeedSourceFile(t, owner, library.LibraryID, ownerLocal.DeviceID, sourceFileID, []byte("liked-remote-lossless"))

	now := time.Now().UTC()
	for _, app := range []*App{owner, joiner} {
		if err := app.db.WithContext(ctx).Create(&TrackVariantModel{
			LibraryID:      library.LibraryID,
			TrackVariantID: requestedID,
			TrackClusterID: clusterID,
			KeyNorm:        requestedID,
			Title:          requestedID,
			DurationMS:     180000,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatalf("seed requested variant: %v", err)
		}
		if err := app.db.WithContext(ctx).Create(&AlbumTrack{
			LibraryID:      library.LibraryID,
			AlbumVariantID: albumID,
			TrackVariantID: requestedID,
			DiscNo:         1,
			TrackNo:        2,
		}).Error; err != nil {
			t.Fatalf("seed requested album track: %v", err)
		}
	}
	if err := joiner.catalog.SetPreferredRecordingVariant(ctx, clusterID, requestedID); err != nil {
		t.Fatalf("set preferred recording variant on joiner: %v", err)
	}
	if err := joiner.LikeRecording(ctx, clusterID); err != nil {
		t.Fatalf("like recording on joiner: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	likedPlaylistID := likedPlaylistIDForLibrary(joinerLocal.LibraryID)
	if _, err := startLikedPinJob(t, ctx, joiner, likedPlaylistID, "desktop"); err != nil {
		t.Fatalf("start liked pin: %v", err)
	}
	waitForJobPhase(t, ctx, joiner, pinJobID(joinerLocal.LibraryID, "playlist", likedPlaylistID, "desktop"), JobPhaseCompleted)

	availability, err := joiner.GetRecordingAvailability(ctx, clusterID, "desktop")
	if err != nil {
		t.Fatalf("get recording availability after liked pin: %v", err)
	}
	if availability.State != apitypes.AvailabilityPlayableCachedOpt {
		t.Fatalf("availability state after liked pin = %q, want %q", availability.State, apitypes.AvailabilityPlayableCachedOpt)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("remote transcode call count = %d, want 1", len(builder.calls))
	}
}

func TestExactRecordingPinDoesNotReuseSiblingVariantCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("exact-sibling")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "exact-sibling-cache")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-exact-sibling"
		variantA  = "rec-exact-sibling-a"
		variantB  = "rec-exact-sibling-b"
	)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-exact-sibling-b",
		AlbumClusterID: "album-exact-sibling-b",
		SourceFileID:   "src-exact-sibling-b",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-exact-sibling-b", []byte("variant-b"))

	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).Create(&TrackVariantModel{
		LibraryID:      library.LibraryID,
		TrackVariantID: variantA,
		TrackClusterID: clusterID,
		KeyNorm:        variantA,
		Title:          variantA,
		DurationMS:     180000,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed exact sibling variant: %v", err)
	}

	if _, err := app.EnsureRecordingEncoding(ctx, variantB, "desktop"); err != nil {
		t.Fatalf("ensure sibling variant encoding: %v", err)
	}
	if ok, err := app.pin.hasPinnedCachedEncoding(ctx, local, variantA, "desktop"); err != nil {
		t.Fatalf("pinned cached lookup for exact sibling: %v", err)
	} else if ok {
		t.Fatalf("expected exact pin lookup to ignore sibling cached encoding")
	}

	if _, err := startExactRecordingPinJob(t, ctx, app, variantA, "desktop"); err != nil {
		t.Fatalf("start exact recording pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "recording", variantA, "desktop"), JobPhaseCompleted)

	pin := loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "recording", variantA)
	if pin.PendingCount != 1 {
		t.Fatalf("exact recording pin pending count = %d, want 1", pin.PendingCount)
	}
	if blobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "recording", variantA, "desktop"); len(blobIDs) != 0 {
		t.Fatalf("expected exact pin to protect no blobs while exact variant is unavailable, got %v", blobIDs)
	}
}

func TestPinnedAlbumAutoRefreshReconcilesReplacementTrackSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buildIndex atomic.Int32
	builder := &fakeAACBuilder{}
	builder.before = func() {
		builder.result = []byte(fmt.Sprintf("album-refresh-%d", buildIndex.Add(1)))
	}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "album-refresh")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-album-refresh-remote", time.Now().UTC())

	const albumID = "album-refresh"
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-album-refresh-a",
		TrackClusterID: "rec-album-refresh-a",
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   "src-album-refresh-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-album-refresh-a", []byte("album-a"))
	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-album-refresh-b",
		AlbumID:        albumID,
		SourceFileID:   "src-album-refresh-b",
		EncodingID:     "enc-album-refresh-b",
		BlobID:         testBlobID("b"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})

	if _, err := startAlbumPinJob(t, ctx, app, albumID, "desktop"); err != nil {
		t.Fatalf("start album pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "album", albumID, "desktop"), JobPhaseCompleted)
	blobB, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-album-refresh-b", "desktop")
	if err != nil || !ok {
		t.Fatalf("cached encoding for track b = %v, %v", ok, err)
	}
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-album-refresh-a", "desktop"); err != nil {
		t.Fatalf("cached encoding for track a: %v", err)
	} else if ok {
		t.Fatalf("expected local album track a to remain uncached")
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-album-refresh-c",
		TrackClusterID: "rec-album-refresh-c",
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   "src-album-refresh-c",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-album-refresh-c", []byte("album-c"))
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND album_variant_id = ? AND track_variant_id = ?", library.LibraryID, albumID, "rec-album-refresh-b").
		Delete(&AlbumTrack{}).Error; err != nil {
		t.Fatalf("delete replaced album track: %v", err)
	}

	app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateBase,
		Entity:   apitypes.CatalogChangeEntityAlbum,
		EntityID: albumID,
		AlbumIDs: []string{albumID},
	})

	final := waitForJobPhase(t, ctx, app, refreshPinScopeJobID(local.LibraryID, "album", albumID, "desktop"), JobPhaseCompleted)
	if final.Kind != jobKindRefreshPinnedAlbum {
		t.Fatalf("refresh job kind = %q, want %q", final.Kind, jobKindRefreshPinnedAlbum)
	}
	if _, _, ok, err := app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, "rec-album-refresh-c", "desktop"); err != nil {
		t.Fatalf("cached encoding for replacement track: %v", err)
	} else if ok {
		t.Fatalf("expected local replacement track to remain uncached")
	}

	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "album", albumID, "desktop")
	if slicesContains(protectedBlobIDs, blobB) {
		t.Fatalf("expected removed track blob %q to stop being protected, got %v", blobB, protectedBlobIDs)
	}
	if len(protectedBlobIDs) != 0 {
		t.Fatalf("expected local-only replacement album to avoid protecting transcoded blobs, got %v", protectedBlobIDs)
	}
}

func TestPinAlbumAggregatesCachedTracks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "pin-album")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-pin-album-batch-remote", time.Now().UTC())

	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-a",
		AlbumID:        "album-batch",
		SourceFileID:   "src-a",
		EncodingID:     "enc-a",
		BlobID:         testBlobID("2"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-b",
		AlbumID:        "album-batch",
		SourceFileID:   "src-b",
		EncodingID:     "enc-b",
		BlobID:         testBlobID("3"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})

	if _, err := startAlbumPinJob(t, ctx, app, "album-batch", "desktop"); err != nil {
		t.Fatalf("start album pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "album", "album-batch", "desktop"), JobPhaseCompleted)

	pin := loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "album", "album-batch")
	if pin.Profile != "desktop" {
		t.Fatalf("album pin profile = %q, want desktop", pin.Profile)
	}
}

func TestPinAlbumUsesExactVariantScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "pin-album-variant")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-pin-album-variant-remote", time.Now().UTC())

	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-variant-a",
		AlbumID:        "album-variant-a",
		SourceFileID:   "src-variant-a",
		EncodingID:     "enc-variant-a",
		BlobID:         testBlobID("e"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-variant-b",
		AlbumID:        "album-variant-b",
		SourceFileID:   "src-variant-b",
		EncodingID:     "enc-variant-b",
		BlobID:         testBlobID("f"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	if err := app.db.WithContext(ctx).
		Model(&AlbumVariantModel{}).
		Where("library_id = ? AND album_variant_id IN ?", library.LibraryID, []string{"album-variant-a", "album-variant-b"}).
		Update("album_cluster_id", "album-cluster-variants").Error; err != nil {
		t.Fatalf("update album cluster ids: %v", err)
	}

	if _, err := startAlbumPinJob(t, ctx, app, "album-variant-b", "desktop"); err != nil {
		t.Fatalf("start album variant pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, pinJobID(local.LibraryID, "album", "album-variant-b", "desktop"), JobPhaseCompleted)

	_ = loadPinRoot(t, ctx, app, local.LibraryID, local.DeviceID, "album", "album-variant-b")

	summaryA := mustAlbumAvailabilitySummary(t, app, ctx, "album-variant-a")
	if summaryA.ScopePinned {
		t.Fatalf("expected first variant to remain unpinned")
	}
	summaryB := mustAlbumAvailabilitySummary(t, app, ctx, "album-variant-b")
	if !summaryB.ScopePinned {
		t.Fatalf("expected selected variant to report scope pinned")
	}

	protectedBlobIDs := pinRootBlobIDs(t, ctx, app, local.LibraryID, local.DeviceID, "album", "album-variant-b", "desktop")
	if slicesContains(protectedBlobIDs, testBlobID("e")) {
		t.Fatalf("expected exact variant pin not to protect other variant blobs: %v", protectedBlobIDs)
	}
	if !slicesContains(protectedBlobIDs, testBlobID("f")) {
		t.Fatalf("expected exact variant pin to protect selected variant blob: %v", protectedBlobIDs)
	}
}

func TestPinAlbumAllowsFullyLocalAlbum(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, &fakeAACBuilder{result: []byte("local-album")})
	library, err := app.CreateLibrary(ctx, "pin-local-album")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-local-album-a",
		TrackClusterID: "rec-local-album-a",
		AlbumID:        "album-local-only",
		AlbumClusterID: "album-local-only",
		SourceFileID:   "src-local-album-a",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-local-album-a", []byte("local-a"))

	job, err := startAlbumPinJob(t, ctx, app, "album-local-only", "desktop")
	if err != nil {
		t.Fatalf("start local album pin: %v", err)
	}
	waitForJobPhase(t, ctx, app, job.JobID, JobPhaseCompleted)

	if count := countPinRoots(t, ctx, app, local.LibraryID, local.DeviceID, "album", "album-local-only"); count != 1 {
		t.Fatalf("local album pin count = %d, want 1", count)
	}
}

func TestAvailabilityOverviewsReflectLocalAndRemoteDevices(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "overview")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-local",
		AlbumID:        "album-overview",
		SourceFileID:   "src-local",
		EncodingID:     "enc-local",
		BlobID:         testBlobID("4"),
		Profile:        "desktop",
		LastVerifiedAt: now,
	})
	writeCacheBlob(t, app, testBlobID("4"), 72)

	remoteDeviceID := "dev-remote"
	if err := app.db.WithContext(ctx).Create(&Device{
		DeviceID:   remoteDeviceID,
		Name:       "remote-device",
		JoinedAt:   now,
		LastSeenAt: &now,
	}).Error; err != nil {
		t.Fatalf("create remote device: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         remoteDeviceID,
		Role:             roleMember,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("create remote membership: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
		RecordingID:    "rec-local",
		TrackClusterID: "rec-local",
		AlbumID:        "album-overview",
		AlbumClusterID: "album-overview",
		SourceFileID:   "src-remote-local",
		QualityRank:    80,
	})
	seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
		RecordingID:    "rec-remote",
		TrackClusterID: "rec-remote",
		AlbumID:        "album-overview",
		AlbumClusterID: "album-overview",
		SourceFileID:   "src-remote-only",
		QualityRank:    70,
	})

	recordingOverview, err := app.GetRecordingAvailabilityOverview(ctx, "rec-local", "desktop")
	if err != nil {
		t.Fatalf("get recording availability overview: %v", err)
	}
	if recordingOverview.Playback.State != apitypes.AvailabilityPlayableCachedOpt {
		t.Fatalf("recording playback state = %q, want %q", recordingOverview.Playback.State, apitypes.AvailabilityPlayableCachedOpt)
	}
	if !recordingOverview.Availability.HasLocalCachedOptimized {
		t.Fatalf("expected recording overview to report cached local availability")
	}
	if !recordingOverview.Availability.HasRemoteSource {
		t.Fatalf("expected recording overview to report remote source availability")
	}
	if len(recordingOverview.Devices) != 2 {
		t.Fatalf("recording devices = %d, want 2", len(recordingOverview.Devices))
	}

	albumOverview, err := app.GetAlbumAvailabilityOverview(ctx, "album-overview", "desktop")
	if err != nil {
		t.Fatalf("get album availability overview: %v", err)
	}
	if len(albumOverview.Tracks) != 2 {
		t.Fatalf("album tracks = %d, want 2", len(albumOverview.Tracks))
	}
	if albumOverview.Availability.LocalTrackCount != 1 {
		t.Fatalf("local track count = %d, want 1", albumOverview.Availability.LocalTrackCount)
	}
	if albumOverview.Availability.AvailableTrackCount != 2 {
		t.Fatalf("available track count = %d, want 2", albumOverview.Availability.AvailableTrackCount)
	}
	if albumOverview.Availability.UnavailableTrackCount != 0 {
		t.Fatalf("unavailable track count = %d, want 0", albumOverview.Availability.UnavailableTrackCount)
	}
	if albumOverview.Availability.RemoteTrackCount != 2 {
		t.Fatalf("remote track count = %d, want 2", albumOverview.Availability.RemoteTrackCount)
	}
}

func TestListRecordingPlaybackAvailabilityMatchesSingleItemResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, recordingIDs, _ := seedAvailabilityFixture(t)

	items, err := app.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:     recordingIDs,
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list recording playback availability: %v", err)
	}
	if len(items) != len(recordingIDs) {
		t.Fatalf("batch availability items = %d, want %d", len(items), len(recordingIDs))
	}

	for index, recordingID := range recordingIDs {
		single, err := app.GetRecordingAvailability(ctx, recordingID, "desktop")
		if err != nil {
			t.Fatalf("get recording availability %s: %v", recordingID, err)
		}
		if !reflect.DeepEqual(items[index], single) {
			t.Fatalf("batch availability for %s = %+v, want %+v", recordingID, items[index], single)
		}
	}
}

func TestListRecordingPlaybackAvailabilityRespectsPreferredVariantSelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "batch-availability-preferred-variant")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	if _, err := app.requireActiveContext(ctx); err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-preferred-remote", now)

	const (
		albumID              = "album-preferred"
		trackClusterID       = "cluster-preferred"
		requestedRecordingID = "rec-preferred-base"
		preferredRecordingID = "rec-preferred-remote"
	)
	blobID := testBlobID("a")

	seedCacheRecording(t, app, library.LibraryID, remoteDeviceID, cacheSeedInput{
		RecordingID:    preferredRecordingID,
		AlbumID:        albumID,
		SourceFileID:   "src-preferred-remote",
		EncodingID:     "enc-preferred-remote",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: now,
	})
	if err := app.db.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id = ?", library.LibraryID, preferredRecordingID).
		Update("track_cluster_id", trackClusterID).Error; err != nil {
		t.Fatalf("update preferred variant cluster: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&TrackVariantModel{
		LibraryID:      library.LibraryID,
		TrackVariantID: requestedRecordingID,
		TrackClusterID: trackClusterID,
		KeyNorm:        requestedRecordingID,
		Title:          requestedRecordingID,
		DurationMS:     180000,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed requested variant: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&AlbumTrack{
		LibraryID:      library.LibraryID,
		AlbumVariantID: albumID,
		TrackVariantID: requestedRecordingID,
		DiscNo:         1,
		TrackNo:        1,
	}).Error; err != nil {
		t.Fatalf("seed requested album track: %v", err)
	}
	writeCacheBlob(t, app, blobID, 96)

	if err := app.catalog.SetPreferredRecordingVariant(ctx, requestedRecordingID, preferredRecordingID); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}

	batchItems, err := app.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:     []string{trackClusterID},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list recording playback availability: %v", err)
	}
	if len(batchItems) != 1 {
		t.Fatalf("batch availability items = %d, want 1", len(batchItems))
	}

	single, err := app.GetRecordingAvailability(ctx, trackClusterID, "desktop")
	if err != nil {
		t.Fatalf("get recording availability: %v", err)
	}
	if !reflect.DeepEqual(batchItems[0], single) {
		t.Fatalf("batch availability = %+v, want %+v", batchItems[0], single)
	}
	if batchItems[0].State != apitypes.AvailabilityPlayableRemoteOpt {
		t.Fatalf("batch availability state = %q, want %q", batchItems[0].State, apitypes.AvailabilityPlayableRemoteOpt)
	}
	if batchItems[0].SourceKind != apitypes.PlaybackSourceRemoteOpt {
		t.Fatalf("batch availability source kind = %q, want %q", batchItems[0].SourceKind, apitypes.PlaybackSourceRemoteOpt)
	}
}

func TestListAlbumAvailabilitySummariesMatchesOverviewAvailability(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, _, albumID := seedAvailabilityFixture(t)

	items, err := app.ListAlbumAvailabilitySummaries(ctx, apitypes.AlbumAvailabilitySummaryListRequest{
		AlbumIDs:         []string{albumID},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list album availability summaries: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("album availability summary items = %d, want 1", len(items))
	}

	overview, err := app.GetAlbumAvailabilityOverview(ctx, albumID, "desktop")
	if err != nil {
		t.Fatalf("get album availability overview: %v", err)
	}
	if items[0].AlbumID != albumID {
		t.Fatalf("album summary id = %q, want %q", items[0].AlbumID, albumID)
	}
	if items[0].PreferredProfile != "desktop" {
		t.Fatalf("album summary preferred profile = %q, want desktop", items[0].PreferredProfile)
	}
	if !reflect.DeepEqual(items[0].Availability, overview.Availability) {
		t.Fatalf("album summary availability = %+v, want %+v", items[0].Availability, overview.Availability)
	}
}

func TestAlbumAvailabilitySummaryStates(t *testing.T) {
	t.Parallel()

	t.Run("local when every track has a local source file", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-local")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}
		local, err := app.requireActiveContext(ctx)
		if err != nil {
			t.Fatalf("active context: %v", err)
		}

		seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
			RecordingID:    "rec-local-a",
			TrackClusterID: "rec-local-a",
			AlbumID:        "album-local",
			AlbumClusterID: "album-local",
			SourceFileID:   "src-local-a",
			QualityRank:    100,
		})
		seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
			RecordingID:    "rec-local-b",
			TrackClusterID: "rec-local-b",
			AlbumID:        "album-local",
			AlbumClusterID: "album-local",
			SourceFileID:   "src-local-b",
			QualityRank:    100,
		})

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-local")
		if summary.State != apitypes.AggregateAvailabilityStateLocal {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStateLocal)
		}
		if summary.LocalSourceTrackCount != 2 {
			t.Fatalf("local source track count = %d, want 2", summary.LocalSourceTrackCount)
		}
	})

	t.Run("pinned when every track is pinned even without an album pin", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-pinned")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}
		local, err := app.requireActiveContext(ctx)
		if err != nil {
			t.Fatalf("active context: %v", err)
		}

		remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-pinned-remote", time.Now().UTC())
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-pinned-a",
			TrackClusterID: "rec-pinned-a",
			AlbumID:        "album-pinned",
			AlbumClusterID: "album-pinned",
			SourceFileID:   "src-pinned-a",
			QualityRank:    90,
		})
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-pinned-b",
			TrackClusterID: "rec-pinned-b",
			AlbumID:        "album-pinned",
			AlbumClusterID: "album-pinned",
			SourceFileID:   "src-pinned-b",
			QualityRank:    90,
		})
		seedPinRoot(t, app, library.LibraryID, local.DeviceID, "recording", "rec-pinned-a", "desktop")
		seedPinRoot(t, app, library.LibraryID, local.DeviceID, "recording", "rec-pinned-b", "desktop")

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-pinned")
		if summary.State != apitypes.AggregateAvailabilityStatePinned {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStatePinned)
		}
		if summary.PinnedTrackCount != 2 {
			t.Fatalf("pinned track count = %d, want 2", summary.PinnedTrackCount)
		}
	})

	t.Run("cached when every track is cached locally from remote sources", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-cached")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}
		local, err := app.requireActiveContext(ctx)
		if err != nil {
			t.Fatalf("active context: %v", err)
		}

		remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-cached-remote", time.Now().UTC())
		seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
			RecordingID:    "rec-cached-a",
			AlbumID:        "album-cached",
			SourceFileID:   "src-cached-a",
			EncodingID:     "enc-cached-a",
			BlobID:         testBlobID("a"),
			Profile:        "desktop",
			LastVerifiedAt: time.Now().UTC(),
		})
		seedRemoteCachedRecording(t, app, library.LibraryID, remoteDeviceID, local.DeviceID, cacheSeedInput{
			RecordingID:    "rec-cached-b",
			AlbumID:        "album-cached",
			SourceFileID:   "src-cached-b",
			EncodingID:     "enc-cached-b",
			BlobID:         testBlobID("b"),
			Profile:        "desktop",
			LastVerifiedAt: time.Now().UTC(),
		})

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-cached")
		if summary.State != apitypes.AggregateAvailabilityStateCached {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStateCached)
		}
		if summary.CachedTrackCount != 2 {
			t.Fatalf("cached track count = %d, want 2", summary.CachedTrackCount)
		}
		if summary.LocalSourceTrackCount != 0 {
			t.Fatalf("local source track count = %d, want 0", summary.LocalSourceTrackCount)
		}
	})

	t.Run("available when every track is fetchable from online peers", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-available")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}

		remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-available-remote", time.Now().UTC())
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-available-a",
			TrackClusterID: "rec-available-a",
			AlbumID:        "album-available",
			AlbumClusterID: "album-available",
			SourceFileID:   "src-available-a",
			QualityRank:    88,
		})
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-available-b",
			TrackClusterID: "rec-available-b",
			AlbumID:        "album-available",
			AlbumClusterID: "album-available",
			SourceFileID:   "src-available-b",
			QualityRank:    88,
		})

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-available")
		if summary.State != apitypes.AggregateAvailabilityStateAvailable {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStateAvailable)
		}
		if summary.AvailableNowTrackCount != 2 {
			t.Fatalf("available-now track count = %d, want 2", summary.AvailableNowTrackCount)
		}
	})

	t.Run("partial when only some tracks are currently reachable", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-partial")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}

		remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-partial-remote", time.Now().UTC())
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-partial-a",
			TrackClusterID: "rec-partial-a",
			AlbumID:        "album-partial",
			AlbumClusterID: "album-partial",
			SourceFileID:   "src-partial-a",
			QualityRank:    88,
		})
		seedAlbumTrackWithoutSources(t, app, library.LibraryID, "album-partial", "rec-partial-b")

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-partial")
		if summary.State != apitypes.AggregateAvailabilityStatePartial {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStatePartial)
		}
		if summary.AvailableNowTrackCount != 1 {
			t.Fatalf("available-now track count = %d, want 1", summary.AvailableNowTrackCount)
		}
	})

	t.Run("offline when remote paths exist but nothing is currently reachable", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-offline")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}

		lastSeen := time.Now().UTC().Add(-3 * availabilityOnlineWindow)
		remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-offline-remote", lastSeen)
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-offline-a",
			TrackClusterID: "rec-offline-a",
			AlbumID:        "album-offline",
			AlbumClusterID: "album-offline",
			SourceFileID:   "src-offline-a",
			QualityRank:    88,
		})

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-offline")
		if summary.State != apitypes.AggregateAvailabilityStateOffline {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStateOffline)
		}
		if summary.OfflineTrackCount != 1 {
			t.Fatalf("offline track count = %d, want 1", summary.OfflineTrackCount)
		}
	})

	t.Run("unavailable when no track has any source", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-unavailable")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}

		seedAlbumTrackWithoutSources(t, app, library.LibraryID, "album-unavailable", "rec-unavailable-a")

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-unavailable")
		if summary.State != apitypes.AggregateAvailabilityStateUnavailable {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStateUnavailable)
		}
		if summary.UnavailableTrackCount != 1 {
			t.Fatalf("unavailable track count = %d, want 1", summary.UnavailableTrackCount)
		}
	})

	t.Run("offline when network is disabled even if providers are online", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		app := openCacheTestApp(t, 1024)
		library, err := app.CreateLibrary(ctx, "album-state-network-off")
		if err != nil {
			t.Fatalf("create library: %v", err)
		}
		app.transportService = nil

		remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-network-off-remote", time.Now().UTC())
		seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
			RecordingID:    "rec-network-off-a",
			TrackClusterID: "rec-network-off-a",
			AlbumID:        "album-network-off",
			AlbumClusterID: "album-network-off",
			SourceFileID:   "src-network-off-a",
			QualityRank:    88,
		})

		summary := mustAlbumAvailabilitySummary(t, app, ctx, "album-network-off")
		if summary.State != apitypes.AggregateAvailabilityStateOffline {
			t.Fatalf("summary state = %q, want %q", summary.State, apitypes.AggregateAvailabilityStateOffline)
		}
	})
}

func seedAvailabilityFixture(t *testing.T) (*App, []string, string) {
	t.Helper()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "overview")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	albumID := "album-overview"
	recordingIDs := []string{"rec-local", "rec-remote"}

	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    recordingIDs[0],
		AlbumID:        albumID,
		SourceFileID:   "src-local",
		EncodingID:     "enc-local",
		BlobID:         testBlobID("4"),
		Profile:        "desktop",
		LastVerifiedAt: now,
	})
	writeCacheBlob(t, app, testBlobID("4"), 72)

	remoteDeviceID := "dev-remote"
	if err := app.db.WithContext(ctx).Create(&Device{
		DeviceID:   remoteDeviceID,
		Name:       "remote-device",
		JoinedAt:   now,
		LastSeenAt: &now,
	}).Error; err != nil {
		t.Fatalf("create remote device: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         remoteDeviceID,
		Role:             roleMember,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("create remote membership: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
		RecordingID:    recordingIDs[0],
		TrackClusterID: recordingIDs[0],
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   "src-remote-local",
		QualityRank:    80,
	})
	seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
		RecordingID:    recordingIDs[1],
		TrackClusterID: recordingIDs[1],
		AlbumID:        albumID,
		AlbumClusterID: albumID,
		SourceFileID:   "src-remote-only",
		QualityRank:    70,
	})

	return app, recordingIDs, albumID
}

func mustAlbumAvailabilitySummary(t *testing.T, app *App, ctx context.Context, albumID string) apitypes.AggregateAvailabilitySummary {
	t.Helper()

	items, err := app.ListAlbumAvailabilitySummaries(ctx, apitypes.AlbumAvailabilitySummaryListRequest{
		AlbumIDs:         []string{albumID},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list album availability summaries: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("album availability summary items = %d, want 1", len(items))
	}
	return items[0].Availability
}

func seedRemoteLibraryMember(t *testing.T, app *App, libraryID, deviceID string, lastSeen time.Time) string {
	t.Helper()

	if err := app.db.WithContext(context.Background()).Create(&Device{
		DeviceID:   deviceID,
		Name:       deviceID,
		JoinedAt:   lastSeen,
		LastSeenAt: &lastSeen,
	}).Error; err != nil {
		t.Fatalf("create remote device %s: %v", deviceID, err)
	}
	if err := app.db.WithContext(context.Background()).Create(&Membership{
		LibraryID:        libraryID,
		DeviceID:         deviceID,
		Role:             roleMember,
		CapabilitiesJSON: "{}",
		JoinedAt:         lastSeen,
	}).Error; err != nil {
		t.Fatalf("create remote membership %s: %v", deviceID, err)
	}
	return deviceID
}

func slicesContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func seedAlbumTrackWithoutSources(t *testing.T, app *App, libraryID, albumID, recordingID string) {
	t.Helper()

	now := time.Now().UTC()
	var count int64
	if err := app.db.WithContext(context.Background()).
		Model(&AlbumVariantModel{}).
		Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).
		Count(&count).Error; err != nil {
		t.Fatalf("count missing album %s: %v", albumID, err)
	}
	if count == 0 {
		if err := app.db.WithContext(context.Background()).Create(&AlbumVariantModel{
			LibraryID:      libraryID,
			AlbumVariantID: albumID,
			AlbumClusterID: albumID,
			KeyNorm:        albumID,
			Title:          albumID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatalf("seed missing album %s: %v", albumID, err)
		}
	}
	count = 0
	if err := app.db.WithContext(context.Background()).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id = ?", libraryID, recordingID).
		Count(&count).Error; err != nil {
		t.Fatalf("count missing track %s: %v", recordingID, err)
	}
	if count == 0 {
		if err := app.db.WithContext(context.Background()).Create(&TrackVariantModel{
			LibraryID:      libraryID,
			TrackVariantID: recordingID,
			TrackClusterID: recordingID,
			KeyNorm:        recordingID,
			Title:          recordingID,
			DurationMS:     180000,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatalf("seed missing track %s: %v", recordingID, err)
		}
	}
	if err := app.db.WithContext(context.Background()).Create(&AlbumTrack{
		LibraryID:      libraryID,
		AlbumVariantID: albumID,
		TrackVariantID: recordingID,
		DiscNo:         1,
		TrackNo:        1,
	}).Error; err != nil {
		t.Fatalf("seed missing album track %s: %v", recordingID, err)
	}
}

func seedRemoteCachedRecording(t *testing.T, app *App, libraryID, remoteDeviceID, localDeviceID string, in cacheSeedInput) {
	t.Helper()

	var count int64
	if err := app.db.WithContext(context.Background()).
		Model(&AlbumVariantModel{}).
		Where("library_id = ? AND album_variant_id = ?", libraryID, in.AlbumID).
		Count(&count).Error; err != nil {
		t.Fatalf("count cached album %s: %v", in.AlbumID, err)
	}
	if count == 0 {
		seedCacheRecording(t, app, libraryID, remoteDeviceID, in)
	} else {
		seedCachedRecordingForExistingAlbum(t, app, libraryID, remoteDeviceID, in)
	}

	lastVerified := in.LastVerifiedAt
	if err := app.db.WithContext(context.Background()).Create(&DeviceAssetCacheModel{
		LibraryID:        libraryID,
		DeviceID:         localDeviceID,
		OptimizedAssetID: in.EncodingID,
		IsCached:         true,
		LastVerifiedAt:   &lastVerified,
		UpdatedAt:        time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed local cache mirror %s: %v", in.EncodingID, err)
	}
	writeCacheBlob(t, app, in.BlobID, 64)
}

type playbackSeedInput struct {
	RecordingID    string
	TrackClusterID string
	AlbumID        string
	AlbumClusterID string
	SourceFileID   string
	QualityRank    int
}

func seedSourceOnlyRecording(t *testing.T, app *App, libraryID, deviceID string, in playbackSeedInput) {
	t.Helper()

	now := time.Now().UTC()
	var count int64
	if err := app.db.WithContext(context.Background()).
		Model(&AlbumVariantModel{}).
		Where("library_id = ? AND album_variant_id = ?", libraryID, in.AlbumID).
		Count(&count).Error; err != nil {
		t.Fatalf("count album %s: %v", in.AlbumID, err)
	}
	if count == 0 {
		if err := app.db.Create(&AlbumVariantModel{
			LibraryID:      libraryID,
			AlbumVariantID: in.AlbumID,
			AlbumClusterID: in.AlbumClusterID,
			KeyNorm:        in.AlbumID,
			Title:          in.AlbumID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatalf("seed album %s: %v", in.AlbumID, err)
		}
	}

	count = 0
	if err := app.db.WithContext(context.Background()).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id = ?", libraryID, in.RecordingID).
		Count(&count).Error; err != nil {
		t.Fatalf("count track %s: %v", in.RecordingID, err)
	}
	if count == 0 {
		if err := app.db.Create(&TrackVariantModel{
			LibraryID:      libraryID,
			TrackVariantID: in.RecordingID,
			TrackClusterID: in.TrackClusterID,
			KeyNorm:        in.RecordingID,
			Title:          in.RecordingID,
			DurationMS:     180000,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatalf("seed track %s: %v", in.RecordingID, err)
		}
	}

	count = 0
	if err := app.db.WithContext(context.Background()).
		Model(&AlbumTrack{}).
		Where("library_id = ? AND album_variant_id = ? AND track_variant_id = ? AND disc_no = 1 AND track_no = 1", libraryID, in.AlbumID, in.RecordingID).
		Count(&count).Error; err != nil {
		t.Fatalf("count album track %s: %v", in.RecordingID, err)
	}
	if count == 0 {
		if err := app.db.Create(&AlbumTrack{
			LibraryID:      libraryID,
			AlbumVariantID: in.AlbumID,
			TrackVariantID: in.RecordingID,
			DiscNo:         1,
			TrackNo:        1,
		}).Error; err != nil {
			t.Fatalf("seed album track %s: %v", in.RecordingID, err)
		}
	}

	sourcePath := filepath.Join(t.TempDir(), in.SourceFileID+".flac")
	if err := app.db.Create(&SourceFileModel{
		LibraryID:         libraryID,
		DeviceID:          deviceID,
		SourceFileID:      in.SourceFileID,
		TrackVariantID:    in.RecordingID,
		LocalPath:         sourcePath,
		PathKey:           sourcePath,
		SourceFingerprint: in.SourceFileID + "-fp",
		HashAlgo:          "b3",
		HashHex:           "abcd",
		MTimeNS:           now.UnixNano(),
		SizeBytes:         1024,
		Container:         "flac",
		Codec:             "flac",
		Bitrate:           1411200,
		SampleRate:        44100,
		Channels:          2,
		IsLossless:        true,
		QualityRank:       in.QualityRank,
		DurationMS:        180000,
		TagsJSON:          "{}",
		LastSeenAt:        now,
		IsPresent:         true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed source file %s: %v", in.SourceFileID, err)
	}
}

func seedCachedRecordingForExistingAlbum(t *testing.T, app *App, libraryID, deviceID string, in cacheSeedInput) {
	t.Helper()

	now := time.Now().UTC()
	if err := app.db.Create(&TrackVariantModel{
		LibraryID:      libraryID,
		TrackVariantID: in.RecordingID,
		TrackClusterID: in.RecordingID,
		KeyNorm:        in.RecordingID,
		Title:          in.RecordingID,
		DurationMS:     180000,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed recording %s: %v", in.RecordingID, err)
	}
	if err := app.db.Create(&AlbumTrack{
		LibraryID:      libraryID,
		AlbumVariantID: in.AlbumID,
		TrackVariantID: in.RecordingID,
		DiscNo:         1,
		TrackNo:        2,
	}).Error; err != nil {
		t.Fatalf("seed album track %s: %v", in.RecordingID, err)
	}

	path := filepath.Join(t.TempDir(), in.SourceFileID+".flac")
	if err := app.db.Create(&SourceFileModel{
		LibraryID:         libraryID,
		DeviceID:          deviceID,
		SourceFileID:      in.SourceFileID,
		TrackVariantID:    in.RecordingID,
		LocalPath:         path,
		PathKey:           path,
		SourceFingerprint: in.SourceFileID + "-fp",
		HashAlgo:          "b3",
		HashHex:           in.BlobID[len("b3:"):],
		MTimeNS:           now.UnixNano(),
		SizeBytes:         1024,
		Container:         "flac",
		Codec:             "flac",
		Bitrate:           1411200,
		SampleRate:        44100,
		Channels:          2,
		IsLossless:        true,
		QualityRank:       100,
		DurationMS:        180000,
		TagsJSON:          "{}",
		LastSeenAt:        now,
		IsPresent:         true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed source file %s: %v", in.SourceFileID, err)
	}
	if err := app.db.Create(&OptimizedAssetModel{
		LibraryID:         libraryID,
		OptimizedAssetID:  in.EncodingID,
		SourceFileID:      in.SourceFileID,
		TrackVariantID:    in.RecordingID,
		Profile:           in.Profile,
		BlobID:            in.BlobID,
		MIME:              "audio/mp4",
		DurationMS:        180000,
		Bitrate:           128000,
		Codec:             "aac",
		Container:         "m4a",
		CreatedByDeviceID: deviceID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed optimized asset %s: %v", in.EncodingID, err)
	}
	lastVerified := in.LastVerifiedAt
	if err := app.db.Create(&DeviceAssetCacheModel{
		LibraryID:        libraryID,
		DeviceID:         deviceID,
		OptimizedAssetID: in.EncodingID,
		IsCached:         true,
		LastVerifiedAt:   &lastVerified,
		UpdatedAt:        now,
	}).Error; err != nil {
		t.Fatalf("seed cache row %s: %v", in.EncodingID, err)
	}
}
