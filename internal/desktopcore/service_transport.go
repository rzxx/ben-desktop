package desktopcore

import (
	"context"
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

type transportFactory func(context.Context, apitypes.LocalContext) (managedSyncTransport, error)

type TransportService struct {
	app                *App
	factory            transportFactory
	backgroundInterval time.Duration

	mu sync.RWMutex
}

type activeTransportRuntime struct {
	libraryID string
	deviceID  string
	transport managedSyncTransport
	ctx       context.Context
	cancel    context.CancelFunc
	state     apitypes.NetworkSyncState
}

func newTransportService(app *App) *TransportService {
	return &TransportService{
		app:                app,
		factory:            app.newLibp2pSyncTransport,
		backgroundInterval: 30 * time.Second,
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

func (s *TransportService) syncActive(ctx context.Context) error {
	if s == nil || s.app == nil {
		return nil
	}
	if s.app.hasTransportOverride() {
		s.Stop()
		return nil
	}

	local, runtime, ok, err := s.app.syncActiveLibraryRuntimeState(ctx)
	if err != nil {
		return err
	}
	if !ok {
		s.Stop()
		return nil
	}
	return s.syncRuntime(ctx, local, runtime)
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
	go s.runBackgroundLoop(nextRuntime)
	return nil
}

func (s *TransportService) stopRuntime(runtime *activeTransportRuntime) {
	if runtime == nil {
		return
	}
	if runtime.cancel != nil {
		runtime.cancel()
	}
	if runtime.transport != nil {
		_ = runtime.transport.Close()
	}
}

func (s *TransportService) runBackgroundLoop(runtime *activeTransportRuntime) {
	if s == nil || s.app == nil || runtime == nil {
		return
	}
	s.runCatchupAndCheckpoint(runtime, apitypes.NetworkSyncReasonStartup)
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
			s.runCatchupAndCheckpoint(runtime, apitypes.NetworkSyncReasonTimer)
		}
	}
}

func (s *TransportService) runCatchupAndCheckpoint(runtime *activeTransportRuntime, reason apitypes.NetworkSyncReason) {
	if s == nil || s.app == nil || runtime == nil {
		return
	}
	local, ok := s.runtimeLocalContext(runtime)
	if !ok {
		return
	}
	s.beginRuntimeSync(runtime, reason)
	if err := s.app.catchupAllPeers(runtime.ctx, local, reason, nil, false); err != nil && s.app.cfg.Logger != nil && runtime.ctx.Err() == nil {
		s.finishRuntimeSync(runtime, err)
		s.app.cfg.Logger.Errorf("desktopcore: background catch-up failed for %s: %v", local.LibraryID, err)
	} else {
		s.finishRuntimeSync(runtime, nil)
	}
	if !canManageLibrary(local.Role) || runtime.ctx.Err() != nil {
		return
	}
	if err := s.app.backgroundCheckpointMaintenance(runtime.ctx, local.LibraryID); err != nil && s.app.cfg.Logger != nil && runtime.ctx.Err() == nil {
		s.app.cfg.Logger.Errorf("desktopcore: background checkpoint maintenance failed for %s: %v", local.LibraryID, err)
	}
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
		updates := map[string]any{
			"peer_id":      peerID,
			"last_seen_at": cloneTimePtr(&now),
		}
		if strings.TrimSpace(existing.Name) == "" || strings.TrimSpace(existing.Name) == deviceID {
			updates["name"] = deviceName
		}
		return s.app.storage.WithContext(ctx).Model(&Device{}).Where("device_id = ?", deviceID).Updates(updates).Error
	case err == gorm.ErrRecordNotFound:
		return s.app.storage.WithContext(ctx).Create(&Device{
			DeviceID:   deviceID,
			Name:       deviceName,
			PeerID:     peerID,
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error
	default:
		return err
	}
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
