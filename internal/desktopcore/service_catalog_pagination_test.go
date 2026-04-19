package desktopcore

import (
	"context"
	"reflect"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestCatalogTrackPaginationParity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-track-pagination")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	seedPlaylistRecording(t, app, library.LibraryID, "cluster-b", "Bravo")
	seedPlaylistRecording(t, app, library.LibraryID, "cluster-c", "alpha")
	seedPlaylistRecording(t, app, library.LibraryID, "cluster-a", "Alpha")

	offsetOrder := collectTrackRecordingOrderByOffset(t, ctx, app, 2)
	cursorOrder := collectTrackRecordingOrderByCursor(t, ctx, app, 2)
	want := []string{"cluster-a", "cluster-c", "cluster-b"}

	if !reflect.DeepEqual(offsetOrder, want) {
		t.Fatalf("offset track order = %v, want %v", offsetOrder, want)
	}
	if !reflect.DeepEqual(cursorOrder, want) {
		t.Fatalf("cursor track order = %v, want %v", cursorOrder, want)
	}
}

func TestCatalogPlaylistPaginationParity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-playlist-pagination")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	playlist, err := app.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	seedPlaylistRecording(t, app, library.LibraryID, "rec-2", "Two")
	seedPlaylistRecording(t, app, library.LibraryID, "rec-1", "One")
	seedPlaylistRecording(t, app, library.LibraryID, "rec-3", "Three")
	for _, recordingID := range []string{"rec-2", "rec-1", "rec-3"} {
		if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
			PlaylistID:  playlist.PlaylistID,
			RecordingID: recordingID,
		}); err != nil {
			t.Fatalf("add playlist item %s: %v", recordingID, err)
		}
	}

	offsetOrder := collectPlaylistRecordingOrderByOffset(t, ctx, app, playlist.PlaylistID, 2)
	cursorOrder := collectPlaylistRecordingOrderByCursor(t, ctx, app, playlist.PlaylistID, 2)
	want := []string{"rec-2", "rec-1", "rec-3"}

	if !reflect.DeepEqual(offsetOrder, want) {
		t.Fatalf("offset playlist order = %v, want %v", offsetOrder, want)
	}
	if !reflect.DeepEqual(cursorOrder, want) {
		t.Fatalf("cursor playlist order = %v, want %v", cursorOrder, want)
	}
}

func TestCatalogLikedPaginationParity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-liked-pagination")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	for _, recordingID := range []string{"rec-1", "rec-2", "rec-3"} {
		seedPlaylistRecording(t, app, library.LibraryID, recordingID, "Track "+recordingID)
		if err := app.LikeRecording(ctx, recordingID); err != nil {
			t.Fatalf("like recording %s: %v", recordingID, err)
		}
	}

	likedPlaylistID := likedPlaylistIDForLibrary(library.LibraryID)
	timestamps := map[string]time.Time{
		"rec-1": time.Unix(100, 0).UTC(),
		"rec-2": time.Unix(200, 0).UTC(),
		"rec-3": time.Unix(300, 0).UTC(),
	}
	for recordingID, ts := range timestamps {
		if err := app.db.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND track_variant_id = ?", library.LibraryID, likedPlaylistID, recordingID).
			Updates(map[string]any{
				"added_at":   ts,
				"updated_at": ts,
			}).Error; err != nil {
			t.Fatalf("update liked recording timestamp %s: %v", recordingID, err)
		}
	}

	offsetOrder := collectLikedRecordingOrderByOffset(t, ctx, app, 2)
	cursorOrder := collectLikedRecordingOrderByCursor(t, ctx, app, 2)
	want := []string{"rec-3", "rec-2", "rec-1"}

	if !reflect.DeepEqual(offsetOrder, want) {
		t.Fatalf("offset liked order = %v, want %v", offsetOrder, want)
	}
	if !reflect.DeepEqual(cursorOrder, want) {
		t.Fatalf("cursor liked order = %v, want %v", cursorOrder, want)
	}
}

