package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
)

const (
	jobKindWatchDeltaScan = "watch-scan-delta"
	jobKindStartupScan    = "startup-scan"
)

type scanRequestClass string

const (
	scanRequestDelta   scanRequestClass = "delta"
	scanRequestStartup scanRequestClass = "startup"
	scanRequestRepair  scanRequestClass = "repair"
)

type scanCoordinatorRequest struct {
	class                 scanRequestClass
	fullRoots             []string
	deltaPaths            []string
	deltaPresenceRoots    []string
	deltaArtworkRoots     []string
	callerCtx             context.Context
	job                   *JobTracker
	callerCanceledMessage string
	canceledMessage       string
	wait                  chan scanCoordinatorResult
}

type scanCoordinatorResult struct {
	stats apitypes.ScanStats
	err   error
}

type scanCoordinator struct {
	app       *App
	libraryID string
	deviceID  string
	ctx       context.Context
	cancel    context.CancelFunc

	mu                 sync.Mutex
	pending            []scanCoordinatorRequest
	workerRunning      bool
	runningStartupKeys map[string]struct{}
}

func newScanCoordinator(app *App, libraryID, deviceID string, ctx context.Context) *scanCoordinator {
	if ctx == nil {
		ctx = context.Background()
	}
	coordCtx, cancel := context.WithCancel(ctx)
	return &scanCoordinator{
		app:       app,
		libraryID: strings.TrimSpace(libraryID),
		deviceID:  strings.TrimSpace(deviceID),
		ctx:       coordCtx,
		cancel:    cancel,
	}
}

func (c *scanCoordinator) stop() {
	if c == nil || c.cancel == nil {
		return
	}
	c.cancel()
}

func (a *App) activeScanCoordinator(ctx context.Context, libraryID, deviceID string) (*scanCoordinator, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: app is nil", errActiveLibraryRuntimeStopped)
	}

	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	local, runtime, ok, err := a.syncActiveLibraryRuntimeState(ctx)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(local.LibraryID) != libraryID || strings.TrimSpace(local.DeviceID) != deviceID {
		return nil, fmt.Errorf("%w: %s", errActiveLibraryRuntimeStopped, libraryID)
	}

	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	current := a.activeRuntime
	if current == nil || current != runtime || strings.TrimSpace(current.libraryID) != libraryID || strings.TrimSpace(current.deviceID) != deviceID {
		return nil, fmt.Errorf("%w: %s", errActiveLibraryRuntimeStopped, libraryID)
	}
	if current.scanCoordinator == nil {
		current.scanCoordinator = newScanCoordinator(a, libraryID, deviceID, current.ctx)
	}
	return current.scanCoordinator, nil
}

func (c *scanCoordinator) queueStartupFull(roots []string) error {
	if c == nil {
		return nil
	}
	roots = normalizedWatcherRoots(roots)
	if len(roots) == 0 {
		return nil
	}

	startupKey := scanCoordinatorStartupKeyForRoots(roots)
	c.mu.Lock()
	if _, ok := c.runningStartupKeys[startupKey]; ok {
		c.mu.Unlock()
		return nil
	}
	for _, req := range c.pending {
		if scanCoordinatorStartupKey(req) == startupKey {
			c.mu.Unlock()
			return nil
		}
	}
	c.mu.Unlock()

	req := scanCoordinatorRequest{
		class:     scanRequestStartup,
		fullRoots: roots,
		job:       c.newInternalJob(jobKindStartupScan, "queued startup scan"),
	}
	return c.submit(req)
}

func (c *scanCoordinator) queueWatchDelta(paths []string, presenceRoots []string, artworkRoots []string) error {
	if c == nil {
		return nil
	}
	req := scanCoordinatorRequest{
		class:              scanRequestDelta,
		deltaPaths:         normalizeScanPaths(paths),
		deltaPresenceRoots: normalizedWatcherRoots(presenceRoots),
		deltaArtworkRoots:  normalizedWatcherRoots(artworkRoots),
		job:                c.newInternalJob(jobKindWatchDeltaScan, "queued watch delta scan"),
	}
	if len(req.deltaPaths) == 0 && len(req.deltaPresenceRoots) == 0 && len(req.deltaArtworkRoots) == 0 {
		return nil
	}
	return c.submit(req)
}

func (c *scanCoordinator) submitRepair(ctx context.Context, roots []string, job *JobTracker, callerCanceledMessage, canceledMessage string) (apitypes.ScanStats, error) {
	if c == nil {
		return apitypes.ScanStats{}, nil
	}
	req := scanCoordinatorRequest{
		class:                 scanRequestRepair,
		fullRoots:             normalizedWatcherRoots(roots),
		callerCtx:             ctx,
		job:                   job,
		callerCanceledMessage: strings.TrimSpace(callerCanceledMessage),
		canceledMessage:       strings.TrimSpace(canceledMessage),
		wait:                  make(chan scanCoordinatorResult, 1),
	}
	if err := c.submit(req); err != nil {
		return apitypes.ScanStats{}, err
	}
	select {
	case <-ctx.Done():
		if canceledReq, ok := c.cancelPendingRequest(req.wait); ok {
			c.finishRequest(canceledReq, apitypes.ScanStats{}, ctx.Err())
		}
		return apitypes.ScanStats{}, ctx.Err()
	case result := <-req.wait:
		return result.stats, result.err
	}
}

