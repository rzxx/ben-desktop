package playback

import (
	"encoding/json"
	"testing"

	apitypes "ben/desktop/api/types"
)

func TestBuildQueueEventSnapshotTrimsContextOriginAndKeepsTotalCount(t *testing.T) {
	t.Parallel()

	snapshot := SessionSnapshot{
		ContextQueue: &ContextQueue{
			Title:      "All tracks",
			HasBefore:  true,
			HasAfter:   true,
			TotalCount: 1500,
			Entries: []SessionEntry{{
				EntryID:      "ctx-1",
				Origin:       EntryOriginContext,
				ContextIndex: 42,
				Item: SessionItem{
					RecordingID:  "rec-1",
					Title:        "Track 1",
					Subtitle:     "Artist",
					ArtworkRef:   "art-1",
					SourceKind:   SourceKindTracks,
					SourceID:     "tracks",
					SourceItemID: "source-item",
					AlbumID:      "album-1",
				},
			}},
		},
		UserQueue: []SessionEntry{{
			EntryID: "queued-1",
			Origin:  EntryOriginQueued,
			Item: SessionItem{
				RecordingID: "rec-2",
				Title:       "Queued",
				ArtworkRef:  "art-2",
			},
		}},
	}

	event := BuildQueueEventSnapshot(snapshot)
	if event.ContextQueue == nil {
		t.Fatalf("expected context queue payload")
	}
	if event.QueueVersion != 0 {
		t.Fatalf("queue version = %d, want 0", event.QueueVersion)
	}
	if event.ContextQueue.TotalCount != 1500 {
		t.Fatalf("total count = %d, want 1500", event.ContextQueue.TotalCount)
	}
	if len(event.ContextQueue.Entries) != 1 {
		t.Fatalf("context entries = %d, want 1", len(event.ContextQueue.Entries))
	}
	if event.ContextQueue.Entries[0].Origin != "" {
		t.Fatalf("context origin = %q, want empty", event.ContextQueue.Entries[0].Origin)
	}
	if event.ContextQueue.Entries[0].Item.SourceItemID != "source-item" {
		t.Fatalf("context source item id = %q, want source-item", event.ContextQueue.Entries[0].Item.SourceItemID)
	}
	if len(event.UserQueue) != 1 {
		t.Fatalf("user queue entries = %d, want 1", len(event.UserQueue))
	}
	if event.UserQueue[0].Origin != EntryOriginQueued {
		t.Fatalf("user queue origin = %q, want %q", event.UserQueue[0].Origin, EntryOriginQueued)
	}
}

