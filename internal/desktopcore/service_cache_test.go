package desktopcore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apitypes "ben/core/api/types"
)

func TestCacheOverviewAndListingIncludePinsAndArtwork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "cache-overview")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	pinnedBlob := testBlobID("a")
	unpinnedBlob := testBlobID("b")
	artworkBlob := testBlobID("c")

	seedCacheRecording(t, app, local.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pinned",
		AlbumID:        "album-1",
		SourceFileID:   "src-pinned",
		EncodingID:     "enc-pinned",
		BlobID:         pinnedBlob,
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-2 * time.Hour),
	})
	seedCacheRecording(t, app, local.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-unpinned",
		AlbumID:        "album-2",
		SourceFileID:   "src-unpinned",
		EncodingID:     "enc-unpinned",
		BlobID:         unpinnedBlob,
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-time.Hour),
	})
	seedArtworkCache(t, app, local.LibraryID, "album", "album-1", artworkBlob, now.Add(-30*time.Minute))
	seedOfflinePin(t, app, local.LibraryID, local.DeviceID, "recording", "rec-pinned", "desktop")

	writeCacheBlob(t, app, pinnedBlob, 100)
	writeCacheBlob(t, app, unpinnedBlob, 200)
	writeCacheBlob(t, app, artworkBlob, 50)

	overview, err := app.GetCacheOverview(ctx)
	if err != nil {
		t.Fatalf("get cache overview: %v", err)
	}
	if overview.UsedBytes != 350 {
		t.Fatalf("used bytes = %d, want 350", overview.UsedBytes)
	}
	if overview.EntryCount != 3 {
		t.Fatalf("entry count = %d, want 3", overview.EntryCount)
	}
	if overview.PinnedEntries != 2 {
		t.Fatalf("pinned entries = %d, want 2", overview.PinnedEntries)
	}
	if overview.UnpinnedEntries != 1 {
		t.Fatalf("unpinned entries = %d, want 1", overview.UnpinnedEntries)
	}
	if overview.ReclaimableBytes != 200 {
		t.Fatalf("reclaimable bytes = %d, want 200", overview.ReclaimableBytes)
	}

	entries, err := app.ListCacheEntries(ctx, apitypes.CacheEntryListRequest{})
	if err != nil {
		t.Fatalf("list cache entries: %v", err)
	}
	if len(entries.Items) != 3 {
		t.Fatalf("cache items = %d, want 3", len(entries.Items))
	}

	byBlob := make(map[string]apitypes.CacheEntryItem, len(entries.Items))
	for _, item := range entries.Items {
		byBlob[item.BlobID] = item
	}
	if !byBlob[pinnedBlob].Pinned || byBlob[pinnedBlob].RecordingID != "rec-pinned" {
		t.Fatalf("unexpected pinned recording entry: %+v", byBlob[pinnedBlob])
	}
	if byBlob[unpinnedBlob].Pinned {
		t.Fatalf("expected unpinned blob to remain unpinned: %+v", byBlob[unpinnedBlob])
	}
	if !byBlob[artworkBlob].Pinned || byBlob[artworkBlob].Kind != apitypes.CacheKindThumbnail {
		t.Fatalf("unexpected artwork entry: %+v", byBlob[artworkBlob])
	}

	var recordingPinned bool
	for _, scope := range byBlob[pinnedBlob].PinScopes {
		if scope.Scope == "recording" && scope.ScopeID == "rec-pinned" && scope.Durable {
			recordingPinned = true
		}
	}
	if !recordingPinned {
		t.Fatalf("expected durable recording pin scope: %+v", byBlob[pinnedBlob].PinScopes)
	}

	var thumbnailPinned bool
	for _, scope := range byBlob[artworkBlob].PinScopes {
		if scope.Scope == "thumbnail" && scope.ScopeID == "album:album-1" && scope.Durable {
			thumbnailPinned = true
		}
	}
	if !thumbnailPinned {
		t.Fatalf("expected durable thumbnail pin scope: %+v", byBlob[artworkBlob].PinScopes)
	}

	if library.LibraryID != local.LibraryID {
		t.Fatalf("active library mismatch after setup")
	}
}

