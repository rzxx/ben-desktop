package desktopcore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
)

type managedSyncTransport interface {
	SyncTransport
	LocalPeerID() string
	ListenAddrs() []string
	Close() error
}

type transportPeerIdentityResolver interface {
	ResolvePeerByIdentity(ctx context.Context, local apitypes.LocalContext, peerID, deviceID string) (SyncPeer, error)
}

type transportFactory func(context.Context, apitypes.LocalContext) (managedSyncTransport, error)

type TransportService struct {
	app                *App
	factory            transportFactory
	backgroundInterval time.Duration
	eventSyncDebounce  time.Duration
	peerRetryDelay     time.Duration
	peerRetryMaxDelay  time.Duration
	peerRetryMaxCount  int

	mu         sync.RWMutex
	scheduleMu sync.Mutex

	catchupRunHook               func(*activeTransportRuntime, apitypes.NetworkSyncReason)
	catchupPeerRunHook           func(*activeTransportRuntime, SyncPeer, apitypes.NetworkSyncReason)
	peerUpdateBroadcastHook      func(*activeTransportRuntime)
	checkpointMaintenanceRunHook func(*activeTransportRuntime)
}

type transportTaskLaneState struct {
	timer            *time.Timer
	running          bool
	rerun            bool
	full             bool
	fullRetryAttempt int
	peerRetries      map[string]transportPeerUpdateRetryState
	peerRetryTimers  map[string]*time.Timer
}

type transportPeerUpdateRetryState struct {
	address  string
	deviceID string
	peerID   string
	attempt  int
}

type transportCatchupPeerRequest struct {
	reason apitypes.NetworkSyncReason
	peer   SyncPeer
}

type transportCatchupRequestSet struct {
	all       bool
	allReason apitypes.NetworkSyncReason
	peers     map[string]transportCatchupPeerRequest
}

type transportCatchupRequest struct {
	reason apitypes.NetworkSyncReason
	peer   SyncPeer
}

type transportCatchupLaneState struct {
	timer     *time.Timer
	running   bool
	ready     transportCatchupRequestSet
	scheduled transportCatchupRequestSet
}

type activeTransportRuntime struct {
	libraryID string
	deviceID  string
	transport managedSyncTransport
	ctx       context.Context
	cancel    context.CancelFunc
	state     apitypes.NetworkSyncState

	catchupLane    transportCatchupLaneState
	peerUpdateLane transportTaskLaneState
	checkpointLane transportTaskLaneState
}

func newTransportService(app *App) *TransportService {
	return &TransportService{
		app:                app,
		factory:            app.newLibp2pSyncTransport,
		backgroundInterval: 15 * time.Second,
		eventSyncDebounce:  400 * time.Millisecond,
		peerRetryDelay:     5 * time.Second,
		peerRetryMaxDelay:  time.Minute,
		peerRetryMaxCount:  5,
	}
}

func (s *TransportService) SyncTransport() SyncTransport {
	if s == nil {
		return nil
	}
	runtime := s.activeRuntime()
	if runtime == nil {
		return nil
	}
	return runtime.transport
}

func (s *TransportService) Running() bool {
	if s == nil {
		return false
	}
	runtime := s.activeRuntime()
	return runtime != nil && runtime.transport != nil && strings.TrimSpace(runtime.transport.LocalPeerID()) != ""
}

func (s *TransportService) ListenAddrs() []string {
	if s == nil {
		return nil
	}
	runtime := s.activeRuntime()
	if runtime == nil || runtime.transport == nil {
		return nil
	}
	return append([]string(nil), runtime.transport.ListenAddrs()...)
}

func (s *TransportService) Stop() {
	if s == nil {
		return
	}
	current := s.activeRuntime()
	if s.app != nil {
		s.app.runtimeMu.Lock()
		if s.app.activeRuntime != nil && s.app.activeRuntime.transportRuntime == current {
			s.app.activeRuntime.transportRuntime = nil
		}
		s.app.runtimeMu.Unlock()
	}
	s.stopRuntime(current)
}

func (s *TransportService) syncRuntime(ctx context.Context, local apitypes.LocalContext, runtime *activeLibraryRuntime) error {
	if s == nil || s.app == nil {
		return nil
	}
	if runtime == nil {
		s.Stop()
		return nil
	}
	if s.hasTransportOverride() {
		s.Stop()
		return nil
	}

	s.app.runtimeMu.Lock()
	current := runtime.transportRuntime
	if current != nil &&
		strings.TrimSpace(current.libraryID) == strings.TrimSpace(local.LibraryID) &&
		strings.TrimSpace(current.deviceID) == strings.TrimSpace(local.DeviceID) &&
		current.transport != nil {
		s.app.runtimeMu.Unlock()
		return nil
	}
	s.app.runtimeMu.Unlock()

	next, err := s.factory(ctx, local)
	if err != nil {
		return err
	}

	runtimeCtx, cancel := context.WithCancel(runtime.ctx)
	nextRuntime := &activeTransportRuntime{
		libraryID: strings.TrimSpace(local.LibraryID),
		deviceID:  strings.TrimSpace(local.DeviceID),
		transport: next,
		ctx:       runtimeCtx,
		cancel:    cancel,
		state: apitypes.NetworkSyncState{
			Mode: apitypes.NetworkSyncModeIdle,
		},
	}
	s.app.runtimeMu.Lock()
	if s.app.activeRuntime != runtime {
		s.app.runtimeMu.Unlock()
		cancel()
		_ = next.Close()
		return nil
	}
	current = runtime.transportRuntime
	runtime.transportRuntime = nextRuntime
	s.app.runtimeMu.Unlock()

	s.stopRuntime(current)
	s.scheduleRuntimeCatchup(nextRuntime, apitypes.NetworkSyncReasonStartup, 0)
	go s.runBackgroundLoop(nextRuntime)
	return nil
}

func (s *TransportService) stopRuntime(runtime *activeTransportRuntime) {
	if runtime == nil {
		return
	}
	s.clearScheduledRuntime(runtime)
	if runtime.cancel != nil {
		runtime.cancel()
	}
	if runtime.transport != nil {
		_ = runtime.transport.Close()
	}
}

