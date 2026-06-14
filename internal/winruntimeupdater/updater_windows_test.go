//go:build windows

package winruntimeupdater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareRuntimeVersion(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		local  string
		want   int
	}{
		{name: "missing local updates", remote: "1", local: "", want: 1},
		{name: "same", remote: "1.2.3", local: "1.2.3", want: 0},
		{name: "newer patch", remote: "1.2.4", local: "1.2.3", want: 1},
		{name: "older major", remote: "1.9.0", local: "2.0.0", want: -1},
		{name: "monotonic integer", remote: "11", local: "2", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareRuntimeVersion(tt.remote, tt.local)
			if got != tt.want {
				t.Fatalf("compareRuntimeVersion(%q, %q) = %d, want %d", tt.remote, tt.local, got, tt.want)
			}
		})
	}
}

func TestCleanManifestPath(t *testing.T) {
	valid, err := cleanManifestPath("runtime/ffmpeg/bin/ffmpeg.exe")
	if err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	if valid == "" {
		t.Fatal("valid path cleaned to empty")
	}

	for _, path := range []string{"", "../libmpv.dll", "runtime/../../libmpv.dll", `C:\Program Files\ben\libmpv.dll`, "/tmp/libmpv.dll"} {
		t.Run(path, func(t *testing.T) {
			if _, err := cleanManifestPath(path); err == nil {
				t.Fatalf("unsafe path %q was accepted", path)
			}
		})
	}
}

func TestLocalRuntimeMatchesManifest(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeFile(t, dir, "runtime/ffmpeg/bin/ffmpeg.exe", "ffmpeg")
	writeRuntimeFile(t, dir, "libmpv.dll", "mpv")

	manifest := runtimeManifest{
		Files: []runtimeManifestFile{
			manifestFile(t, dir, "runtime/ffmpeg/bin/ffmpeg.exe"),
			manifestFile(t, dir, "libmpv.dll"),
		},
	}
	if !localRuntimeMatchesManifest(dir, manifest) {
		t.Fatal("expected complete runtime to match manifest")
	}

	if err := os.Remove(filepath.Join(dir, "libmpv.dll")); err != nil {
		t.Fatalf("remove libmpv: %v", err)
	}
	if localRuntimeMatchesManifest(dir, manifest) {
		t.Fatal("expected missing runtime file to fail manifest match")
	}
}

func writeRuntimeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func manifestFile(t *testing.T, root, rel string) runtimeManifestFile {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	sum := sha256.Sum256(body)
	return runtimeManifestFile{
		Path:   rel,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}
}
