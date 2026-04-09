package desktopcore

import (
	"context"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestListAlbumAvailabilitySummariesResolvesClusterTracksToVariants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "album-cluster-availability")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-cluster-remote", time.Now().UTC())
	seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
		RecordingID:    "rec-variant-1",
		TrackClusterID: "track-cluster-1",
		AlbumID:        "album-variant-1",
		AlbumClusterID: "album-cluster-1",
		SourceFileID:   "src-cluster-1",
		QualityRank:    90,
	})

	items, err := app.ListAlbumAvailabilitySummaries(ctx, apitypes.AlbumAvailabilitySummaryListRequest{
		AlbumIDs:         []string{"album-cluster-1", "album-variant-1"},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list album availability summaries: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("album availability summary items = %d, want 2", len(items))
	}

	for _, item := range items {
		if item.Availability.State != apitypes.AggregateAvailabilityStateAvailable {
			t.Fatalf("album %s summary state = %q, want %q", item.AlbumID, item.Availability.State, apitypes.AggregateAvailabilityStateAvailable)
		}
		if item.Availability.AvailableNowTrackCount != 1 {
			t.Fatalf("album %s available-now track count = %d, want 1", item.AlbumID, item.Availability.AvailableNowTrackCount)
		}
		if item.Availability.UnavailableTrackCount != 0 {
			t.Fatalf("album %s unavailable track count = %d, want 0", item.AlbumID, item.Availability.UnavailableTrackCount)
		}
	}
}

func TestExactVariantPlaybackAvailabilityDoesNotBorrowLocalSibling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "explicit-variant-availability")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-local-sibling",
		TrackClusterID: "cluster-explicit",
		AlbumID:        "album-local-edition",
		AlbumClusterID: "album-family",
		SourceFileID:   "src-local-sibling",
		QualityRank:    100,
	})

	lastSeen := time.Now().UTC().Add(-3 * availabilityOnlineWindow)
	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "dev-explicit-remote", lastSeen)
	seedSourceOnlyRecording(t, app, library.LibraryID, remoteDeviceID, playbackSeedInput{
		RecordingID:    "rec-remote-explicit",
		TrackClusterID: "cluster-explicit",
		AlbumID:        "album-remote-edition",
		AlbumClusterID: "album-family",
		SourceFileID:   "src-remote-explicit",
		QualityRank:    90,
	})

	item, err := app.GetRecordingAvailability(ctx, "rec-remote-explicit", "desktop")
	if err != nil {
		t.Fatalf("get exact variant playback availability: %v", err)
	}
	if item.State != apitypes.AvailabilityUnavailableProvider {
		t.Fatalf("exact variant state = %q, want %q", item.State, apitypes.AvailabilityUnavailableProvider)
	}
	if item.Reason != apitypes.PlaybackUnavailableProviderOffline {
		t.Fatalf("exact variant reason = %q, want %q", item.Reason, apitypes.PlaybackUnavailableProviderOffline)
	}
	if item.LocalPath != "" {
		t.Fatalf("exact variant unexpectedly resolved local path %q", item.LocalPath)
	}

	items, err := app.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:     []string{"rec-remote-explicit"},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list exact variant playback availability: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("batch exact variant count = %d, want 1", len(items))
	}
	if items[0].State != apitypes.AvailabilityUnavailableProvider {
		t.Fatalf("batch exact variant state = %q, want %q", items[0].State, apitypes.AvailabilityUnavailableProvider)
	}
	if items[0].Reason != apitypes.PlaybackUnavailableProviderOffline {
		t.Fatalf("batch exact variant reason = %q, want %q", items[0].Reason, apitypes.PlaybackUnavailableProviderOffline)
	}
	if items[0].LocalPath != "" {
		t.Fatalf("batch exact variant unexpectedly resolved local path %q", items[0].LocalPath)
	}
}

func TestExactVariantPlaybackAvailabilityDoesNotBorrowCachedSibling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "explicit-variant-cached-availability")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-explicit-cached"
		variantA  = "rec-explicit-cached-a"
		variantB  = "rec-explicit-cached-b"
		blobID    = "b3:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    variantB,
		AlbumID:        "album-explicit-cached-b",
		SourceFileID:   "src-explicit-cached-b",
		EncodingID:     "enc-explicit-cached-b",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Now().UTC(),
	})
	writeCacheBlob(t, app, blobID, 64)

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
		t.Fatalf("seed exact cached sibling variant: %v", err)
	}
	if err := app.db.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id = ?", library.LibraryID, variantB).
		Update("track_cluster_id", clusterID).Error; err != nil {
		t.Fatalf("update cached sibling cluster: %v", err)
	}

	item, err := app.GetRecordingAvailability(ctx, variantA, "desktop")
	if err != nil {
		t.Fatalf("get exact variant cached availability: %v", err)
	}
	if item.State != apitypes.AvailabilityUnavailableNoPath {
		t.Fatalf("exact cached variant state = %q, want %q", item.State, apitypes.AvailabilityUnavailableNoPath)
	}
	if item.SourceKind != "" {
		t.Fatalf("exact cached variant source kind = %q, want empty", item.SourceKind)
	}

	items, err := app.ListRecordingPlaybackAvailability(ctx, apitypes.RecordingPlaybackAvailabilityListRequest{
		RecordingIDs:     []string{variantA},
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("list exact variant cached availability: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("batch exact cached variant count = %d, want 1", len(items))
	}
	if items[0].State != apitypes.AvailabilityUnavailableNoPath {
		t.Fatalf("batch exact cached variant state = %q, want %q", items[0].State, apitypes.AvailabilityUnavailableNoPath)
	}
	if items[0].SourceKind != "" {
		t.Fatalf("batch exact cached variant source kind = %q, want empty", items[0].SourceKind)
	}
}
