package desktopcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestScanRootsRequireActiveLibrary(t *testing.T) {
	t.Parallel()

	app := openCacheTestApp(t, 1024)
	_, err := app.ScanRoots(context.Background())
	if !errors.Is(err, apitypes.ErrNoActiveLibrary) {
		t.Fatalf("scan roots err = %v, want ErrNoActiveLibrary", err)
	}
}

func TestSetAndRemoveScanRootsNormalizeAndPersist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "scan-roots"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	rootBase := t.TempDir()
	rootA := filepath.Join(rootBase, "music-a")
	rootB := filepath.Join(rootBase, "music-b")

	if err := app.SetScanRoots(ctx, []string{
		rootA,
		filepath.Join(rootBase, ".", "music-a"),
		rootB,
		"",
	}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	got, err := app.ScanRoots(ctx)
	if err != nil {
		t.Fatalf("scan roots: %v", err)
	}
	want := []string{
		filepath.Clean(rootA),
		filepath.Clean(rootB),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scan roots = %v, want %v", got, want)
	}

	remaining, err := app.RemoveScanRoots(ctx, []string{filepath.Join(rootBase, ".", "music-b")})
	if err != nil {
		t.Fatalf("remove scan roots: %v", err)
	}
	if !reflect.DeepEqual(remaining, []string{filepath.Clean(rootA)}) {
		t.Fatalf("remaining roots = %v, want [%s]", remaining, filepath.Clean(rootA))
	}
}

func TestAddScanRootsReturnsMergedNormalizedRoots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "scan-roots-add"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	rootBase := t.TempDir()
	rootA := filepath.Join(rootBase, "music-a")
	rootB := filepath.Join(rootBase, "music-b")
	if err := app.SetScanRoots(ctx, []string{rootA}); err != nil {
		t.Fatalf("seed scan roots: %v", err)
	}

	got, err := app.AddScanRoots(ctx, []string{filepath.Join(rootBase, ".", "music-a"), rootB})
	if err != nil {
		t.Fatalf("add scan roots: %v", err)
	}
	want := []string{
		filepath.Clean(rootA),
		filepath.Clean(rootB),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("added roots = %v, want %v", got, want)
	}
}

func TestScanRootUpdatesRejectGuestRole(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "scan-roots-guest")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.db.WithContext(ctx).
		Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", library.LibraryID, local.DeviceID).
		Update("role", roleGuest).Error; err != nil {
		t.Fatalf("set guest role: %v", err)
	}

	root := filepath.Join(t.TempDir(), "guest-root")
	if err := app.SetScanRoots(ctx, []string{root}); err == nil || err.Error() != "scan root updates require owner, admin, or member role" {
		t.Fatalf("set scan roots err = %v", err)
	}
	if _, err := app.AddScanRoots(ctx, []string{root}); err == nil || err.Error() != "scan root updates require owner, admin, or member role" {
		t.Fatalf("add scan roots err = %v", err)
	}
	if _, err := app.RemoveScanRoots(ctx, []string{root}); err == nil || err.Error() != "scan root updates require owner, admin, or member role" {
		t.Fatalf("remove scan roots err = %v", err)
	}
}

func TestRescanNowImportsMetadataAndPublishesCompletedJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "track.flac")
	if err := os.WriteFile(audioPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Track One",
				Album:       "Album One",
				AlbumArtist: "Artist One",
				Artists:     []string{"Artist One"},
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
			},
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	library, err := app.CreateLibrary(ctx, "scan-import")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	stats, err := app.RescanNow(ctx)
	if err != nil {
		t.Fatalf("rescan now: %v", err)
	}
	if stats.Scanned != 1 || stats.Imported != 1 || stats.Errors != 0 {
		t.Fatalf("unexpected scan stats: %+v", stats)
	}

	recordings, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list recordings: %v", err)
	}
	if len(recordings.Items) != 1 || recordings.Items[0].Title != "Track One" {
		t.Fatalf("unexpected recordings page: %+v", recordings)
	}

	activity, err := app.ActivityStatus(ctx)
	if err != nil {
		t.Fatalf("activity status: %v", err)
	}
	if activity.Scan.Phase != "completed" || activity.Scan.RootsDone != 1 || activity.Scan.TracksDone != 1 {
		t.Fatalf("unexpected scan activity: %+v", activity.Scan)
	}

	jobID := scanJobID(library.LibraryID, local.DeviceID, []string{filepath.Clean(root)}, jobKindRescanAll)
	job, ok, err := app.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get scan job: %v", err)
	}
	if !ok {
		t.Fatalf("expected scan job %q", jobID)
	}
	if job.Phase != JobPhaseCompleted || job.Kind != jobKindRescanAll {
		t.Fatalf("unexpected scan job: %+v", job)
	}
}

func TestStartRescanNowQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "async-track.flac")
	if err := os.WriteFile(audioPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Async Track",
				Album:       "Async Album",
				AlbumArtist: "Async Artist",
				Artists:     []string{"Async Artist"},
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
			},
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	library, err := app.CreateLibrary(ctx, "scan-async")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	job, err := app.StartRescanNow(ctx)
	if err != nil {
		t.Fatalf("start rescan now: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindRescanAll {
		t.Fatalf("unexpected queued job: %+v", job)
	}

	jobID := scanJobID(library.LibraryID, local.DeviceID, []string{filepath.Clean(root)}, jobKindRescanAll)
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseCompleted)
	if final.Kind != jobKindRescanAll || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final job snapshot: %+v", final)
	}

	recordings, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list recordings: %v", err)
	}
	if len(recordings.Items) != 1 || recordings.Items[0].Title != "Async Track" {
		t.Fatalf("unexpected recordings page: %+v", recordings)
	}
}

func TestStartRescanNowCancelsWhenActiveLibraryChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	audioA1 := filepath.Join(rootA, "async-one.flac")
	audioA2 := filepath.Join(rootA, "async-two.flac")
	if err := os.WriteFile(audioA1, []byte("fake-audio-1"), 0o644); err != nil {
		t.Fatalf("write first audio file: %v", err)
	}
	if err := os.WriteFile(audioA2, []byte("fake-audio-2"), 0o644); err != nil {
		t.Fatalf("write second audio file: %v", err)
	}

	release := make(chan struct{})
	started := make(chan string, 2)
	reader := blockingTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioA1): {
				Title:       "Cancelable Track One",
				Album:       "Cancelable Album",
				AlbumArtist: "Cancelable Artist",
				Artists:     []string{"Cancelable Artist"},
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
			},
			filepath.Clean(audioA2): {
				Title:       "Cancelable Track Two",
				Album:       "Cancelable Album",
				AlbumArtist: "Cancelable Artist",
				Artists:     []string{"Cancelable Artist"},
				TrackNo:     2,
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
			},
		},
		started: started,
		release: release,
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)

	first, err := app.CreateLibrary(ctx, "scan-cancel-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootA}); err != nil {
		t.Fatalf("set first scan roots: %v", err)
	}
	second, err := app.CreateLibrary(ctx, "scan-cancel-b")
	if err != nil {
		t.Fatalf("create second library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootB}); err != nil {
		t.Fatalf("set second scan roots: %v", err)
	}
	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("reselect first library: %v", err)
	}

	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	job, err := app.StartRescanNow(ctx)
	if err != nil {
		t.Fatalf("start rescan now: %v", err)
	}
	if job.Phase != JobPhaseQueued {
		t.Fatalf("unexpected queued scan job: %+v", job)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scan ingest to start")
	}

	if _, err := app.SelectLibrary(ctx, second.LibraryID); err != nil {
		t.Fatalf("switch active library: %v", err)
	}
	close(release)

	jobID := scanJobID(first.LibraryID, local.DeviceID, []string{filepath.Clean(rootA)}, jobKindRescanAll)
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseFailed)
	if !strings.Contains(final.Message, "no longer active") {
		t.Fatalf("expected cancellation message, got %+v", final)
	}

	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("reselect first library after cancellation: %v", err)
	}
	recordings, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list partially scanned recordings: %v", err)
	}
	if len(recordings.Items) >= 2 {
		t.Fatalf("expected library switch to stop the old scan before both tracks imported, got %+v", recordings.Items)
	}
}

func TestRescanRootMarksMissingFilesAbsent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "track.flac")
	if err := os.WriteFile(audioPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Track One",
				Album:       "Album One",
				AlbumArtist: "Artist One",
				Artists:     []string{"Artist One"},
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
			},
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "scan-missing"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if _, err := app.RescanRoot(ctx, root); err != nil {
		t.Fatalf("initial rescan root: %v", err)
	}

	if err := os.Remove(audioPath); err != nil {
		t.Fatalf("remove audio file: %v", err)
	}
	stats, err := app.RescanRoot(ctx, root)
	if err != nil {
		t.Fatalf("rescan missing root: %v", err)
	}
	if stats.Scanned != 0 {
		t.Fatalf("missing-root scan should not rescan files: %+v", stats)
	}

	var row SourceFileModel
	if err := app.db.WithContext(ctx).Where("library_id <> ''").Take(&row).Error; err != nil {
		t.Fatalf("load source file: %v", err)
	}
	if row.IsPresent {
		t.Fatalf("expected removed source file to be marked absent: %+v", row)
	}
}

