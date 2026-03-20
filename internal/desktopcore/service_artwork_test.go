package desktopcore

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

type artworkBuilderByPathStub struct {
	results map[string]ArtworkBuildResult
	errs    map[string]error
}

func (s artworkBuilderByPathStub) BuildForAudio(_ context.Context, audioPath string) (ArtworkBuildResult, error) {
	audioPath = filepath.Clean(audioPath)
	if err, ok := s.errs[audioPath]; ok {
		return ArtworkBuildResult{}, err
	}
	result, ok := s.results[audioPath]
	if !ok {
		return ArtworkBuildResult{}, ErrNoArtworkFound
	}
	return result, nil
}

func TestRescanNowBuildsAlbumArtworkVariants(t *testing.T) {
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
				Title:       "Artwork Track",
				Album:       "Artwork Album",
				AlbumArtist: "Artwork Artist",
				Artists:     []string{"Artwork Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
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
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(audioPath): {
				SourceKind: "embedded",
				SourceRef:  filepath.Clean(audioPath),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif"), W: 1024, H: 1024},
				},
			},
		},
	}
	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "artwork-build"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("rescan now: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums.Items) != 1 {
		t.Fatalf("album count = %d, want 1", len(albums.Items))
	}
	if albums.Items[0].Thumb.Variant != defaultArtworkVariant320 {
		t.Fatalf("album thumb variant = %q, want %q", albums.Items[0].Thumb.Variant, defaultArtworkVariant320)
	}

	rows := loadAlbumArtworkRows(t, app, albums.Items[0].AlbumID)
	if len(rows) != 3 {
		t.Fatalf("artwork variant count = %d, want 3", len(rows))
	}
	gotVariants := make([]string, 0, len(rows))
	for _, row := range rows {
		gotVariants = append(gotVariants, row.Variant)
		if row.ChosenSourceRef != "" {
			t.Fatalf("expected materialized artwork row to scrub source ref, got %q", row.ChosenSourceRef)
		}
		if got := loadLocalArtworkSourceRef(t, app, albums.Items[0].AlbumID, row.Variant); got != filepath.Clean(audioPath) {
			t.Fatalf("local chosen source ref = %q, want %q", got, filepath.Clean(audioPath))
		}
		typedPath, err := app.blobs.ArtworkPath(row.BlobID, row.FileExt)
		if err != nil {
			t.Fatalf("resolve typed artwork path: %v", err)
		}
		if _, err := os.Stat(typedPath); err != nil {
			t.Fatalf("stat typed artwork path %q: %v", typedPath, err)
		}
		basePath, err := app.blobs.Path(row.BlobID)
		if err != nil {
			t.Fatalf("resolve legacy artwork path: %v", err)
		}
		if _, err := os.Stat(basePath); !os.IsNotExist(err) {
			t.Fatalf("expected no extensionless artwork blob at %q, err=%v", basePath, err)
		}
	}
	sort.Strings(gotVariants)
	wantVariants := []string{defaultArtworkVariant1024, defaultArtworkVariant320, defaultArtworkVariant96}
	sort.Strings(wantVariants)
	for i := range wantVariants {
		if gotVariants[i] != wantVariants[i] {
			t.Fatalf("artwork variants = %v, want %v", gotVariants, wantVariants)
		}
	}
}

