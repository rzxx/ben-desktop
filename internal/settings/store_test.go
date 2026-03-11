package settings

import (
	"path/filepath"
	"testing"
)

func TestStoreLoadMissingRowReturnsZeroState(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	state, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if state != (State{}) {
		t.Fatalf("expected zero state, got %#v", state)
	}
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	input := State{
		Core: CoreRuntimeSettings{
			DBPath:           "  C:\\ben\\library.db  ",
			BlobRoot:         "  C:\\ben\\blobs  ",
			IdentityKeyPath:  "  C:\\ben\\identity.key  ",
			FFmpegPath:       "  C:\\tools\\ffmpeg.exe  ",
			TranscodeProfile: "  desktop  ",
		},
	}

	if err := store.Save(input); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	state, err := store.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	want := State{
		Core: CoreRuntimeSettings{
			DBPath:           "C:\\ben\\library.db",
			BlobRoot:         "C:\\ben\\blobs",
			IdentityKeyPath:  "C:\\ben\\identity.key",
			FFmpegPath:       "C:\\tools\\ffmpeg.exe",
			TranscodeProfile: "desktop",
		},
	}
	if state != want {
		t.Fatalf("expected %#v, got %#v", want, state)
	}
}
