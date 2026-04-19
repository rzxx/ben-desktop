package playback

import (
	"sync"
	"sync/atomic"
	"time"
)

const debugTraceLimit = 240

type DebugTraceEntry struct {
	TimestampMS          int64  `json:"timestampMs"`
	Kind                 string `json:"kind"`
	Reason               string `json:"reason,omitempty"`
	CurrentEntryID       string `json:"currentEntryId,omitempty"`
	LoadingEntryID       string `json:"loadingEntryId,omitempty"`
	Status               Status `json:"status,omitempty"`
	PositionMS           int64  `json:"positionMs"`
	PositionCapturedAtMS int64  `json:"positionCapturedAtMs,omitempty"`
	DurationMS           *int64 `json:"durationMs,omitempty"`
	QueueVersion         int64  `json:"queueVersion,omitempty"`
	TargetPositionMS     *int64 `json:"targetPositionMs,omitempty"`
	ObservedPositionMS   *int64 `json:"observedPositionMs,omitempty"`
	Message              string `json:"message,omitempty"`
}

type debugTraceBuffer struct {
	mu      sync.Mutex
	entries []DebugTraceEntry
}

var playbackDebugTrace = &debugTraceBuffer{}
var playbackDebugTraceEnabled atomic.Bool

func NewDebugTraceEntry(kind string, snapshot *SessionSnapshot) DebugTraceEntry {
	entry := DebugTraceEntry{
		TimestampMS: time.Now().UTC().UnixMilli(),
		Kind:        kind,
	}
	if snapshot == nil {
		return entry
	}
	entry.Status = snapshot.Status
	entry.PositionMS = snapshot.PositionMS
	entry.PositionCapturedAtMS = snapshot.PositionCapturedAtMS
	entry.QueueVersion = snapshot.QueueVersion
	if snapshot.CurrentEntry != nil {
		entry.CurrentEntryID = snapshot.CurrentEntry.EntryID
	}
	if snapshot.LoadingEntry != nil {
		entry.LoadingEntryID = snapshot.LoadingEntry.EntryID
	}
	if snapshot.DurationMS != nil {
		durationMS := *snapshot.DurationMS
		entry.DurationMS = &durationMS
	}
	return entry
}

func RecordDebugTrace(entry DebugTraceEntry) {
	if !DebugTraceEnabled() {
		return
	}
	if entry.TimestampMS <= 0 {
		entry.TimestampMS = time.Now().UTC().UnixMilli()
	}

	playbackDebugTrace.mu.Lock()
	defer playbackDebugTrace.mu.Unlock()

	playbackDebugTrace.entries = append(playbackDebugTrace.entries, entry)
	if len(playbackDebugTrace.entries) <= debugTraceLimit {
		return
	}

	overflow := len(playbackDebugTrace.entries) - debugTraceLimit
	playbackDebugTrace.entries = append(
		[]DebugTraceEntry(nil),
		playbackDebugTrace.entries[overflow:]...,
	)
}

func SnapshotDebugTrace() []DebugTraceEntry {
	if !DebugTraceEnabled() {
		return nil
	}
	playbackDebugTrace.mu.Lock()
	defer playbackDebugTrace.mu.Unlock()

	return append([]DebugTraceEntry(nil), playbackDebugTrace.entries...)
}

func ClearDebugTrace() {
	playbackDebugTrace.mu.Lock()
	defer playbackDebugTrace.mu.Unlock()
	playbackDebugTrace.entries = nil
}

func SetDebugTraceEnabled(enabled bool) {
	playbackDebugTraceEnabled.Store(enabled)
	if !enabled {
		ClearDebugTrace()
	}
}

func DebugTraceEnabled() bool {
	return playbackDebugTraceEnabled.Load()
}
