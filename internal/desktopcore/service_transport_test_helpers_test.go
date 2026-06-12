package desktopcore

import (
	"time"

	apitypes "ben/desktop/api/types"
)

func (s *TransportService) setTransportFactoryForTest(factory transportFactory) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factory = factory
}

func (s *TransportService) setBackgroundIntervalForTest(interval time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backgroundInterval = interval
}

func (s *TransportService) setEventSyncDebounceForTest(delay time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventSyncDebounce = delay
}

func (s *TransportService) setPeerRetryDelayForTest(delay time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerRetryDelay = delay
}

func (s *TransportService) setPeerRetryMaxDelayForTest(delay time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerRetryMaxDelay = delay
}

func (s *TransportService) setPeerRetryMaxCountForTest(count int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerRetryMaxCount = count
}

func (s *TransportService) setCatchupRunHookForTest(hook func(*activeTransportRuntime, apitypes.NetworkSyncReason)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catchupRunHook = hook
}

func (s *TransportService) setCatchupPeerRunHookForTest(hook func(*activeTransportRuntime, SyncPeer, apitypes.NetworkSyncReason)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catchupPeerRunHook = hook
}

func (s *TransportService) setPeerUpdateBroadcastHookForTest(hook func(*activeTransportRuntime)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerUpdateBroadcastHook = hook
}

func (s *TransportService) setCheckpointMaintenanceRunHookForTest(hook func(*activeTransportRuntime)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpointMaintenanceRunHook = hook
}
