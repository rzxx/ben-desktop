package desktopcore

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apitypes "ben/desktop/api/types"
)

const networkDebugTraceLimit = 240

var networkDebugTrace = struct {
	mu      sync.Mutex
	entries []apitypes.NetworkDebugTraceEntry
}{}
var networkDebugTraceEnabled atomic.Bool

func RecordNetworkDebugTrace(entry apitypes.NetworkDebugTraceEntry) {
	if !NetworkDebugTraceEnabled() {
		return
	}
	if entry.TimestampMS <= 0 {
		entry.TimestampMS = time.Now().UTC().UnixMilli()
	}
	entry.Level = strings.TrimSpace(entry.Level)
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Message = strings.TrimSpace(entry.Message)
	entry.LibraryID = strings.TrimSpace(entry.LibraryID)
	entry.DeviceID = strings.TrimSpace(entry.DeviceID)
	entry.PeerID = strings.TrimSpace(entry.PeerID)
	entry.Address = strings.TrimSpace(entry.Address)
	entry.Reason = strings.TrimSpace(entry.Reason)
	entry.Error = strings.TrimSpace(entry.Error)

	networkDebugTrace.mu.Lock()
	defer networkDebugTrace.mu.Unlock()

	networkDebugTrace.entries = append(networkDebugTrace.entries, entry)
	if len(networkDebugTrace.entries) <= networkDebugTraceLimit {
		return
	}

	overflow := len(networkDebugTrace.entries) - networkDebugTraceLimit
	networkDebugTrace.entries = append(
		[]apitypes.NetworkDebugTraceEntry(nil),
		networkDebugTrace.entries[overflow:]...,
	)
}

func SnapshotNetworkDebugTrace() []apitypes.NetworkDebugTraceEntry {
	networkDebugTrace.mu.Lock()
	defer networkDebugTrace.mu.Unlock()

	return append([]apitypes.NetworkDebugTraceEntry(nil), networkDebugTrace.entries...)
}

func ClearNetworkDebugTrace() {
	networkDebugTrace.mu.Lock()
	defer networkDebugTrace.mu.Unlock()

	networkDebugTrace.entries = nil
}

func SetNetworkDebugTraceEnabled(enabled bool) {
	networkDebugTraceEnabled.Store(enabled)
	if !enabled {
		ClearNetworkDebugTrace()
	}
}

func NetworkDebugTraceEnabled() bool {
	return networkDebugTraceEnabled.Load()
}

func (a *App) recordNetworkDebug(entry apitypes.NetworkDebugTraceEntry) {
	if a == nil {
		return
	}
	RecordNetworkDebugTrace(entry)
}
