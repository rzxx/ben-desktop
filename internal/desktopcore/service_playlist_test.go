package desktopcore

import (
	"context"
	"os"
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
	summary, err := app.GetPlaylistSummary(ctx, playlist.PlaylistID)
	if err != nil {
		t.Fatalf("get playlist summary after item updates: %v", err)
	}
	if summary.ItemCount != 2 {
		t.Fatalf("playlist item count = %d, want 2", summary.ItemCount)
	}

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

func TestPlaylistCoverLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-cover")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	playlist, err := app.CreatePlaylist(ctx, "Queue", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	events := make(chan apitypes.CatalogChangeEvent, 8)
	stop := app.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		events <- event
	})
	defer stop()

	coverPath := writePlaylistCoverImage(t)
	app.artwork.builder = artworkBuilderByPathStub{
		buildBySourceRef: map[string]ArtworkBuildResult{
			filepath.Clean(coverPath): {
				SourceKind: "manual",
				SourceRef:  filepath.Clean(coverPath),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("cover-96"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("cover-320"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("cover-1024"), W: 1024, H: 1024},
				},
			},
		},
	}

	cover, err := app.SetPlaylistCover(ctx, apitypes.PlaylistCoverUploadRequest{
		PlaylistID: playlist.PlaylistID,
		SourcePath: coverPath,
	})
	if err != nil {
		t.Fatalf("set playlist cover: %v", err)
	}
	if !cover.HasCustomCover {
		t.Fatalf("expected uploaded cover to be marked custom")
	}
	if cover.Thumb.Variant != defaultArtworkVariant320 || cover.Thumb.BlobID == "" {
		t.Fatalf("unexpected cover thumb: %+v", cover.Thumb)
	}
	if len(cover.Variants) != 3 {
		t.Fatalf("cover variants = %d, want 3", len(cover.Variants))
	}

	got, found, err := app.GetPlaylistCover(ctx, playlist.PlaylistID)
	if err != nil {
		t.Fatalf("get playlist cover: %v", err)
	}
	if !found || got.Thumb.BlobID != cover.Thumb.BlobID {
		t.Fatalf("got cover = %+v, found=%v, want blob %q", got, found, cover.Thumb.BlobID)
	}

	summary, err := app.GetPlaylistSummary(ctx, playlist.PlaylistID)
	if err != nil {
		t.Fatalf("get playlist summary: %v", err)
	}
	if !summary.HasCustomCover || summary.Thumb.BlobID != cover.Thumb.BlobID {
		t.Fatalf("playlist summary cover = %+v", summary)
	}

	var artworkCount int64
	if err := app.db.WithContext(ctx).
		Model(&ArtworkVariant{}).
		Where("library_id = ? AND scope_type = ? AND scope_id = ?", library.LibraryID, "playlist", playlist.PlaylistID).
		Count(&artworkCount).Error; err != nil {
		t.Fatalf("count playlist artwork: %v", err)
	}
	if artworkCount != 3 {
		t.Fatalf("playlist artwork count = %d, want 3", artworkCount)
	}

	if !waitForPlaylistEvent(events, playlist.PlaylistID) {
		t.Fatalf("expected playlist invalidation event after cover upload")
	}

	if err := app.ClearPlaylistCover(ctx, playlist.PlaylistID); err != nil {
		t.Fatalf("clear playlist cover: %v", err)
	}

	cleared, found, err := app.GetPlaylistCover(ctx, playlist.PlaylistID)
	if err != nil {
		t.Fatalf("get cleared playlist cover: %v", err)
	}
	if found || cleared.HasCustomCover || cleared.Thumb.BlobID != "" {
		t.Fatalf("cleared cover = %+v, found=%v", cleared, found)
	}

	summary, err = app.GetPlaylistSummary(ctx, playlist.PlaylistID)
	if err != nil {
		t.Fatalf("get playlist summary after clear: %v", err)
	}
	if summary.HasCustomCover || summary.Thumb.BlobID != "" {
		t.Fatalf("playlist summary after clear = %+v", summary)
	}

	if err := app.db.WithContext(ctx).
		Model(&ArtworkVariant{}).
		Where("library_id = ? AND scope_type = ? AND scope_id = ?", library.LibraryID, "playlist", playlist.PlaylistID).
		Count(&artworkCount).Error; err != nil {
		t.Fatalf("count cleared playlist artwork: %v", err)
	}
	if artworkCount != 0 {
		t.Fatalf("cleared playlist artwork count = %d, want 0", artworkCount)
	}

	if !waitForPlaylistEvent(events, playlist.PlaylistID) {
		t.Fatalf("expected playlist invalidation event after cover clear")
	}
}

func TestPlaylistCoverRejectsLikedPlaylist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-liked-cover")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-liked-cover", "Liked Cover")
	if err := app.LikeRecording(ctx, "rec-liked-cover"); err != nil {
		t.Fatalf("like recording: %v", err)
	}

	likedPlaylistID := likedPlaylistIDForLibrary(library.LibraryID)
	coverPath := writePlaylistCoverImage(t)
	if _, err := app.SetPlaylistCover(ctx, apitypes.PlaylistCoverUploadRequest{
		PlaylistID: likedPlaylistID,
		SourcePath: coverPath,
	}); err == nil {
		t.Fatalf("expected liked playlist cover upload to fail")
	}
	if err := app.ClearPlaylistCover(ctx, likedPlaylistID); err == nil {
		t.Fatalf("expected liked playlist cover clear to fail")
	}

	cover, found, err := app.GetPlaylistCover(ctx, likedPlaylistID)
	if err != nil {
		t.Fatalf("get liked playlist cover: %v", err)
	}
	if found || cover.HasCustomCover || cover.Thumb.BlobID != "" {
		t.Fatalf("liked playlist cover = %+v, found=%v", cover, found)
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

func waitForPlaylistEvent(events <-chan apitypes.CatalogChangeEvent, playlistID string) bool {
	timeout := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Entity == apitypes.CatalogChangeEntityPlaylists && event.QueryKey == "playlists" && event.EntityID == playlistID {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

func writePlaylistCoverImage(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cover.png")
	data := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
		0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write cover image: %v", err)
	}
	return path
}