func TestRescanRootRebuildsArtworkFromSurvivingSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	pathA := filepath.Join(root, "track-a.flac")
	pathB := filepath.Join(root, "track-b.flac")
	if err := os.WriteFile(pathA, []byte("audio-a"), 0o644); err != nil {
		t.Fatalf("write first audio file: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("audio-b"), 0o644); err != nil {
		t.Fatalf("write second audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(pathA): {
				Title:       "Track A",
				Album:       "Shared Album",
				AlbumArtist: "Shared Artist",
				Artists:     []string{"Shared Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
				DurationMS:  180000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
			filepath.Clean(pathB): {
				Title:       "Track B",
				Album:       "Shared Album",
				AlbumArtist: "Shared Artist",
				Artists:     []string{"Shared Artist"},
				TrackNo:     2,
				DiscNo:      1,
				Year:        2026,
				DurationMS:  181000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 320000,
			},
		},
	}
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(pathA): {
				SourceKind: "embedded",
				SourceRef:  filepath.Clean(pathA),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-a"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-a"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-a"), W: 1024, H: 1024},
				},
			},
			filepath.Clean(pathB): {
				SourceKind: "embedded",
				SourceRef:  filepath.Clean(pathB),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-b"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-b"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-b"), W: 1024, H: 1024},
				},
			},
		},
	}
	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "artwork-rebuild"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("initial rescan: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	albumID := albums.Items[0].AlbumID
	before := loadAlbumArtworkRows(t, app, albumID)
	if len(before) != 3 {
		t.Fatalf("initial artwork variant count = %d, want 3", len(before))
	}
	if got := loadLocalArtworkSourceRef(t, app, albumID, before[0].Variant); got != filepath.Clean(pathA) {
		t.Fatalf("initial local chosen source ref = %q, want %q", got, filepath.Clean(pathA))
	}
	beforeBlobIDs := artworkBlobIDs(before)

	if err := os.Remove(pathA); err != nil {
		t.Fatalf("remove first audio file: %v", err)
	}
	if _, err := app.RescanRoot(ctx, root); err != nil {
		t.Fatalf("rescan root after removal: %v", err)
	}

	after := loadAlbumArtworkRows(t, app, albumID)
	if len(after) != 3 {
		t.Fatalf("rebuilt artwork variant count = %d, want 3", len(after))
	}
	if got := loadLocalArtworkSourceRef(t, app, albumID, after[0].Variant); got != filepath.Clean(pathB) {
		t.Fatalf("rebuilt local chosen source ref = %q, want %q", got, filepath.Clean(pathB))
	}
	if equalStringSlices(beforeBlobIDs, artworkBlobIDs(after)) {
		t.Fatalf("expected rebuilt artwork blobs to change, before=%v after=%v", beforeBlobIDs, artworkBlobIDs(after))
	}
}

func TestRescanNowReusesUnchangedSidecarArtwork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "track.flac")
	sidecarPath := filepath.Join(root, "cover.jpg")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}
	if err := os.WriteFile(sidecarPath, []byte("cover-v1"), 0o644); err != nil {
		t.Fatalf("write sidecar file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Track One",
				Album:       "Sidecar Album",
				AlbumArtist: "Sidecar Artist",
				Artists:     []string{"Sidecar Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
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
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(audioPath): {
				SourceKind: "sidecar",
				SourceRef:  filepath.Clean(sidecarPath),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-sidecar"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-sidecar"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-sidecar"), W: 1024, H: 1024},
				},
			},
		},
	}
	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "sidecar-reuse"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("initial rescan: %v", err)
	}

	albums, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums.Items) != 1 {
		t.Fatalf("album count = %d, want 1", len(albums.Items))
	}
	albumID := albums.Items[0].AlbumID
	before := loadAlbumArtworkRows(t, app, albumID)
	if len(before) != 3 {
		t.Fatalf("initial artwork variant count = %d, want 3", len(before))
	}
	beforeBlobIDs := artworkBlobIDs(before)

	time.Sleep(10 * time.Millisecond)
	if _, err := app.RescanNow(ctx); err != nil {
		t.Fatalf("second rescan: %v", err)
	}

	after := loadAlbumArtworkRows(t, app, albumID)
	if len(after) != 3 {
		t.Fatalf("updated artwork variant count = %d, want 3", len(after))
	}
	if !equalStringSlices(beforeBlobIDs, artworkBlobIDs(after)) {
		t.Fatalf("expected unchanged sidecar artwork blobs to stay stable, before=%v after=%v", beforeBlobIDs, artworkBlobIDs(after))
	}
	if got := loadLocalArtworkSourceRef(t, app, albumID, after[0].Variant); got != filepath.Clean(sidecarPath) {
		t.Fatalf("local chosen source ref = %q, want %q", got, filepath.Clean(sidecarPath))
	}
}

