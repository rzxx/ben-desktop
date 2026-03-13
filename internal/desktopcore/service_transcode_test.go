package desktopcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apitypes "ben/core/api/types"
)

type fakeAACBuilder struct {
	mu      sync.Mutex
	calls   []fakeAACCall
	result  []byte
	waitFor <-chan struct{}
	before  func()
}

type fakeAACCall struct {
	sourcePath string
	profile    string
}

func (b *fakeAACBuilder) BuildAAC(ctx context.Context, sourcePath string, profile AudioProfile) ([]byte, error) {
	b.mu.Lock()
	b.calls = append(b.calls, fakeAACCall{
		sourcePath: strings.TrimSpace(sourcePath),
		profile:    strings.TrimSpace(profile.ID),
	})
	b.mu.Unlock()
	if b.before != nil {
		b.before()
	}
	if b.waitFor != nil {
		select {
		case <-b.waitFor:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(b.result) == 0 {
		return []byte("encoded-aac"), nil
	}
	return append([]byte(nil), b.result...), nil
}

func TestEnsureRecordingEncodingCreatesLocalCachedAsset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("transcoded-data")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "transcode-create")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-transcode",
		TrackClusterID: "rec-transcode",
		AlbumID:        "album-transcode",
		AlbumClusterID: "album-transcode",
		SourceFileID:   "src-transcode",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-transcode", []byte("lossless"))

	created, err := app.EnsureRecordingEncoding(ctx, "rec-transcode", "desktop")
	if err != nil {
		t.Fatalf("ensure recording encoding: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if len(builder.calls) != 1 {
		t.Fatalf("transcode calls = %d, want 1", len(builder.calls))
	}
	if builder.calls[0].profile != audioProfileVBRHigh {
		t.Fatalf("builder profile = %q, want %q", builder.calls[0].profile, audioProfileVBRHigh)
	}

	var asset OptimizedAssetModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND source_file_id = ? AND profile = ?", library.LibraryID, "src-transcode", audioProfileVBRHigh).
		Take(&asset).Error; err != nil {
		t.Fatalf("load optimized asset: %v", err)
	}
	if asset.TrackVariantID != "rec-transcode" {
		t.Fatalf("asset track variant = %q, want rec-transcode", asset.TrackVariantID)
	}
	if asset.OptimizedAssetID == "" || asset.BlobID == "" {
		t.Fatalf("expected optimized asset identifiers to be populated: %+v", asset)
	}
	if !blobExists(t, app, asset.BlobID) {
		t.Fatalf("expected encoded blob %s to exist", asset.BlobID)
	}

	var cacheRow DeviceAssetCacheModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", library.LibraryID, local.DeviceID, asset.OptimizedAssetID).
		Take(&cacheRow).Error; err != nil {
		t.Fatalf("load device cache row: %v", err)
	}
	if !cacheRow.IsCached {
		t.Fatalf("expected cache row to be marked cached")
	}
}

func TestEnsureRecordingEncodingSkipsExistingCachedAsset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "transcode-skip")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-skip",
		AlbumID:        "album-skip",
		SourceFileID:   "src-skip",
		EncodingID:     "enc-skip",
		BlobID:         testBlobID("6"),
		Profile:        audioProfileVBRHigh,
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, testBlobID("6"), 128)

	created, err := app.EnsureRecordingEncoding(ctx, "rec-skip", audioProfileVBRHigh)
	if err != nil {
		t.Fatalf("ensure recording encoding: %v", err)
	}
	if created {
		t.Fatalf("expected existing asset to be reused")
	}
	if len(builder.calls) != 0 {
		t.Fatalf("transcode calls = %d, want 0", len(builder.calls))
	}
}

func TestEnsureRecordingEncodingRejectsGuestProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "transcode-guest")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-guest",
		TrackClusterID: "rec-guest",
		AlbumID:        "album-guest",
		AlbumClusterID: "album-guest",
		SourceFileID:   "src-guest",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-guest", []byte("lossless"))
	if err := app.db.WithContext(ctx).
		Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", library.LibraryID, local.DeviceID).
		Update("role", roleGuest).Error; err != nil {
		t.Fatalf("update local role to guest: %v", err)
	}

	created, err := app.EnsureRecordingEncoding(ctx, "rec-guest", "desktop")
	if !errors.Is(err, ErrProviderOnlyTranscode) {
		t.Fatalf("expected provider-only transcode error, got %v", err)
	}
	if created {
		t.Fatalf("expected created=false for guest transcode")
	}
	if len(builder.calls) != 0 {
		t.Fatalf("transcode calls = %d, want 0", len(builder.calls))
	}
}