func (s *TransportService) runBackgroundLoop(runtime *activeTransportRuntime) {
	if s == nil || runtime == nil {
		return
	}
	interval := s.backgroundInterval
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-runtime.ctx.Done():
			return
		case <-ticker.C:
			if !s.isActiveRuntime(runtime) || runtime.ctx.Err() != nil {
				return
			}
			s.scheduleRuntimeCatchup(runtime, apitypes.NetworkSyncReasonTimer, 0)
		}
	}
}

func (s *TransportService) noteLocalLibraryMutation(libraryID string) {
	if s == nil || s.app == nil {
		return
	}
	runtime := s.activeRuntimeForLibrary(libraryID)
	if runtime == nil {
		return
	}
	s.scheduleRuntimePeerUpdateBroadcast(runtime, s.eventSyncDebounce)
	s.scheduleRuntimeCheckpointMaintenance(runtime, s.eventSyncDebounce)
}

func (s *TransportService) scheduleRuntimeCatchup(runtime *activeTransportRuntime, reason apitypes.NetworkSyncReason, delay time.Duration) {
	s.scheduleRuntimeCatchupPeer(runtime, reason, nil, delay)
}

func (s *TransportService) scheduleRuntimeCatchupPeer(runtime *activeTransportRuntime, reason apitypes.NetworkSyncReason, peer SyncPeer, delay time.Duration) {
	if s == nil || runtime == nil {
		return
	}
	s.scheduleMu.Lock()
	lane := &runtime.catchupLane
	if delay > 0 {
		mergeTransportCatchupRequestSet(&lane.scheduled, reason, peer)
		if lane.timer != nil {
			lane.timer.Stop()
		}
		lane.timer = time.AfterFunc(delay, func() {
			s.triggerRuntimeCatchup(runtime)
		})
		s.scheduleMu.Unlock()
		return
	}
	if peer == nil {
		if lane.timer != nil {
			lane.timer.Stop()
			lane.timer = nil
		}
		clearTransportCatchupRequestSet(&lane.scheduled)
	}
	mergeTransportCatchupRequestSet(&lane.ready, reason, peer)
	if lane.running {
		s.scheduleMu.Unlock()
		return
	}
	req, ok := popTransportCatchupRequestSet(&lane.ready)
	if !ok {
		s.scheduleMu.Unlock()
		return
	}
	lane.running = true
	s.scheduleMu.Unlock()
	s.startRuntimeCatchup(runtime, req)
}

func (s *TransportService) triggerRuntimeCatchup(runtime *activeTransportRuntime) {
	if s == nil || runtime == nil {
		return
	}

	s.scheduleMu.Lock()
	lane := &runtime.catchupLane
	if lane.timer != nil {
		lane.timer.Stop()
		lane.timer = nil
	}
	mergeTransportCatchupRequestSets(&lane.ready, &lane.scheduled)
	clearTransportCatchupRequestSet(&lane.scheduled)
	if lane.running {
		s.scheduleMu.Unlock()
		return
	}
	req, ok := popTransportCatchupRequestSet(&lane.ready)
	if !ok {
		s.scheduleMu.Unlock()
		return
	}
	lane.running = true
	s.scheduleMu.Unlock()
	s.startRuntimeCatchup(runtime, req)
}

func (s *TransportService) startRuntimeCatchup(runtime *activeTransportRuntime, req transportCatchupRequest) {
	go func() {
		defer func() {
			s.scheduleMu.Lock()
			lane := &runtime.catchupLane
			lane.running = false
			next, ok := popTransportCatchupRequestSet(&lane.ready)
			if ok {
				lane.running = true
			}
			s.scheduleMu.Unlock()

			if ok && runtime.ctx.Err() == nil {
				s.startRuntimeCatchup(runtime, next)
			}
		}()

		if !s.isActiveRuntime(runtime) || runtime.ctx.Err() != nil {
			return
		}
		s.runRuntimeCatchup(runtime, req.reason, req.peer)
	}()
}

func (s *TransportService) scheduleRuntimePeerUpdateBroadcast(runtime *activeTransportRuntime, delay time.Duration) {
	if s == nil || runtime == nil {
		return
	}
	s.scheduleMu.Lock()
	lane := &runtime.peerUpdateLane
	lane.full = true
	lane.fullRetryAttempt = 0
	clearTransportPeerUpdateRetries(&lane.peerRetries)
	clearTransportPeerUpdateRetryTimers(&lane.peerRetryTimers)
	s.scheduleMu.Unlock()
	s.scheduleRuntimeTask(runtime, &runtime.peerUpdateLane, delay, func() {
		s.runRuntimePeerUpdateBroadcast(runtime)
	})
}

func (s *TransportService) scheduleRuntimePeerUpdateFullRetry(runtime *activeTransportRuntime, attempt int) {
	if s == nil || runtime == nil {
		return
	}
	delay, ok := s.peerUpdateRetryDelay(attempt)
	if !ok {
		if s.app != nil {
			s.app.logf("desktopcore: suppressing further library change retries for %s after %d failed attempts", runtime.libraryID, attempt)
		}
		return
	}
	s.scheduleMu.Lock()
	lane := &runtime.peerUpdateLane
	if lane.full {
		s.scheduleMu.Unlock()
		return
	}
	lane.full = true
	lane.fullRetryAttempt = attempt
	clearTransportPeerUpdateRetries(&lane.peerRetries)
	clearTransportPeerUpdateRetryTimers(&lane.peerRetryTimers)
	s.scheduleMu.Unlock()
	s.scheduleRuntimeTask(runtime, &runtime.peerUpdateLane, delay, func() {
		s.runRuntimePeerUpdateBroadcast(runtime)
	})
}

func (s *TransportService) scheduleRuntimePeerUpdateRetries(runtime *activeTransportRuntime, retries map[string]transportPeerUpdateRetryState) {
	if s == nil || runtime == nil || len(retries) == 0 {
		return
	}

	for key, retry := range retries {
		delay, ok := s.peerUpdateRetryDelay(retry.attempt)
		if !ok {
			if s.app != nil {
				s.app.logf("desktopcore: suppressing further library change retries for peer %s after %d failed attempts", transportPeerUpdateRetryLabel(retry), retry.attempt)
			}
			continue
		}
		s.scheduleRuntimePeerUpdateRetry(runtime, key, retry, delay)
	}
}