func TestRescanNowReplacesRemovedAlbumVariantMaterialization(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	oldA := filepath.Join(root, "01-old.flac")
	oldB := filepath.Join(root, "02-old.flac")
	for _, path := range []string{oldA, oldB} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write audio file %s: %v", path, err)
		}
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(oldA): {
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
			},
			filepath.Clean(oldB): {
				Title:       "Track Two",
				Album:       "Mutable Album",
				AlbumArtist: "Mutable Artist",
				Artists:     []string{"Mutable Artist"},
				TrackNo:     2,
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
			},
		},
	}
	buildResult := func(label, sourcePath string) ArtworkBuildResult {
		return ArtworkBuildResult{
			SourceKind: "embedded",
			SourceRef:  filepath.Clean(sourcePath),
			Variants: []GeneratedArtworkVariant{
				{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte(label + "-96"), W: 96, H: 96},
				{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte(label + "-320"), W: 320, H: 320},
				{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte(label + "-1024"), W: 1024, H: 1024},
			},
		}
	}
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(oldA): buildResult("old", oldA),
		},
	}

	app := openArtworkIngestTestApp(t, reader, builder)
	library, err := app.CreateLibrary(ctx, "mutable-album")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("initial rescan: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list initial albums: %v", err)
	}
	if len(albums.Items) != 1 {
		t.Fatalf("initial album count = %d, want 1", len(albums.Items))
	}
	oldAlbumID := albums.Items[0].AlbumID
	oldVariantAlbumID := albums.Items[0].PreferredVariantAlbumID
	if err := app.SetPreferredAlbumVariant(ctx, oldAlbumID, oldVariantAlbumID); err != nil {
		t.Fatalf("set preferred album variant: %v", err)
	}
	seedPinRoot(t, app, library.LibraryID, local.DeviceID, "album", oldAlbumID, "desktop")
	if got := len(loadAlbumArtworkRows(t, app, oldAlbumID)); got != 3 {
		t.Fatalf("old artwork row count = %d, want 3", got)
	}

	for _, path := range []string{oldA, oldB} {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove old audio file %s: %v", path, err)
		}
		delete(reader.tagsByPath, filepath.Clean(path))
	}

	newA := filepath.Join(root, "01-new.flac")
	newB := filepath.Join(root, "02-new.flac")
	newC := filepath.Join(root, "03-new.flac")
	for _, path := range []string{newA, newB, newC} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write new audio file %s: %v", path, err)
		}
	}
	reader.tagsByPath[filepath.Clean(newA)] = Tags{
		Title:       "Track One",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	reader.tagsByPath[filepath.Clean(newB)] = Tags{
		Title:       "Track Two",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     2,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  181000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	reader.tagsByPath[filepath.Clean(newC)] = Tags{
		Title:       "Track Three",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     3,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  182000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	builder.results[filepath.Clean(newA)] = buildResult("new", newA)

	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("updated rescan: %v", err)
	}

	updatedAlbums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list updated albums: %v", err)
	}
	if len(updatedAlbums.Items) != 1 {
		t.Fatalf("updated album count = %d, want 1", len(updatedAlbums.Items))
	}
	newAlbum := updatedAlbums.Items[0]
	if newAlbum.AlbumID != oldAlbumID {
		t.Fatalf("expected library album id to remain stable after year bump")
	}
	if newAlbum.TrackCount != 3 {
		t.Fatalf("updated track count = %d, want 3", newAlbum.TrackCount)
	}
	if newAlbum.VariantCount != 1 || newAlbum.HasVariants {
		t.Fatalf("unexpected updated album variants: %+v", newAlbum)
	}

	variants, err := app.ListAlbumVariants(ctx, apitypes.AlbumVariantListRequest{
		AlbumID:     newAlbum.AlbumID,
		PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		t.Fatalf("list updated album variants: %v", err)
	}
	if len(variants.Items) != 1 {
		t.Fatalf("updated variant count = %d, want 1", len(variants.Items))
	}
	if variants.Items[0].IsExplicitPreferred {
		t.Fatalf("expected stale explicit preference to be pruned")
	}

	recordings, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list updated recordings: %v", err)
	}
	if len(recordings.Items) != 3 {
		t.Fatalf("updated recording count = %d, want 3", len(recordings.Items))
	}

	var oldArtworkCount int64
	if err := app.db.WithContext(ctx).
		Model(&ArtworkVariant{}).
		Where("library_id = ? AND scope_type = ? AND scope_id = ?", library.LibraryID, "album", oldVariantAlbumID).
		Count(&oldArtworkCount).Error; err != nil {
		t.Fatalf("count old artwork rows after rebuild: %v", err)
	}
	if oldArtworkCount != 0 {
		t.Fatalf("old artwork row count after rebuild = %d, want 0", oldArtworkCount)
	}
	if got := len(loadAlbumArtworkRows(t, app, newAlbum.AlbumID)); got != 3 {
		t.Fatalf("new artwork row count = %d, want 3", got)
	}
	if got := loadLocalArtworkSourceRef(t, app, newAlbum.AlbumID, defaultArtworkVariant320); got != filepath.Clean(newA) {
		t.Fatalf("new artwork source ref = %q, want %q", got, filepath.Clean(newA))
	}

	var prefCount int64
	if err := app.db.WithContext(ctx).
		Model(&DeviceVariantPreference{}).
		Where("library_id = ? AND chosen_variant_id = ?", library.LibraryID, oldVariantAlbumID).
		Count(&prefCount).Error; err != nil {
		t.Fatalf("count stale variant preferences: %v", err)
	}
	if prefCount != 0 {
		t.Fatalf("stale variant preference count = %d, want 0", prefCount)
	}

	assertAlbumPinCount(t, app, library.LibraryID, local.DeviceID, oldAlbumID, 1)
}

