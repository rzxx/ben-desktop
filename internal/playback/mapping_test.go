package playback

import (
	"testing"

	apitypes "ben/desktop/api/types"
)

func TestItemFromRecordingCarriesAlbumID(t *testing.T) {
	t.Parallel()

	item := ItemFromRecording(apitypes.RecordingListItem{
		LibraryRecordingID:          "cluster-1",
		PreferredVariantRecordingID: "variant-1",
		RecordingID:                 "cluster-1",
		Title:                       "Track",
		AlbumID:                     "album-1",
	})

	if item.AlbumID != "album-1" {
		t.Fatalf("album id = %q, want album-1", item.AlbumID)
	}
	if item.VariantAlbumID != "album-1" {
		t.Fatalf("variant album id = %q, want album-1", item.VariantAlbumID)
	}
}

func TestItemsFromPlaylistTracksCarryAlbumID(t *testing.T) {
	t.Parallel()

	items := ItemsFromPlaylistTracks("playlist-1", []apitypes.PlaylistTrackItem{{
		ItemID:             "item-1",
		LibraryRecordingID: "cluster-1",
		RecordingID:        "variant-1",
		Title:              "Track",
		AlbumID:            "album-1",
	}})

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].AlbumID != "album-1" {
		t.Fatalf("album id = %q, want album-1", items[0].AlbumID)
	}
}

func TestItemsFromLikedRecordingsCarryAlbumID(t *testing.T) {
	t.Parallel()

	items := ItemsFromLikedRecordings([]apitypes.LikedRecordingItem{{
		LibraryRecordingID: "cluster-1",
		RecordingID:        "variant-1",
		Title:              "Track",
		AlbumID:            "album-1",
	}})

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].AlbumID != "album-1" {
		t.Fatalf("album id = %q, want album-1", items[0].AlbumID)
	}
}
