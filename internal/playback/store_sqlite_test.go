package playback

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestSQLiteStoreRestoresPlayingAsPaused(t *testing.T) {
	t.Parallel()

	storePath := t.TempDir() + "\\playback-state.db"
	store, err := NewSQLiteStore(storePath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	duration := int64(180000)
	err = store.Save(context.Background(), SessionSnapshot{
		ContextQueue: &ContextQueue{
			Kind: ContextKindAlbum,
			ID:   "album-1",
			StartIndex: 0,
			CurrentIndex: 0,
			ResumeIndex: 0,
			ShuffleBag: []int{0},
			Entries: []SessionEntry{
				{
					EntryID:      "ctx-1",
					Origin:       EntryOriginContext,
					ContextIndex: 0,
					Item: SessionItem{
						RecordingID: "rec-1",
						Title:       "Track 1",
						DurationMS:  duration,
					},
				},
			},
		},
		CurrentEntryID: "ctx-1",
		CurrentEntry:   &SessionEntry{EntryID: "ctx-1", Origin: EntryOriginContext, ContextIndex: 0, Item: SessionItem{RecordingID: "rec-1", Title: "Track 1", DurationMS: duration}},
		RepeatMode:     RepeatAll,
		Shuffle:        true,
		Volume:         42,
		Status:         StatusPlaying,
		PositionMS:     1500,
		DurationMS:     &duration,
		UpdatedAt:      formatTimestamp(time.Now().UTC()),
		LastError:      "seed",
	})
	if err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != StatusPaused {
		t.Fatalf("expected restored status %q, got %q", StatusPaused, snapshot.Status)
	}
	if snapshot.PositionMS != 1500 {
		t.Fatalf("expected restored position 1500, got %d", snapshot.PositionMS)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current entry rec-1, got %#v", snapshot.CurrentEntry)
	}
	if snapshot.LastError != "seed" {
		t.Fatalf("expected last error to round-trip, got %q", snapshot.LastError)
	}
}

func TestSQLiteStoreMissingDatabaseLoadsIdle(t *testing.T) {
	t.Parallel()

	storePath := t.TempDir() + "\\playback-state.db"
	store, err := NewSQLiteStore(storePath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != StatusIdle {
		t.Fatalf("expected idle status, got %q", snapshot.Status)
	}
	if snapshot.QueueLength != 0 {
		t.Fatalf("expected empty queue length, got %d", snapshot.QueueLength)
	}
}

func TestSQLiteStoreCorruptPayloadResetsCleanly(t *testing.T) {
	t.Parallel()

	storePath := t.TempDir() + "\\playback-state.db"
	store, err := NewSQLiteStore(storePath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	raw, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatalf("open raw sqlite connection: %v", err)
	}
	defer raw.Close()

	_, err = raw.Exec(`
INSERT INTO playback_session_state
	(id, snapshot_json, updated_at)
VALUES
	(1, '{not-json', CURRENT_TIMESTAMP)
`)
	if err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load corrupt snapshot: %v", err)
	}
	if snapshot.Status != StatusIdle {
		t.Fatalf("expected idle status after reset, got %q", snapshot.Status)
	}
	if snapshot.QueueLength != 0 {
		t.Fatalf("expected empty queue after reset, got %d", snapshot.QueueLength)
	}
}