func TestRescanNowKeepsCatalogWhenArtworkRebuildFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	oldPath := filepath.Join(root, "01-old.flac")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old audio: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(oldPath): {
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
			},
		},
	}
	buildResult := func(label, sourcePath string) ArtworkBuildResult {
		return ArtworkBuildResult{
			SourceKind: "embedded",
			SourceRef:  filepath.Clean(sourcePath),
			Variants: []GeneratedArtworkVariant{
				{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte(label + "-96"), W: 96, H: 96},
				{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte(label + "-320"), W: 320, H: 320},
				{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte(label + "-1024"), W: 1024, H: 1024},
			},
		}
	}
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(oldPath): buildResult("old", oldPath),
		},
		errs: map[string]error{},
	}

	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "artwork-failure"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("initial rescan: %v", err)
	}

	initialAlbums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list initial albums: %v", err)
	}
	if len(initialAlbums.Items) != 1 {
		t.Fatalf("initial album count = %d, want 1", len(initialAlbums.Items))
	}
	oldAlbumID := initialAlbums.Items[0].AlbumID

	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("remove old audio: %v", err)
	}
	delete(reader.tagsByPath, filepath.Clean(oldPath))

	newPath := filepath.Join(root, "01-new.flac")
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new audio: %v", err)
	}
	reader.tagsByPath[filepath.Clean(newPath)] = Tags{
		Title:       "Track One",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	builder.errs[filepath.Clean(newPath)] = errors.New("artwork rebuild failed")
	builder.results[filepath.Clean(newPath)] = buildResult("new", newPath)

	stats, err := app.RescanNow(ctx)
	if err != nil {
		t.Fatalf("rescan with artwork failure: %v", err)
	}
	if stats.Imported != 1 {
		t.Fatalf("imported count = %d, want 1", stats.Imported)
	}

	updatedAlbums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list updated albums: %v", err)
	}
	if len(updatedAlbums.Items) != 1 {
		t.Fatalf("updated album count = %d, want 1", len(updatedAlbums.Items))
	}
	newAlbumID := updatedAlbums.Items[0].AlbumID
	if newAlbumID != oldAlbumID {
		t.Fatalf("expected library album id to remain stable after metadata-only update")
	}
	if updatedAlbums.Items[0].TrackCount != 1 {
		t.Fatalf("updated track count = %d, want 1", updatedAlbums.Items[0].TrackCount)
	}
	if got := len(loadAlbumArtworkRows(t, app, oldAlbumID)); got != 0 {
		t.Fatalf("old artwork row count = %d, want 0", got)
	}
	if got := len(loadAlbumArtworkRows(t, app, newAlbumID)); got != 0 {
		t.Fatalf("new artwork row count = %d, want 0 when rebuild fails", got)
	}
	if got := app.ActivityStatusSnapshot().Artwork.Phase; got != "failed" {
		t.Fatalf("artwork activity phase = %q, want failed", got)
	}
}

