package desktopcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	jobKindWatchRescan = "watch-scan"
	watchDebounceDelay = 350 * time.Millisecond
)

type activeScanWatcher struct {
	app       *App
	libraryID string
	deviceID  string
	roots     []string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	ready  chan error
}

func newActiveScanWatcher(app *App, libraryID, deviceID string, roots []string) *activeScanWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &activeScanWatcher{
		app:       app,
		libraryID: strings.TrimSpace(libraryID),
		deviceID:  strings.TrimSpace(deviceID),
		roots:     append([]string(nil), normalizedWatcherRoots(roots)...),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		ready:     make(chan error, 1),
	}
}

func (w *activeScanWatcher) start() error {
	if w == nil {
		return nil
	}
	go w.run()
	return <-w.ready
}

func (w *activeScanWatcher) stop() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
}

func (w *activeScanWatcher) sameConfig(libraryID, deviceID string, roots []string) bool {
	if w == nil {
		return false
	}
	if strings.TrimSpace(w.libraryID) != strings.TrimSpace(libraryID) {
		return false
	}
	if strings.TrimSpace(w.deviceID) != strings.TrimSpace(deviceID) {
		return false
	}
	normalized := normalizedWatcherRoots(roots)
	if len(w.roots) != len(normalized) {
		return false
	}
	for index := range normalized {
		if scanRootKey(w.roots[index]) != scanRootKey(normalized[index]) {
			return false
		}
	}
	return true
}

func (w *activeScanWatcher) run() {
	defer close(w.done)
	if w == nil || len(w.roots) == 0 {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.ready <- err
		w.app.logf("desktopcore: create scan watcher failed: %v", err)
		return
	}
	defer watcher.Close()

	watchedDirs := make(map[string]struct{})
	for _, root := range w.roots {
		if err := addRecursiveWatch(watcher, watchedDirs, root); err != nil && !errors.Is(err, os.ErrNotExist) {
			w.app.logf("desktopcore: watch root %s failed: %v", root, err)
		}
	}
	w.ready <- nil

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	var (
		timerActive  bool
		pendingRoots = make(map[string]string)
		scanWG       sync.WaitGroup
	)
	defer scanWG.Wait()

	queueRoots := func(candidates []string) {
		for _, root := range candidates {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			pendingRoots[scanRootKey(root)] = root
		}
		if len(pendingRoots) == 0 {
			return
		}
		if timerActive {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer.Reset(watchDebounceDelay)
		timerActive = true
	}

	for {
		select {
		case <-w.ctx.Done():
			return
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			if err != nil && w.ctx.Err() == nil {
				w.app.logf("desktopcore: scan watcher error: %v", err)
			}
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if err := addEventDirectoryWatch(watcher, watchedDirs, event.Name, event.Op); err != nil && w.ctx.Err() == nil && !errors.Is(err, os.ErrNotExist) {
				w.app.logf("desktopcore: update watch for %s failed: %v", event.Name, err)
			}
			if !shouldTriggerWatchRescan(event.Op) {
				continue
			}
			queueRoots(w.affectedRoots(event.Name))
		case <-timer.C:
			timerActive = false
			roots := sortedWatcherRoots(pendingRoots)
			clear(pendingRoots)
			if len(roots) == 0 {
				continue
			}
			scanWG.Add(1)
			go func(roots []string) {
				defer scanWG.Done()
				if _, err := w.app.ingest.runTrackedScan(w.ctx, w.libraryID, w.deviceID, roots, jobKindWatchRescan); err != nil && w.ctx.Err() == nil {
					w.app.logf("desktopcore: watch scan failed: %v", err)
				}
			}(roots)
		}
	}
}

func (w *activeScanWatcher) affectedRoots(path string) []string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return append([]string(nil), w.roots...)
	}
	out := make([]string, 0, len(w.roots))
	for _, root := range w.roots {
		if pathWithinRoot(path, root) || pathWithinRoot(root, path) {
			out = append(out, root)
		}
	}
	if len(out) > 0 {
		return out
	}
	return append([]string(nil), w.roots...)
}

func (a *ScannerService) syncActiveScanWatcher(ctx context.Context) error {
	if a == nil {
		return nil
	}

	local, ok, err := a.syncActiveLibraryRuntime(ctx)
	if err != nil {
		return err
	}
	if !ok {
		a.stopActiveScanWatcher()
		return nil
	}

	roots, err := a.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return err
	}
	if !canProvideLocalMedia(local.Role) || len(roots) == 0 {
		a.stopActiveScanWatcher()
		return nil
	}

	a.watcherMu.Lock()
	current := a.scanWatcher
	if current != nil && current.sameConfig(local.LibraryID, local.DeviceID, roots) {
		a.watcherMu.Unlock()
		return nil
	}
	next := newActiveScanWatcher(a.App, local.LibraryID, local.DeviceID, roots)
	a.scanWatcher = next
	a.watcherMu.Unlock()

	if current != nil {
		current.stop()
	}
	return next.start()
}

func (a *ScannerService) stopActiveScanWatcher() {
	if a == nil {
		return
	}
	a.watcherMu.Lock()
	current := a.scanWatcher
	a.scanWatcher = nil
	a.watcherMu.Unlock()
	if current != nil {
		current.stop()
	}
}

func addRecursiveWatch(watcher *fsnotify.Watcher, watchedDirs map[string]struct{}, root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return filepath.WalkDir(filepath.Dir(root), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filepath.Clean(path) != filepath.Clean(filepath.Dir(root)) {
				return filepath.SkipDir
			}
			return addSingleWatch(watcher, watchedDirs, path)
		})
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		return addSingleWatch(watcher, watchedDirs, path)
	})
}

func addEventDirectoryWatch(watcher *fsnotify.Watcher, watchedDirs map[string]struct{}, path string, op fsnotify.Op) error {
	if op&(fsnotify.Create|fsnotify.Rename) == 0 {
		return nil
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return addRecursiveWatch(watcher, watchedDirs, path)
}

func addSingleWatch(watcher *fsnotify.Watcher, watchedDirs map[string]struct{}, path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	key := scanRootKey(path)
	if _, ok := watchedDirs[key]; ok {
		return nil
	}
	if err := watcher.Add(path); err != nil {
		return err
	}
	watchedDirs[key] = struct{}{}
	return nil
}

func shouldTriggerWatchRescan(op fsnotify.Op) bool {
	return op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0
}

func sortedWatcherRoots(items map[string]string) []string {
	out := make([]string, 0, len(items))
	for _, root := range items {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		out = append(out, root)
	}
	sort.Strings(out)
	return out
}
