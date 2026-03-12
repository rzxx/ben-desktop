package desktopcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	apitypes "ben/core/api/types"
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