func TestRebuildCatalogMaterializationPreservesPresentCrossOwnerVariants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "present-variants")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	seedPresentSourceFileWithTags(t, app, library.LibraryID, local.DeviceID, "source-2024", Tags{
		Title:       "Shared Track",
		Album:       "Shared Album",
		AlbumArtist: "Shared Artist",
		Artists:     []string{"Shared Artist"},
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
	})
	seedPresentSourceFileWithTags(t, app, library.LibraryID, "remote-device", "source-2025", Tags{
		Title:       "Shared Track",
		Album:       "Shared Album",
		AlbumArtist: "Shared Artist",
		Artists:     []string{"Shared Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	})

	if err := app.rebuildCatalogMaterialization(ctx, library.LibraryID, nil); err != nil {
		t.Fatalf("rebuild catalog materialization: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums.Items) != 1 {
		t.Fatalf("album count = %d, want 1", len(albums.Items))
	}
	if albums.Items[0].VariantCount != 2 || !albums.Items[0].HasVariants {
		t.Fatalf("expected present cross-owner variants to survive rebuild: %+v", albums.Items[0])
	}

	variants, err := app.ListAlbumVariants(ctx, apitypes.AlbumVariantListRequest{
		AlbumID:     albums.Items[0].AlbumID,
		PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		t.Fatalf("list album variants: %v", err)
	}
	if len(variants.Items) != 2 {
		t.Fatalf("variant count = %d, want 2", len(variants.Items))
	}
}

func TestScanWatcherImportsNewFileForActiveLibrary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "watch-track.flac")

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Watched Track",
				Album:       "Watcher Album",
				AlbumArtist: "Watcher Artist",
				Artists:     []string{"Watcher Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2025,
				DurationMS:  210000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "watch-import"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	if err := os.WriteFile(audioPath, []byte("watch-audio"), 0o644); err != nil {
		t.Fatalf("write watched audio file: %v", err)
	}

	recordings := waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Watched Track" {
		t.Fatalf("watched recording title = %q, want Watched Track", recordings[0].Title)
	}
}

func TestScanWatcherFollowsActiveLibrarySelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	audioA := filepath.Join(rootA, "inactive.flac")
	audioB := filepath.Join(rootB, "active.flac")
	audioA2 := filepath.Join(rootA, "reactivated.flac")

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioA): {
				Title:       "Inactive Track",
				Album:       "Library A",
				AlbumArtist: "Artist A",
				Artists:     []string{"Artist A"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2025,
				DurationMS:  180000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
			filepath.Clean(audioB): {
				Title:       "Active Track",
				Album:       "Library B",
				AlbumArtist: "Artist B",
				Artists:     []string{"Artist B"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2025,
				DurationMS:  180000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
			filepath.Clean(audioA2): {
				Title:       "Reactivated Track",
				Album:       "Library A",
				AlbumArtist: "Artist A",
				Artists:     []string{"Artist A"},
				TrackNo:     2,
				DiscNo:      1,
				Year:        2025,
				DurationMS:  181000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)

	first, err := app.CreateLibrary(ctx, "watch-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootA}); err != nil {
		t.Fatalf("set first scan roots: %v", err)
	}

	second, err := app.CreateLibrary(ctx, "watch-b")
	if err != nil {
		t.Fatalf("create second library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootB}); err != nil {
		t.Fatalf("set second scan roots: %v", err)
	}

	if err := os.WriteFile(audioA, []byte("inactive"), 0o644); err != nil {
		t.Fatalf("write inactive audio: %v", err)
	}
	if err := os.WriteFile(audioB, []byte("active"), 0o644); err != nil {
		t.Fatalf("write active audio: %v", err)
	}

	recordings := waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Active Track" {
		t.Fatalf("active library watched title = %q, want Active Track", recordings[0].Title)
	}

	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("select first library: %v", err)
	}
	page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list first library recordings: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("inactive library should not have auto-imported while deselected: %+v", page.Items)
	}

	if err := os.WriteFile(audioA2, []byte("reactivated"), 0o644); err != nil {
		t.Fatalf("write reactivated audio: %v", err)
	}
	recordings = waitForRecordingCount(t, ctx, app, 2)
	titles := map[string]bool{}
	for _, item := range recordings {
		titles[item.Title] = true
	}
	if !titles["Inactive Track"] || !titles["Reactivated Track"] {
		t.Fatalf("reactivated watcher recordings = %+v, want Inactive Track and Reactivated Track", recordings)
	}

	if _, err := app.SelectLibrary(ctx, second.LibraryID); err != nil {
		t.Fatalf("reselect second library: %v", err)
	}
	page, err = app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list second library recordings: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Active Track" {
		t.Fatalf("second library recordings = %+v, want only Active Track", page.Items)
	}
}

func waitForRecordingCount(t *testing.T, ctx context.Context, app *App, want int) []apitypes.RecordingListItem {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
		if err == nil && len(page.Items) == want {
			return page.Items
		}
		time.Sleep(50 * time.Millisecond)
	}

	page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list recordings after wait: %v", err)
	}
	t.Fatalf("recordings count = %d, want %d", len(page.Items), want)
	return nil
}