func TestCatalogPlaylistTrackListingPreservesLikedOrdering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-liked-playlist-order")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	for _, recordingID := range []string{"rec-1", "rec-2", "rec-3"} {
		seedPlaylistRecording(t, app, library.LibraryID, recordingID, "Track "+recordingID)
		if err := app.LikeRecording(ctx, recordingID); err != nil {
			t.Fatalf("like recording %s: %v", recordingID, err)
		}
	}

	likedPlaylistID := likedPlaylistIDForLibrary(library.LibraryID)
	timestamps := map[string]time.Time{
		"rec-1": time.Unix(100, 0).UTC(),
		"rec-2": time.Unix(200, 0).UTC(),
		"rec-3": time.Unix(300, 0).UTC(),
	}
	for recordingID, ts := range timestamps {
		if err := app.db.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND track_variant_id = ?", library.LibraryID, likedPlaylistID, recordingID).
			Updates(map[string]any{
				"added_at":   ts,
				"updated_at": ts,
			}).Error; err != nil {
			t.Fatalf("update liked recording timestamp %s: %v", recordingID, err)
		}
	}

	offsetOrder := collectPlaylistRecordingOrderByOffset(t, ctx, app, likedPlaylistID, 2)
	cursorOrder := collectPlaylistRecordingOrderByCursor(t, ctx, app, likedPlaylistID, 2)
	want := []string{"rec-3", "rec-2", "rec-1"}

	if !reflect.DeepEqual(offsetOrder, want) {
		t.Fatalf("playlist offset liked order = %v, want %v", offsetOrder, want)
	}
	if !reflect.DeepEqual(cursorOrder, want) {
		t.Fatalf("playlist cursor liked order = %v, want %v", cursorOrder, want)
	}
}

func TestCatalogTrackCursorStableWithDuplicateTitles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-track-duplicate-titles")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	for _, recordingID := range []string{"cluster-c", "cluster-a", "cluster-b"} {
		seedPlaylistRecording(t, app, library.LibraryID, recordingID, "Same")
	}

	offsetOrder := collectTrackRecordingOrderByOffset(t, ctx, app, 1)
	cursorOrder := collectTrackRecordingOrderByCursor(t, ctx, app, 1)
	want := []string{"cluster-a", "cluster-b", "cluster-c"}

	if !reflect.DeepEqual(offsetOrder, want) {
		t.Fatalf("offset duplicate-title order = %v, want %v", offsetOrder, want)
	}
	if !reflect.DeepEqual(cursorOrder, want) {
		t.Fatalf("cursor duplicate-title order = %v, want %v", cursorOrder, want)
	}
}

func TestCatalogLikedCursorStableWithDuplicateTimestamps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-liked-duplicate-timestamps")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	for _, recordingID := range []string{"rec-1", "rec-2", "rec-3"} {
		seedPlaylistRecording(t, app, library.LibraryID, recordingID, "Track "+recordingID)
		if err := app.LikeRecording(ctx, recordingID); err != nil {
			t.Fatalf("like recording %s: %v", recordingID, err)
		}
	}

	likedPlaylistID := likedPlaylistIDForLibrary(library.LibraryID)
	timestamps := map[string]time.Time{
		"rec-1": time.Unix(200, 0).UTC(),
		"rec-2": time.Unix(200, 0).UTC(),
		"rec-3": time.Unix(300, 0).UTC(),
	}
	for recordingID, ts := range timestamps {
		if err := app.db.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND track_variant_id = ?", library.LibraryID, likedPlaylistID, recordingID).
			Updates(map[string]any{
				"added_at":   ts,
				"updated_at": ts,
			}).Error; err != nil {
			t.Fatalf("update liked recording timestamp %s: %v", recordingID, err)
		}
	}

	offsetOrder := collectLikedRecordingOrderByOffset(t, ctx, app, 1)
	cursorOrder := collectLikedRecordingOrderByCursor(t, ctx, app, 1)
	if !reflect.DeepEqual(cursorOrder, offsetOrder) {
		t.Fatalf("cursor duplicate-timestamp order = %v, want offset order %v", cursorOrder, offsetOrder)
	}
}

func TestCatalogCursorRejectsMalformedTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-invalid-cursors")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	playlist, err := app.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-1", "Track 1")
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-1",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	if err := app.LikeRecording(ctx, "rec-1"); err != nil {
		t.Fatalf("like recording: %v", err)
	}

	if _, err := app.ListRecordingsCursor(ctx, apitypes.RecordingCursorRequest{
		CursorPageRequest: apitypes.CursorPageRequest{Limit: 1, Cursor: "%%%"},
	}); err == nil {
		t.Fatalf("expected malformed recordings cursor to fail")
	}
	if _, err := app.ListPlaylistTracksCursor(ctx, apitypes.PlaylistTrackCursorRequest{
		PlaylistID:        playlist.PlaylistID,
		CursorPageRequest: apitypes.CursorPageRequest{Limit: 1, Cursor: "%%%"},
	}); err == nil {
		t.Fatalf("expected malformed playlist cursor to fail")
	}
	if _, err := app.ListLikedRecordingsCursor(ctx, apitypes.LikedRecordingCursorRequest{
		CursorPageRequest: apitypes.CursorPageRequest{Limit: 1, Cursor: "%%%"},
	}); err == nil {
		t.Fatalf("expected malformed liked cursor to fail")
	}
}

func collectTrackRecordingOrderByOffset(t *testing.T, ctx context.Context, app *App, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	for offset := 0; ; {
		page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{
			PageRequest: apitypes.PageRequest{
				Limit:  limit,
				Offset: offset,
			},
		})
		if err != nil {
			t.Fatalf("list recordings offset page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.RecordingID)
		}
		if !page.Page.HasMore {
			return order
		}
		offset = page.Page.NextOffset
	}
}

func collectTrackRecordingOrderByCursor(t *testing.T, ctx context.Context, app *App, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	cursor := ""
	for {
		page, err := app.ListRecordingsCursor(ctx, apitypes.RecordingCursorRequest{
			CursorPageRequest: apitypes.CursorPageRequest{
				Limit:  limit,
				Cursor: cursor,
			},
		})
		if err != nil {
			t.Fatalf("list recordings cursor page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.RecordingID)
		}
		if !page.Page.HasMore {
			return order
		}
		cursor = page.Page.NextCursor
	}
}

func collectPlaylistRecordingOrderByOffset(t *testing.T, ctx context.Context, app *App, playlistID string, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	for offset := 0; ; {
		page, err := app.ListPlaylistTracks(ctx, apitypes.PlaylistTrackListRequest{
			PlaylistID: playlistID,
			PageRequest: apitypes.PageRequest{
				Limit:  limit,
				Offset: offset,
			},
		})
		if err != nil {
			t.Fatalf("list playlist tracks offset page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.RecordingID)
		}
		if !page.Page.HasMore {
			return order
		}
		offset = page.Page.NextOffset
	}
}

func collectPlaylistRecordingOrderByCursor(t *testing.T, ctx context.Context, app *App, playlistID string, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	cursor := ""
	for {
		page, err := app.ListPlaylistTracksCursor(ctx, apitypes.PlaylistTrackCursorRequest{
			PlaylistID: playlistID,
			CursorPageRequest: apitypes.CursorPageRequest{
				Limit:  limit,
				Cursor: cursor,
			},
		})
		if err != nil {
			t.Fatalf("list playlist tracks cursor page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.RecordingID)
		}
		if !page.Page.HasMore {
			return order
		}
		cursor = page.Page.NextCursor
	}
}

func collectLikedRecordingOrderByOffset(t *testing.T, ctx context.Context, app *App, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	for offset := 0; ; {
		page, err := app.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{
			PageRequest: apitypes.PageRequest{
				Limit:  limit,
				Offset: offset,
			},
		})
		if err != nil {
			t.Fatalf("list liked recordings offset page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.RecordingID)
		}
		if !page.Page.HasMore {
			return order
		}
		offset = page.Page.NextOffset
	}
}

func collectLikedRecordingOrderByCursor(t *testing.T, ctx context.Context, app *App, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	cursor := ""
	for {
		page, err := app.ListLikedRecordingsCursor(ctx, apitypes.LikedRecordingCursorRequest{
			CursorPageRequest: apitypes.CursorPageRequest{
				Limit:  limit,
				Cursor: cursor,
			},
		})
		if err != nil {
			t.Fatalf("list liked recordings cursor page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.RecordingID)
		}
		if !page.Page.HasMore {
			return order
		}
		cursor = page.Page.NextCursor
	}
}
