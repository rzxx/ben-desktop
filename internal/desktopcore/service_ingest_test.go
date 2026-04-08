package desktopcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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

func TestRemovingScanRootAutomaticallyRemovesIndexedContent(t *testing.T) {
	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	audioA := filepath.Join(rootA, "keep.flac")
	audioB := filepath.Join(rootB, "drop.flac")
	for path, payload := range map[string]string{
		audioA: "keep",
		audioB: "drop",
	} {
		if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
			t.Fatalf("write audio file %s: %v", path, err)
		}
	}

	app := openCacheTestAppWithTagReader(t, 1024, staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioA): {
				Title:       "Keep Track",
				Album:       "Root A Album",
				AlbumArtist: "Root A Artist",
				Artists:     []string{"Root A Artist"},
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
				Title:       "Drop Track",
				Album:       "Root B Album",
				AlbumArtist: "Root B Artist",
				Artists:     []string{"Root B Artist"},
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
		},
	})
	if _, err := app.CreateLibrary(ctx, "scan-root-removal"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootA, rootB}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 2)
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)

	remaining, err := app.RemoveScanRoots(ctx, []string{rootB})
	if err != nil {
		t.Fatalf("remove scan roots: %v", err)
	}
	if len(remaining) != 1 || scanRootKey(remaining[0]) != scanRootKey(rootA) {
		t.Fatalf("remaining roots = %+v, want [%s]", remaining, filepath.Clean(rootA))
	}

	recordings := waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Keep Track" {
		t.Fatalf("recordings after root removal = %+v, want Keep Track only", recordings)
	}
}

