package desktopcore

import (
	"context"
	"fmt"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func openInspectorFromApp(t *testing.T, app *App) *Inspector {
	t.Helper()
	if err := app.Close(); err != nil {
		t.Fatalf("close app before inspector: %v", err)
	}
	inspector, err := OpenInspector(InspectConfig{
		DBPath:           app.cfg.DBPath,
		BlobRoot:         app.cfg.BlobRoot,
		PreferredProfile: "desktop",
	})
	if err != nil {
		t.Fatalf("open inspector: %v", err)
	}
	t.Cleanup(func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("close inspector: %v", err)
		}
	})
	return inspector
}

func TestOpenInspectorIsReadOnlyAndDoesNotCreateCurrentDevice(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)

	var setting LocalSetting
	if err := app.db.WithContext(ctx).Where("key = ?", localSettingCurrentDevice).Take(&setting).Error; err != nil {
		t.Fatalf("load current device setting: %v", err)
	}
	if err := app.db.WithContext(ctx).Where("key = ?", localSettingCurrentDevice).Delete(&LocalSetting{}).Error; err != nil {
		t.Fatalf("delete current device setting: %v", err)
	}
	if err := app.db.WithContext(ctx).Where("device_id = ?", setting.Value).Delete(&Device{}).Error; err != nil {
		t.Fatalf("delete current device row: %v", err)
	}

	var beforeSettings int64
	if err := app.db.WithContext(ctx).Model(&LocalSetting{}).Where("key = ?", localSettingCurrentDevice).Count(&beforeSettings).Error; err != nil {
		t.Fatalf("count settings before: %v", err)
	}
	var beforeDevices int64
	if err := app.db.WithContext(ctx).Model(&Device{}).Where("device_id = ?", setting.Value).Count(&beforeDevices).Error; err != nil {
		t.Fatalf("count devices before: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	if _, err := inspector.ResolveContext(ctx, ResolveInspectContextRequest{}); err == nil {
		t.Fatalf("expected ambiguous resolve-context error")
	}
	if err := inspector.app.db.Exec("INSERT INTO local_settings(key, value, updated_at) VALUES (?, ?, ?)", "inspect-read-only", "x", time.Now().UTC()).Error; err == nil {
		t.Fatalf("expected inspector sqlite handle to reject writes")
	}

	db, err := openSQLite(app.cfg.DBPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() {
		if err := closeSQL(db); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	}()
	var afterSettings int64
	if err := db.WithContext(ctx).Model(&LocalSetting{}).Where("key = ?", localSettingCurrentDevice).Count(&afterSettings).Error; err != nil {
		t.Fatalf("count settings after: %v", err)
	}
	var afterDevices int64
	if err := db.WithContext(ctx).Model(&Device{}).Where("device_id = ?", setting.Value).Count(&afterDevices).Error; err != nil {
		t.Fatalf("count devices after: %v", err)
	}

	if afterSettings != beforeSettings {
		t.Fatalf("current-device setting count changed from %d to %d", beforeSettings, afterSettings)
	}
	if afterDevices != beforeDevices {
		t.Fatalf("current-device row count changed from %d to %d", beforeDevices, afterDevices)
	}
}

func TestResolveContextAmbiguousInferenceReturnsCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "inspect-ambiguous-a"); err != nil {
		t.Fatalf("create first library: %v", err)
	}
	if _, err := app.CreateLibrary(ctx, "inspect-ambiguous-b"); err != nil {
		t.Fatalf("create second library: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	resolution, err := inspector.ResolveContext(ctx, ResolveInspectContextRequest{})
	if err == nil {
		t.Fatalf("expected ambiguous context error")
	}
	if len(resolution.AvailableLibraries) != 2 {
		t.Fatalf("available libraries = %d, want 2", len(resolution.AvailableLibraries))
	}
	if !resolution.Ambiguous {
		t.Fatalf("expected ambiguous resolution")
	}
}

func TestTraceRecordingReportsPreferenceOverrideAndExactShadowing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-recording-preference")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-inspect-pref"
		variantA  = "rec-inspect-pref-a"
		variantB  = "rec-inspect-pref-b"
	)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-inspect-pref-a",
		AlbumClusterID: "album-cluster-a",
		SourceFileID:   "src-inspect-pref-a",
		QualityRank:    220,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-inspect-pref-a", []byte("variant-a"))
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-inspect-pref-b",
		AlbumClusterID: "album-cluster-b",
		SourceFileID:   "src-inspect-pref-b",
		QualityRank:    100,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-inspect-pref-b", []byte("variant-b"))
	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred variant: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	trace, err := inspector.TraceRecording(ctx, TraceRecordingRequest{
		ID: variantA,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace recording: %v", err)
	}
	if !hasAnomaly(trace.Anomalies, anomalyExplicitVariantDiffersFromPreferredResolution) {
		t.Fatalf("expected explicit preference anomaly, got %+v", trace.Anomalies)
	}
	if !hasAnomaly(trace.Anomalies, anomalyRequestedExactShadowedByClusterResolution) {
		t.Fatalf("expected exact-shadowing anomaly, got %+v", trace.Anomalies)
	}
	if !hasAnomaly(trace.Anomalies, anomalyRecordingClusterSpansMultipleAlbumClusters) {
		t.Fatalf("expected multi-album-cluster anomaly, got %+v", trace.Anomalies)
	}
	if got := trace.ComputedOutput["explicit_preferred_variant_id"]; got != variantB {
		t.Fatalf("explicit preferred variant = %v, want %s", got, variantB)
	}
	if got := trace.ComputedOutput["heuristic_preferred_variant_id"]; got != variantA {
		t.Fatalf("heuristic preferred variant = %v, want %s", got, variantA)
	}
}