func TestCleanupCacheRemovesOnlyUnpinnedLocalBlob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "cache-cleanup"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	pinnedBlob := testBlobID("d")
	unpinnedBlob := testBlobID("e")

	seedCacheRecording(t, app, local.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pinned",
		AlbumID:        "album-keep",
		SourceFileID:   "src-pinned",
		EncodingID:     "enc-pinned",
		BlobID:         pinnedBlob,
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-time.Hour),
	})
	seedCacheRecording(t, app, local.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-drop",
		AlbumID:        "album-drop",
		SourceFileID:   "src-drop",
		EncodingID:     "enc-drop",
		BlobID:         unpinnedBlob,
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-2 * time.Hour),
	})
	seedOfflinePin(t, app, local.LibraryID, local.DeviceID, "recording", "rec-pinned", "desktop")

	writeCacheBlob(t, app, pinnedBlob, 120)
	writeCacheBlob(t, app, unpinnedBlob, 180)

	result, err := app.CleanupCache(ctx, apitypes.CacheCleanupRequest{Mode: apitypes.CacheCleanupAllUnpinned})
	if err != nil {
		t.Fatalf("cleanup cache: %v", err)
	}
	if len(result.DeletedBlobs) != 1 || result.DeletedBlobs[0] != unpinnedBlob {
		t.Fatalf("deleted blobs = %v, want [%s]", result.DeletedBlobs, unpinnedBlob)
	}
	if result.DeletedBytes != 180 {
		t.Fatalf("deleted bytes = %d, want 180", result.DeletedBytes)
	}
	if result.RemainingBytes != 120 {
		t.Fatalf("remaining bytes = %d, want 120", result.RemainingBytes)
	}

	if blobExists(t, app, unpinnedBlob) {
		t.Fatalf("expected unpinned blob file to be removed")
	}
	if !blobExists(t, app, pinnedBlob) {
		t.Fatalf("expected pinned blob file to remain")
	}

	var dropped DeviceAssetCacheModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", local.LibraryID, local.DeviceID, "enc-drop").
		Take(&dropped).Error; err != nil {
		t.Fatalf("load dropped cache row: %v", err)
	}
	if dropped.IsCached {
		t.Fatalf("expected dropped cache row to be marked uncached")
	}
}

func TestCleanupCacheKeepsBlobFileWhenAnotherLibraryStillCachesIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	first, err := app.CreateLibrary(ctx, "cache-shared-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	second, err := app.CreateLibrary(ctx, "cache-shared-b")
	if err != nil {
		t.Fatalf("create second library: %v", err)
	}

	sharedBlob := testBlobID("f")
	now := time.Now().UTC()

	seedCacheRecording(t, app, first.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-a",
		AlbumID:        "album-a",
		SourceFileID:   "src-a",
		EncodingID:     "enc-a",
		BlobID:         sharedBlob,
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-2 * time.Hour),
	})
	seedCacheRecording(t, app, second.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-b",
		AlbumID:        "album-b",
		SourceFileID:   "src-b",
		EncodingID:     "enc-b",
		BlobID:         sharedBlob,
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-time.Hour),
	})
	writeCacheBlob(t, app, sharedBlob, 240)

	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("select first library: %v", err)
	}

	result, err := app.CleanupCache(ctx, apitypes.CacheCleanupRequest{Mode: apitypes.CacheCleanupAllUnpinned})
	if err != nil {
		t.Fatalf("cleanup shared cache blob: %v", err)
	}
	if len(result.DeletedBlobs) != 0 {
		t.Fatalf("expected shared blob file to be retained, deleted=%v", result.DeletedBlobs)
	}
	if !blobExists(t, app, sharedBlob) {
		t.Fatalf("expected shared blob file to remain on disk")
	}

	var firstRow DeviceAssetCacheModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", first.LibraryID, local.DeviceID, "enc-a").
		Take(&firstRow).Error; err != nil {
		t.Fatalf("load first cache row: %v", err)
	}
	if firstRow.IsCached {
		t.Fatalf("expected first library cache row to be uncached")
	}

	var secondRow DeviceAssetCacheModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", second.LibraryID, local.DeviceID, "enc-b").
		Take(&secondRow).Error; err != nil {
		t.Fatalf("load second cache row: %v", err)
	}
	if !secondRow.IsCached {
		t.Fatalf("expected second library cache row to stay cached")
	}
}

