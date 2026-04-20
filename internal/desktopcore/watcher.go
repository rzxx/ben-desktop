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

	apitypes "ben/desktop/api/types"
	"github.com/fsnotify/fsnotify"
)

const (
	watchDebounceDelay = 350 * time.Millisecond
)

var newFSNotifyWatcher = fsnotify.NewWatcher

type activeScanWatcher struct {
	app       *App
	coord     *scanCoordinator
	libraryID string
	deviceID  string
	roots     []string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	ready  chan error
}

func newActiveScanWatcher(app *App, coord *scanCoordinator, libraryID, deviceID string, roots []string) *activeScanWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &activeScanWatcher{
		app:       app,
		coord:     coord,
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
	if w == nil {
		return
	}
	defer close(w.done)
	if len(w.roots) == 0 {
		return
	}

	watcher, err := newFSNotifyWatcher()
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
		timerActive          bool
		pendingPresenceRoots = make(map[string]string)
		pendingArtworkRoots  = make(map[string]string)
		pendingStartupRoots  = make(map[string]string)
		pendingPaths         = make(map[string]string)
		scanWG               sync.WaitGroup
	)
	defer scanWG.Wait()

	queueDelta := func(paths []string, presenceRoots []string, artworkRoots []string, startupRoots []string) {
		for _, path := range paths {
			path = filepath.Clean(strings.TrimSpace(path))
			if path == "" {
				continue
			}
			pendingPaths[localPathKey(path)] = path
		}
		for _, root := range presenceRoots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			pendingPresenceRoots[scanRootKey(root)] = root
		}
		for _, root := range artworkRoots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			pendingArtworkRoots[scanRootKey(root)] = root
		}
		for _, root := range startupRoots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			pendingStartupRoots[scanRootKey(root)] = root
		}
		if len(pendingPaths) == 0 && len(pendingPresenceRoots) == 0 && len(pendingArtworkRoots) == 0 && len(pendingStartupRoots) == 0 {
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
			paths, presenceRoots, artworkRoots, startupRoots := w.classifyEvent(event)
			queueDelta(paths, presenceRoots, artworkRoots, startupRoots)
		case <-timer.C:
			timerActive = false
			paths := sortedWatcherRoots(pendingPaths)
			presenceRoots := sortedWatcherRoots(pendingPresenceRoots)
			artworkRoots := sortedWatcherRoots(pendingArtworkRoots)
			startupRoots := sortedWatcherRoots(pendingStartupRoots)
			clear(pendingPaths)
			clear(pendingPresenceRoots)
			clear(pendingArtworkRoots)
			clear(pendingStartupRoots)
			if len(paths) == 0 && len(presenceRoots) == 0 && len(artworkRoots) == 0 && len(startupRoots) == 0 {
				continue
			}
			scanWG.Add(1)
			go func(paths []string, presenceRoots []string, artworkRoots []string, startupRoots []string) {
				defer scanWG.Done()
				if w.coord == nil {
					return
				}
				if len(startupRoots) > 0 {
					if err := w.coord.queueStartupFull(startupRoots); err != nil && w.ctx.Err() == nil {
						w.app.logf("desktopcore: startup scan submission failed: %v", err)
					}
					paths = prunePathsUnderRoots(paths, startupRoots)
					presenceRoots = pruneRootsUnderRoots(presenceRoots, startupRoots)
					artworkRoots = pruneRootsUnderRoots(artworkRoots, startupRoots)
				}
				if err := w.coord.queueWatchDelta(paths, presenceRoots, artworkRoots); err != nil && w.ctx.Err() == nil {
					w.app.logf("desktopcore: watch scan submission failed: %v", err)
				}
			}(paths, presenceRoots, artworkRoots, startupRoots)
		}
	}
}

func (w *activeScanWatcher) classifyEvent(event fsnotify.Event) ([]string, []string, []string, []string) {
	if w == nil {
		return nil, nil, nil, nil
	}
	path := filepath.Clean(strings.TrimSpace(event.Name))
	if path == "" {
		return nil, nil, nil, append([]string(nil), w.roots...)
	}

	affectedRoots := w.presenceRootsForPath(path)
	artworkRoots := w.artworkRootsForPath(path)
	startupRoots := w.startupRootsForPath(path)
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		if isAudioPath(path) {
			return []string{path}, nil, nil, nil
		}
		if isArtworkSidecarPath(path) {
			return nil, nil, artworkRoots, nil
		}
		return nil, affectedRoots, nil, nil
	}
	if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
		info, err := os.Stat(path)
		switch {
		case err == nil && info.IsDir():
			return nil, affectedRoots, nil, startupRoots
		case isAudioPath(path):
			return []string{path}, nil, nil, nil
		case isArtworkSidecarPath(path):
			return nil, nil, artworkRoots, nil
		case err == nil:
			return nil, nil, nil, nil
		case errors.Is(err, os.ErrNotExist) && isAudioPath(path):
			return []string{path}, nil, nil, nil
		case errors.Is(err, os.ErrNotExist) && isArtworkSidecarPath(path):
			return nil, nil, artworkRoots, nil
		default:
			return nil, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *activeScanWatcher) presenceRootsForPath(path string) []string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return append([]string(nil), w.roots...)
	}

	out := make(map[string]string, len(w.roots))
	for _, root := range w.roots {
		switch {
		case scanRootKey(path) == scanRootKey(root):
			out[scanRootKey(root)] = root
		case pathWithinRoot(path, root):
			out[scanRootKey(path)] = path
		case pathWithinRoot(root, path):
			out[scanRootKey(root)] = root
		}
	}
	if len(out) > 0 {
		return sortedWatcherRoots(out)
	}
	return nil
}