func (s *TransportService) scheduleRuntimePeerUpdateRetry(runtime *activeTransportRuntime, key string, retry transportPeerUpdateRetryState, delay time.Duration) {
	if s == nil || runtime == nil || strings.TrimSpace(key) == "" {
		return
	}

	s.scheduleMu.Lock()
	lane := &runtime.peerUpdateLane
	if lane.full {
		s.scheduleMu.Unlock()
		return
	}
	if lane.peerRetryTimers == nil {
		lane.peerRetryTimers = make(map[string]*time.Timer)
	}
	if timer, ok := lane.peerRetryTimers[key]; ok {
		timer.Stop()
	}
	lane.peerRetryTimers[key] = time.AfterFunc(delay, func() {
		s.triggerRuntimePeerUpdateRetry(runtime, key, retry)
	})
	s.scheduleMu.Unlock()
}

func (s *TransportService) triggerRuntimePeerUpdateRetry(runtime *activeTransportRuntime, key string, retry transportPeerUpdateRetryState) {
	if s == nil || runtime == nil || strings.TrimSpace(key) == "" {
		return
	}

	s.scheduleMu.Lock()
	lane := &runtime.peerUpdateLane
	delete(lane.peerRetryTimers, key)
	if lane.full {
		s.scheduleMu.Unlock()
		return
	}
	mergeTransportPeerUpdateRetries(&lane.peerRetries, map[string]transportPeerUpdateRetryState{key: retry})
	if lane.running {
		lane.rerun = true
		s.scheduleMu.Unlock()
		return
	}
	lane.running = true
	s.scheduleMu.Unlock()
	s.startRuntimeTask(runtime, lane, func() {
		s.runRuntimePeerUpdateBroadcast(runtime)
	})
}

func (s *TransportService) peerUpdateRetryDelay(attempt int) (time.Duration, bool) {
	if attempt <= 0 {
		return 0, false
	}
	maxCount := s.peerRetryMaxCount
	if maxCount <= 0 {
		maxCount = 1
	}
	if attempt >= maxCount {
		return 0, false
	}
	delay := s.peerRetryDelay
	if delay <= 0 {
		delay = time.Second
	}
	for step := 1; step < attempt; step++ {
		if delay >= s.peerRetryMaxDelay && s.peerRetryMaxDelay > 0 {
			return s.peerRetryMaxDelay, true
		}
		delay *= 2
	}
	if maxDelay := s.peerRetryMaxDelay; maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	return delay, true
}

