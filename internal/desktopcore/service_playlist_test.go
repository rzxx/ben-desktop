package desktopcore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestPlaylistMutationsRejectMissingRecordingTargets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-contracts")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	playlist, err := app.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "missing-recording",
	}); err == nil {
		t.Fatalf("expected add playlist item to reject missing recording")
	}
	if err := app.LikeRecording(ctx, "missing-recording"); err == nil {
		t.Fatalf("expected like recording to reject missing recording")
	}

	playlists, err := app.ListPlaylists(ctx, apitypes.PlaylistListRequest{})
	if err != nil {
		t.Fatalf("list playlists: %v", err)
	}
	if len(playlists.Items) != 2 {
		t.Fatalf("playlist count = %d, want 2", len(playlists.Items))
	}
	if playlists.Items[0].Kind != apitypes.PlaylistKindLiked {
		t.Fatalf("first playlist kind = %q, want liked", playlists.Items[0].Kind)
	}
	if playlists.Items[1].ItemCount != 0 {
		t.Fatalf("queue item count = %d, want 0", playlists.Items[1].ItemCount)
	}

	liked, err := app.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{})
	if err != nil {
		t.Fatalf("list liked recordings: %v", err)
	}
	if len(liked.Items) != 0 {
		t.Fatalf("liked recordings = %d, want 0", len(liked.Items))
	}

	var likedPlaylists int64
	if err := app.db.WithContext(ctx).
		Model(&Playlist{}).
		Where("library_id = ? AND kind = ? AND deleted_at IS NULL", library.LibraryID, playlistKindLiked).
		Count(&likedPlaylists).Error; err != nil {
		t.Fatalf("count liked playlists: %v", err)
	}
	if likedPlaylists != 1 {
		t.Fatalf("liked playlist count = %d, want 1", likedPlaylists)
	}
}

func TestUnlikeRecordingNoOpKeepsReservedLikedPlaylist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-unlike")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-unlike", "Unlike")

	if err := app.UnlikeRecording(ctx, "rec-unlike"); err != nil {
		t.Fatalf("unlike recording: %v", err)
	}

	liked, err := app.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{})
	if err != nil {
		t.Fatalf("list liked recordings: %v", err)
	}
	if len(liked.Items) != 0 {
		t.Fatalf("liked recordings = %d, want 0", len(liked.Items))
	}

	var likedPlaylists int64
	if err := app.db.WithContext(ctx).
		Model(&Playlist{}).
		Where("library_id = ? AND kind = ? AND deleted_at IS NULL", library.LibraryID, playlistKindLiked).
		Count(&likedPlaylists).Error; err != nil {
		t.Fatalf("count liked playlists: %v", err)
	}
	if likedPlaylists != 1 {
		t.Fatalf("liked playlist count = %d, want 1", likedPlaylists)
	}
}

func TestDeletePlaylistRejectsMissingAndLikedPlaylists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-delete")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-delete", "Delete")

	if err := app.DeletePlaylist(ctx, "missing-playlist"); err == nil {
		t.Fatalf("expected deleting a missing playlist to fail")
	}
	if err := app.LikeRecording(ctx, "rec-delete"); err != nil {
		t.Fatalf("like recording: %v", err)
	}
	if err := app.DeletePlaylist(ctx, likedPlaylistIDForLibrary(library.LibraryID)); err == nil {
		t.Fatalf("expected deleting liked playlist to fail")
	}
}

func TestPlaylistItemMutationsAndLikedLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-order")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-1", "One")
	seedPlaylistRecording(t, app, library.LibraryID, "rec-2", "Two")
	seedPlaylistRecording(t, app, library.LibraryID, "rec-3", "Three")

	playlist, err := app.CreatePlaylist(ctx, "Queue", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	first, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{PlaylistID: playlist.PlaylistID, RecordingID: "rec-1"})
	if err != nil {
		t.Fatalf("add first item: %v", err)
	}
	second, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{PlaylistID: playlist.PlaylistID, RecordingID: "rec-2"})
	if err != nil {
		t.Fatalf("add second item: %v", err)
	}
	third, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:   playlist.PlaylistID,
		RecordingID:  "rec-3",
		BeforeItemID: second.ItemID,
	})
	if err != nil {
		t.Fatalf("add anchored item: %v", err)
	}

	assertPlaylistTrackOrder(t, ctx, app, playlist.PlaylistID, []string{"rec-1", "rec-3", "rec-2"})

	if _, err := app.MovePlaylistItem(ctx, apitypes.PlaylistMoveItemRequest{
		PlaylistID:  playlist.PlaylistID,
		ItemID:      first.ItemID,
		AfterItemID: second.ItemID,
	}); err != nil {
		t.Fatalf("move playlist item: %v", err)
	}
	assertPlaylistTrackOrder(t, ctx, app, playlist.PlaylistID, []string{"rec-3", "rec-2", "rec-1"})

	if err := app.RemovePlaylistItem(ctx, playlist.PlaylistID, third.ItemID); err != nil {
		t.Fatalf("remove playlist item: %v", err)
	}
	assertPlaylistTrackOrder(t, ctx, app, playlist.PlaylistID, []string{"rec-2", "rec-1"})

	likedPlaylistID := likedPlaylistIDForLibrary(library.LibraryID)
	likedItem, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  likedPlaylistID,
		RecordingID: "rec-2",
	})
	if err != nil {
		t.Fatalf("add liked item: %v", err)
	}
	isLiked, err := app.IsRecordingLiked(ctx, "rec-2")
	if err != nil {
		t.Fatalf("is recording liked: %v", err)
	}
	if !isLiked {
		t.Fatalf("expected recording to be liked")
	}

	liked, err := app.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{})
	if err != nil {
		t.Fatalf("list liked recordings: %v", err)
	}
	if len(liked.Items) != 1 || liked.Items[0].RecordingID != "rec-2" {
		t.Fatalf("unexpected liked recordings: %+v", liked.Items)
	}

	if err := app.RemovePlaylistItem(ctx, likedPlaylistID, likedItem.ItemID); err != nil {
		t.Fatalf("remove liked item: %v", err)
	}
	isLiked, err = app.IsRecordingLiked(ctx, "rec-2")
	if err != nil {
		t.Fatalf("is recording liked after remove: %v", err)
	}
	if isLiked {
		t.Fatalf("expected recording to be unliked")
	}
}

func openPlaylistTestApp(t *testing.T) *App {
	t.Helper()

	root := t.TempDir()
	app, err := Open(context.Background(), Config{
		DBPath:          filepath.Join(root, "library.db"),
		BlobRoot:        filepath.Join(root, "blobs"),
		IdentityKeyPath: filepath.Join(root, "identity.key"),
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

func seedPlaylistRecording(t *testing.T, app *App, libraryID, recordingID, title string) {
	t.Helper()

	now := time.Now().UTC()
	if err := app.db.Create(&TrackVariantModel{
		LibraryID:      libraryID,
		TrackVariantID: recordingID,
		TrackClusterID: recordingID,
		KeyNorm:        title,
		Title:          title,
		DurationMS:     180000,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed recording %q: %v", recordingID, err)
	}
}

func assertPlaylistTrackOrder(t *testing.T, ctx context.Context, app *App, playlistID string, want []string) {
	t.Helper()

	page, err := app.ListPlaylistTracks(ctx, apitypes.PlaylistTrackListRequest{
		PlaylistID: playlistID,
		PageRequest: apitypes.PageRequest{
			Limit: 100,
		},
	})
	if err != nil {
		t.Fatalf("list playlist tracks: %v", err)
	}
	if len(page.Items) != len(want) {
		t.Fatalf("playlist track count = %d, want %d", len(page.Items), len(want))
	}
	for i, item := range page.Items {
		if item.RecordingID != want[i] {
			t.Fatalf("playlist item %d = %q, want %q", i, item.RecordingID, want[i])
		}
	}
}
