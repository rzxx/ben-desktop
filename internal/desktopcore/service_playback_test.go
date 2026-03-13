package desktopcore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	apitypes "ben/core/api/types"
)

func TestPinRecordingOfflinePersistsAndUnpinsCachedAsset(t *testing.T) {
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

	result, err := app.PinRecordingOffline(ctx, "rec-pin", "desktop")
	if err != nil {
		t.Fatalf("pin recording offline: %v", err)
	}
	if result.BlobID != blobID {
		t.Fatalf("blob id = %q, want %q", result.BlobID, blobID)
	}
	if result.SourceKind != apitypes.PlaybackSourceCachedOpt {
		t.Fatalf("source kind = %q, want %q", result.SourceKind, apitypes.PlaybackSourceCachedOpt)
	}
	if !result.FromLocal {
		t.Fatalf("expected pinned result to be local")
	}
	if result.Bytes != 128 {
		t.Fatalf("bytes = %d, want 128", result.Bytes)
	}

	var pin OfflinePin
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, "recording", "rec-pin").
		Take(&pin).Error; err != nil {
		t.Fatalf("load recording pin: %v", err)
	}
	if pin.Profile != "desktop" {
		t.Fatalf("pin profile = %q, want desktop", pin.Profile)
	}

	if err := app.UnpinRecordingOffline(ctx, "rec-pin"); err != nil {
		t.Fatalf("unpin recording offline: %v", err)
	}

	var count int64
	if err := app.db.WithContext(ctx).
		Model(&OfflinePin{}).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, "recording", "rec-pin").
		Count(&count).Error; err != nil {
		t.Fatalf("count recording pins: %v", err)
	}
	if count != 0 {
		t.Fatalf("recording pin count = %d, want 0", count)
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

func TestPinAlbumOfflineAggregatesCachedTracks(t *testing.T) {
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

	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-a",
		AlbumID:        "album-batch",
		SourceFileID:   "src-a",
		EncodingID:     "enc-a",
		BlobID:         testBlobID("2"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	seedCachedRecordingForExistingAlbum(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-b",
		AlbumID:        "album-batch",
		SourceFileID:   "src-b",
		EncodingID:     "enc-b",
		BlobID:         testBlobID("3"),
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, testBlobID("2"), 64)
	writeCacheBlob(t, app, testBlobID("3"), 96)

	result, err := app.PinAlbumOffline(ctx, "album-batch", "desktop")
	if err != nil {
		t.Fatalf("pin album offline: %v", err)
	}
	if result.Tracks != 2 {
		t.Fatalf("tracks = %d, want 2", result.Tracks)
	}
	if result.TotalBytes != 160 {
		t.Fatalf("total bytes = %d, want 160", result.TotalBytes)
	}
	if result.LocalHits != 2 {
		t.Fatalf("local hits = %d, want 2", result.LocalHits)
	}

	var pin OfflinePin
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, "album", "album-batch").
		Take(&pin).Error; err != nil {
		t.Fatalf("load album pin: %v", err)
	}
	if pin.Profile != "desktop" {
		t.Fatalf("album pin profile = %q, want desktop", pin.Profile)
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
