package desktopcore

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestPlaylistMutationsAppendOplogAndAdvanceClock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "playlist-oplog")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-1", "One")
	seedPlaylistRecording(t, app, library.LibraryID, "rec-2", "Two")

	playlist, err := app.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	first, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-1",
	})
	if err != nil {
		t.Fatalf("add first item: %v", err)
	}
	second, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-2",
	})
	if err != nil {
		t.Fatalf("add second item: %v", err)
	}
	if _, err := app.MovePlaylistItem(ctx, apitypes.PlaylistMoveItemRequest{
		PlaylistID:  playlist.PlaylistID,
		ItemID:      first.ItemID,
		AfterItemID: second.ItemID,
	}); err != nil {
		t.Fatalf("move item: %v", err)
	}
	if err := app.RemovePlaylistItem(ctx, playlist.PlaylistID, second.ItemID); err != nil {
		t.Fatalf("remove item: %v", err)
	}
	if _, err := app.RenamePlaylist(ctx, playlist.PlaylistID, "Queue 2"); err != nil {
		t.Fatalf("rename playlist: %v", err)
	}
	if err := app.DeletePlaylist(ctx, playlist.PlaylistID); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}

	entries := loadLibraryDeviceOplogEntries(t, app, library.LibraryID, local.DeviceID)
	if len(entries) != 7 {
		t.Fatalf("oplog entry count = %d, want 7", len(entries))
	}

	wantKinds := []string{"upsert", "upsert", "upsert", "move", "delete", "upsert", "delete"}
	wantEntities := []string{"playlist", "playlist_item", "playlist_item", "playlist_item", "playlist_item", "playlist", "playlist"}
	wantEntityIDs := []string{playlist.PlaylistID, first.ItemID, second.ItemID, first.ItemID, second.ItemID, playlist.PlaylistID, playlist.PlaylistID}
	for i, entry := range entries {
		if entry.LibraryID != library.LibraryID {
			t.Fatalf("entry %d library = %q, want %q", i, entry.LibraryID, library.LibraryID)
		}
		if entry.DeviceID != local.DeviceID {
			t.Fatalf("entry %d device = %q, want %q", i, entry.DeviceID, local.DeviceID)
		}
		if entry.Seq != int64(i+1) {
			t.Fatalf("entry %d seq = %d, want %d", i, entry.Seq, i+1)
		}
		if entry.OpID != local.DeviceID+":"+strconv.Itoa(i+1) {
			t.Fatalf("entry %d op id = %q, want %q", i, entry.OpID, local.DeviceID+":"+strconv.Itoa(i+1))
		}
		if entry.OpKind != wantKinds[i] {
			t.Fatalf("entry %d op kind = %q, want %q", i, entry.OpKind, wantKinds[i])
		}
		if entry.EntityType != wantEntities[i] {
			t.Fatalf("entry %d entity type = %q, want %q", i, entry.EntityType, wantEntities[i])
		}
		if entry.EntityID != wantEntityIDs[i] {
			t.Fatalf("entry %d entity id = %q, want %q", i, entry.EntityID, wantEntityIDs[i])
		}
	}

	clock := loadDeviceClock(t, app, library.LibraryID, local.DeviceID)
	if clock.LastSeqSeen != int64(len(entries)) {
		t.Fatalf("device clock = %d, want %d", clock.LastSeqSeen, len(entries))
	}
}

func TestLikeMutationsAppendOplogOnlyForStateChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "likes-oplog")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	seedPlaylistRecording(t, app, library.LibraryID, "rec-like", "Like")

	if err := app.UnlikeRecording(ctx, "rec-like"); err != nil {
		t.Fatalf("initial unlike: %v", err)
	}
	if err := app.LikeRecording(ctx, "rec-like"); err != nil {
		t.Fatalf("like recording: %v", err)
	}
	if err := app.LikeRecording(ctx, "rec-like"); err != nil {
		t.Fatalf("duplicate like recording: %v", err)
	}
	if err := app.UnlikeRecording(ctx, "rec-like"); err != nil {
		t.Fatalf("unlike recording: %v", err)
	}
	if err := app.UnlikeRecording(ctx, "rec-like"); err != nil {
		t.Fatalf("duplicate unlike recording: %v", err)
	}

	entries := loadLibraryDeviceOplogEntries(t, app, library.LibraryID, local.DeviceID)
	if len(entries) != 2 {
		t.Fatalf("oplog entry count = %d, want 2", len(entries))
	}
	if entries[0].OpKind != "upsert" || entries[1].OpKind != "delete" {
		t.Fatalf("unexpected like/unlike op kinds: %+v", entries)
	}
	for _, entry := range entries {
		if entry.EntityType != "playlist_item" {
			t.Fatalf("entity type = %q, want playlist_item", entry.EntityType)
		}
		if !strings.Contains(entry.PayloadJSON, `"liked":true`) {
			t.Fatalf("expected liked payload marker, payload=%s", entry.PayloadJSON)
		}
	}

	clock := loadDeviceClock(t, app, library.LibraryID, local.DeviceID)
	if clock.LastSeqSeen != 2 {
		t.Fatalf("device clock = %d, want 2", clock.LastSeqSeen)
	}
}