func (s *TransportService) claimRuntimePeerUpdateWork(runtime *activeTransportRuntime) (bool, int, map[string]transportPeerUpdateRetryState) {
	if s == nil || runtime == nil {
		return false, 0, nil
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	lane := &runtime.peerUpdateLane
	full := lane.full
	fullRetryAttempt := lane.fullRetryAttempt
	retries := cloneTransportPeerUpdateRetries(lane.peerRetries)
	lane.full = false
	lane.fullRetryAttempt = 0
	clearTransportPeerUpdateRetries(&lane.peerRetries)
	return full, fullRetryAttempt, retries
}

func (s *TransportService) scheduleRuntimeCheckpointMaintenance(runtime *activeTransportRuntime, delay time.Duration) {
	s.scheduleRuntimeTask(runtime, &runtime.checkpointLane, delay, func() {
		s.runRuntimeCheckpointMaintenance(runtime)
	})
}

func (s *TransportService) scheduleRuntimeTask(runtime *activeTransportRuntime, lane *transportTaskLaneState, delay time.Duration, run func()) {
	if s == nil || runtime == nil {
		return
	}
	s.scheduleMu.Lock()
	if delay > 0 {
		if lane.timer != nil {
			lane.timer.Stop()
		}
		lane.timer = time.AfterFunc(delay, func() {
			s.triggerRuntimeTask(runtime, lane, run)
		})
		s.scheduleMu.Unlock()
		return
	}
	if lane.timer != nil {
		lane.timer.Stop()
		lane.timer = nil
	}
	if lane.running {
		lane.rerun = true
		s.scheduleMu.Unlock()
		return
	}
	lane.running = true
	s.scheduleMu.Unlock()
	s.startRuntimeTask(runtime, lane, run)
}

func (s *TransportService) triggerRuntimeTask(runtime *activeTransportRuntime, lane *transportTaskLaneState, run func()) {
	if s == nil || runtime == nil {
		return
	}
	s.scheduleMu.Lock()
	if lane.timer != nil {
		lane.timer.Stop()
		lane.timer = nil
	}
	if lane.running {
		lane.rerun = true
		s.scheduleMu.Unlock()
		return
	}
	lane.running = true
	s.scheduleMu.Unlock()
	s.startRuntimeTask(runtime, lane, run)
}

func (s *TransportService) startRuntimeTask(runtime *activeTransportRuntime, lane *transportTaskLaneState, run func()) {
	go func() {
		defer func() {
			s.scheduleMu.Lock()
			rerun := lane.rerun
			lane.running = false
			lane.rerun = false
			s.scheduleMu.Unlock()

			if rerun && runtime.ctx.Err() == nil {
				s.scheduleRuntimeTask(runtime, lane, 0, run)
			}
		}()

		if !s.isActiveRuntime(runtime) || runtime.ctx.Err() != nil {
			return
		}
		run()
	}()
}

func (s *TransportService) clearScheduledRuntime(runtime *activeTransportRuntime) {
	if s == nil || runtime == nil {
		return
	}

	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	for _, timer := range []*time.Timer{
		runtime.catchupLane.timer,
		runtime.peerUpdateLane.timer,
		runtime.checkpointLane.timer,
	} {
		if timer != nil {
			timer.Stop()
		}
	}
	runtime.catchupLane.timer = nil
	clearTransportCatchupRequestSet(&runtime.catchupLane.ready)
	clearTransportCatchupRequestSet(&runtime.catchupLane.scheduled)
	runtime.peerUpdateLane.timer = nil
	runtime.peerUpdateLane.rerun = false
	runtime.peerUpdateLane.full = false
	runtime.peerUpdateLane.fullRetryAttempt = 0
	clearTransportPeerUpdateRetries(&runtime.peerUpdateLane.peerRetries)
	clearTransportPeerUpdateRetryTimers(&runtime.peerUpdateLane.peerRetryTimers)
	runtime.checkpointLane.timer = nil
	runtime.checkpointLane.rerun = false
}

func (s *TransportService) runRuntimeCatchup(runtime *activeTransportRuntime, reason apitypes.NetworkSyncReason, peer SyncPeer) {
	if s == nil || s.app == nil || runtime == nil {
		return
	}
	if s.catchupPeerRunHook != nil {
		s.catchupPeerRunHook(runtime, peer, reason)
		s.scheduleRuntimeCheckpointMaintenance(runtime, s.eventSyncDebounce)
		return
	}
	if s.catchupRunHook != nil {
		s.catchupRunHook(runtime, reason)
		s.scheduleRuntimeCheckpointMaintenance(runtime, s.eventSyncDebounce)
		return
	}
	local, ok := s.runtimeLocalContext(runtime)
	if !ok {
		return
	}
	s.beginRuntimeSync(runtime, reason)
	var err error
	if peer != nil {
		_, err = s.app.syncPeerCatchup(runtime.ctx, local, peer, reason, nil)
	} else {
		err = s.app.catchupAllPeers(runtime.ctx, local, reason, nil, false)
	}
	if err != nil && s.app.cfg.Logger != nil && runtime.ctx.Err() == nil {
		s.finishRuntimeSync(runtime, err)
		target := syncPeerLabel(peer)
		if target == "" {
			s.app.cfg.Logger.Errorf("desktopcore: background catch-up failed for %s: %v", local.LibraryID, err)
		} else {
			s.app.cfg.Logger.Errorf("desktopcore: background catch-up failed for %s via %s: %v", local.LibraryID, target, err)
		}
	} else {
		s.finishRuntimeSync(runtime, nil)
	}
	if runtime.ctx.Err() == nil {
		s.scheduleRuntimeCheckpointMaintenance(runtime, s.eventSyncDebounce)
	}
}

func (s *TransportService) runRuntimePeerUpdateBroadcast(runtime *activeTransportRuntime) {
	if s == nil || s.app == nil || runtime == nil {
		return
	}
	if s.peerUpdateBroadcastHook != nil {
		s.peerUpdateBroadcastHook(runtime)
		return
	}

	full, fullRetryAttempt, pendingRetries := s.claimRuntimePeerUpdateWork(runtime)
	if !full && len(pendingRetries) == 0 {
		return
	}
	local, ok := s.runtimeLocalContext(runtime)
	if !ok {
		return
	}
	auth, err := s.app.ensureLocalTransportMembershipAuth(runtime.ctx, local, local.PeerID)
	if err != nil {
		s.app.logf("desktopcore: build library change auth failed for %s: %v", local.LibraryID, err)
		if full {
			s.scheduleRuntimePeerUpdateFullRetry(runtime, fullRetryAttempt+1)
		} else {
			s.scheduleRuntimePeerUpdateRetries(runtime, incrementTransportPeerUpdateRetries(pendingRetries))
		}
		return
	}
	req := LibraryChangedRequest{
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    local.PeerID,
		Auth:      auth,
	}
	failedRetries := make(map[string]transportPeerUpdateRetryState)
	if !full {
		for key, retry := range pendingRetries {
			peer, resolveErr := s.resolveRuntimePeerUpdateRetry(runtime.ctx, runtime, retry)
			if resolveErr != nil {
				s.app.logf("desktopcore: resolve peer %s for library change retry failed: %v", transportPeerUpdateRetryLabel(retry), resolveErr)
				failedRetries[key] = nextTransportPeerUpdateRetryState(nil, retry)
				continue
			}
			resp, err := peer.NotifyLibraryChanged(runtime.ctx, req)
			if err != nil {
				s.app.logf("desktopcore: notify peer %s about library change failed: %v", key, err)
				failedRetries[key] = nextTransportPeerUpdateRetryState(peer, retry)
				continue
			}
			if strings.TrimSpace(resp.LibraryID) != strings.TrimSpace(local.LibraryID) {
				s.app.logf("desktopcore: notify peer %s returned library mismatch", key)
				continue
			}
			actualPeerID := firstNonEmpty(peer.PeerID(), resp.PeerID)
			if _, err := s.app.verifyTransportPeerAuth(runtime.ctx, local.LibraryID, resp.DeviceID, resp.PeerID, actualPeerID, resp.Auth); err != nil {
				s.app.logf("desktopcore: notify peer %s returned invalid auth: %v", key, err)
			}
		}
		if len(failedRetries) > 0 {
			s.scheduleRuntimePeerUpdateRetries(runtime, failedRetries)
		}
		return
	}
	peers, err := runtime.transport.ListPeers(runtime.ctx, local)
	if err != nil {
		s.app.logf("desktopcore: list peers for library change broadcast failed for %s: %v", local.LibraryID, err)
		s.scheduleRuntimePeerUpdateFullRetry(runtime, fullRetryAttempt+1)
		return
	}
	seen := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		key := transportPeerUpdateKey(peer)
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		resp, err := peer.NotifyLibraryChanged(runtime.ctx, req)
		if err != nil {
			s.app.logf("desktopcore: notify peer %s about library change failed: %v", key, err)
			failedRetries[key] = nextTransportPeerUpdateRetryState(peer, transportPeerUpdateRetryState{attempt: fullRetryAttempt})
			continue
		}
		if strings.TrimSpace(resp.LibraryID) != strings.TrimSpace(local.LibraryID) {
			s.app.logf("desktopcore: notify peer %s returned library mismatch", key)
			continue
		}
		actualPeerID := firstNonEmpty(peer.PeerID(), resp.PeerID)
		if _, err := s.app.verifyTransportPeerAuth(runtime.ctx, local.LibraryID, resp.DeviceID, resp.PeerID, actualPeerID, resp.Auth); err != nil {
			s.app.logf("desktopcore: notify peer %s returned invalid auth: %v", key, err)
		}
	}
	if len(failedRetries) > 0 {
		s.scheduleRuntimePeerUpdateRetries(runtime, failedRetries)
	}
}

