package desktopcore

import (
	"context"
	"path/filepath"
	"strings"
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
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedPinRoot(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	overwriteSourceFileTags(t, app, library.LibraryID, "source-old", newTags)
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("updated rebuild: %v", err)
	}

	newAlbumID := onlyAlbumID(t, app, ctx)
	if newAlbumID != oldAlbumID {
		t.Fatalf("expected library album id to remain stable across metadata-only rebuilds")
	}
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, newAlbumID, 1)
}

func TestDeriveCatalogMaterializationKeepsSameNamedTracksDistinctAcrossAlbumFamilies(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	makeRow := func(trackVariantID string, tags Tags) SourceFileModel {
		tagsJSON, err := tagsSnapshotJSON(tags)
		if err != nil {
			t.Fatalf("marshal tags snapshot: %v", err)
		}
		return SourceFileModel{
			LibraryID:      "library-distinct-album-families",
			TrackVariantID: trackVariantID,
			TagsJSON:       tagsJSON,
			EditionScopeKey: normalizeCatalogKey(strings.Join([]string{
				firstNonEmpty(tags.AlbumArtist, firstArtist(tags.Artists)),
				tags.Album,
				strings.ToLower(trackVariantID),
			}, "|")),
			CreatedAt:  now,
			UpdatedAt:  now,
			LastSeenAt: now,
			IsPresent:  true,
		}
	}

	rows := []SourceFileModel{
		makeRow("track-lucre-2", Tags{
			Title:       "2",
			Album:       "Lucre",
			AlbumArtist: "Dean Blunt",
			Artists:     []string{"Dean Blunt"},
			TrackNo:     2,
			DiscNo:      1,
			Year:        2025,
			DurationMS:  129800,
		}),
		makeRow("track-zushi-2", Tags{
			Title:       "2",
			Album:       "ZUSHI",
			AlbumArtist: "Dean Blunt",
			Artists:     []string{"Dean Blunt"},
			TrackNo:     2,
			DiscNo:      1,
			Year:        2020,
			DurationMS:  159720,
		}),
	}

	materialized, err := deriveCatalogMaterializationRows(rows)
	if err != nil {
		t.Fatalf("derive catalog materialization rows: %v", err)
	}

	lucreTrack := materialized.tracks["track-lucre-2"]
	zushiTrack := materialized.tracks["track-zushi-2"]
	if lucreTrack.TrackClusterID == "" || zushiTrack.TrackClusterID == "" {
		t.Fatalf("expected non-empty track cluster ids, got %+v %+v", lucreTrack, zushiTrack)
	}
	if lucreTrack.TrackClusterID == zushiTrack.TrackClusterID {
		t.Fatalf("expected same-named tracks on different album families to keep distinct clusters, got %q", lucreTrack.TrackClusterID)
	}
}