func TestRescanNowRefreshesArtworkWhenAlbumVariantSurvives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	pathA := filepath.Join(root, "track-a.flac")
	if err := os.WriteFile(pathA, []byte("audio-a-v1"), 0o644); err != nil {
		t.Fatalf("write initial audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(pathA): {
				Title:       "Track A",
				Album:       "Stable Album",
				AlbumArtist: "Stable Artist",
				Artists:     []string{"Stable Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
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
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(pathA): {
				SourceKind: "embedded",
				SourceRef:  filepath.Clean(pathA),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-a-v1"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-a-v1"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-a-v1"), W: 1024, H: 1024},
				},
			},
		},
	}
	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "artwork-refresh"); err != nil {
		t.Fatalf("create library: %v", err)
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
	albumID := albums.Items[0].AlbumID
	before := loadAlbumArtworkRows(t, app, albumID)
	if len(before) != 3 {
		t.Fatalf("initial artwork variant count = %d, want 3", len(before))
	}
	beforeBlobIDs := artworkBlobIDs(before)

	if err := os.WriteFile(pathA, []byte("audio-a-v2"), 0o644); err != nil {
		t.Fatalf("rewrite primary audio file: %v", err)
	}
	pathB := filepath.Join(root, "track-b.flac")
	if err := os.WriteFile(pathB, []byte("audio-b-v1"), 0o644); err != nil {
		t.Fatalf("write new audio file: %v", err)
	}
	reader.tagsByPath[filepath.Clean(pathB)] = Tags{
		Title:       "Track B",
		Album:       "Stable Album",
		AlbumArtist: "Stable Artist",
		Artists:     []string{"Stable Artist"},
		TrackNo:     2,
		DiscNo:      1,
		Year:        2026,
		DurationMS:  181000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	builder.results[filepath.Clean(pathA)] = ArtworkBuildResult{
		SourceKind: "embedded",
		SourceRef:  filepath.Clean(pathA),
		Variants: []GeneratedArtworkVariant{
			{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-a-v2"), W: 96, H: 96},
			{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-a-v2"), W: 320, H: 320},
			{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-a-v2"), W: 1024, H: 1024},
		},
	}

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
	if updatedAlbums.Items[0].AlbumID != albumID {
		t.Fatalf("album id = %q, want %q", updatedAlbums.Items[0].AlbumID, albumID)
	}
	if updatedAlbums.Items[0].TrackCount != 2 {
		t.Fatalf("track count = %d, want 2", updatedAlbums.Items[0].TrackCount)
	}

	after := loadAlbumArtworkRows(t, app, albumID)
	if len(after) != 3 {
		t.Fatalf("updated artwork variant count = %d, want 3", len(after))
	}
	if got := loadLocalArtworkSourceRef(t, app, albumID, after[0].Variant); got != filepath.Clean(pathA) {
		t.Fatalf("updated local chosen source ref = %q, want %q", got, filepath.Clean(pathA))
	}
	if equalStringSlices(beforeBlobIDs, artworkBlobIDs(after)) {
		t.Fatalf("expected refreshed artwork blobs to change, before=%v after=%v", beforeBlobIDs, artworkBlobIDs(after))
	}
}

func TestRescanNowPrefersNewerTrackArtworkWhenAlbumVariantSurvives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	oldPath := filepath.Join(root, "01-old.flac")
	if err := os.WriteFile(oldPath, []byte("audio-old"), 0o644); err != nil {
		t.Fatalf("write initial audio file: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(oldPath): {
				Title:       "Track One",
				Album:       "Mutable Artwork Album",
				AlbumArtist: "Mutable Artwork Artist",
				Artists:     []string{"Mutable Artwork Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
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
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(oldPath): {
				SourceKind: "embedded",
				SourceRef:  filepath.Clean(oldPath),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-old"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-old"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-old"), W: 1024, H: 1024},
				},
			},
		},
	}
	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "artwork-newer-track"); err != nil {
		t.Fatalf("create library: %v", err)
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
	albumID := albums.Items[0].AlbumID
	before := loadAlbumArtworkRows(t, app, albumID)
	if len(before) != 3 {
		t.Fatalf("initial artwork variant count = %d, want 3", len(before))
	}
	beforeBlobIDs := artworkBlobIDs(before)

	time.Sleep(10 * time.Millisecond)
	newPath := filepath.Join(root, "02-new.flac")
	if err := os.WriteFile(newPath, []byte("audio-new"), 0o644); err != nil {
		t.Fatalf("write new audio file: %v", err)
	}
	reader.tagsByPath[filepath.Clean(newPath)] = Tags{
		Title:       "Track Two",
		Album:       "Mutable Artwork Album",
		AlbumArtist: "Mutable Artwork Artist",
		Artists:     []string{"Mutable Artwork Artist"},
		TrackNo:     2,
		DiscNo:      1,
		Year:        2026,
		DurationMS:  181000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	builder.results[filepath.Clean(newPath)] = ArtworkBuildResult{
		SourceKind: "embedded",
		SourceRef:  filepath.Clean(newPath),
		Variants: []GeneratedArtworkVariant{
			{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg-new"), W: 96, H: 96},
			{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp-new"), W: 320, H: 320},
			{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif-new"), W: 1024, H: 1024},
		},
	}

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
	if updatedAlbums.Items[0].AlbumID != albumID {
		t.Fatalf("album id = %q, want %q", updatedAlbums.Items[0].AlbumID, albumID)
	}
	if updatedAlbums.Items[0].TrackCount != 2 {
		t.Fatalf("track count = %d, want 2", updatedAlbums.Items[0].TrackCount)
	}

	after := loadAlbumArtworkRows(t, app, albumID)
	if len(after) != 3 {
		t.Fatalf("updated artwork variant count = %d, want 3", len(after))
	}
	if equalStringSlices(beforeBlobIDs, artworkBlobIDs(after)) {
		t.Fatalf("expected newer track artwork blobs to replace old blobs, before=%v after=%v", beforeBlobIDs, artworkBlobIDs(after))
	}
	if got := loadLocalArtworkSourceRef(t, app, albumID, after[0].Variant); got != filepath.Clean(newPath) {
		t.Fatalf("local chosen source ref = %q, want %q", got, filepath.Clean(newPath))
	}
}

func TestRescanRootDeletesArtworkWhenLocalSourceDisappears(t *testing.T) {
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
				Title:       "Delete Track",
				Album:       "Delete Album",
				AlbumArtist: "Delete Artist",
				Artists:     []string{"Delete Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
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
	builder := artworkBuilderByPathStub{
		results: map[string]ArtworkBuildResult{
			filepath.Clean(audioPath): {
				SourceKind: "embedded",
				SourceRef:  filepath.Clean(audioPath),
				Variants: []GeneratedArtworkVariant{
					{Variant: defaultArtworkVariant96, MIME: "image/jpeg", FileExt: ".jpg", Bytes: []byte("jpeg"), W: 96, H: 96},
					{Variant: defaultArtworkVariant320, MIME: "image/webp", FileExt: ".webp", Bytes: []byte("webp"), W: 320, H: 320},
					{Variant: defaultArtworkVariant1024, MIME: "image/avif", FileExt: ".avif", Bytes: []byte("avif"), W: 1024, H: 1024},
				},
			},
		},
	}
	app := openArtworkIngestTestApp(t, reader, builder)
	if _, err := app.CreateLibrary(ctx, "artwork-delete"); err != nil {
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
		t.Fatalf("list albums: %v", err)
	}
	albumID := albums.Items[0].AlbumID
	if got := len(loadAlbumArtworkRows(t, app, albumID)); got != 3 {
		t.Fatalf("initial artwork variant count = %d, want 3", got)
	}

	if err := os.Remove(audioPath); err != nil {
		t.Fatalf("remove audio file: %v", err)
	}
	if _, err := app.RescanRoot(ctx, root); err != nil {
		t.Fatalf("rescan root after delete: %v", err)
	}

	var count int64
	if err := app.db.WithContext(ctx).
		Model(&ArtworkVariant{}).
		Where("library_id = ? AND scope_type = 'album' AND scope_id = ?", local.LibraryID, albumID).
		Count(&count).Error; err != nil {
		t.Fatalf("count artwork rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("artwork row count = %d, want 0", count)
	}
}

func openArtworkIngestTestApp(t *testing.T, reader TagReader, artworkBuilder ArtworkBuilder) *App {
	t.Helper()

	root := t.TempDir()
	app, err := Open(context.Background(), Config{
		DBPath:           filepath.Join(root, "library.db"),
		BlobRoot:         filepath.Join(root, "blobs"),
		IdentityKeyPath:  filepath.Join(root, "identity.key"),
		CacheBytes:       1024,
		TagReader:        reader,
		TranscodeBuilder: &fakeAACBuilder{result: []byte("test-encoded")},
		ArtworkBuilder:   artworkBuilder,
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := app.Close(); closeErr != nil {
			t.Fatalf("close app: %v", closeErr)
		}
	})
	return app
}

func loadAlbumArtworkRows(t *testing.T, app *App, albumID string) []ArtworkVariant {
	t.Helper()

	var rows []ArtworkVariant
	if err := app.db.WithContext(context.Background()).
		Where("library_id <> '' AND scope_type = 'album' AND scope_id = ?", albumID).
		Order("variant ASC").
		Find(&rows).Error; err != nil {
		t.Fatalf("load artwork rows: %v", err)
	}
	return rows
}

func loadLocalArtworkSourceRef(t *testing.T, app *App, albumID, variant string) string {
	t.Helper()

	local, err := app.EnsureLocalContext(context.Background())
	if err != nil {
		t.Fatalf("ensure local context: %v", err)
	}
	_, chosenSourceRef, ok, err := localArtworkSourceRefForScopeTx(
		app.db.WithContext(context.Background()),
		local.LibraryID,
		"album",
		albumID,
		variant,
	)
	if err != nil {
		t.Fatalf("load local artwork source ref: %v", err)
	}
	if !ok {
		return ""
	}
	return chosenSourceRef
}

func artworkBlobIDs(rows []ArtworkVariant) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.BlobID)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
