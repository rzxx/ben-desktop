package desktopcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestClassifyEventScopesNestedDirectoryToSubtree(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "artist", "album")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	watcher := &activeScanWatcher{roots: []string{filepath.Clean(root)}}
	paths, presenceRoots, artworkRoots, startupRoots := watcher.classifyEvent(fsnotify.Event{
		Name: nested,
		Op:   fsnotify.Create,
	})
	if len(paths) != 0 {
		t.Fatalf("paths = %+v, want none", paths)
	}
	if len(artworkRoots) != 0 {
		t.Fatalf("artwork roots = %+v, want none", artworkRoots)
	}
	if len(startupRoots) != 0 {
		t.Fatalf("startup roots = %+v, want none", startupRoots)
	}
	if len(presenceRoots) != 1 || scanRootKey(presenceRoots[0]) != scanRootKey(nested) {
		t.Fatalf("presence roots = %+v, want [%s]", presenceRoots, nested)
	}
}

func TestClassifyEventScopesRemovedDirectoryToRemovedPath(t *testing.T) {
	root := t.TempDir()
	removed := filepath.Join(root, "artist", "removed-album")

	watcher := &activeScanWatcher{roots: []string{filepath.Clean(root)}}
	paths, presenceRoots, artworkRoots, startupRoots := watcher.classifyEvent(fsnotify.Event{
		Name: removed,
		Op:   fsnotify.Remove,
	})
	if len(paths) != 0 {
		t.Fatalf("paths = %+v, want none", paths)
	}
	if len(artworkRoots) != 0 {
		t.Fatalf("artwork roots = %+v, want none", artworkRoots)
	}
	if len(startupRoots) != 0 {
		t.Fatalf("startup roots = %+v, want none", startupRoots)
	}
	if len(presenceRoots) != 1 || scanRootKey(presenceRoots[0]) != scanRootKey(removed) {
		t.Fatalf("presence roots = %+v, want [%s]", presenceRoots, removed)
	}
}