func (s *TransportService) resolveRuntimePeerUpdateRetry(ctx context.Context, runtime *activeTransportRuntime, retry transportPeerUpdateRetryState) (SyncPeer, error) {
	if s == nil || runtime == nil || runtime.transport == nil {
		return nil, fmt.Errorf("transport runtime is not active")
	}
	if strings.TrimSpace(retry.peerID) != "" || strings.TrimSpace(retry.deviceID) != "" {
		return s.resolveRuntimePeerByIdentity(ctx, runtime, retry.deviceID, retry.peerID)
	}
	local, ok := s.runtimeLocalContext(runtime)
	if !ok {
		return nil, fmt.Errorf("local transport context is not available")
	}
	if addr := strings.TrimSpace(retry.address); addr != "" {
		return runtime.transport.ResolvePeer(ctx, local, addr)
	}
	return nil, fmt.Errorf("peer identity is required")
}

func (s *TransportService) runRuntimeCheckpointMaintenance(runtime *activeTransportRuntime) {
	if s == nil || s.app == nil || runtime == nil {
		return
	}
	if s.checkpointMaintenanceRunHook != nil {
		s.checkpointMaintenanceRunHook(runtime)
		return
	}
	local, ok := s.runtimeLocalContext(runtime)
	if !ok || !canManageLibrary(local.Role) || runtime.ctx.Err() != nil {
		return
	}
	if err := s.app.backgroundCheckpointMaintenance(runtime.ctx, local.LibraryID); err != nil && s.app.cfg.Logger != nil && runtime.ctx.Err() == nil {
		s.app.cfg.Logger.Errorf("desktopcore: background checkpoint maintenance failed for %s: %v", local.LibraryID, err)
	}
}

func (s *TransportService) handleLibraryChangedSignal(ctx context.Context, runtime *activeTransportRuntime, actualPeerID string, req LibraryChangedRequest) (LibraryChangedResponse, error) {
	if s == nil || s.app == nil || runtime == nil || runtime.transport == nil {
		return LibraryChangedResponse{}, fmt.Errorf("transport runtime is not active")
	}
	req.LibraryID = strings.TrimSpace(req.LibraryID)
	if req.LibraryID == "" || req.LibraryID != strings.TrimSpace(runtime.libraryID) {
		return LibraryChangedResponse{}, fmt.Errorf("library mismatch")
	}
	actualPeerID = strings.TrimSpace(actualPeerID)
	if actualPeerID == "" {
		actualPeerID = strings.TrimSpace(req.PeerID)
	}
	if _, err := s.app.verifyTransportPeerAuth(ctx, runtime.libraryID, req.DeviceID, req.PeerID, actualPeerID, req.Auth); err != nil {
		return LibraryChangedResponse{}, err
	}
	resp, err := s.app.buildLibraryChangedResponse(ctx, runtime.libraryID, runtime.deviceID, runtime.transport.LocalPeerID())
	if err != nil {
		return LibraryChangedResponse{}, err
	}
	scheduleRuntime := runtime
	if activeRuntime := s.activeRuntimeForLibrary(runtime.libraryID); activeRuntime != nil && activeRuntime != runtime {
		if activeRuntime.transport != nil && activeRuntime.ctx.Err() == nil {
			scheduleRuntime = activeRuntime
		}
	}
	if scheduleRuntime.transport != nil && scheduleRuntime.ctx.Err() == nil {
		target, resolveErr := s.resolveRuntimePeerByIdentity(ctx, scheduleRuntime, req.DeviceID, actualPeerID)
		if resolveErr != nil {
			s.app.logf("desktopcore: resolve signaled peer for targeted catch-up failed for %s: %v", scheduleRuntime.libraryID, resolveErr)
			s.scheduleRuntimeCatchup(scheduleRuntime, apitypes.NetworkSyncReasonUpdate, s.eventSyncDebounce)
		} else {
			s.scheduleRuntimeCatchupPeer(scheduleRuntime, apitypes.NetworkSyncReasonUpdate, target, s.eventSyncDebounce)
		}
	}
	return resp, nil
}

func (s *TransportService) resolveRuntimePeerByIdentity(ctx context.Context, runtime *activeTransportRuntime, deviceID, peerID string) (SyncPeer, error) {
	if s == nil || runtime == nil || runtime.transport == nil {
		return nil, fmt.Errorf("transport runtime is not active")
	}
	local, ok := s.runtimeLocalContext(runtime)
	if !ok {
		return nil, fmt.Errorf("local transport context is not available")
	}
	peerID = strings.TrimSpace(peerID)
	deviceID = strings.TrimSpace(deviceID)
	if resolver, ok := runtime.transport.(transportPeerIdentityResolver); ok {
		return resolver.ResolvePeerByIdentity(ctx, local, peerID, deviceID)
	}
	peers, err := runtime.transport.ListPeers(ctx, local)
	if err != nil {
		return nil, err
	}
	for _, candidate := range peers {
		if candidate == nil {
			continue
		}
		if peerID != "" && strings.TrimSpace(candidate.PeerID()) == peerID {
			return candidate, nil
		}
		if deviceID != "" && strings.TrimSpace(candidate.DeviceID()) == deviceID {
			return candidate, nil
		}
	}
	if peerID != "" {
		return nil, fmt.Errorf("peer %s is not connected", peerID)
	}
	if deviceID != "" {
		return nil, fmt.Errorf("device %s is not connected", deviceID)
	}
	return nil, fmt.Errorf("peer identity is required")
}

func (s *TransportService) runtimeLocalContext(runtime *activeTransportRuntime) (apitypes.LocalContext, bool) {
	if s == nil || s.app == nil || runtime == nil || runtime.transport == nil {
		return apitypes.LocalContext{}, false
	}
	local, err := s.app.EnsureLocalContext(runtime.ctx)
	if err != nil || strings.TrimSpace(local.LibraryID) != strings.TrimSpace(runtime.libraryID) || strings.TrimSpace(local.DeviceID) != strings.TrimSpace(runtime.deviceID) {
		return apitypes.LocalContext{}, false
	}
	local.PeerID = strings.TrimSpace(runtime.transport.LocalPeerID())
	return local, true
}