func TestPreferencePinAndCacheMutationsAppendOplogOnlyForStateChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "replicated-state-oplog")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-pref",
		AlbumID:        "album-pref",
		SourceFileID:   "src-pref",
		EncodingID:     "enc-pref",
		BlobID:         testBlobID("1"),
		Profile:        "desktop",
		LastVerifiedAt: now,
	})
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-drop",
		AlbumID:        "album-drop",
		SourceFileID:   "src-drop",
		EncodingID:     "enc-drop",
		BlobID:         testBlobID("2"),
		Profile:        "desktop",
		LastVerifiedAt: now,
	})
	writeCacheBlob(t, app, testBlobID("1"), 128)
	writeCacheBlob(t, app, testBlobID("2"), 96)

	if err := app.SetPreferredRecordingVariant(ctx, "rec-pref", "rec-pref"); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if err := app.SetPreferredRecordingVariant(ctx, "rec-pref", "rec-pref"); err != nil {
		t.Fatalf("repeat preferred recording variant: %v", err)
	}
	if err := app.SetPreferredAlbumVariant(ctx, "album-pref", "album-pref"); err != nil {
		t.Fatalf("set preferred album variant: %v", err)
	}
	if err := app.SetPreferredAlbumVariant(ctx, "album-pref", "album-pref"); err != nil {
		t.Fatalf("repeat preferred album variant: %v", err)
	}
	if _, err := app.PinRecordingOffline(ctx, "rec-pref", "desktop"); err != nil {
		t.Fatalf("pin recording offline: %v", err)
	}
	if _, err := app.PinRecordingOffline(ctx, "rec-pref", "desktop"); err != nil {
		t.Fatalf("repeat pin recording offline: %v", err)
	}
	if _, err := app.CleanupCache(ctx, apitypes.CacheCleanupRequest{Mode: apitypes.CacheCleanupAllUnpinned}); err != nil {
		t.Fatalf("cleanup cache: %v", err)
	}
	if err := app.UnpinRecordingOffline(ctx, "rec-pref"); err != nil {
		t.Fatalf("unpin recording offline: %v", err)
	}
	if err := app.UnpinRecordingOffline(ctx, "rec-pref"); err != nil {
		t.Fatalf("repeat unpin recording offline: %v", err)
	}

	entries := loadLibraryDeviceOplogEntries(t, app, library.LibraryID, local.DeviceID)
	if len(entries) != 5 {
		t.Fatalf("oplog entry count = %d, want 5", len(entries))
	}

	want := []struct {
		entityType string
		opKind     string
		entityID   string
	}{
		{entityTypeDeviceVariantPreference, "upsert", deviceVariantPreferenceEntityID(local.DeviceID, "track", "rec-pref")},
		{entityTypeDeviceVariantPreference, "upsert", deviceVariantPreferenceEntityID(local.DeviceID, "album", "album-pref")},
		{entityTypeOfflinePin, "upsert", offlinePinEntityID(local.DeviceID, "recording", "rec-pref")},
		{entityTypeDeviceAssetCache, "upsert", deviceAssetCacheEntityID(local.DeviceID, "enc-drop")},
		{entityTypeOfflinePin, "delete", offlinePinEntityID(local.DeviceID, "recording", "rec-pref")},
	}
	for i, entry := range entries {
		if entry.Seq != int64(i+1) {
			t.Fatalf("entry %d seq = %d, want %d", i, entry.Seq, i+1)
		}
		if entry.OpKind != want[i].opKind || entry.EntityType != want[i].entityType || entry.EntityID != want[i].entityID {
			t.Fatalf("entry %d = (%s %s %s), want (%s %s %s)", i, entry.EntityType, entry.OpKind, entry.EntityID, want[i].entityType, want[i].opKind, want[i].entityID)
		}
	}

	clock := loadDeviceClock(t, app, library.LibraryID, local.DeviceID)
	if clock.LastSeqSeen != int64(len(entries)) {
		t.Fatalf("device clock = %d, want %d", clock.LastSeqSeen, len(entries))
	}

	var dropped DeviceAssetCacheModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", library.LibraryID, local.DeviceID, "enc-drop").
		Take(&dropped).Error; err != nil {
		t.Fatalf("load dropped cache row: %v", err)
	}
	if dropped.IsCached {
		t.Fatalf("expected enc-drop to be marked uncached")
	}
}

func loadLibraryDeviceOplogEntries(t *testing.T, app *App, libraryID, deviceID string) []OplogEntry {
	t.Helper()

	var entries []OplogEntry
	if err := app.db.WithContext(context.Background()).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Order("seq ASC").
		Find(&entries).Error; err != nil {
		t.Fatalf("load oplog entries: %v", err)
	}
	return entries
}

func loadDeviceClock(t *testing.T, app *App, libraryID, deviceID string) DeviceClock {
	t.Helper()

	var clock DeviceClock
	if err := app.db.WithContext(context.Background()).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Take(&clock).Error; err != nil {
		t.Fatalf("load device clock: %v", err)
	}
	return clock
}
