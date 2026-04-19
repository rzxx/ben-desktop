package playback

import "testing"

func TestDebugTraceToggleDisablesRecordingAndClearsBuffer(t *testing.T) {
	ClearDebugTrace()
	SetDebugTraceEnabled(true)
	RecordDebugTrace(DebugTraceEntry{Kind: "enabled"})

	if got := len(SnapshotDebugTrace()); got != 1 {
		t.Fatalf("expected 1 trace entry while enabled, got %d", got)
	}

	SetDebugTraceEnabled(false)

	if got := len(SnapshotDebugTrace()); got != 0 {
		t.Fatalf("expected trace buffer to clear when disabled, got %d", got)
	}

	RecordDebugTrace(DebugTraceEntry{Kind: "disabled"})

	if got := len(SnapshotDebugTrace()); got != 0 {
		t.Fatalf("expected no trace entries while disabled, got %d", got)
	}

	SetDebugTraceEnabled(true)
	ClearDebugTrace()
}