func TestSetScanRootsCancelsRepairForRemovedRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "stale-repair.flac")
	if err := os.WriteFile(audioPath, []byte("stale-repair"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	baseReader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Stale Repair Track",
				Album:       "Stale Repair Album",
				AlbumArtist: "Stale Repair Artist",
				Artists:     []string{"Stale Repair Artist"},
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
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, baseReader)
	if _, err := app.CreateLibrary(ctx, "stale-repair-root-change"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
	if err := os.WriteFile(audioPath, []byte("stale-repair-updated"), 0o644); err != nil {
		t.Fatalf("rewrite audio file before repair: %v", err)
	}

	started := make(chan string, 1)
	release := make(chan struct{})
	app.tagReader = blockingTagReader{
		tagsByPath: baseReader.tagsByPath,
		started:    started,
		release:    release,
	}

	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	job, err := app.StartRepairLibrary(ctx)
	if err != nil {
		t.Fatalf("start repair library: %v", err)
	}

	select {
	case startedPath := <-started:
		if startedPath != filepath.Clean(audioPath) {
			t.Fatalf("started path = %q, want %q", startedPath, filepath.Clean(audioPath))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for repair ingest to start")
	}

	if err := app.SetScanRoots(ctx, nil); err != nil {
		t.Fatalf("clear scan roots: %v", err)
	}
	close(release)

	final := waitForJobPhase(t, ctx, app, job.JobID, JobPhaseFailed)
	if final.Kind != jobKindRepairLibrary || final.LibraryID != local.LibraryID {
		t.Fatalf("unexpected final repair job: %+v", final)
	}

	recordings, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list recordings after root removal: %v", err)
	}
	if len(recordings.Items) != 0 {
		t.Fatalf("recordings after root removal = %+v, want empty", recordings.Items)
	}

	roots, err := app.ScanRoots(ctx)
	if err != nil {
		t.Fatalf("scan roots after root removal: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("scan roots after root removal = %+v, want empty", roots)
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

func TestRepairLibraryImportsMetadataAndPublishesCompletedJob(t *testing.T) {
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
	waitForRecordingCount(t, ctx, app, 1)
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)

	stats, err := app.RepairLibrary(ctx)
	if err != nil {
		t.Fatalf("repair library: %v", err)
	}
	if stats.Scanned != 1 || stats.Imported != 0 || stats.SkippedUnchanged != 1 || stats.Errors != 0 {
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

	jobID := scanJobID(library.LibraryID, local.DeviceID, []string{filepath.Clean(root)}, jobKindRepairLibrary)
	job, ok, err := app.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get scan job: %v", err)
	}
	if !ok {
		t.Fatalf("expected scan job %q", jobID)
	}
	if job.Phase != JobPhaseCompleted || job.Kind != jobKindRepairLibrary {
		t.Fatalf("unexpected scan job: %+v", job)
	}
}

func TestStartRepairLibraryQueuesAsyncJob(t *testing.T) {
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

	job, err := app.StartRepairLibrary(ctx)
	if err != nil {
		t.Fatalf("start repair library: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindRepairLibrary {
		t.Fatalf("unexpected queued job: %+v", job)
	}

	jobID := scanJobID(library.LibraryID, local.DeviceID, []string{filepath.Clean(root)}, jobKindRepairLibrary)
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseCompleted)
	if final.Kind != jobKindRepairLibrary || final.LibraryID != library.LibraryID {
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

func TestAutomaticScansStayScopedAndRepairUsesFullRebuild(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.flac")
	secondPath := filepath.Join(root, "second.flac")
	if err := os.WriteFile(firstPath, []byte("fake-audio-1"), 0o644); err != nil {
		t.Fatalf("write first audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(firstPath): {
				Title:       "First Track",
				Album:       "Scoped Album",
				AlbumArtist: "Scoped Artist",
				Artists:     []string{"Scoped Artist"},
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
			filepath.Clean(secondPath): {
				Title:       "Second Track",
				Album:       "Scoped Album",
				AlbumArtist: "Scoped Artist",
				Artists:     []string{"Scoped Artist"},
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

	app := openCacheTestAppWithTagReader(t, 1024, reader)
	var fullRebuildCalls int32
	app.rebuildCatalogMaterializationFullHook = func() {
		atomic.AddInt32(&fullRebuildCalls, 1)
	}
	defer func() {
		app.rebuildCatalogMaterializationFullHook = nil
	}()
	library, err := app.CreateLibrary(ctx, "scoped-auto-scan")
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
	waitForRecordingCount(t, ctx, app, 1)
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
	if got := atomic.LoadInt32(&fullRebuildCalls); got != 0 {
		t.Fatalf("startup scan invoked full rebuild %d times, want 0", got)
	}

	if err := os.WriteFile(secondPath, []byte("fake-audio-2"), 0o644); err != nil {
		t.Fatalf("write second audio file: %v", err)
	}
	stats, err := app.ingest.runDeltaScanPass(
		ctx,
		library.LibraryID,
		local.DeviceID,
		deltaScanScope{audioPaths: []string{secondPath}},
		nil,
	)
	if err != nil {
		t.Fatalf("delta scan pass: %v", err)
	}
	if stats.Imported != 1 || stats.Errors != 0 {
		t.Fatalf("unexpected delta scan stats: %+v", stats)
	}
	waitForRecordingCount(t, ctx, app, 2)
	if got := atomic.LoadInt32(&fullRebuildCalls); got != 0 {
		t.Fatalf("delta scan invoked full rebuild %d times, want 0", got)
	}

	if err := app.markScanRepairRequired(ctx, library.LibraryID, local.DeviceID, "test-reason", "test detail"); err != nil {
		t.Fatalf("mark scan repair required: %v", err)
	}
	activity, err := app.ActivityStatus(ctx)
	if err != nil {
		t.Fatalf("activity status before repair: %v", err)
	}
	if !activity.Maintenance.RepairRequired {
		t.Fatalf("expected repair required before repair, got %+v", activity.Maintenance)
	}

	repairStats, err := app.RepairLibrary(ctx)
	if err != nil {
		t.Fatalf("repair library: %v", err)
	}
	if repairStats.Scanned != 2 || repairStats.Errors != 0 {
		t.Fatalf("unexpected repair stats: %+v", repairStats)
	}
	if got := atomic.LoadInt32(&fullRebuildCalls); got != 1 {
		t.Fatalf("repair invoked full rebuild %d times, want 1", got)
	}

	activity, err = app.ActivityStatus(ctx)
	if err != nil {
		t.Fatalf("activity status after repair: %v", err)
	}
	if activity.Maintenance.RepairRequired {
		t.Fatalf("repair state should be cleared after repair, got %+v", activity.Maintenance)
	}

	oplog, err := app.InspectLibraryOplog(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("inspect library oplog: %v", err)
	}
	if oplog.Maintenance.RepairRequired {
		t.Fatalf("oplog maintenance should be cleared after repair, got %+v", oplog.Maintenance)
	}
}

func TestStartRepairLibraryCancelsWhenActiveLibraryChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	audioA1 := filepath.Join(rootA, "async-one.flac")
	audioA2 := filepath.Join(rootA, "async-two.flac")

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

	second, err := app.CreateLibrary(ctx, "scan-cancel-b")
	if err != nil {
		t.Fatalf("create second library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootB}); err != nil {
		t.Fatalf("set second scan roots: %v", err)
	}

	first, err := app.CreateLibrary(ctx, "scan-cancel-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootA}); err != nil {
		t.Fatalf("set first scan roots: %v", err)
	}
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
	if app.scanner != nil {
		app.scanner.stopActiveScanWatcher()
	}

	if err := os.WriteFile(audioA1, []byte("fake-audio-1"), 0o644); err != nil {
		t.Fatalf("write first audio file: %v", err)
	}
	if err := os.WriteFile(audioA2, []byte("fake-audio-2"), 0o644); err != nil {
		t.Fatalf("write second audio file: %v", err)
	}

	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	job, err := app.StartRepairLibrary(ctx)
	if err != nil {
		t.Fatalf("start repair library: %v", err)
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

	jobID := scanJobID(first.LibraryID, local.DeviceID, []string{filepath.Clean(rootA)}, jobKindRepairLibrary)
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseFailed)
	if !strings.Contains(final.Message, "no longer active") {
		t.Fatalf("expected cancellation message, got %+v", final)
	}

	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("reselect first library after cancellation: %v", err)
	}
}

func TestRepairLibraryDropsQueuedManualScanOnCallerCancel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "queued-cancel.flac")
	if err := os.WriteFile(audioPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	started := make(chan string, 4)
	release := make(chan struct{})
	reader := blockingTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Queued Cancel Track",
				Album:       "Queued Cancel Album",
				AlbumArtist: "Queued Cancel Artist",
				Artists:     []string{"Queued Cancel Artist"},
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
		started: started,
		release: release,
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "scan-queued-cancel"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	job, err := app.StartRepairLibrary(ctx)
	if err != nil {
		t.Fatalf("start blocking scan: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial scan ingest to start")
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		_, err := app.RepairLibrary(cancelCtx)
		result <- err
	}()

	foundPending := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.runtimeMu.Lock()
		runtime := app.activeRuntime
		app.runtimeMu.Unlock()
		if runtime != nil && runtime.scanCoordinator != nil {
			runtime.scanCoordinator.mu.Lock()
			pending := len(runtime.scanCoordinator.pending)
			runtime.scanCoordinator.mu.Unlock()
			if pending > 0 {
				foundPending = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !foundPending {
		t.Fatal("timed out waiting for queued manual repair request")
	}

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued repair err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued repair cancellation")
	}

	close(release)
	final := waitForJobPhase(t, ctx, app, job.JobID, JobPhaseCompleted)
	if final.Kind != jobKindRepairLibrary || final.LibraryID != local.LibraryID {
		t.Fatalf("unexpected blocking scan completion: %+v", final)
	}

	select {
	case path := <-started:
		t.Fatalf("canceled queued repair still ingested %q", path)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRepairLibraryCancelsActiveManualScanOnCallerCancel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	audioA := filepath.Join(root, "active-cancel-a.flac")
	audioB := filepath.Join(root, "active-cancel-b.flac")

	started := make(chan string, 4)
	release := make(chan struct{})
	reader := blockingTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioA): {
				Title:       "Active Cancel Track A",
				Album:       "Active Cancel Album",
				AlbumArtist: "Active Cancel Artist",
				Artists:     []string{"Active Cancel Artist"},
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
			filepath.Clean(audioB): {
				Title:       "Active Cancel Track B",
				Album:       "Active Cancel Album",
				AlbumArtist: "Active Cancel Artist",
				Artists:     []string{"Active Cancel Artist"},
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
	if _, err := app.CreateLibrary(ctx, "scan-active-cancel"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
	if app.scanner != nil {
		app.scanner.stopActiveScanWatcher()
	}

	for _, path := range []string{audioA, audioB} {
		if err := os.WriteFile(path, []byte("fake-audio"), 0o644); err != nil {
			t.Fatalf("write audio file %q: %v", path, err)
		}
	}

	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		_, err := app.RepairLibrary(cancelCtx)
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for active repair ingest to start")
	}

	cancel()
	close(release)

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("active repair err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for active repair cancellation")
	}

	jobID := scanJobID(local.LibraryID, local.DeviceID, []string{filepath.Clean(root)}, jobKindRepairLibrary)
	final := waitForJobPhase(t, ctx, app, jobID, JobPhaseFailed)
	if !strings.Contains(final.Message, "repair canceled") {
		t.Fatalf("expected cancellation message, got %+v", final)
	}

	recordings, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list partially scanned recordings: %v", err)
	}
	if len(recordings.Items) >= 2 {
		t.Fatalf("expected caller cancellation to stop the scan before both tracks imported, got %+v", recordings.Items)
	}
}

func TestRepairRootsMarksMissingFilesAbsent(t *testing.T) {
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
	if _, err := app.ingest.repairRoots(ctx, []string{root}); err != nil {
		t.Fatalf("initial repair roots: %v", err)
	}

	if err := os.Remove(audioPath); err != nil {
		t.Fatalf("remove audio file: %v", err)
	}
	stats, err := app.ingest.repairRoots(ctx, []string{root})
	if err != nil {
		t.Fatalf("repair missing root: %v", err)
	}
	if stats.Scanned != 0 {
		t.Fatalf("missing-root repair should not rescan files: %+v", stats)
	}

	var row SourceFileModel
	if err := app.db.WithContext(ctx).Where("library_id <> ''").Take(&row).Error; err != nil {
		t.Fatalf("load source file: %v", err)
	}
	if row.IsPresent {
		t.Fatalf("expected removed source file to be marked absent: %+v", row)
	}
}

func TestRepairLibraryReplacesRemovedAlbumVariantMaterialization(t *testing.T) {
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

	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("initial repair: %v", err)
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

	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("updated repair: %v", err)
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

func TestRepairLibraryKeepsCatalogWhenArtworkRebuildFails(t *testing.T) {
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
	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("initial repair: %v", err)
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

	stats, err := app.RepairLibrary(ctx)
	if err != nil {
		t.Fatalf("repair with artwork failure: %v", err)
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

	if err := app.rebuildCatalogMaterializationFull(ctx, library.LibraryID, nil); err != nil {
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

func TestScanWatcherRemovesDeletedFileForActiveLibrary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "watch-delete.flac")
	if err := os.WriteFile(audioPath, []byte("watch-delete"), 0o644); err != nil {
		t.Fatalf("write watched audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Watched Delete",
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
	if _, err := app.CreateLibrary(ctx, "watch-delete"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("initial repair: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 1)

	if err := os.Remove(audioPath); err != nil {
		t.Fatalf("remove watched audio file: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 0)
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
	recordings = waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Inactive Track" {
		t.Fatalf("startup scan should catch up inactive library on activation, got %+v", recordings)
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
	page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list second library recordings: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Active Track" {
		t.Fatalf("second library recordings = %+v, want only Active Track", page.Items)
	}
}

func TestSetScanRootsTriggersStartupScanForExistingFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "late-root.flac")
	if err := os.WriteFile(audioPath, []byte("late-root"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Late Root Track",
				Album:       "Late Root Album",
				AlbumArtist: "Late Root Artist",
				Artists:     []string{"Late Root Artist"},
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
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "late-root-startup"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list recordings before roots: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("recordings before roots = %+v, want empty", page.Items)
	}

	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	recordings := waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Late Root Track" {
		t.Fatalf("startup scan after adding roots = %+v, want Late Root Track", recordings)
	}
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
}

func TestStartupScanRemovesDeletedFileWhileLibraryWasInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	audioA := filepath.Join(rootA, "inactive-delete.flac")
	audioB := filepath.Join(rootB, "active.flac")
	if err := os.WriteFile(audioA, []byte("inactive"), 0o644); err != nil {
		t.Fatalf("write inactive audio: %v", err)
	}
	if err := os.WriteFile(audioB, []byte("active"), 0o644); err != nil {
		t.Fatalf("write active audio: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioA): {
				Title:       "Inactive Delete",
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
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)

	first, err := app.CreateLibrary(ctx, "startup-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootA}); err != nil {
		t.Fatalf("set first scan roots: %v", err)
	}
	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("initial repair first library: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 1)

	if _, err := app.CreateLibrary(ctx, "startup-b"); err != nil {
		t.Fatalf("create second library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{rootB}); err != nil {
		t.Fatalf("set second scan roots: %v", err)
	}
	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("repair second library: %v", err)
	}

	if err := os.Remove(audioA); err != nil {
		t.Fatalf("remove inactive library audio: %v", err)
	}
	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("reselect first library: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 0)
}

func TestStartupScanRetriesAfterFailureOnNextRuntimeSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "retry-startup.flac")
	if err := os.WriteFile(audioPath, []byte("retry-startup"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := &flakyTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Retried Startup Track",
				Album:       "Retried Startup Album",
				AlbumArtist: "Retried Startup Artist",
				Artists:     []string{"Retried Startup Artist"},
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
		},
		failuresRemainingByPath: map[string]int{
			filepath.Clean(audioPath): 1,
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "startup-retry"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	failed := waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseFailed)
	if !strings.Contains(failed.Error, "forced tag read failure") {
		t.Fatalf("startup failure job = %+v, want forced tag read failure", failed)
	}

	page, err := app.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list recordings after failed startup scan: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("recordings after failed startup scan = %+v, want empty", page.Items)
	}

	if err := app.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("retry startup scan: %v", err)
	}

	recordings := waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Retried Startup Track" {
		t.Fatalf("recordings after retry = %+v, want Retried Startup Track", recordings)
	}
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
}

func TestRepairLibraryEmitsAvailabilityInvalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "availability.flac")
	if err := os.WriteFile(audioPath, []byte("availability"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Availability Track",
				Album:       "Availability Album",
				AlbumArtist: "Availability Artist",
				Artists:     []string{"Availability Artist"},
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
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "scan-availability"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	events := make(chan apitypes.CatalogChangeEvent, 16)
	stop := app.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		select {
		case events <- event:
		default:
		}
	})
	defer stop()

	if _, err := app.RepairLibrary(ctx); err != nil {
		t.Fatalf("repair library: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind == apitypes.CatalogChangeInvalidateAvailability && event.InvalidateAll {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for availability invalidation after scan")
		}
	}
}

func TestDeltaScanReconcilesChangedAlbumWithoutFullRebuild(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	firstPath := filepath.Join(root, "01-first.flac")
	secondPath := filepath.Join(root, "02-second.flac")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write audio file %s: %v", path, err)
		}
	}

	reader := &mutableTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(firstPath): {
				Title:       "First Track",
				Album:       "Original Album",
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
			},
			filepath.Clean(secondPath): {
				Title:       "Second Track",
				Album:       "Original Album",
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
			},
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "delta-reconcile"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 2)
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)

	initialAlbums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list initial albums: %v", err)
	}
	if len(initialAlbums.Items) != 1 || initialAlbums.Items[0].TrackCount != 2 {
		t.Fatalf("unexpected initial albums: %+v", initialAlbums.Items)
	}

	reader.tagsByPath[filepath.Clean(firstPath)] = Tags{
		Title:       "First Track",
		Album:       "Split Album",
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
	if err := os.WriteFile(firstPath, []byte("updated-first-track"), 0o644); err != nil {
		t.Fatalf("rewrite first audio file: %v", err)
	}

	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	stats, err := app.ingest.runDeltaScanPass(ctx, local.LibraryID, local.DeviceID, deltaScanScope{audioPaths: []string{firstPath}}, nil)
	if err != nil {
		t.Fatalf("delta scan: %v", err)
	}
	if stats.Imported != 1 || stats.Errors != 0 {
		t.Fatalf("unexpected delta scan stats: %+v", stats)
	}

	updatedAlbums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list updated albums: %v", err)
	}
	if len(updatedAlbums.Items) != 2 {
		t.Fatalf("updated album count = %d, want 2", len(updatedAlbums.Items))
	}

	sawOriginalAlbum := false
	sawSplitAlbum := false
	sawSecondTrack := false
	sawFirstTrack := false
	for _, album := range updatedAlbums.Items {
		if album.TrackCount != 1 {
			t.Fatalf("album %q track count = %d, want 1", album.Title, album.TrackCount)
		}
		tracks, err := app.ListAlbumTracks(ctx, apitypes.AlbumTrackListRequest{
			AlbumID:     album.PreferredVariantAlbumID,
			PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
		})
		if err != nil {
			t.Fatalf("list album tracks for %s: %v", album.Title, err)
		}
		if len(tracks.Items) != 1 {
			t.Fatalf("album %q tracks = %+v, want exactly one track", album.Title, tracks.Items)
		}
		switch tracks.Items[0].Title {
		case "Second Track":
			sawSecondTrack = true
			if album.Title != "Original Album" {
				t.Fatalf("second track stayed in %q, want Original Album", album.Title)
			}
			sawOriginalAlbum = true
		case "First Track":
			sawFirstTrack = true
			if album.Title != "Split Album" {
				t.Fatalf("first track moved to %q, want Split Album", album.Title)
			}
			sawSplitAlbum = true
		default:
			t.Fatalf("unexpected album tracks for %q: %+v", album.Title, tracks.Items)
		}
	}
	if !sawOriginalAlbum || !sawSplitAlbum || !sawFirstTrack || !sawSecondTrack {
		t.Fatalf(
			"unexpected updated albums: original=%v split=%v first=%v second=%v albums=%+v",
			sawOriginalAlbum,
			sawSplitAlbum,
			sawFirstTrack,
			sawSecondTrack,
			updatedAlbums.Items,
		)
	}
}

func TestScanEmitsCatalogQueryInvalidationsForTargetedRefresh(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "01-event.flac")
	if err := os.WriteFile(audioPath, []byte("event-track"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	reader := &mutableTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Event Track",
				Album:       "Event Album",
				AlbumArtist: "Event Artist",
				Artists:     []string{"Event Artist"},
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
		},
	}
	app := openCacheTestAppWithTagReader(t, 1024, reader)
	if _, err := app.CreateLibrary(ctx, "scan-events"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 1)
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)

	reader.tagsByPath[filepath.Clean(audioPath)] = Tags{
		Title:       "Event Track",
		Album:       "Renamed Event Album",
		AlbumArtist: "Event Artist",
		Artists:     []string{"Event Artist"},
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
	if err := os.WriteFile(audioPath, []byte("event-track-updated"), 0o644); err != nil {
		t.Fatalf("rewrite audio file: %v", err)
	}

	events := make(chan apitypes.CatalogChangeEvent, 32)
	stop := app.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		select {
		case events <- event:
		default:
		}
	})
	defer stop()

	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if _, err := app.ingest.runDeltaScanPass(ctx, local.LibraryID, local.DeviceID, deltaScanScope{audioPaths: []string{audioPath}}, nil); err != nil {
		t.Fatalf("delta scan: %v", err)
	}

	queryKeys := make(map[string]struct{})
	sawScopedBase := false
	sawAlbumTracks := false
	sawArtistAlbums := false
	drainDeadline := time.After(500 * time.Millisecond)
	for {
		select {
		case event := <-events:
			if len(event.RecordingIDs) > 0 && len(event.AlbumIDs) > 0 {
				sawScopedBase = true
			}
			if strings.HasPrefix(event.QueryKey, "albumTracks:") {
				sawAlbumTracks = true
			}
			if strings.HasPrefix(event.QueryKey, "artistAlbums:") {
				sawArtistAlbums = true
			}
			if event.QueryKey != "" {
				queryKeys[event.QueryKey] = struct{}{}
			}
		case <-drainDeadline:
			goto assertions
		}
	}

