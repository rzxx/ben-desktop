package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoadMissingFileReturnsZeroState(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
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
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new settings store: %v", err)
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

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved settings: %v", err)
	}
	if len(payload) == 0 || payload[0] != '{' {
		t.Fatalf("expected JSON payload, got %q", string(payload))
	}
}
