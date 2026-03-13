package desktopcore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	apitypes "ben/core/api/types"
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

	mu      sync.RWMutex
	current *activeTransportRuntime
}

type activeTransportRuntime struct {
	libraryID string
	deviceID  string
	transport managedSyncTransport
	ctx       context.Context
	cancel    context.CancelFunc
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil
	}
	return s.current.transport
}

func (s *TransportService) Running() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current != nil && s.current.transport != nil && strings.TrimSpace(s.current.transport.LocalPeerID()) != ""
}

func (s *TransportService) ListenAddrs() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil || s.current.transport == nil {
		return nil
	}
	return append([]string(nil), s.current.transport.ListenAddrs()...)
}

func (s *TransportService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	current := s.current
	s.current = nil
	s.mu.Unlock()
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

	local, ok, err := s.app.syncActiveLibraryRuntime(ctx)
	if err != nil {
		return err
	}
	if !ok {
		s.Stop()
		return nil
	}

	s.mu.RLock()
	current := s.current
	if current != nil &&
		strings.TrimSpace(current.libraryID) == strings.TrimSpace(local.LibraryID) &&
		strings.TrimSpace(current.deviceID) == strings.TrimSpace(local.DeviceID) &&
		current.transport != nil {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	next, err := s.factory(ctx, local)
	if err != nil {
		return err
	}

	s.mu.Lock()
	current = s.current
	runtimeCtx, cancel := context.WithCancel(context.Background())
	nextRuntime := &activeTransportRuntime{
		libraryID: strings.TrimSpace(local.LibraryID),
		deviceID:  strings.TrimSpace(local.DeviceID),
		transport: next,
		ctx:       runtimeCtx,
		cancel:    cancel,
	}
	s.current = nextRuntime
	s.mu.Unlock()

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
	if err := s.app.catchupAllPeers(runtime.ctx, local, reason, nil, false); err != nil && s.app.cfg.Logger != nil && runtime.ctx.Err() == nil {
		s.app.cfg.Logger.Errorf("desktopcore: background catch-up failed for %s: %v", local.LibraryID, err)
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

	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()

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

	type row struct {
		PeerID        string
		LastAttemptAt *time.Time
		LastSuccessAt *time.Time
		LastError     string
		LastApplied   int64
	}
	var latest row
	err = s.app.db.WithContext(context.Background()).
		Table("peer_sync_states").
		Select("peer_id, last_attempt_at, last_success_at, last_error, last_applied").
		Where("library_id = ?", out.LibraryID).
		Order("updated_at DESC, last_applied DESC, peer_id ASC").
		Limit(1).
		Scan(&latest).Error
	if err != nil {
		return out
	}
	out.ActivePeerID = strings.TrimSpace(latest.PeerID)
	out.LastBatchApplied = int(latest.LastApplied)
	out.LastSyncError = strings.TrimSpace(latest.LastError)
	out.CompletedAt = cloneTimePtr(latest.LastSuccessAt)
	if latest.LastAttemptAt != nil && (latest.LastSuccessAt == nil || latest.LastAttemptAt.After(*latest.LastSuccessAt)) {
		out.StartedAt = cloneTimePtr(latest.LastAttemptAt)
		out.Activity = apitypes.NetworkSyncActivityOps
		out.Reason = apitypes.NetworkSyncReasonManual
	}
	return out
}

func (a *App) syncActiveRuntimeServices(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if a.transportService != nil {
		if err := a.transportService.syncActive(ctx); err != nil {
			return err
		}
	}
	return a.syncActiveScanWatcher(ctx)
}

func (a *App) hasTransportOverride() bool {
	if a == nil {
		return false
	}
	a.transportMu.RLock()
	defer a.transportMu.RUnlock()
	return a.transport != nil
}

func (a *App) activeSyncTransport() SyncTransport {
	if a == nil {
		return nil
	}
	a.transportMu.RLock()
	override := a.transport
	a.transportMu.RUnlock()
	if override != nil {
		return override
	}
	if a.transportService == nil {
		return nil
	}
	return a.transportService.SyncTransport()
}

func (a *App) transportRunning() bool {
	if a == nil {
		return false
	}
	a.transportMu.RLock()
	override := a.transport
	a.transportMu.RUnlock()
	if override != nil {
		return true
	}
	return a.transportService != nil && a.transportService.Running()
}

func (a *App) updateDevicePeerID(ctx context.Context, libraryID, deviceID, peerID, deviceName string) error {
	if a == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if deviceID == "" || peerID == "" {
		return nil
	}
	if libraryID != "" && !a.isLibraryMember(ctx, libraryID, deviceID) {
		return nil
	}
	return a.touchDevicePeerID(ctx, deviceID, peerID, deviceName)
}

func (a *App) touchDevicePeerID(ctx context.Context, deviceID, peerID, deviceName string) error {
	if a == nil {
		return nil
	}
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if deviceID == "" || peerID == "" {
		return nil
	}
	return a.upsertDevicePresence(ctx, deviceID, peerID, deviceName)
}

func (a *App) upsertDevicePresence(ctx context.Context, deviceID, peerID, deviceName string) error {
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if deviceID == "" || peerID == "" {
		return nil
	}
	now := time.Now().UTC()
	deviceName = chooseDeviceName("", deviceName, deviceID)

	var existing Device
	err := a.db.WithContext(ctx).Where("device_id = ?", deviceID).Take(&existing).Error
	switch {
	case err == nil:
		updates := map[string]any{
			"peer_id":      peerID,
			"last_seen_at": cloneTimePtr(&now),
		}
		if strings.TrimSpace(existing.Name) == "" || strings.TrimSpace(existing.Name) == deviceID {
			updates["name"] = deviceName
		}
		return a.db.WithContext(ctx).Model(&Device{}).Where("device_id = ?", deviceID).Updates(updates).Error
	case err == gorm.ErrRecordNotFound:
		return a.db.WithContext(ctx).Create(&Device{
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

func (a *App) memberDeviceIDForPeer(ctx context.Context, libraryID, peerID string) (string, bool, error) {
	if a == nil {
		return "", false, nil
	}
	type row struct {
		DeviceID string
	}
	var result row
	err := a.db.WithContext(ctx).
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
