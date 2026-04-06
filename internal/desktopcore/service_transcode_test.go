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

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
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

	batchItems, err := app.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:      []string{"rec-remote-cached"},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list recording playback availability: %v", err)
	}
	if len(batchItems) != 1 {
		t.Fatalf("batch availability items = %d, want 1", len(batchItems))
	}
	if batchItems[0].State != apitypes.AvailabilityPlayableRemoteOpt {
		t.Fatalf("batch availability state = %q, want %q", batchItems[0].State, apitypes.AvailabilityPlayableRemoteOpt)
	}
	if batchItems[0].SourceKind != apitypes.PlaybackSourceRemoteOpt {
		t.Fatalf("batch availability source kind = %q, want %q", batchItems[0].SourceKind, apitypes.PlaybackSourceRemoteOpt)
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

func TestEnsurePlaybackRecordingFetchesRemoteCachedAsset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "remote-playback-fetch")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	cacheInput := cacheSeedInput{
		RecordingID:    "rec-remote-fetch",
		AlbumID:        "album-remote-fetch",
		SourceFileID:   "src-remote-fetch",
		EncodingID:     "enc-remote-fetch",
		Profile:        audioProfileVBRHigh,
		LastVerifiedAt: time.Now().UTC(),
	}
	remoteBlob := []byte(strings.Repeat("r", 192))
	cacheInput.BlobID = blobIDForBytes(remoteBlob)
	seedCacheRecording(t, owner, library.LibraryID, ownerLocal.DeviceID, cacheInput)
	seedCacheRecording(t, joiner, library.LibraryID, ownerLocal.DeviceID, cacheInput)
	if _, err := owner.transcode.storeBlobBytes(remoteBlob); err != nil {
		t.Fatalf("store remote playback blob: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	result, err := joiner.EnsurePlaybackRecording(ctx, cacheInput.RecordingID, "desktop")
	if err != nil {
		t.Fatalf("ensure playback recording: %v", err)
	}
	if result.SourceKind != apitypes.PlaybackSourceRemoteOpt {
		t.Fatalf("source kind = %q, want %q", result.SourceKind, apitypes.PlaybackSourceRemoteOpt)
	}
	if result.FromLocal {
		t.Fatalf("expected remote fetch result to report remote origin")
	}
	if result.BlobID != cacheInput.BlobID || result.EncodingID != cacheInput.EncodingID {
		t.Fatalf("unexpected remote fetch result: %+v", result)
	}
	if result.Bytes != 192 {
		t.Fatalf("bytes = %d, want 192", result.Bytes)
	}
	if !blobExists(t, joiner, cacheInput.BlobID) {
		t.Fatalf("expected fetched blob %s to exist locally", cacheInput.BlobID)
	}

	var cacheRow DeviceAssetCacheModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", library.LibraryID, joinerLocal.DeviceID, cacheInput.EncodingID).
		Take(&cacheRow).Error; err != nil {
		t.Fatalf("load local fetched cache row: %v", err)
	}
	if !cacheRow.IsCached {
		t.Fatalf("expected fetched asset to be marked cached locally")
	}
}

func TestPreparePlaybackRecordingRequestsProviderTranscodeFromRemotePeer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("remote-transcoded")}
	owner := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "remote-provider-transcode")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)

	seedInput := playbackSeedInput{
		RecordingID:    "rec-provider-remote",
		TrackClusterID: "rec-provider-remote",
		AlbumID:        "album-provider-remote",
		AlbumClusterID: "album-provider-remote",
		SourceFileID:   "src-provider-remote",
		QualityRank:    100,
	}
	seedSourceOnlyRecording(t, owner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	seedSourceOnlyRecording(t, joiner, library.LibraryID, ownerLocal.DeviceID, seedInput)
	writeSeedSourceFile(t, owner, library.LibraryID, ownerLocal.DeviceID, seedInput.SourceFileID, []byte("lossless-remote"))

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	status, err := joiner.PreparePlaybackRecording(ctx, seedInput.RecordingID, "desktop", apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		t.Fatalf("prepare playback recording: %v", err)
	}
	if status.Phase != apitypes.PlaybackPreparationPreparingTranscode {
		t.Fatalf("preparation phase = %q, want %q", status.Phase, apitypes.PlaybackPreparationPreparingTranscode)
	}
	if status.SourceKind != apitypes.PlaybackSourceRemoteOpt {
		t.Fatalf("preparation source kind = %q, want %q", status.SourceKind, apitypes.PlaybackSourceRemoteOpt)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err = joiner.GetPlaybackPreparation(ctx, seedInput.RecordingID, "desktop")
		if err != nil {
			t.Fatalf("get playback preparation: %v", err)
		}
		if status.Phase == apitypes.PlaybackPreparationReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Phase != apitypes.PlaybackPreparationReady {
		t.Fatalf("expected playback preparation to become ready, got %+v", status)
	}
	if status.SourceKind != apitypes.PlaybackSourceCachedOpt {
		t.Fatalf("ready preparation source kind = %q, want %q", status.SourceKind, apitypes.PlaybackSourceCachedOpt)
	}
	if status.PlayableURI == "" || status.BlobID == "" || status.EncodingID == "" {
		t.Fatalf("expected ready preparation with local cached artifact: %+v", status)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("remote transcode call count = %d, want 1", len(builder.calls))
	}
	if builder.calls[0].profile != audioProfileVBRHigh {
		t.Fatalf("remote transcode profile = %q, want %q", builder.calls[0].profile, audioProfileVBRHigh)
	}
	if !blobExists(t, joiner, status.BlobID) {
		t.Fatalf("expected remotely transcoded blob %s to exist locally", status.BlobID)
	}

	availability, err := joiner.GetRecordingAvailability(ctx, seedInput.RecordingID, "desktop")
	if err != nil {
		t.Fatalf("get recording availability after prepare: %v", err)
	}
	if availability.State != apitypes.AvailabilityPlayableCachedOpt {
		t.Fatalf("availability state after remote prepare = %q, want %q", availability.State, apitypes.AvailabilityPlayableCachedOpt)
	}
}

func TestPreparePlaybackRecordingRequestsProviderTranscodeAcrossClusterVariantMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	builder := &fakeAACBuilder{result: []byte("remote-cluster-transcoded")}
	owner := openCacheTestAppWithTranscodeBuilder(t, 1024, builder)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "remote-provider-transcode-cluster")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)

	const (
		clusterID       = "rec-provider-cluster"
		requestedID     = "rec-provider-requested"
		providerOnlyID  = "rec-provider-remote-only"
		albumID         = "album-provider-cluster"
		sourceFileID    = "src-provider-cluster-remote"
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
	writeSeedSourceFile(t, owner, library.LibraryID, ownerLocal.DeviceID, sourceFileID, []byte("lossless-remote-cluster"))

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
			t.Fatalf("seed requested variant on app: %v", err)
		}
		if err := app.db.WithContext(ctx).Create(&AlbumTrack{
			LibraryID:      library.LibraryID,
			AlbumVariantID: albumID,
			TrackVariantID: requestedID,
			DiscNo:         1,
			TrackNo:        2,
		}).Error; err != nil {
			t.Fatalf("seed requested album track on app: %v", err)
		}
	}
	if err := joiner.catalog.SetPreferredRecordingVariant(ctx, clusterID, requestedID); err != nil {
		t.Fatalf("set preferred recording variant on joiner: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	availability, err := joiner.GetRecordingAvailability(ctx, clusterID, "desktop")
	if err != nil {
		t.Fatalf("get recording availability before prepare: %v", err)
	}
	if availability.State != apitypes.AvailabilityWaitingProviderTranscode {
		t.Fatalf("availability state before prepare = %q, want %q", availability.State, apitypes.AvailabilityWaitingProviderTranscode)
	}

	status, err := joiner.PreparePlaybackRecording(ctx, clusterID, "desktop", apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		t.Fatalf("prepare playback recording: %v", err)
	}
	if status.Phase != apitypes.PlaybackPreparationPreparingTranscode {
		t.Fatalf("preparation phase = %q, want %q", status.Phase, apitypes.PlaybackPreparationPreparingTranscode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err = joiner.GetPlaybackPreparation(ctx, clusterID, "desktop")
		if err != nil {
			t.Fatalf("get playback preparation: %v", err)
		}
		if status.Phase == apitypes.PlaybackPreparationReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Phase != apitypes.PlaybackPreparationReady {
		t.Fatalf("expected playback preparation to become ready, got %+v", status)
	}
	if status.SourceKind != apitypes.PlaybackSourceCachedOpt {
		t.Fatalf("ready preparation source kind = %q, want %q", status.SourceKind, apitypes.PlaybackSourceCachedOpt)
	}
	if status.BlobID == "" || status.EncodingID == "" || status.PlayableURI == "" {
		t.Fatalf("expected ready preparation with local cached artifact: %+v", status)
	}
	if len(builder.calls) != 1 {
		t.Fatalf("remote transcode call count = %d, want 1", len(builder.calls))
	}
	if !blobExists(t, joiner, status.BlobID) {
		t.Fatalf("expected remotely transcoded blob %s to exist locally", status.BlobID)
	}

	availability, err = joiner.GetRecordingAvailability(ctx, clusterID, "desktop")
	if err != nil {
		t.Fatalf("get recording availability after prepare: %v", err)
	}
	if availability.State != apitypes.AvailabilityPlayableCachedOpt {
		t.Fatalf("availability state after prepare = %q, want %q", availability.State, apitypes.AvailabilityPlayableCachedOpt)
	}
}

func TestEnsurePlaybackRecordingRejectsGuestReserveWithTamperedBlob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	guest := openCacheTestApp(t, 1024)
	requester := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "guest-rereserve-verify")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	_, guestLocal := seedSharedLibraryForSync(t, owner, guest, library)
	_, requesterLocal := seedSharedLibraryForSync(t, owner, requester, library)

	now := time.Now().UTC()
	requesterCertRow, ok, err := owner.loadMembershipCert(ctx, library.LibraryID, requesterLocal.DeviceID)
	if err != nil {
		t.Fatalf("load requester membership cert: %v", err)
	}
	if !ok {
		t.Fatal("expected requester membership cert")
	}
	if err := requester.db.WithContext(ctx).Create(&Device{
		DeviceID:   guestLocal.DeviceID,
		Name:       guestLocal.Device,
		PeerID:     guestLocal.PeerID,
		JoinedAt:   now,
		LastSeenAt: &now,
	}).Error; err != nil {
		t.Fatalf("seed requester guest device: %v", err)
	}
	if err := requester.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         guestLocal.DeviceID,
		Role:             roleGuest,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed requester guest membership: %v", err)
	}
	if err := guest.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&Device{
			DeviceID:   requesterLocal.DeviceID,
			Name:       requesterLocal.Device,
			PeerID:     requesterLocal.PeerID,
			JoinedAt:   now,
			LastSeenAt: &now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Membership{
			LibraryID:        library.LibraryID,
			DeviceID:         requesterLocal.DeviceID,
			Role:             roleMember,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		return saveMembershipCertTx(tx, membershipCertEnvelopeFromRow(requesterCertRow))
	}); err != nil {
		t.Fatalf("seed guest requester membership material: %v", err)
	}

	for _, app := range []*App{owner, guest, requester} {
		if err := app.db.WithContext(ctx).
			Model(&Membership{}).
			Where("library_id = ? AND device_id = ?", library.LibraryID, guestLocal.DeviceID).
			Update("role", roleGuest).Error; err != nil {
			t.Fatalf("update guest role: %v", err)
		}
	}

	cacheInput := cacheSeedInput{
		RecordingID:    "rec-guest-tampered",
		AlbumID:        "album-guest-tampered",
		SourceFileID:   "src-guest-tampered",
		EncodingID:     "enc-guest-tampered",
		Profile:        audioProfileVBRHigh,
		LastVerifiedAt: time.Now().UTC(),
	}
	cacheInput.BlobID = blobIDForBytes([]byte("expected-guest-bytes"))
	seedCacheRecording(t, guest, library.LibraryID, guestLocal.DeviceID, cacheInput)
	seedCacheRecording(t, requester, library.LibraryID, guestLocal.DeviceID, cacheInput)

	guestBlobPath, err := guest.cache.blobPath(cacheInput.BlobID)
	if err != nil {
		t.Fatalf("resolve guest blob path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(guestBlobPath), 0o755); err != nil {
		t.Fatalf("mkdir guest blob dir: %v", err)
	}
	if err := os.WriteFile(guestBlobPath, []byte("tampered-guest-bytes"), 0o644); err != nil {
		t.Fatalf("write tampered guest blob: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	guest.SetSyncTransport(registry.transport("memory://guest", guest))
	requester.SetSyncTransport(registry.transport("memory://requester", requester))

	_, err = requester.EnsurePlaybackRecording(ctx, cacheInput.RecordingID, "desktop")
	if err == nil || !strings.Contains(err.Error(), "blob hash mismatch") {
		t.Fatalf("expected guest re-serve blob hash verification error, got %v", err)
	}

	if blobExists(t, requester, cacheInput.BlobID) {
		t.Fatalf("expected requester to reject tampered guest blob %s", cacheInput.BlobID)
	}
	if requesterLocal.LibraryID != library.LibraryID {
		t.Fatalf("requester active library = %q, want %q", requesterLocal.LibraryID, library.LibraryID)
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
