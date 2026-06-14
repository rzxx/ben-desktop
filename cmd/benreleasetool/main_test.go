package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectRuntimeEntries_IdempotentSameSource(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)

	mk := func(rel, content string) {
		dir := filepath.Join(root, filepath.Dir(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create dir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatalf("write file %s: %v", rel, err)
		}
	}

	// Empty directories walked by addTree.
	if err := os.MkdirAll(filepath.Join(root, "ffmpeg"), 0o755); err != nil {
		t.Fatalf("create ffmpeg dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "licenses"), 0o755); err != nil {
		t.Fatalf("create licenses dir: %v", err)
	}

	// This file is added both by addTree(licenses) and by the explicit require
	// loop with the same source path. The duplicate should be idempotent.
	mk(filepath.Join("licenses", "media-runtime-source-record.json"), "{}")

	// Other files required by the explicit require loop.
	mk(filepath.Join("licenses", "media-runtime-build.txt"), "")
	mk(filepath.Join("licenses", "ffmpeg-buildconf.txt"), "")
	mk(filepath.Join("licenses", "mpv-meson-configure.txt"), "")
	mk(filepath.Join("licenses", "ffmpeg-local-changes.diff"), "")
	mk(filepath.Join("licenses", "mpv-local-changes.diff"), "")

	// Required top-level files for require=true.
	mk("LICENSE", "license")
	mk("THIRD_PARTY_NOTICES.md", "notices")
	mk(filepath.Join("docs", "dependency-sources.md"), "sources")
	mk(filepath.Join("build", "deps", "manifest.json"), "[]")

	entries, err := collectRuntimeEntries(root, "v1.0.0", true)
	if err != nil {
		t.Fatalf("collectRuntimeEntries failed: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.target == "licenses/media-runtime/media-runtime-source-record.json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected media-runtime-source-record.json entry not found")
	}
}

func TestCollectRuntimeEntries_DifferentSourcesSameTarget(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)

	mk := func(rel, content string) {
		dir := filepath.Join(root, filepath.Dir(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create dir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatalf("write file %s: %v", rel, err)
		}
	}

	// Two different DLLs with the same basename map to the same target via the
	// glob patterns in collectRuntimeEntries.
	mk("foo.dll", "a")
	mk(filepath.Join("mpv", "foo.dll"), "b")

	_, err := collectRuntimeEntries(root, "v1.0.0", false)
	if err == nil {
		t.Fatalf("expected duplicate target error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate runtime bundle target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
}
