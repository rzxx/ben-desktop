package desktopcore

import (
	"context"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestRebuildCatalogMaterializationMigratesAlbumPinToSurvivingVariant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "rebuild-pin-migrate")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	oldTags := Tags{
		Title:       "Track One",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2024,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	newTags := oldTags
	newTags.Year = 2025

	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-old", oldTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedOfflinePin(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	overwriteSourceFileTags(t, app, library.LibraryID, "source-old", newTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("updated rebuild: %v", err)
	}

	newAlbumID := onlyAlbumID(t, app, ctx)
	if newAlbumID != oldAlbumID {
		t.Fatalf("expected library album id to remain stable across metadata-only rebuilds")
	}
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, newAlbumID, 1)
}

func TestRebuildCatalogMaterializationPrunesAlbumPinWhenClusterRemoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "rebuild-pin-prune")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	oldTags := Tags{
		Title:       "Track One",
		Album:       "Transient Album",
		AlbumArtist: "Transient Artist",
		Artists:     []string{"Transient Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2024,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}

	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-old", oldTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedOfflinePin(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	if err := app.db.WithContext(ctx).
		Model(&SourceFileModel{}).
		Where("library_id = ? AND source_file_id = ?", library.LibraryID, "source-old").
		Update("is_present", false).Error; err != nil {
		t.Fatalf("mark source missing: %v", err)
	}
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("rebuild after removal: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list albums after prune: %v", err)
	}
	if len(albums.Items) != 0 {
		t.Fatalf("album count = %d, want 0", len(albums.Items))
	}
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, oldAlbumID, 0)
}

func TestRebuildCatalogMaterializationMigratesAlbumPinToExplicitPreferredVariant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "rebuild-pin-preferred")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	oldTags := Tags{
		Title:       "Shared Track",
		Album:       "Shared Album",
		AlbumArtist: "Shared Artist",
		Artists:     []string{"Shared Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2023,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	altTags := oldTags
	altTags.Year = 2024
	preferredTags := oldTags
	preferredTags.Year = 2025
	preferredTags.QualityRank = 2000000

	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-old", oldTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedOfflinePin(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	_, _, groupKey := normalizedRecordKeys(oldTags)
	clusterID := stableNameID("album_cluster", groupKey)
	_, preferredAlbumKey, _ := normalizedRecordKeys(preferredTags)
	preferredAlbumID := stableNameID("album", preferredAlbumKey)
	if err := app.db.WithContext(ctx).Create(&DeviceVariantPreference{
		LibraryID:       library.LibraryID,
		DeviceID:        local.DeviceID,
		ScopeType:       "album",
		ClusterID:       clusterID,
		ChosenVariantID: preferredAlbumID,
		UpdatedAt:       time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed preferred album variant: %v", err)
	}

	overwriteSourceFileTags(t, app, library.LibraryID, "source-old", altTags)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-preferred", preferredTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("rebuild with preferred survivor: %v", err)
	}

	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, oldAlbumID, 1)
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, preferredAlbumID, 0)
}

func TestRebuildCatalogMaterializationMigratesAlbumPinToLocalSurvivingVariant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "rebuild-pin-local-survivor")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	oldTags := Tags{
		Title:       "Track One",
		Album:       "Shared Album",
		AlbumArtist: "Shared Artist",
		Artists:     []string{"Shared Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2023,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	localSurvivorTags := oldTags
	localSurvivorTags.Year = 2024
	remoteTrackOneTags := oldTags
	remoteTrackOneTags.Year = 2025
	remoteTrackOneTags.Title = "Remote Track One"
	remoteTrackOneTags.TrackNo = 1
	remoteTrackTwoTags := remoteTrackOneTags
	remoteTrackTwoTags.Title = "Remote Track Two"
	remoteTrackTwoTags.TrackNo = 2
	remoteTrackTwoTags.DurationMS = 181000
	remoteAlbumID := stableNameID("album", normalizedRecordAlbumKey(t, remoteTrackOneTags))
	localAlbumID := stableNameID("album", normalizedRecordAlbumKey(t, localSurvivorTags))

	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-old", oldTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedOfflinePin(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	overwriteSourceFileTags(t, app, library.LibraryID, "source-old", localSurvivorTags)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, "remote-device", "source-remote-1", remoteTrackOneTags)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, "remote-device", "source-remote-2", remoteTrackTwoTags)
	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("rebuild with local and remote survivors: %v", err)
	}

	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, oldAlbumID, 1)
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, localAlbumID, 0)
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, remoteAlbumID, 0)
}

func overwriteSourceFileTags(t *testing.T, app *App, libraryID, sourceFileID string, tags Tags) {
	t.Helper()

	tagsJSON, err := tagsSnapshotJSON(tags)
	if err != nil {
		t.Fatalf("marshal tags snapshot: %v", err)
	}
	recordingKey, _, _ := normalizedRecordKeys(tags)
	trackVariantID := stableNameID("recording", recordingKey)
	now := time.Now().UTC()
	updates := map[string]any{
		"track_variant_id": trackVariantID,
		"container":        tags.Container,
		"codec":            tags.Codec,
		"bitrate":          tags.Bitrate,
		"sample_rate":      tags.SampleRate,
		"channels":         tags.Channels,
		"is_lossless":      tags.IsLossless,
		"quality_rank":     tags.QualityRank,
		"duration_ms":      tags.DurationMS,
		"tags_json":        tagsJSON,
		"last_seen_at":     now,
		"updated_at":       now,
		"is_present":       true,
	}
	if err := app.db.WithContext(context.Background()).
		Model(&SourceFileModel{}).
		Where("library_id = ? AND source_file_id = ?", libraryID, sourceFileID).
		Updates(updates).Error; err != nil {
		t.Fatalf("overwrite source tags %s: %v", sourceFileID, err)
	}
}

func normalizedRecordAlbumKey(t *testing.T, tags Tags) string {
	t.Helper()

	_, albumKey, _ := normalizedRecordKeys(tags)
	if albumKey == "" {
		t.Fatal("album key is empty")
	}
	return albumKey
}

func onlyAlbumID(t *testing.T, app *App, ctx context.Context) string {
	t.Helper()

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums.Items) != 1 {
		t.Fatalf("album count = %d, want 1", len(albums.Items))
	}
	return albums.Items[0].AlbumID
}