func (w *activeScanWatcher) artworkRootsForPath(path string) []string {
	if w == nil || !isArtworkSidecarPath(path) {
		return nil
	}
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(path)))
	if dir == "" {
		return nil
	}
	for _, root := range w.roots {
		if pathWithinRoot(dir, root) {
			return []string{dir}
		}
	}
	return nil
}

func (w *activeScanWatcher) startupRootsForPath(path string) []string {
	if w == nil {
		return nil
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	out := make(map[string]string, len(w.roots))
	for _, root := range w.roots {
		switch {
		case scanRootKey(root) == scanRootKey(path):
			out[scanRootKey(root)] = root
		case pathWithinRoot(path, root):
			out[scanRootKey(path)] = path
		}
	}
	if len(out) == 0 {
		return nil
	}
	return sortedWatcherRoots(out)
}

func (a *ScannerService) syncActiveScanWatcher(ctx context.Context) error {
	if a == nil {
		return nil
	}

	local, runtime, ok, err := a.syncActiveLibraryRuntimeState(ctx)
	if err != nil {
		return err
	}
	if !ok {
		a.stopActiveScanWatcher()
		return nil
	}
	return a.syncRuntime(ctx, local, runtime)
}

func (a *ScannerService) resetRuntime(libraryID, deviceID string) {
	if a == nil {
		return
	}

	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)

	a.runtimeMu.Lock()
	runtime := a.activeRuntime
	if runtime == nil || strings.TrimSpace(runtime.libraryID) != libraryID || strings.TrimSpace(runtime.deviceID) != deviceID {
		a.runtimeMu.Unlock()
		return
	}
	currentWatcher := runtime.scanWatcher
	currentCoordinator := runtime.scanCoordinator
	runtime.scanWatcher = nil
	runtime.scanCoordinator = nil
	runtime.startupScanPending = true
	a.runtimeMu.Unlock()

	if currentCoordinator != nil {
		currentCoordinator.stop()
	}
	if currentWatcher != nil {
		currentWatcher.stop()
	}
}

func (a *ScannerService) syncRuntime(ctx context.Context, local apitypes.LocalContext, runtime *activeLibraryRuntime) error {
	if a == nil {
		return nil
	}
	if runtime == nil {
		a.stopActiveScanWatcher()
		return nil
	}

	roots, err := a.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return err
	}
	if !canProvideLocalMedia(local.Role) || len(roots) == 0 {
		a.runtimeMu.Lock()
		current := runtime.scanWatcher
		runtime.scanWatcher = nil
		a.runtimeMu.Unlock()
		if current != nil {
			current.stop()
		}
		return nil
	}

	a.runtimeMu.Lock()
	if runtime.scanCoordinator == nil {
		runtime.scanCoordinator = newScanCoordinator(a.App, local.LibraryID, local.DeviceID, runtime.ctx)
	}
	coord := runtime.scanCoordinator
	current := runtime.scanWatcher
	configMatches := current != nil && current.sameConfig(local.LibraryID, local.DeviceID, roots)
	shouldQueueStartup := runtime.startupScanPending || !configMatches
	if configMatches {
		a.runtimeMu.Unlock()
		if shouldQueueStartup {
			return coord.queueStartupFull(roots)
		}
		return nil
	}
	next := newActiveScanWatcher(a.App, coord, local.LibraryID, local.DeviceID, roots)
	a.runtimeMu.Unlock()

	if err := next.start(); err != nil {
		return err
	}
	a.runtimeMu.Lock()
	active := a.activeRuntime
	if active != runtime {
		a.runtimeMu.Unlock()
		next.stop()
		return errActiveLibraryRuntimeStopped
	}
	previous := runtime.scanWatcher
	runtime.scanWatcher = next
	a.runtimeMu.Unlock()

	if previous != nil {
		previous.stop()
	}
	if shouldQueueStartup {
		return coord.queueStartupFull(roots)
	}
	return nil
}

func (a *ScannerService) stopActiveScanWatcher() {
	if a == nil {
		return
	}
	a.runtimeMu.Lock()
	var current *activeScanWatcher
	if a.activeRuntime != nil {
		current = a.activeRuntime.scanWatcher
		a.activeRuntime.scanWatcher = nil
	}
	a.runtimeMu.Unlock()
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
		if errors.Is(err, os.ErrNotExist) {
			fallback := nearestExistingWatchPath(root)
			if fallback == "" {
				return err
			}
			return addSingleWatch(watcher, watchedDirs, fallback)
		}
		return err
	}
	if !info.IsDir() {
		return addSingleWatch(watcher, watchedDirs, filepath.Dir(root))
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

func nearestExistingWatchPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	for path != "" {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	return ""
}

func prunePathsUnderRoots(paths, roots []string) []string {
	if len(paths) == 0 || len(roots) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if pathWithinAnyRoot(path, roots) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func pruneRootsUnderRoots(candidates, roots []string) []string {
	if len(candidates) == 0 || len(roots) == 0 {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if rootWithinAny(candidate, roots) {
			continue
		}
		out = append(out, candidate)
	}
	return out
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