func (s *TransportService) NetworkStatus() apitypes.NetworkStatus {
	if s == nil || s.app == nil {
		return apitypes.NetworkStatus{}
	}
	local, err := s.app.EnsureLocalContext(context.Background())
	if err != nil {
		return apitypes.NetworkStatus{}
	}

	current := s.activeRuntime()

	out := apitypes.NetworkStatus{
		LibraryID: strings.TrimSpace(local.LibraryID),
		DeviceID:  strings.TrimSpace(local.DeviceID),
	}
	if current != nil && current.transport != nil {
		out.Running = strings.TrimSpace(current.transport.LocalPeerID()) != ""
		out.PeerID = strings.TrimSpace(current.transport.LocalPeerID())
		out.ListenAddrs = append([]string(nil), current.transport.ListenAddrs()...)
	} else {
		out.PeerID = strings.TrimSpace(local.PeerID)
	}
	if out.LibraryID == "" {
		return out
	}
	out.ServiceTag = serviceTagForLibrary(out.LibraryID)
	out.Mode = apitypes.NetworkSyncModeIdle

	if current != nil {
		s.mu.RLock()
		state := current.state
		s.mu.RUnlock()
		out.NetworkSyncState = cloneNetworkSyncState(state)
		if out.Mode == "" {
			out.Mode = apitypes.NetworkSyncModeIdle
		}
	}

	type row struct {
		PeerID        string
		LastAttemptAt *time.Time
		LastSuccessAt *time.Time
		LastError     string
		LastApplied   int64
	}
	var latest row
	err = s.app.storage.WithContext(context.Background()).
		Table("peer_sync_states").
		Select("peer_id, last_attempt_at, last_success_at, last_error, last_applied").
		Where("library_id = ?", out.LibraryID).
		Order("updated_at DESC, last_applied DESC, peer_id ASC").
		Limit(1).
		Scan(&latest).Error
	if err != nil {
		return out
	}
	if out.ActivePeerID == "" {
		out.ActivePeerID = strings.TrimSpace(latest.PeerID)
	}
	if out.LastBatchApplied == 0 {
		out.LastBatchApplied = int(latest.LastApplied)
	}
	if out.LastSyncError == "" {
		out.LastSyncError = strings.TrimSpace(latest.LastError)
	}
	if out.CompletedAt == nil {
		out.CompletedAt = cloneTimePtr(latest.LastSuccessAt)
	}
	if out.StartedAt == nil && latest.LastAttemptAt != nil && (latest.LastSuccessAt == nil || latest.LastAttemptAt.After(*latest.LastSuccessAt)) {
		out.StartedAt = cloneTimePtr(latest.LastAttemptAt)
		if out.Activity == "" {
			out.Activity = apitypes.NetworkSyncActivityOps
		}
		if out.Reason == "" {
			out.Reason = apitypes.NetworkSyncReasonManual
		}
	}
	return out
}

func (s *TransportService) beginRuntimeSync(runtime *activeTransportRuntime, reason apitypes.NetworkSyncReason) {
	if s == nil || runtime == nil {
		return
	}
	startedAt := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isActiveRuntime(runtime) {
		return
	}
	runtime.state.Mode = syncModeForReason(reason)
	runtime.state.Activity = apitypes.NetworkSyncActivityOps
	runtime.state.Reason = reason
	runtime.state.StartedAt = cloneTimePtr(&startedAt)
	runtime.state.CompletedAt = nil
	runtime.state.LastSyncError = ""
	runtime.state.ActivePeerID = ""
	runtime.state.BacklogEstimate = 0
}

func (s *TransportService) noteRuntimeSyncPeer(libraryID, peerID string) {
	if s == nil {
		return
	}
	runtime := s.activeRuntimeForLibrary(libraryID)
	if runtime == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isActiveRuntime(runtime) || strings.TrimSpace(runtime.libraryID) != strings.TrimSpace(libraryID) {
		return
	}
	runtime.state.ActivePeerID = strings.TrimSpace(peerID)
}

func (s *TransportService) noteRuntimeSyncProgress(libraryID, peerID string, activity apitypes.NetworkSyncActivity, backlog int64, applied int) {
	if s == nil {
		return
	}
	runtime := s.activeRuntimeForLibrary(libraryID)
	if runtime == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isActiveRuntime(runtime) || strings.TrimSpace(runtime.libraryID) != strings.TrimSpace(libraryID) {
		return
	}
	if strings.TrimSpace(peerID) != "" {
		runtime.state.ActivePeerID = strings.TrimSpace(peerID)
	}
	if activity != "" {
		runtime.state.Activity = activity
	}
	if backlog >= 0 {
		runtime.state.BacklogEstimate = backlog
	}
	if applied >= 0 {
		runtime.state.LastBatchApplied = applied
	}
}

func (s *TransportService) finishRuntimeSync(runtime *activeTransportRuntime, syncErr error) {
	if s == nil || runtime == nil {
		return
	}
	completedAt := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isActiveRuntime(runtime) {
		return
	}
	runtime.state.CompletedAt = cloneTimePtr(&completedAt)
	runtime.state.ActivePeerID = ""
	runtime.state.BacklogEstimate = 0
	runtime.state.Activity = ""
	if syncErr != nil {
		runtime.state.LastSyncError = strings.TrimSpace(syncErr.Error())
	} else {
		runtime.state.LastSyncError = ""
	}
	runtime.state.Mode = apitypes.NetworkSyncModeIdle
}

func (s *TransportService) activeRuntime() *activeTransportRuntime {
	if s == nil || s.app == nil {
		return nil
	}
	s.app.runtimeMu.Lock()
	defer s.app.runtimeMu.Unlock()
	if s.app.activeRuntime == nil {
		return nil
	}
	return s.app.activeRuntime.transportRuntime
}

func (s *TransportService) activeRuntimeForLibrary(libraryID string) *activeTransportRuntime {
	if s == nil || s.app == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)
	s.app.runtimeMu.Lock()
	defer s.app.runtimeMu.Unlock()
	if s.app.activeRuntime == nil || s.app.activeRuntime.transportRuntime == nil {
		return nil
	}
	runtime := s.app.activeRuntime.transportRuntime
	if strings.TrimSpace(runtime.libraryID) != libraryID {
		return nil
	}
	return runtime
}

