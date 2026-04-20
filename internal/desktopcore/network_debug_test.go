package desktopcore

import (
	"testing"

	apitypes "ben/desktop/api/types"
)

func TestNetworkDebugTraceSnapshotAndClear(t *testing.T) {
	ClearNetworkDebugTrace()
	SetNetworkDebugTraceEnabled(true)
	t.Cleanup(ClearNetworkDebugTrace)
	t.Cleanup(func() {
		SetNetworkDebugTraceEnabled(false)
	})

	RecordNetworkDebugTrace(apitypes.NetworkDebugTraceEntry{
		Kind:      "peer.connected",
		Message:   "peer connected",
		LibraryID: "lib-1",
		PeerID:    "peer-1",
	})

	snapshot := SnapshotNetworkDebugTrace()
	if len(snapshot) != 1 {
		t.Fatalf("trace entries = %d, want 1", len(snapshot))
	}
	if snapshot[0].Kind != "peer.connected" {
		t.Fatalf("trace kind = %q", snapshot[0].Kind)
	}
	if snapshot[0].TimestampMS <= 0 {
		t.Fatalf("trace timestamp = %d, want positive", snapshot[0].TimestampMS)
	}

	ClearNetworkDebugTrace()
	if got := len(SnapshotNetworkDebugTrace()); got != 0 {
		t.Fatalf("trace entries after clear = %d, want 0", got)
	}
}

func TestNetworkDebugTraceToggleDisablesRecordingAndClearsBuffer(t *testing.T) {
	ClearNetworkDebugTrace()
	SetNetworkDebugTraceEnabled(true)
	RecordNetworkDebugTrace(apitypes.NetworkDebugTraceEntry{
		Kind:    "enabled",
		Message: "enabled",
	})
	if got := len(SnapshotNetworkDebugTrace()); got != 1 {
		t.Fatalf("trace entries while enabled = %d, want 1", got)
	}

	SetNetworkDebugTraceEnabled(false)
	if got := len(SnapshotNetworkDebugTrace()); got != 0 {
		t.Fatalf("trace entries after disable = %d, want 0", got)
	}

	RecordNetworkDebugTrace(apitypes.NetworkDebugTraceEntry{
		Kind:    "disabled",
		Message: "disabled",
	})
	if got := len(SnapshotNetworkDebugTrace()); got != 0 {
		t.Fatalf("trace entries while disabled = %d, want 0", got)
	}
}