func TestTraceRecordingExactAvailabilityDoesNotBorrowSiblingLocalState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-exact-availability")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-exact-availability"
		variantA  = "rec-exact-availability-a"
		variantB  = "rec-exact-availability-b"
	)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-exact-a",
		AlbumClusterID: "album-exact",
		SourceFileID:   "src-exact-a",
		QualityRank:    10,
	})
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-exact-b",
		AlbumClusterID: "album-exact",
		SourceFileID:   "src-exact-b",
		QualityRank:    200,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-exact-b", []byte("variant-b"))
	if err := app.db.WithContext(ctx).
		Model(&SourceFileModel{}).
		Where("library_id = ? AND device_id = ? AND source_file_id = ?", library.LibraryID, local.DeviceID, "src-exact-a").
		Update("is_present", false).Error; err != nil {
		t.Fatalf("mark variant-a missing: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	trace, err := inspector.TraceRecording(ctx, TraceRecordingRequest{
		ID: variantA,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace exact recording: %v", err)
	}
	availability, ok := trace.ComputedOutput["resolved_playback_availability"].(map[string]any)
	if !ok {
		t.Fatalf("resolved availability shape = %T", trace.ComputedOutput["resolved_playback_availability"])
	}
	if state := availability["state"]; state == string(apitypes.AvailabilityPlayableLocalFile) {
		t.Fatalf("expected exact variant availability to avoid sibling local state, got %+v", availability)
	}
}

