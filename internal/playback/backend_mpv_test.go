//go:build !nompv

package playback

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizePlaybackURIMatchesFileURIAndLocalPath(t *testing.T) {
	t.Parallel()

	if got, want := normalizePlaybackURI("file:///tmp/Track%20One.mp3"), "/tmp/Track One.mp3"; got != want {
		t.Fatalf("posix file uri = %q, want %q", got, want)
	}
	if got, want := normalizePlaybackURI("/tmp/Track One.mp3"), "/tmp/Track One.mp3"; got != want {
		t.Fatalf("posix local path = %q, want %q", got, want)
	}
	if got, want := normalizePlaybackURI("file:///C:/Music/Track%20One.mp3"), "c:/music/track one.mp3"; got != want {
		t.Fatalf("windows file uri = %q, want %q", got, want)
	}
	if got, want := normalizePlaybackURI(`C:\Music\Track One.mp3`), "c:/music/track one.mp3"; got != want {
		t.Fatalf("windows local path = %q, want %q", got, want)
	}
}

func TestMPVPlaylistSnapshotNewestEntryByURIPrefersNewestNonCurrent(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/one.mp3"), Current: true, Playing: true},
			{Index: 1, EntryID: 11, Filename: normalizePlaybackURI("file:///tmp/two.mp3")},
			{Index: 2, EntryID: 12, Filename: normalizePlaybackURI("file:///tmp/two.mp3")},
		},
	}

	entry, ok := snapshot.newestEntryByURI("/tmp/two.mp3")
	if !ok {
		t.Fatalf("expected matching playlist entry")
	}
	if entry.EntryID != 12 {
		t.Fatalf("entry id = %d, want 12", entry.EntryID)
	}
}

func TestMPVPlaylistSnapshotEntryByID(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10},
			{Index: 1, EntryID: 11},
		},
	}

	entry, ok := snapshot.entryByID(11)
	if !ok {
		t.Fatalf("expected to find entry by id")
	}
	if entry.Index != 1 {
		t.Fatalf("entry index = %d, want 1", entry.Index)
	}
}

func TestMPVPlaylistSnapshotEntryForPreloadFallsBackWithoutEntryID(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/one.mp3"), Current: true, Playing: true},
			{Index: 1, EntryID: 0, Filename: normalizePlaybackURI("file:///tmp/two.mp3")},
		},
	}

	entry, ok := snapshot.entryForPreload(mpvPreloadState{
		URI:      normalizePlaybackURI("file:///tmp/two.mp3"),
		EntryID:  0,
		Index:    1,
		Verified: true,
	})
	if !ok {
		t.Fatalf("expected to find preload entry without entry id")
	}
	if entry.Index != 1 {
		t.Fatalf("entry index = %d, want 1", entry.Index)
	}
}

func TestMPVPlaylistSnapshotIsActiveEntryRecognizesCurrentAndPlayingSlots(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		CurrentPos: 0,
		PlayingPos: 0,
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/one.mp3"), Current: true, Playing: true},
			{Index: 1, EntryID: 11, Filename: normalizePlaybackURI("file:///tmp/two.mp3")},
		},
	}

	if !snapshot.isActiveEntry(snapshot.Entries[0]) {
		t.Fatal("expected current entry to be treated as active")
	}
	if snapshot.isActiveEntry(snapshot.Entries[1]) {
		t.Fatal("expected non-current entry to remain removable")
	}
}

func TestMPVCommandErrorIncludesTransportContext(t *testing.T) {
	t.Parallel()

	err := (&mpvCommandError{
		Op:           "activate_preloaded",
		Command:      []string{"playlist-play-index", "3"},
		RequestedURI: "file:///tmp/two.mp3",
		Snapshot: mpvPlaylistSnapshot{
			Count:      2,
			CurrentPos: 0,
			PlayingPos: 0,
			ActivePath: "/tmp/one.mp3",
		},
		Cause: errors.New("unknown error"),
	}).Error()

	for _, fragment := range []string{
		"activate_preloaded",
		"playlist-play-index 3",
		`uri="file:///tmp/two.mp3"`,
		"playlist_count=2",
		`active_path="/tmp/one.mp3"`,
		"unknown error",
	} {
		if !strings.Contains(err, fragment) {
			t.Fatalf("error string %q missing %q", err, fragment)
		}
	}
}