func (s *TransportService) isActiveRuntime(runtime *activeTransportRuntime) bool {
	if s == nil || s.app == nil || runtime == nil {
		return false
	}
	s.app.runtimeMu.Lock()
	defer s.app.runtimeMu.Unlock()
	return s.app.activeRuntime != nil && s.app.activeRuntime.transportRuntime == runtime
}

func syncModeForReason(reason apitypes.NetworkSyncReason) apitypes.NetworkSyncMode {
	if reason == apitypes.NetworkSyncReasonTimer {
		return apitypes.NetworkSyncModePeriodic
	}
	return apitypes.NetworkSyncModeCatchup
}

func syncPeerLabel(peer SyncPeer) string {
	if peer == nil {
		return ""
	}
	label := strings.TrimSpace(peer.Address())
	if label == "" {
		label = strings.TrimSpace(peer.PeerID())
	}
	if label == "" {
		label = strings.TrimSpace(peer.DeviceID())
	}
	return label
}

func transportPeerUpdateKey(peer SyncPeer) string {
	return transportPeerIdentityKey(peer, false)
}

func transportPeerUpdateRetryLabel(retry transportPeerUpdateRetryState) string {
	if peerID := strings.TrimSpace(retry.peerID); peerID != "" {
		return peerID
	}
	if addr := strings.TrimSpace(retry.address); addr != "" {
		return addr
	}
	return strings.TrimSpace(retry.deviceID)
}

func nextTransportPeerUpdateRetryState(peer SyncPeer, previous transportPeerUpdateRetryState) transportPeerUpdateRetryState {
	next := transportPeerUpdateRetryState{
		address:  strings.TrimSpace(previous.address),
		deviceID: strings.TrimSpace(previous.deviceID),
		peerID:   strings.TrimSpace(previous.peerID),
		attempt:  previous.attempt + 1,
	}
	if peer == nil {
		return next
	}
	if addr := strings.TrimSpace(peer.Address()); addr != "" {
		next.address = addr
	}
	if deviceID := strings.TrimSpace(peer.DeviceID()); deviceID != "" {
		next.deviceID = deviceID
	}
	if peerID := strings.TrimSpace(peer.PeerID()); peerID != "" {
		next.peerID = peerID
	}
	return next
}

func cloneTransportPeerUpdateRetries(src map[string]transportPeerUpdateRetryState) map[string]transportPeerUpdateRetryState {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]transportPeerUpdateRetryState, len(src))
	for key, retry := range src {
		dst[key] = retry
	}
	return dst
}

func mergeTransportPeerUpdateRetries(dst *map[string]transportPeerUpdateRetryState, src map[string]transportPeerUpdateRetryState) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]transportPeerUpdateRetryState, len(src))
	}
	for key, retry := range src {
		existing, ok := (*dst)[key]
		if ok && existing.attempt > retry.attempt {
			continue
		}
		(*dst)[key] = retry
	}
}

func clearTransportPeerUpdateRetries(retries *map[string]transportPeerUpdateRetryState) {
	if retries == nil {
		return
	}
	*retries = nil
}

func clearTransportPeerUpdateRetryTimers(timers *map[string]*time.Timer) {
	if timers == nil {
		return
	}
	for key, timer := range *timers {
		if timer != nil {
			timer.Stop()
		}
		delete(*timers, key)
	}
	*timers = nil
}

func incrementTransportPeerUpdateRetries(src map[string]transportPeerUpdateRetryState) map[string]transportPeerUpdateRetryState {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]transportPeerUpdateRetryState, len(src))
	for key, retry := range src {
		retry.attempt++
		out[key] = retry
	}
	return out
}

func catchupPeerKey(peer SyncPeer) string {
	return transportPeerIdentityKey(peer, true)
}

func transportPeerIdentityKey(peer SyncPeer, includePointerFallback bool) string {
	if peer == nil {
		return ""
	}
	if peerID := strings.TrimSpace(peer.PeerID()); peerID != "" {
		return "peer:" + peerID
	}
	if addr := strings.TrimSpace(peer.Address()); addr != "" {
		return "addr:" + addr
	}
	if deviceID := strings.TrimSpace(peer.DeviceID()); deviceID != "" {
		return "device:" + deviceID
	}
	if includePointerFallback {
		return fmt.Sprintf("peer:%p", peer)
	}
	return ""
}

func mergeTransportCatchupRequestSet(set *transportCatchupRequestSet, reason apitypes.NetworkSyncReason, peer SyncPeer) {
	if set == nil {
		return
	}
	if peer == nil {
		set.all = true
		if set.allReason == "" {
			set.allReason = reason
		}
		return
	}
	key := catchupPeerKey(peer)
	if key == "" {
		return
	}
	if set.peers == nil {
		set.peers = make(map[string]transportCatchupPeerRequest, 1)
	}
	if _, exists := set.peers[key]; exists {
		return
	}
	set.peers[key] = transportCatchupPeerRequest{
		reason: reason,
		peer:   peer,
	}
}

func mergeTransportCatchupRequestSets(dst, src *transportCatchupRequestSet) {
	if dst == nil || src == nil || !hasTransportCatchupRequests(*src) {
		return
	}
	if src.all {
		mergeTransportCatchupRequestSet(dst, src.allReason, nil)
		return
	}
	keys := make([]string, 0, len(src.peers))
	for key := range src.peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		req := src.peers[key]
		mergeTransportCatchupRequestSet(dst, req.reason, req.peer)
	}
}

func popTransportCatchupRequestSet(set *transportCatchupRequestSet) (transportCatchupRequest, bool) {
	if set == nil {
		return transportCatchupRequest{}, false
	}
	if len(set.peers) == 0 {
		if !set.all {
			return transportCatchupRequest{}, false
		}
		req := transportCatchupRequest{reason: set.allReason}
		set.all = false
		set.allReason = ""
		return req, true
	}
	keys := make([]string, 0, len(set.peers))
	for key := range set.peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	next := set.peers[keys[0]]
	delete(set.peers, keys[0])
	if len(set.peers) == 0 {
		set.peers = nil
	}
	return transportCatchupRequest{
		reason: next.reason,
		peer:   next.peer,
	}, true
}

