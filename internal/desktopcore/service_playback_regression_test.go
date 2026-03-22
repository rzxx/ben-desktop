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