func TestTracePlaybackContextPreservesPlaylistAndLikedIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-context")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-context"
		variantA  = "rec-context-a"
		variantB  = "rec-context-b"
	)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        "album-context-a",
		AlbumClusterID: "album-context",
		SourceFileID:   "src-context-a",
		QualityRank:    10,
	})
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-context-b",
		AlbumClusterID: "album-context",
		SourceFileID:   "src-context-b",
		QualityRank:    20,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-context-b", []byte("variant-b"))
	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred variant: %v", err)
	}
	playlist, err := app.CreatePlaylist(ctx, "inspect-playlist", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	item, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: clusterID,
	})
	if err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	if err := app.LikeRecording(ctx, clusterID); err != nil {
		t.Fatalf("like recording: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	playlistTrace, err := inspector.TracePlaybackContext(ctx, TracePlaybackContextRequest{
		Kind: "playlist",
		ID:   playlist.PlaylistID,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace playlist context: %v", err)
	}
	items, ok := playlistTrace.ComputedOutput["materialized_session_items"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("playlist materialized items = %#v", playlistTrace.ComputedOutput["materialized_session_items"])
	}
	itemMap := items[0]["item"].(map[string]any)
	targetMap := items[0]["target"].(map[string]any)
	if itemMap["source_item_id"] != item.ItemID {
		t.Fatalf("playlist source item id = %v, want %s", itemMap["source_item_id"], item.ItemID)
	}
	if targetMap["logical_recording_id"] != clusterID {
		t.Fatalf("playlist logical recording id = %v, want %s", targetMap["logical_recording_id"], clusterID)
	}

	likedTrace, err := inspector.TracePlaybackContext(ctx, TracePlaybackContextRequest{
		Kind: "liked",
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace liked context: %v", err)
	}
	likedItems, ok := likedTrace.ComputedOutput["materialized_session_items"].([]map[string]any)
	if !ok || len(likedItems) != 1 {
		t.Fatalf("liked materialized items = %#v", likedTrace.ComputedOutput["materialized_session_items"])
	}
	likedTarget := likedItems[0]["target"].(map[string]any)
	if likedTarget["logical_recording_id"] != clusterID {
		t.Fatalf("liked logical recording id = %v, want %s", likedTarget["logical_recording_id"], clusterID)
	}
}

func TestTracePlaybackContextPaginatesLargePlaylistAndLikedSources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-large-context")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	playlist, err := app.CreatePlaylist(ctx, "inspect-large-playlist", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	const itemCount = 520
	for i := 0; i < itemCount; i++ {
		recordingID := fmt.Sprintf("cluster-large-%03d", i)
		seedPlaylistRecording(t, app, library.LibraryID, recordingID, fmt.Sprintf("Track %03d", i))
		if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
			PlaylistID:  playlist.PlaylistID,
			RecordingID: recordingID,
		}); err != nil {
			t.Fatalf("add playlist item %s: %v", recordingID, err)
		}
		if err := app.LikeRecording(ctx, recordingID); err != nil {
			t.Fatalf("like recording %s: %v", recordingID, err)
		}
	}

	likedPlaylistID := likedPlaylistIDForLibrary(local.LibraryID)
	for i := 0; i < itemCount; i++ {
		recordingID := fmt.Sprintf("cluster-large-%03d", i)
		ts := time.Unix(int64(1_000+i), 0).UTC()
		if err := app.db.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND track_variant_id = ?", library.LibraryID, likedPlaylistID, recordingID).
			Updates(map[string]any{
				"added_at":   ts,
				"updated_at": ts,
			}).Error; err != nil {
			t.Fatalf("update liked timestamp %s: %v", recordingID, err)
		}
	}

	inspector := openInspectorFromApp(t, app)
	playlistTrace, err := inspector.TracePlaybackContext(ctx, TracePlaybackContextRequest{
		Kind: "playlist",
		ID:   playlist.PlaylistID,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace playlist context: %v", err)
	}
	playlistItems := materializedTraceItems(t, playlistTrace.ComputedOutput["materialized_session_items"])
	if len(playlistItems) != itemCount {
		t.Fatalf("playlist materialized item count = %d, want %d", len(playlistItems), itemCount)
	}
	if got := traceLogicalRecordingID(t, playlistItems[0]); got != "cluster-large-000" {
		t.Fatalf("first playlist logical recording id = %v, want cluster-large-000", got)
	}
	if got := traceLogicalRecordingID(t, playlistItems[itemCount-1]); got != "cluster-large-519" {
		t.Fatalf("last playlist logical recording id = %v, want cluster-large-519", got)
	}

	likedTrace, err := inspector.TracePlaybackContext(ctx, TracePlaybackContextRequest{
		Kind: "liked",
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace liked context: %v", err)
	}
	likedItems := materializedTraceItems(t, likedTrace.ComputedOutput["materialized_session_items"])
	if len(likedItems) != itemCount {
		t.Fatalf("liked materialized item count = %d, want %d", len(likedItems), itemCount)
	}
	if got := traceLogicalRecordingID(t, likedItems[0]); got != "cluster-large-519" {
		t.Fatalf("first liked logical recording id = %v, want cluster-large-519", got)
	}
	if got := traceLogicalRecordingID(t, likedItems[itemCount-1]); got != "cluster-large-000" {
		t.Fatalf("last liked logical recording id = %v, want cluster-large-000", got)
	}
}

func TestTraceAlbumKeepsSameNamedTracksDistinctAcrossVariants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-album-distinct")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		albumA   = "album-distinct-a"
		albumB   = "album-distinct-b"
		trackA   = "rec-distinct-a"
		trackB   = "rec-distinct-b"
		titleDup = "Shared Intro"
	)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    trackA,
		TrackClusterID: "cluster-distinct-a",
		AlbumID:        albumA,
		AlbumClusterID: "album-cluster-distinct-a",
		SourceFileID:   "src-distinct-a",
		QualityRank:    100,
	})
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    trackB,
		TrackClusterID: "cluster-distinct-b",
		AlbumID:        albumB,
		AlbumClusterID: "album-cluster-distinct-b",
		SourceFileID:   "src-distinct-b",
		QualityRank:    100,
	})
	if err := app.db.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id IN ?", library.LibraryID, []string{trackA, trackB}).
		Update("title", titleDup).Error; err != nil {
		t.Fatalf("update duplicate titles: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	trace, err := inspector.TraceAlbum(ctx, TraceAlbumRequest{
		ID: albumA,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace album: %v", err)
	}

	tracks, ok := trace.ComputedOutput["selected_album_tracks"].([]map[string]any)
	if !ok || len(tracks) != 1 {
		t.Fatalf("selected album tracks = %#v", trace.ComputedOutput["selected_album_tracks"])
	}
	if tracks[0]["track_variant_id"] != trackA {
		t.Fatalf("selected track variant = %v, want %s", tracks[0]["track_variant_id"], trackA)
	}
	if tracks[0]["title"] != titleDup {
		t.Fatalf("selected track title = %v, want %s", tracks[0]["title"], titleDup)
	}
}