func clearTransportCatchupRequestSet(set *transportCatchupRequestSet) {
	if set == nil {
		return
	}
	set.all = false
	set.allReason = ""
	set.peers = nil
}

func hasTransportCatchupRequests(set transportCatchupRequestSet) bool {
	return set.all || len(set.peers) > 0
}

func cloneNetworkSyncState(state apitypes.NetworkSyncState) apitypes.NetworkSyncState {
	state.ActivePeerID = strings.TrimSpace(state.ActivePeerID)
	state.LastSyncError = strings.TrimSpace(state.LastSyncError)
	state.StartedAt = cloneTimePtr(state.StartedAt)
	state.CompletedAt = cloneTimePtr(state.CompletedAt)
	return state
}

func (s *TransportService) hasTransportOverride() bool {
	if s == nil || s.app == nil {
		return false
	}
	s.app.transportMu.RLock()
	defer s.app.transportMu.RUnlock()
	return s.app.transport != nil
}

func (s *TransportService) activeSyncTransport() SyncTransport {
	if s == nil || s.app == nil {
		return nil
	}
	s.app.transportMu.RLock()
	override := s.app.transport
	s.app.transportMu.RUnlock()
	if override != nil {
		return override
	}
	if s.app.transportService == nil {
		return nil
	}
	return s.app.transportService.SyncTransport()
}

func (s *TransportService) transportRunning() bool {
	if s == nil || s.app == nil {
		return false
	}
	s.app.transportMu.RLock()
	override := s.app.transport
	s.app.transportMu.RUnlock()
	if override != nil {
		return true
	}
	return s.app.transportService != nil && s.app.transportService.Running()
}

func (s *TransportService) updateDevicePeerID(ctx context.Context, libraryID, deviceID, peerID, deviceName string) error {
	if s == nil || s.app == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if deviceID == "" || peerID == "" {
		return nil
	}
	if libraryID != "" && !s.app.isLibraryMember(ctx, libraryID, deviceID) {
		return nil
	}
	return s.touchDevicePeerID(ctx, deviceID, peerID, deviceName)
}

func (s *TransportService) touchDevicePeerID(ctx context.Context, deviceID, peerID, deviceName string) error {
	if s == nil || s.app == nil {
		return nil
	}
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if deviceID == "" || peerID == "" {
		return nil
	}
	return s.upsertDevicePresence(ctx, deviceID, peerID, deviceName)
}

func (s *TransportService) upsertDevicePresence(ctx context.Context, deviceID, peerID, deviceName string) error {
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if deviceID == "" || peerID == "" {
		return nil
	}
	now := time.Now().UTC()
	deviceName = chooseDeviceName("", deviceName, deviceID)

	var existing Device
	err := s.app.storage.WithContext(ctx).Where("device_id = ?", deviceID).Take(&existing).Error
	switch {
	case err == nil:
		wasOffline := existing.LastSeenAt == nil || existing.LastSeenAt.UTC().Before(now.Add(-availabilityOnlineWindow))
		updates := map[string]any{
			"peer_id":      peerID,
			"last_seen_at": cloneTimePtr(&now),
		}
		if strings.TrimSpace(existing.Name) == "" || strings.TrimSpace(existing.Name) == deviceID {
			updates["name"] = deviceName
		}
		if err := s.app.storage.WithContext(ctx).
			Model(&Device{}).
			Where("device_id = ?", deviceID).
			Updates(updates).Error; err != nil {
			return err
		}
		if wasOffline {
			s.app.emitAvailabilityInvalidateAllForActiveMembership(ctx, deviceID)
		}
		return nil
	case err == gorm.ErrRecordNotFound:
		if err := s.app.storage.WithContext(ctx).Create(&Device{
			DeviceID:   deviceID,
			Name:       deviceName,
			PeerID:     peerID,
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		s.app.emitAvailabilityInvalidateAllForActiveMembership(ctx, deviceID)
		return nil
	default:
		return err
	}
}

func (s *TransportService) markDevicePresenceOffline(ctx context.Context, libraryID, peerID string) error {
	if s == nil || s.app == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)
	peerID = strings.TrimSpace(peerID)
	if libraryID == "" || peerID == "" {
		return nil
	}

	deviceID, ok, err := s.memberDeviceIDForPeer(ctx, libraryID, peerID)
	if err != nil || !ok {
		return err
	}

	var existing Device
	err = s.app.storage.WithContext(ctx).
		Where("device_id = ? AND peer_id = ?", deviceID, peerID).
		Take(&existing).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}

	now := time.Now().UTC()
	onlineCutoff := now.Add(-availabilityOnlineWindow)
	wasOnline := existing.LastSeenAt != nil && !existing.LastSeenAt.UTC().Before(onlineCutoff)
	if !wasOnline {
		return nil
	}

	offlineAt := onlineCutoff.Add(-time.Second)
	if err := s.app.storage.WithContext(ctx).
		Model(&Device{}).
		Where("device_id = ? AND peer_id = ?", deviceID, peerID).
		Update("last_seen_at", cloneTimePtr(&offlineAt)).Error; err != nil {
		return err
	}
	s.app.emitAvailabilityInvalidateAllForActiveMembership(ctx, deviceID)
	return nil
}

func (s *TransportService) memberDeviceIDForPeer(ctx context.Context, libraryID, peerID string) (string, bool, error) {
	if s == nil || s.app == nil {
		return "", false, nil
	}
	type row struct {
		DeviceID string
	}
	var result row
	err := s.app.storage.WithContext(ctx).
		Table("memberships AS m").
		Select("m.device_id AS device_id").
		Joins("JOIN devices d ON d.device_id = m.device_id").
		Where("m.library_id = ? AND d.peer_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(peerID)).
		Limit(1).
		Scan(&result).Error
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(result.DeviceID) == "" {
		return "", false, nil
	}
	return strings.TrimSpace(result.DeviceID), true, nil
}

func sortedListenAddrs(items []string) []string {
	out := compactNonEmptyStrings(items)
	sort.Strings(out)
	return out
}