func TestCatalogMaterializationMigrationRebuildsExistingTrackClusters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	openApp := func() *App {
		t.Helper()
		app, err := Open(ctx, Config{
			DBPath:           filepath.Join(root, "library.db"),
			BlobRoot:         filepath.Join(root, "blobs"),
			IdentityKeyPath:  filepath.Join(root, "identity.key"),
			CacheBytes:       1024,
			TranscodeBuilder: &fakeAACBuilder{result: []byte("test-encoded")},
		})
		if err != nil {
			t.Fatalf("open app: %v", err)
		}
		return app
	}

	app := openApp()
	library, err := app.CreateLibrary(ctx, "catalog-materialization-migration")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	sharedClusterID := stableNameID("track_cluster", normalizeCatalogKey("dean blunt|2"))
	makeSource := func(trackVariantID, sourceFileID string, tags Tags) SourceFileModel {
		tagsJSON, err := tagsSnapshotJSON(tags)
		if err != nil {
			t.Fatalf("marshal tags snapshot: %v", err)
		}
		editionScopeKey := normalizeCatalogKey(strings.Join([]string{
			firstNonEmpty(tags.AlbumArtist, firstArtist(tags.Artists)),
			tags.Album,
			strings.ToLower(trackVariantID),
		}, "|"))
		path := filepath.Join(root, sourceFileID+".flac")
		return SourceFileModel{
			LibraryID:         library.LibraryID,
			DeviceID:          local.DeviceID,
			SourceFileID:      sourceFileID,
			TrackVariantID:    trackVariantID,
			LocalPath:         path,
			PathKey:           path,
			SourceFingerprint: sourceFileID + "-fp",
			HashAlgo:          "b3",
			HashHex:           strings.Repeat("a", 64),
			MTimeNS:           now.UnixNano(),
			SizeBytes:         1024,
			Container:         "flac",
			Codec:             "flac",
			Bitrate:           1411200,
			SampleRate:        44100,
			Channels:          2,
			IsLossless:        true,
			QualityRank:       1411200,
			DurationMS:        tags.DurationMS,
			TagsJSON:          tagsJSON,
			EditionScopeKey:   editionScopeKey,
			LastSeenAt:        now,
			IsPresent:         true,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
	}

	for _, track := range []TrackVariantModel{
		{
			LibraryID:      library.LibraryID,
			TrackVariantID: "track-lucre-2",
			TrackClusterID: sharedClusterID,
			KeyNorm:        "dean blunt|2",
			Title:          "2",
			DurationMS:     129800,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			LibraryID:      library.LibraryID,
			TrackVariantID: "track-zushi-2",
			TrackClusterID: sharedClusterID,
			KeyNorm:        "dean blunt|2",
			Title:          "2",
			DurationMS:     159720,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	} {
		if err := app.db.WithContext(ctx).Create(&track).Error; err != nil {
			t.Fatalf("seed legacy track variant %s: %v", track.TrackVariantID, err)
		}
	}

	for _, row := range []SourceFileModel{
		makeSource("track-lucre-2", "src-lucre-2", Tags{
			Title:       "2",
			Album:       "Lucre",
			AlbumArtist: "Dean Blunt",
			Artists:     []string{"Dean Blunt"},
			TrackNo:     2,
			DiscNo:      1,
			Year:        2025,
			DurationMS:  129800,
		}),
		makeSource("track-zushi-2", "src-zushi-2", Tags{
			Title:       "2",
			Album:       "ZUSHI",
			AlbumArtist: "Dean Blunt",
			Artists:     []string{"Dean Blunt"},
			TrackNo:     2,
			DiscNo:      1,
			Year:        2020,
			DurationMS:  159720,
		}),
	} {
		if err := app.db.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("seed source file %s: %v", row.SourceFileID, err)
		}
	}

	if err := app.db.WithContext(ctx).
		Where("key = ?", localSettingCatalogMaterialEpoch).
		Delete(&LocalSetting{}).Error; err != nil {
		t.Fatalf("clear catalog materialization epoch setting: %v", err)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}

	app = openApp()
	defer func() {
		if err := app.Close(); err != nil {
			t.Fatalf("close reopened app: %v", err)
		}
	}()

	var tracks []TrackVariantModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND track_variant_id IN ?", library.LibraryID, []string{"track-lucre-2", "track-zushi-2"}).
		Order("track_variant_id ASC").
		Find(&tracks).Error; err != nil {
		t.Fatalf("load rebuilt track variants: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("rebuilt track variant count = %d, want 2", len(tracks))
	}
	if strings.TrimSpace(tracks[0].TrackClusterID) == "" || strings.TrimSpace(tracks[1].TrackClusterID) == "" {
		t.Fatalf("expected non-empty rebuilt track clusters, got %+v", tracks)
	}
	if tracks[0].TrackClusterID == tracks[1].TrackClusterID {
		t.Fatalf("migration kept same-named tracks in the same cluster: %+v", tracks)
	}

	var setting LocalSetting
	if err := app.db.WithContext(ctx).Where("key = ?", localSettingCatalogMaterialEpoch).Take(&setting).Error; err != nil {
		t.Fatalf("load catalog materialization epoch setting: %v", err)
	}
	if strings.TrimSpace(setting.Value) != catalogMaterializationEpoch {
		t.Fatalf("catalog materialization epoch = %q, want %q", setting.Value, catalogMaterializationEpoch)
	}
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
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedPinRoot(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	if err := app.db.WithContext(ctx).
		Model(&SourceFileModel{}).
		Where("library_id = ? AND source_file_id = ?", library.LibraryID, "source-old").
		Update("is_present", false).Error; err != nil {
		t.Fatalf("mark source missing: %v", err)
	}
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
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

func TestRebuildCatalogMaterializationDoesNotRetargetAlbumPinAcrossTitleCollision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "rebuild-pin-title-collision")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	pinnedTags := Tags{
		Title:       "Pinned Track",
		Album:       "Collision Album",
		AlbumArtist: "Artist A",
		Artists:     []string{"Artist A"},
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
	collisionTrackOne := Tags{
		Title:       "Collision One",
		Album:       "Collision Album",
		AlbumArtist: "Artist B",
		Artists:     []string{"Artist B"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2024,
		DurationMS:  181000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	collisionTrackTwo := collisionTrackOne
	collisionTrackTwo.Title = "Collision Two"
	collisionTrackTwo.TrackNo = 2
	collisionTrackTwo.DurationMS = 182000

	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-pinned", pinnedTags)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-collision-1", collisionTrackOne)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-collision-2", collisionTrackTwo)
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list initial albums: %v", err)
	}
	if len(albums.Items) != 2 {
		t.Fatalf("initial album count = %d, want 2", len(albums.Items))
	}

	pinnedAlbumID := albumIDByArtist(t, albums.Items, "Artist A")
	collisionAlbumID := albumIDByArtist(t, albums.Items, "Artist B")
	seedPinRoot(t, app, library.LibraryID, local.DeviceID, "album", pinnedAlbumID, "desktop")

	updatedPinnedTags := pinnedTags
	updatedPinnedTags.AlbumArtist = "Artist A Updated"
	updatedPinnedTags.Artists = []string{"Artist A Updated"}
	overwriteSourceFileTags(t, app, library.LibraryID, "source-pinned", updatedPinnedTags)
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("updated rebuild: %v", err)
	}

	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, pinnedAlbumID, 0)
	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, collisionAlbumID, 0)

	var pinCount int64
	if err := app.db.WithContext(ctx).
		Model(&PinRoot{}).
		Where("library_id = ? AND device_id = ? AND scope = ?", library.LibraryID, local.DeviceID, "album").
		Count(&pinCount).Error; err != nil {
		t.Fatalf("count album pin roots: %v", err)
	}
	if pinCount != 0 {
		t.Fatalf("album pin count after ambiguous rebuild = %d, want 0", pinCount)
	}
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
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedPinRoot(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

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
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
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
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	oldAlbumID := onlyAlbumID(t, app, ctx)
	seedPinRoot(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")

	overwriteSourceFileTags(t, app, library.LibraryID, "source-old", localSurvivorTags)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, "remote-device", "source-remote-1", remoteTrackOneTags)
	seedPresentSourceFileWithTags(t, app, library.LibraryID, "remote-device", "source-remote-2", remoteTrackTwoTags)
	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
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

func albumIDByArtist(t *testing.T, albums []apitypes.AlbumListItem, artist string) string {
	t.Helper()

	for _, album := range albums {
		for _, candidate := range album.Artists {
			if candidate == artist {
				return album.AlbumID
			}
		}
	}
	t.Fatalf("album for artist %q not found in %+v", artist, albums)
	return ""
}