func TestEnsureRecordingEncodingDedupesConcurrentRequests(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	release := make(chan struct{})
	var callCount int32
	builder := &fakeAACBuilder{
		waitFor: release,
		result:  []byte("deduped-encoded"),
		before: func() {
			atomic.AddInt32(&callCount, 1)
		},
	}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "transcode-dedupe")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-dedupe",
		TrackClusterID: "rec-dedupe",
		AlbumID:        "album-dedupe",
		AlbumClusterID: "album-dedupe",
		SourceFileID:   "src-dedupe",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-dedupe", []byte("lossless"))

	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			created, err := app.EnsureRecordingEncoding(ctx, "rec-dedupe", "desktop")
			results <- created
			errs <- err
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(release)

	createdCount := 0
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("ensure recording encoding: %v", err)
		}
		if <-results {
			createdCount++
		}
	}
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("transcode call count = %d, want 1", got)
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
}

func TestEnsurePlaybackRecordingBuildsCachedOptimizedAsset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("playback-encoded")}
	app := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	library, err := app.CreateLibrary(ctx, "playback-transcode")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-playback",
		TrackClusterID: "rec-playback",
		AlbumID:        "album-playback",
		AlbumClusterID: "album-playback",
		SourceFileID:   "src-playback",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-playback", []byte("lossless"))

	result, err := app.EnsurePlaybackRecording(ctx, "rec-playback", "desktop")
	if err != nil {
		t.Fatalf("ensure playback recording: %v", err)
	}
	if result.SourceKind != apitypes.PlaybackSourceCachedOpt {
		t.Fatalf("source kind = %q, want %q", result.SourceKind, apitypes.PlaybackSourceCachedOpt)
	}
	if result.BlobID == "" || result.EncodingID == "" {
		t.Fatalf("expected playback result to include cached asset ids: %+v", result)
	}
	if !result.FromLocal {
		t.Fatalf("expected cached playback result to be local")
	}
}

func TestGuestCachedRemoteReserveReportsPlayableRemote(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "guest-rereserve")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	if _, err := app.requireActiveContext(ctx); err != nil {
		t.Fatalf("active context: %v", err)
	}
	app.SetSyncTransport(&blockingSyncTransport{})

	now := time.Now().UTC()
	remoteDeviceID := "guest-remote"
	if err := app.db.WithContext(ctx).Create(&Device{
		DeviceID:   remoteDeviceID,
		Name:       "guest-remote",
		PeerID:     "peer-guest-remote",
		JoinedAt:   now,
		LastSeenAt: &now,
	}).Error; err != nil {
		t.Fatalf("create remote device: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         remoteDeviceID,
		Role:             roleGuest,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("create remote membership: %v", err)
	}
	seedCacheRecording(t, app, library.LibraryID, remoteDeviceID, cacheSeedInput{
		RecordingID:    "rec-remote-cached",
		AlbumID:        "album-remote-cached",
		SourceFileID:   "src-remote-cached",
		EncodingID:     "enc-remote-cached",
		BlobID:         testBlobID("7"),
		Profile:        audioProfileVBRHigh,
		LastVerifiedAt: now,
	})

	availability, err := app.GetRecordingAvailability(ctx, "rec-remote-cached", "desktop")
	if err != nil {
		t.Fatalf("get recording availability: %v", err)
	}
	if availability.State != apitypes.AvailabilityPlayableRemoteOpt {
		t.Fatalf("availability state = %q, want %q", availability.State, apitypes.AvailabilityPlayableRemoteOpt)
	}
	if availability.SourceKind != apitypes.PlaybackSourceRemoteOpt {
		t.Fatalf("availability source kind = %q, want %q", availability.SourceKind, apitypes.PlaybackSourceRemoteOpt)
	}

	items, err := app.ListRecordingAvailability(ctx, "rec-remote-cached", "desktop")
	if err != nil {
		t.Fatalf("list recording availability: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("availability items = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.DeviceID != remoteDeviceID {
			continue
		}
		if !item.CachedOptimized {
			t.Fatalf("expected guest remote device to advertise cached optimized availability")
		}
		if !item.SourcePresent {
			t.Fatalf("expected guest remote device source row to remain materialized")
		}
	}
}

func writeSeedSourceFile(t *testing.T, app *App, libraryID, deviceID, sourceFileID string, data []byte) {
	t.Helper()

	var row SourceFileModel
	if err := app.db.WithContext(context.Background()).
		Where("library_id = ? AND device_id = ? AND source_file_id = ?", libraryID, deviceID, sourceFileID).
		Take(&row).Error; err != nil {
		t.Fatalf("load source file %s: %v", sourceFileID, err)
	}
	if err := os.MkdirAll(filepath.Dir(row.LocalPath), 0o755); err != nil {
		t.Fatalf("mkdir source file dir: %v", err)
	}
	if err := os.WriteFile(row.LocalPath, data, 0o644); err != nil {
		t.Fatalf("write source file %s: %v", sourceFileID, err)
	}
}