func (c *scanCoordinator) submit(req scanCoordinatorRequest) error {
	if c == nil {
		return nil
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	c.pending = append(c.pending, req)
	shouldStart := !c.workerRunning
	if shouldStart {
		c.workerRunning = true
	}
	c.mu.Unlock()

	if shouldStart {
		go c.run()
	}
	return nil
}

func (c *scanCoordinator) run() {
	for {
		if err := c.ctx.Err(); err != nil {
			c.failPending(err)
			return
		}

		c.mu.Lock()
		if len(c.pending) == 0 {
			c.workerRunning = false
			c.runningStartupKeys = nil
			c.mu.Unlock()
			return
		}
		batch := c.takeNextBatchLocked()
		startupKeys := scanCoordinatorStartupKeys(batch)
		c.runningStartupKeys = startupKeys
		c.mu.Unlock()
		batch = c.filterCanceledBatch(batch)
		if len(batch) == 0 {
			c.clearRunningStartupKeys(startupKeys)
			continue
		}

		primaryJob := firstBatchJob(batch)
		for _, req := range batch {
			if req.job != nil {
				req.job.Running(0.05, "enumerating scan roots")
			}
		}

		execCtx, stop := c.batchExecutionContext(batch)
		stats, err := c.executeBatch(execCtx, primaryJob, batch)
		stop()
		c.clearRunningStartupKeys(startupKeys)
		if err == nil && len(startupKeys) > 0 && c.app != nil {
			c.app.markStartupScanSatisfied(c.libraryID, c.deviceID)
		}
		for _, req := range batch {
			c.finishRequest(req, stats, err)
		}
	}
}

func (c *scanCoordinator) takeNextBatchLocked() []scanCoordinatorRequest {
	if len(c.pending) == 0 {
		return nil
	}
	if c.pending[0].class == scanRequestRepair {
		batch := []scanCoordinatorRequest{c.pending[0]}
		c.pending = append([]scanCoordinatorRequest(nil), c.pending[1:]...)
		return batch
	}

	batch := make([]scanCoordinatorRequest, 0, len(c.pending))
	rest := make([]scanCoordinatorRequest, 0, len(c.pending))
	for _, req := range c.pending {
		if req.class == scanRequestRepair {
			rest = append(rest, req)
		} else {
			batch = append(batch, req)
		}
	}
	c.pending = rest
	return batch
}

func (c *scanCoordinator) batchExecutionContext(batch []scanCoordinatorRequest) (context.Context, func()) {
	if len(batch) == 0 || batch[0].class != scanRequestRepair || batch[0].callerCtx == nil {
		return c.ctx, func() {}
	}
	execCtx, cancel := context.WithCancel(c.ctx)
	stopLeader := context.AfterFunc(batch[0].callerCtx, cancel)
	return execCtx, func() {
		stopLeader()
		cancel()
	}
}

func (c *scanCoordinator) executeBatch(ctx context.Context, job *JobTracker, batch []scanCoordinatorRequest) (apitypes.ScanStats, error) {
	fullRoots := make(map[string]string)
	deltaPresenceRoots := make(map[string]string)
	deltaArtworkRoots := make(map[string]string)
	deltaPaths := make(map[string]string)
	for _, req := range batch {
		switch req.class {
		case scanRequestRepair, scanRequestStartup:
			for _, root := range req.fullRoots {
				fullRoots[scanRootKey(root)] = root
			}
		case scanRequestDelta:
			for _, root := range req.deltaPresenceRoots {
				deltaPresenceRoots[scanRootKey(root)] = root
			}
			for _, root := range req.deltaArtworkRoots {
				deltaArtworkRoots[scanRootKey(root)] = root
			}
			for _, path := range req.deltaPaths {
				deltaPaths[localPathKey(path)] = path
			}
		}
	}

	combined := apitypes.ScanStats{}
	if len(fullRoots) > 0 {
		roots := sortedWatcherRoots(fullRoots)
		mode := scanModeStartup
		if len(batch) > 0 && batch[0].class == scanRequestRepair {
			mode = scanModeRepair
		}
		stats, err := c.app.ingest.runFullScanPass(ctx, mode, c.libraryID, c.deviceID, roots, job)
		combined = mergeScanStats(combined, stats)
		if err != nil {
			return combined, err
		}
		for key, root := range deltaPresenceRoots {
			if rootWithinAny(root, roots) {
				delete(deltaPresenceRoots, key)
			}
		}
		for key, root := range deltaArtworkRoots {
			if rootWithinAny(root, roots) {
				delete(deltaArtworkRoots, key)
			}
		}
		for key, path := range deltaPaths {
			if pathWithinAnyRoot(path, roots) {
				delete(deltaPaths, key)
			}
		}
	}

	if len(deltaPresenceRoots) > 0 || len(deltaArtworkRoots) > 0 || len(deltaPaths) > 0 {
		scope := deltaScanScope{
			audioPaths:    sortedWatcherRoots(deltaPaths),
			presenceRoots: sortedWatcherRoots(deltaPresenceRoots),
			artworkRoots:  sortedWatcherRoots(deltaArtworkRoots),
		}
		stats, err := c.app.ingest.runDeltaScanPass(ctx, c.libraryID, c.deviceID, scope, job)
		combined = mergeScanStats(combined, stats)
		if err != nil {
			return combined, err
		}
	}

	return combined, nil
}

func (c *scanCoordinator) newInternalJob(kind, queuedMessage string) *JobTracker {
	if c == nil || c.app == nil || c.app.jobs == nil {
		return nil
	}
	jobID := kind + ":" + strings.TrimSpace(c.libraryID) + ":" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	job := c.app.jobs.Track(jobID, kind, c.libraryID)
	if job != nil {
		job.Queued(0, queuedMessage)
	}
	return job
}

func (c *scanCoordinator) failPending(err error) {
	c.mu.Lock()
	batch := append([]scanCoordinatorRequest(nil), c.pending...)
	c.pending = nil
	c.workerRunning = false
	c.runningStartupKeys = nil
	c.mu.Unlock()

	for _, req := range batch {
		c.finishRequest(req, apitypes.ScanStats{}, err)
	}
}

func (c *scanCoordinator) filterCanceledBatch(batch []scanCoordinatorRequest) []scanCoordinatorRequest {
	if len(batch) == 0 {
		return batch
	}
	active := batch[:0]
	for _, req := range batch {
		if req.callerCtx != nil {
			if err := req.callerCtx.Err(); err != nil {
				c.finishRequest(req, apitypes.ScanStats{}, err)
				continue
			}
		}
		active = append(active, req)
	}
	return active
}

func (c *scanCoordinator) cancelPendingRequest(wait chan scanCoordinatorResult) (scanCoordinatorRequest, bool) {
	if c == nil || wait == nil {
		return scanCoordinatorRequest{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for index, req := range c.pending {
		if req.wait != wait {
			continue
		}
		c.pending = append(c.pending[:index], c.pending[index+1:]...)
		return req, true
	}
	return scanCoordinatorRequest{}, false
}

func (c *scanCoordinator) clearRunningStartupKeys(keys map[string]struct{}) {
	if len(keys) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.runningStartupKeys) == 0 {
		return
	}
	for key := range keys {
		delete(c.runningStartupKeys, key)
	}
	if len(c.runningStartupKeys) == 0 {
		c.runningStartupKeys = nil
	}
}

func (c *scanCoordinator) finishRequest(req scanCoordinatorRequest, stats apitypes.ScanStats, err error) {
	if req.job != nil {
		if err != nil {
			message := strings.TrimSpace(req.canceledMessage)
			if req.callerCtx != nil && req.callerCtx.Err() != nil {
				if callerMessage := strings.TrimSpace(req.callerCanceledMessage); callerMessage != "" {
					message = callerMessage
				}
			}
			if message == "" {
				message = "scan canceled"
			}
			if errors.Is(err, context.Canceled) {
				req.job.Fail(1, message, nil)
			} else {
				req.job.Fail(1, "scan failed", err)
			}
		} else {
			req.job.Complete(1, scanCompletionMessage(stats))
		}
	}
	if req.wait != nil {
		req.wait <- scanCoordinatorResult{stats: stats, err: err}
	}
}

func firstBatchJob(batch []scanCoordinatorRequest) *JobTracker {
	for _, req := range batch {
		if req.job != nil {
			return req.job
		}
	}
	return nil
}

func scanCoordinatorStartupKey(req scanCoordinatorRequest) string {
	if req.class != scanRequestStartup {
		return ""
	}
	return scanCoordinatorStartupKeyForRoots(req.fullRoots)
}

func scanCoordinatorStartupKeyForRoots(roots []string) string {
	roots = normalizedWatcherRoots(roots)
	if len(roots) == 0 {
		return ""
	}
	return strings.Join(roots, "|")
}

func scanCoordinatorStartupKeys(batch []scanCoordinatorRequest) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, req := range batch {
		key := scanCoordinatorStartupKey(req)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func normalizeScanPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]string, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		seen[localPathKey(path)] = path
	}
	return sortedWatcherRoots(seen)
}

func rootWithinAny(root string, parents []string) bool {
	for _, parent := range parents {
		if pathWithinRoot(root, parent) {
			return true
		}
	}
	return false
}

func pathWithinAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if pathWithinRoot(path, root) {
			return true
		}
	}
	return false
}

func mergeScanStats(left, right apitypes.ScanStats) apitypes.ScanStats {
	return apitypes.ScanStats{
		Scanned:          left.Scanned + right.Scanned,
		Imported:         left.Imported + right.Imported,
		SkippedUnchanged: left.SkippedUnchanged + right.SkippedUnchanged,
		Errors:           left.Errors + right.Errors,
	}
}