func openCacheTestApp(t *testing.T, cacheBytes int64) *App {
	t.Helper()

	root := t.TempDir()
	app, err := Open(context.Background(), Config{
		DBPath:     filepath.Join(root, "library.db"),
		BlobRoot:   filepath.Join(root, "blobs"),
		CacheBytes: cacheBytes,
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatalf("close app: %v", err)
		}
	})
	return app
}

type cacheSeedInput struct {
	RecordingID    string
	AlbumID        string
	SourceFileID   string
	EncodingID     string
	BlobID         string
	Profile        string
	LastVerifiedAt time.Time
}

func seedCacheRecording(t *testing.T, app *App, libraryID, deviceID string, in cacheSeedInput) {
	t.Helper()

	now := time.Now().UTC()
	if err := app.db.Create(&TrackVariantModel{
		LibraryID:      libraryID,
		TrackVariantID: in.RecordingID,
		TrackClusterID: in.RecordingID,
		KeyNorm:        strings.ToLower(in.RecordingID),
		Title:          in.RecordingID,
		DurationMS:     180000,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed recording %s: %v", in.RecordingID, err)
	}
	if strings.TrimSpace(in.AlbumID) != "" {
		if err := app.db.Create(&AlbumVariantModel{
			LibraryID:      libraryID,
			AlbumVariantID: in.AlbumID,
			AlbumClusterID: in.AlbumID,
			KeyNorm:        strings.ToLower(in.AlbumID),
			Title:          in.AlbumID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatalf("seed album %s: %v", in.AlbumID, err)
		}
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

	path := filepath.Join(t.TempDir(), in.SourceFileID+".flac")
	if err := app.db.Create(&SourceFileModel{
		LibraryID:         libraryID,
		DeviceID:          deviceID,
		SourceFileID:      in.SourceFileID,
		TrackVariantID:    in.RecordingID,
		LocalPath:         path,
		PathKey:           strings.ToLower(path),
		SourceFingerprint: in.SourceFileID + "-fp",
		HashAlgo:          "b3",
		HashHex:           strings.TrimPrefix(in.BlobID, "b3:"),
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
		t.Fatalf("seed device asset cache %s: %v", in.EncodingID, err)
	}
}

func seedArtworkCache(t *testing.T, app *App, libraryID, scopeType, scopeID, blobID string, updatedAt time.Time) {
	t.Helper()

	if err := app.db.Create(&ArtworkVariant{
		LibraryID:       libraryID,
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		Variant:         defaultArtworkVariant320,
		BlobID:          blobID,
		MIME:            "image/webp",
		FileExt:         ".webp",
		W:               320,
		H:               320,
		Bytes:           50,
		ChosenSource:    "embedded_front",
		ChosenSourceRef: "seed",
		UpdatedAt:       updatedAt,
	}).Error; err != nil {
		t.Fatalf("seed artwork cache %s: %v", blobID, err)
	}
}

func seedOfflinePin(t *testing.T, app *App, libraryID, deviceID, scope, scopeID, profile string) {
	t.Helper()

	now := time.Now().UTC()
	if err := app.db.Create(&OfflinePin{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		Scope:     scope,
		ScopeID:   scopeID,
		Profile:   profile,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed offline pin %s/%s: %v", scope, scopeID, err)
	}
}

func writeCacheBlob(t *testing.T, app *App, blobID string, size int) {
	t.Helper()

	path, err := app.cache.blobPath(blobID)
	if err != nil {
		t.Fatalf("resolve blob path %s: %v", blobID, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir blob dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatalf("write blob %s: %v", blobID, err)
	}
}

func blobExists(t *testing.T, app *App, blobID string) bool {
	t.Helper()

	path, err := app.cache.blobPath(blobID)
	if err != nil {
		t.Fatalf("resolve blob path %s: %v", blobID, err)
	}
	_, err = os.Stat(path)
	return err == nil
}

func testBlobID(hexDigit string) string {
	return "b3:" + strings.Repeat(hexDigit, 64)
}
