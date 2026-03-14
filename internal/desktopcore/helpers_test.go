package desktopcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileURIFromPathReturnsCanonicalFileURL(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "track.m4a")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}

	got, err := fileURIFromPath(path)
	if err != nil {
		t.Fatalf("file uri from path: %v", err)
	}
	if !strings.HasPrefix(got, "file:///") {
		t.Fatalf("file uri = %q, want file:/// prefix", got)
	}
}