assertions:
	for _, queryKey := range []string{"tracks", "albums", "artists"} {
		if _, ok := queryKeys[queryKey]; !ok {
			t.Fatalf("missing query invalidation for %q; events = %+v", queryKey, queryKeys)
		}
	}
	if !sawAlbumTracks {
		t.Fatal("expected albumTracks query invalidation from scan")
	}
	if !sawArtistAlbums {
		t.Fatal("expected artistAlbums query invalidation from scan")
	}
	if !sawScopedBase {
		t.Fatal("expected scan to emit scoped base invalidation with recording and album ids")
	}
}

func TestDeltaScanIgnoresCorruptImpactRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "broken.flac")
	if err := os.WriteFile(audioPath, []byte("broken"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	app := openCacheTestAppWithTagReader(t, 1024, staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Recovered Track",
				Album:       "Recovered Album",
				AlbumArtist: "Recovered Artist",
				Artists:     []string{"Recovered Artist"},
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
		},
	})
	if _, err := app.CreateLibrary(ctx, "delta-failure"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	waitForRecordingCount(t, ctx, app, 1)
	waitForJobKindPhase(t, ctx, app, jobKindStartupScan, JobPhaseCompleted)
	if err := os.WriteFile(audioPath, []byte("broken-recovered"), 0o644); err != nil {
		t.Fatalf("rewrite audio file: %v", err)
	}

	now := time.Now().UTC()
	var row SourceFileModel
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND path_key = ?", local.LibraryID, local.DeviceID, localPathKey(audioPath)).
		Take(&row).Error; err != nil {
		t.Fatalf("load current source file: %v", err)
	}
	row.TrackVariantID = "broken-track"
	row.SourceFingerprint = "sha256:broken-source"
	row.EditionScopeKey = "broken-scope"
	row.HashAlgo = "sha256"
	row.HashHex = strings.Repeat("b", 64)
	row.MTimeNS = now.Add(-time.Minute).UnixNano()
	row.SizeBytes = int64(len("broken"))
	row.TagsJSON = "{"
	row.LastSeenAt = now
	row.IsPresent = true
	row.UpdatedAt = now
	if err := app.db.WithContext(ctx).Save(&row).Error; err != nil {
		t.Fatalf("corrupt current source file: %v", err)
	}

	stats, err := app.ingest.runDeltaScanPass(ctx, local.LibraryID, local.DeviceID, deltaScanScope{audioPaths: []string{audioPath}}, nil)
	if err != nil {
		t.Fatalf("delta scan with corrupt impact row: %v", err)
	}
	if stats.Imported != 1 || stats.Errors != 0 {
		t.Fatalf("unexpected delta scan stats: %+v", stats)
	}

	activity, err := app.ActivityStatus(ctx)
	if err != nil {
		t.Fatalf("activity status: %v", err)
	}
	if activity.Scan.Phase != "completed" {
		t.Fatalf("scan activity phase = %q, want completed", activity.Scan.Phase)
	}

	recordings := waitForRecordingCount(t, ctx, app, 1)
	if recordings[0].Title != "Recovered Track" {
		t.Fatalf("unexpected recordings after recovery delta scan: %+v", recordings)
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

func waitForJobKindPhase(t *testing.T, ctx context.Context, app *App, kind string, want JobPhase) JobSnapshot {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := app.ListJobs(ctx, "")
		if err == nil {
			for _, job := range jobs {
				if strings.TrimSpace(job.Kind) == strings.TrimSpace(kind) && job.Phase == want {
					return job
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	jobs, err := app.ListJobs(ctx, "")
	if err != nil {
		t.Fatalf("list jobs after wait: %v", err)
	}
	t.Fatalf("job kind %q with phase %q not found after wait; jobs = %+v", kind, want, jobs)
	return JobSnapshot{}
}

type staticTagReader struct {
	tagsByPath map[string]Tags
}

type mutableTagReader struct {
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

func (r *mutableTagReader) Read(path string) (Tags, error) {
	path = filepath.Clean(path)
	tags, ok := r.tagsByPath[path]
	if !ok {
		return Tags{}, errors.New("missing test tags for " + path)
	}
	return tags, nil
}

type flakyTagReader struct {
	tagsByPath              map[string]Tags
	failuresRemainingByPath map[string]int
}

func (r *flakyTagReader) Read(path string) (Tags, error) {
	path = filepath.Clean(path)
	if r.failuresRemainingByPath != nil {
		if remaining := r.failuresRemainingByPath[path]; remaining > 0 {
			r.failuresRemainingByPath[path] = remaining - 1
			return Tags{}, errors.New("forced tag read failure for " + path)
		}
	}
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