func TestTracePlaybackContextAlbumShowsLogicalAndResolvedIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-album-context")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-album-context"
		variantA  = "rec-album-context-a"
		variantB  = "rec-album-context-b"
		albumA    = "album-context-owning"
	)
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantA,
		TrackClusterID: clusterID,
		AlbumID:        albumA,
		AlbumClusterID: "album-cluster-owning",
		SourceFileID:   "src-album-context-a",
		QualityRank:    10,
	})
	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    variantB,
		TrackClusterID: clusterID,
		AlbumID:        "album-context-foreign",
		AlbumClusterID: "album-cluster-foreign",
		SourceFileID:   "src-album-context-b",
		QualityRank:    20,
	})
	writeSeedSourceFile(t, app, library.LibraryID, local.DeviceID, "src-album-context-b", []byte("album-context-b"))
	if err := app.catalog.SetPreferredRecordingVariant(ctx, clusterID, variantB); err != nil {
		t.Fatalf("set preferred variant: %v", err)
	}

	inspector := openInspectorFromApp(t, app)
	trace, err := inspector.TracePlaybackContext(ctx, TracePlaybackContextRequest{
		Kind: "album",
		ID:   albumA,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace album context: %v", err)
	}
	if !hasAnomaly(trace.Anomalies, anomalyAlbumContextEntryResolvesToForeignAlbumVariant) {
		t.Fatalf("expected foreign-album context anomaly, got %+v", trace.Anomalies)
	}
	items, ok := trace.ComputedOutput["materialized_session_items"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("album context items = %#v", trace.ComputedOutput["materialized_session_items"])
	}
	target := items[0]["target"].(map[string]any)
	if target["logical_recording_id"] != clusterID {
		t.Fatalf("logical recording id = %v, want %s", target["logical_recording_id"], clusterID)
	}
	if items[0]["logical_resolution_variant_id"] != variantB {
		t.Fatalf("logical resolution variant id = %v, want %s", items[0]["logical_resolution_variant_id"], variantB)
	}
	if items[0]["resolved_variant_id"] != variantA {
		t.Fatalf("resolved playback variant id = %v, want %s", items[0]["resolved_variant_id"], variantA)
	}
}

func TestTraceRecordingCacheDetectsMultiAlbumBlobAssociation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "inspect-cache")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	const (
		clusterID = "cluster-cache"
		variantA  = "rec-cache-a"
		variantB  = "rec-cache-b"
		blobID    = "b3:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    variantA,
		AlbumID:        "album-cache-a",
		SourceFileID:   "src-cache-a",
		EncodingID:     "enc-cache-a",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err := app.db.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id = ?", library.LibraryID, variantA).
		Update("track_cluster_id", clusterID).Error; err != nil {
		t.Fatalf("update first cluster id: %v", err)
	}
	seedCachedRecordingForExistingAlbum(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    variantB,
		AlbumID:        "album-cache-b",
		SourceFileID:   "src-cache-b",
		EncodingID:     "enc-cache-b",
		BlobID:         blobID,
		Profile:        "desktop",
		LastVerifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err := app.db.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_variant_id = ?", library.LibraryID, variantB).
		Update("track_cluster_id", clusterID).Error; err != nil {
		t.Fatalf("update second cluster id: %v", err)
	}
	writeCacheBlob(t, app, blobID, 64)

	inspector := openInspectorFromApp(t, app)
	trace, err := inspector.TraceRecordingCache(ctx, TraceRecordingCacheRequest{
		ID: clusterID,
		ResolveInspectContextRequest: ResolveInspectContextRequest{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
		},
	})
	if err != nil {
		t.Fatalf("trace recording cache: %v", err)
	}
	if !hasAnomaly(trace.Anomalies, anomalyCacheBlobAssociatedWithMultipleAlbumClusters) {
		t.Fatalf("expected blob multi-album anomaly, got %+v", trace.Anomalies)
	}
}

func materializedTraceItems(t *testing.T, raw any) []map[string]any {
	t.Helper()
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("materialized trace items = %#v", raw)
	}
	return items
}

func traceLogicalRecordingID(t *testing.T, raw map[string]any) string {
	t.Helper()
	target, ok := raw["target"].(map[string]any)
	if !ok {
		t.Fatalf("trace target = %#v", raw["target"])
	}
	logicalID, _ := target["logical_recording_id"].(string)
	return logicalID
}

func hasAnomaly(items []InspectAnomaly, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}