func TestBuildTransportEventSnapshotKeepsAuthoritativeCurrentEntryState(t *testing.T) {
	t.Parallel()

	snapshot := SessionSnapshot{
		CurrentEntry: &SessionEntry{
			EntryID:      "queued-1",
			Origin:       EntryOriginQueued,
			ContextIndex: -1,
			Item: SessionItem{
				RecordingID: "rec-2",
				Title:       "Queued",
				Subtitle:    "Artist",
				ArtworkRef:  "art-2",
			},
		},
		LoadingEntry: &SessionEntry{
			EntryID:      "ctx-42",
			Origin:       EntryOriginContext,
			ContextIndex: 42,
			Item: SessionItem{
				RecordingID: "rec-42",
				Title:       "Context Track",
				Subtitle:    "Artist",
				ArtworkRef:  "art-42",
			},
		},
		PositionMS:           1500,
		PositionCapturedAtMS: 42000,
		Status:               StatusPlaying,
	}

	event := BuildTransportEventSnapshot(snapshot)
	if event.CurrentEntry == nil {
		t.Fatalf("expected current entry payload")
	}
	if event.CurrentEntry.Origin != EntryOriginQueued {
		t.Fatalf("current origin = %q, want %q", event.CurrentEntry.Origin, EntryOriginQueued)
	}
	if event.CurrentEntry.Item.RecordingID != "rec-2" {
		t.Fatalf("current recording id = %q, want rec-2", event.CurrentEntry.Item.RecordingID)
	}
	if event.LoadingEntry == nil {
		t.Fatalf("expected loading entry payload")
	}
	if event.LoadingEntry.Origin != EntryOriginContext {
		t.Fatalf("loading origin = %q, want %q", event.LoadingEntry.Origin, EntryOriginContext)
	}
	if event.LoadingEntry.ContextIndex != 42 {
		t.Fatalf("loading context index = %d, want 42", event.LoadingEntry.ContextIndex)
	}
	if event.PositionMS != 1500 || event.PositionCapturedAtMS != 42000 {
		t.Fatalf("unexpected transport position payload: %+v", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal transport event snapshot: %v", err)
	}
	if jsonContainsKey(payload, "queueLength") {
		t.Fatalf("transport event leaked queue length payload: %s", string(payload))
	}
}

func TestBuildQueueEventSnapshotPublishesAuthoritativeQueueSlice(t *testing.T) {
	t.Parallel()

	snapshot := SessionSnapshot{
		ContextQueue: &ContextQueue{
			Title:         "Windowed queue",
			Entries:       []SessionEntry{{EntryID: "ctx-2", Item: SessionItem{RecordingID: "rec-2", Title: "Track 2"}}},
			allEntries:    []SessionEntry{{EntryID: "ctx-1", Item: SessionItem{RecordingID: "rec-1", Title: "Track 1"}}},
			HasBefore:     true,
			HasAfter:      true,
			TotalCount:    1500,
			CurrentIndex:  512,
			ResumeIndex:   480,
			WindowStart:   500,
			WindowCount:   50,
			Loading:       true,
			SourceVersion: 77,
			Source: &PlaybackSourceDescriptor{
				Kind:  SourceKindTracks,
				ID:    "tracks",
				Title: "Tracks",
				Live:  true,
			},
			Anchor: &PlaybackSourceAnchor{
				RecordingID: "rec-2",
			},
			ShuffleBag: []int{9, 7, 3},
		},
		UserQueue: []SessionEntry{{
			EntryID: "queued-1",
			Origin:  EntryOriginQueued,
			Item:    SessionItem{RecordingID: "rec-q", Title: "Queued"},
		}},
		EntryAvailability: map[string]apitypes.RecordingPlaybackAvailability{
			"ctx-2": makePlayableAvailability("rec-2"),
		},
		QueueLength:  1501,
		QueueVersion: 42,
	}

	event := BuildQueueEventSnapshot(snapshot)
	if event.ContextQueue == nil {
		t.Fatal("expected context queue payload")
	}
	if event.ContextQueue.CurrentIndex != 512 || event.ContextQueue.ResumeIndex != 480 {
		t.Fatalf("unexpected queue cursor fields: %+v", event.ContextQueue)
	}
	if event.ContextQueue.WindowStart != 500 || event.ContextQueue.WindowCount != 50 {
		t.Fatalf("unexpected queue window fields: %+v", event.ContextQueue)
	}
	if !event.ContextQueue.Loading || event.ContextQueue.SourceVersion != 77 {
		t.Fatalf("unexpected loading/source version fields: %+v", event.ContextQueue)
	}
	if event.ContextQueue.Source == nil || event.ContextQueue.Source.Kind != SourceKindTracks {
		t.Fatalf("expected source descriptor, got %+v", event.ContextQueue.Source)
	}
	if event.ContextQueue.Anchor == nil || event.ContextQueue.Anchor.RecordingID != "rec-2" {
		t.Fatalf("expected anchor payload, got %+v", event.ContextQueue.Anchor)
	}
	if len(event.ContextQueue.ShuffleBag) != 3 {
		t.Fatalf("expected shuffle bag payload, got %+v", event.ContextQueue.ShuffleBag)
	}
	if event.QueueLength != 1501 || event.QueueVersion != 42 {
		t.Fatalf("unexpected queue metadata: length=%d version=%d", event.QueueLength, event.QueueVersion)
	}
	if event.EntryAvailability["ctx-2"].RecordingID != "rec-2" {
		t.Fatalf("expected entry availability payload, got %+v", event.EntryAvailability)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal queue event snapshot: %v", err)
	}
	if jsonContainsKey(payload, "allEntries") {
		t.Fatalf("queue event leaked internal allEntries payload: %s", string(payload))
	}
}

func jsonContainsKey(payload []byte, key string) bool {
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return false
	}
	_, ok := decoded[key]
	if ok {
		return true
	}
	contextQueue, ok := decoded["contextQueue"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = contextQueue[key]
	return ok
}
