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
	app     *App
	factory transportFactory

	mu      sync.RWMutex
	current *activeTransportRuntime
}

type activeTransportRuntime struct {
	libraryID string
	deviceID  string
	transport managedSyncTransport
}

func newTransportService(app *App) *TransportService {
	return &TransportService{
		app:     app,
		factory: app.newLibp2pSyncTransport,
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
	return s.SyncTransport() != nil
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
	if current != nil && current.transport != nil {
		_ = current.transport.Close()
	}
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
	s.current = &activeTransportRuntime{
		libraryID: strings.TrimSpace(local.LibraryID),
		deviceID:  strings.TrimSpace(local.DeviceID),
		transport: next,
	}
	s.mu.Unlock()

	if current != nil && current.transport != nil {
		_ = current.transport.Close()
	}
	return nil
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