func waitForJobPhase(t *testing.T, ctx context.Context, app *App, jobID string, want JobPhase) JobSnapshot {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok, err := app.GetJob(ctx, jobID)
		if err == nil && ok && job.Phase == want {
			return job
		}
		time.Sleep(25 * time.Millisecond)
	}

	job, ok, err := app.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job after wait: %v", err)
	}
	if !ok {
		t.Fatalf("job %q not found after wait", jobID)
	}
	t.Fatalf("job %q phase = %q, want %q", jobID, job.Phase, want)
	return JobSnapshot{}
}

type staticTagReader struct {
	tagsByPath map[string]Tags
}

func (r staticTagReader) Read(path string) (Tags, error) {
	path = filepath.Clean(path)
	tags, ok := r.tagsByPath[path]
	if !ok {
		return Tags{}, errors.New("missing test tags for " + path)
	}
	return tags, nil
}

func seedPresentSourceFileWithTags(t *testing.T, app *App, libraryID, deviceID, sourceFileID string, tags Tags) {
	t.Helper()

	now := time.Now().UTC()
	tagsJSON, err := tagsSnapshotJSON(tags)
	if err != nil {
		t.Fatalf("marshal tags snapshot: %v", err)
	}
	path := filepath.Join(t.TempDir(), sourceFileID+".flac")
	recordingKey, _, _ := normalizedRecordKeys(tags)
	trackVariantID := stableNameID("recording", recordingKey)
	hashHex := strings.Repeat("a", 64)
	if len(sourceFileID) <= len(hashHex) {
		hashHex = hashHex[:len(hashHex)-len(sourceFileID)] + sourceFileID
	}
	if err := app.db.WithContext(context.Background()).Create(&SourceFileModel{
		LibraryID:         libraryID,
		DeviceID:          deviceID,
		SourceFileID:      sourceFileID,
		TrackVariantID:    trackVariantID,
		LocalPath:         path,
		PathKey:           localPathKey(path),
		SourceFingerprint: "sha256:" + sourceFileID,
		HashAlgo:          "sha256",
		HashHex:           hashHex,
		MTimeNS:           now.UnixNano(),
		SizeBytes:         1024,
		Container:         tags.Container,
		Codec:             tags.Codec,
		Bitrate:           tags.Bitrate,
		SampleRate:        tags.SampleRate,
		Channels:          tags.Channels,
		IsLossless:        tags.IsLossless,
		QualityRank:       tags.QualityRank,
		DurationMS:        tags.DurationMS,
		TagsJSON:          tagsJSON,
		LastSeenAt:        now,
		IsPresent:         true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed source file %s: %v", sourceFileID, err)
	}
}

func assertAlbumPinCount(t *testing.T, app *App, libraryID, deviceID, albumID string, want int64) {
	t.Helper()

	var count int64
	if err := app.db.WithContext(context.Background()).
		Model(&PinRoot{}).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", libraryID, deviceID, "album", albumID).
		Count(&count).Error; err != nil {
		t.Fatalf("count album pins for %s: %v", albumID, err)
	}
	if count != want {
		t.Fatalf("album pin count for %s = %d, want %d", albumID, count, want)
	}
}

type blockingTagReader struct {
	tagsByPath map[string]Tags
	started    chan<- string
	release    <-chan struct{}
}

func (r blockingTagReader) Read(path string) (Tags, error) {
	path = filepath.Clean(path)
	if r.started != nil {
		select {
		case r.started <- path:
		default:
		}
	}
	if r.release != nil {
		<-r.release
	}
	tags, ok := r.tagsByPath[path]
	if !ok {
		return Tags{}, errors.New("missing test tags for " + path)
	}
	return tags, nil
}
