package playback

import (
	"context"
	"database/sql"
	"encoding/json"
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
	defer func() { _ = store.Close() }()

	duration := int64(180000)
	err = store.Save(context.Background(), SessionSnapshot{
		ContextQueue: &ContextQueue{
			Kind:         ContextKindAlbum,
			ID:           "album-1",
			StartIndex:   0,
			CurrentIndex: 0,
			ResumeIndex:  0,
			ShuffleBag:   []int{0},
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
	defer func() { _ = store.Close() }()

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != StatusIdle {
		t.Fatalf("expected idle status, got %q", snapshot.Status)
	}
	if snapshot.Volume != DefaultVolume {
		t.Fatalf("expected default volume %d, got %d", DefaultVolume, snapshot.Volume)
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
	defer func() { _ = store.Close() }()

	raw, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatalf("open raw sqlite connection: %v", err)
	}
	defer func() { _ = raw.Close() }()

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
	if snapshot.Volume != DefaultVolume {
		t.Fatalf("expected default volume %d after reset, got %d", DefaultVolume, snapshot.Volume)
	}
	if snapshot.QueueLength != 0 {
		t.Fatalf("expected empty queue after reset, got %d", snapshot.QueueLength)
	}
}

func TestSQLiteStoreSchemaMismatchPreservesShufflePreference(t *testing.T) {
	t.Parallel()

	storePath := t.TempDir() + "\\playback-state.db"
	store, err := NewSQLiteStore(storePath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Save(context.Background(), SessionSnapshot{
		Shuffle:   true,
		Volume:    37,
		Status:    StatusPaused,
		UpdatedAt: formatTimestamp(time.Now().UTC()),
	}); err != nil {
		t.Fatalf("save shuffled snapshot: %v", err)
	}

	raw, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatalf("open raw sqlite connection: %v", err)
	}
	defer func() { _ = raw.Close() }()

	if _, err := raw.Exec(`UPDATE playback_session_state SET schema_version = ? WHERE id = ?`, playbackSessionSchemaVersion-1, playbackSessionStateRowID); err != nil {
		t.Fatalf("downgrade schema version: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM playback_preferences WHERE id = ?`, playbackPreferenceRowID); err != nil {
		t.Fatalf("delete independent preference row: %v", err)
	}

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load schema-mismatched snapshot: %v", err)
	}
	if !snapshot.Shuffle {
		t.Fatalf("expected shuffle preference to survive schema reset, got %+v", snapshot)
	}
	if snapshot.Volume != DefaultVolume {
		t.Fatalf("expected incompatible session snapshot to reset volume to default %d, got %d", DefaultVolume, snapshot.Volume)
	}

	var sessionRows int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM playback_session_state WHERE id = ?`, playbackSessionStateRowID).Scan(&sessionRows); err != nil {
		t.Fatalf("count session rows: %v", err)
	}
	if sessionRows != 0 {
		t.Fatalf("expected incompatible session snapshot row to be deleted, got %d rows", sessionRows)
	}

	snapshot, err = store.Load(context.Background())
	if err != nil {
		t.Fatalf("reload after schema reset: %v", err)
	}
	if !snapshot.Shuffle {
		t.Fatalf("expected independent shuffle preference to survive after session row deletion, got %+v", snapshot)
	}
}

func TestSQLiteStoreSavedShufflePreferenceFollowsLatestSnapshot(t *testing.T) {
	t.Parallel()

	storePath := t.TempDir() + "\\playback-state.db"
	store, err := NewSQLiteStore(storePath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Save(context.Background(), SessionSnapshot{
		Shuffle:   true,
		Status:    StatusPaused,
		UpdatedAt: formatTimestamp(time.Now().UTC()),
	}); err != nil {
		t.Fatalf("save shuffled snapshot: %v", err)
	}
	if err := store.Save(context.Background(), SessionSnapshot{
		Shuffle:   false,
		Status:    StatusPaused,
		UpdatedAt: formatTimestamp(time.Now().UTC()),
	}); err != nil {
		t.Fatalf("save unshuffled snapshot: %v", err)
	}

	raw, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatalf("open raw sqlite connection: %v", err)
	}
	defer func() { _ = raw.Close() }()

	if _, err := raw.Exec(`DELETE FROM playback_session_state WHERE id = ?`, playbackSessionStateRowID); err != nil {
		t.Fatalf("delete session row: %v", err)
	}

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load after session deletion: %v", err)
	}
	if snapshot.Shuffle {
		t.Fatalf("expected latest saved shuffle preference to be false, got %+v", snapshot)
	}
}

func TestDefaultSessionSnapshotUsesDefaultVolumeWhenPayloadOmitsIt(t *testing.T) {
	t.Parallel()

	snapshot := defaultSessionSnapshot()
	if err := json.Unmarshal([]byte(`{"status":"paused","queueLength":0}`), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot without volume: %v", err)
	}

	if snapshot.Volume != DefaultVolume {
		t.Fatalf("expected default volume %d, got %d", DefaultVolume, snapshot.Volume)
	}
	if snapshot.Status != StatusPaused {
		t.Fatalf("expected paused status, got %q", snapshot.Status)
	}
}

func TestSQLiteStoreExplicitMuteVolumeSurvivesLoad(t *testing.T) {
	t.Parallel()

	storePath := t.TempDir() + "\\playback-state.db"
	store, err := NewSQLiteStore(storePath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Save(context.Background(), SessionSnapshot{
		Volume:    0,
		Status:    StatusPaused,
		UpdatedAt: formatTimestamp(time.Now().UTC()),
	}); err != nil {
		t.Fatalf("save muted snapshot: %v", err)
	}

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load muted snapshot: %v", err)
	}
	if snapshot.Volume != 0 {
		t.Fatalf("expected muted volume to survive load, got %d", snapshot.Volume)
	}
}