func TestMPVPlaylistSnapshotHasUniqueURI(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/one.mp3")},
			{Index: 1, EntryID: 11, Filename: normalizePlaybackURI("file:///tmp/two.mp3")},
		},
	}

	if !snapshot.hasUniqueURI("file:///tmp/two.mp3") {
		t.Fatal("expected unique uri to be detectable")
	}
	if snapshot.hasUniqueURI("file:///tmp/missing.mp3") {
		t.Fatal("expected missing uri to be non-unique")
	}
}

func TestMPVPlaylistSnapshotHasUniqueURIRejectsDuplicates(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/shared.mp3")},
			{Index: 1, EntryID: 11, Filename: normalizePlaybackURI("file:///tmp/shared.mp3")},
		},
	}

	if snapshot.hasUniqueURI("file:///tmp/shared.mp3") {
		t.Fatal("expected duplicate uri to be rejected")
	}
}

func TestMPVPlaylistSnapshotMatchActivationAttemptFallsBackToUniqueURI(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/one.mp3")},
			{Index: 1, EntryID: 11, Filename: normalizePlaybackURI("file:///tmp/two.mp3")},
		},
	}

	attemptID, ok := snapshot.matchActivationAttempt(
		mpvPendingActivationState{
			AttemptID: 7,
			URI:       normalizePlaybackURI("file:///tmp/two.mp3"),
			EntryID:   11,
			Index:     1,
		},
		"file:///tmp/two.mp3",
		-1,
		0,
	)
	if !ok || attemptID != 7 {
		t.Fatalf("expected unique-uri fallback to match activation attempt, got ok=%v attemptID=%d", ok, attemptID)
	}
}

func TestMPVPlaylistSnapshotMatchActivationAttemptRejectsDuplicateURIWithoutIDs(t *testing.T) {
	t.Parallel()

	snapshot := mpvPlaylistSnapshot{
		Entries: []mpvPlaylistEntry{
			{Index: 0, EntryID: 10, Filename: normalizePlaybackURI("file:///tmp/shared.mp3")},
			{Index: 1, EntryID: 11, Filename: normalizePlaybackURI("file:///tmp/shared.mp3")},
		},
	}

	attemptID, ok := snapshot.matchActivationAttempt(
		mpvPendingActivationState{
			AttemptID: 7,
			URI:       normalizePlaybackURI("file:///tmp/shared.mp3"),
			EntryID:   11,
			Index:     1,
		},
		"file:///tmp/shared.mp3",
		-1,
		0,
	)
	if ok || attemptID != 0 {
		t.Fatalf("expected duplicate-uri fallback to stay unresolved, got ok=%v attemptID=%d", ok, attemptID)
	}
}

func TestResolveEOFStateFallsBackToEndedURIWhenMPVClearsActiveState(t *testing.T) {
	t.Parallel()

	activeURI, activePos, activeEntryID := resolveEOFState(
		"",
		0,
		44,
		errors.New("unknown error"),
		"file:///tmp/ended.mp3",
		mpvPreloadState{},
	)

	if activeURI != "/tmp/ended.mp3" {
		t.Fatalf("active uri = %q, want %q", activeURI, "/tmp/ended.mp3")
	}
	if activePos != -1 {
		t.Fatalf("active pos = %d, want -1", activePos)
	}
	if activeEntryID != 0 {
		t.Fatalf("active entry id = %d, want 0", activeEntryID)
	}
}

func TestResolveEOFStatePrefersPreloadedURIOverEndedURIAfterReadFailure(t *testing.T) {
	t.Parallel()

	activeURI, activePos, activeEntryID := resolveEOFState(
		"",
		0,
		44,
		errors.New("unknown error"),
		"file:///tmp/ended.mp3",
		mpvPreloadState{
			URI:      normalizePlaybackURI("file:///tmp/next.mp3"),
			EntryID:  55,
			Index:    1,
			Verified: true,
		},
	)

	if activeURI != "/tmp/next.mp3" {
		t.Fatalf("active uri = %q, want %q", activeURI, "/tmp/next.mp3")
	}
	if activePos != -1 {
		t.Fatalf("active pos = %d, want -1", activePos)
	}
	if activeEntryID != 0 {
		t.Fatalf("active entry id = %d, want 0", activeEntryID)
	}
}
