package playback

import (
	"context"
	"testing"

	apitypes "ben/desktop/api/types"
)

func TestSessionPlayRestoresPersistedPositionOnFirstPlay(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	store := &memoryStore{
		snapshot: SessionSnapshot{
			ContextQueue: &ContextQueue{
				Kind: ContextKindAlbum,
				ID:   "album-1",
				StartIndex: 0,
				CurrentIndex: 0,
				ResumeIndex: 0,
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
			Volume:         60,
			Status:         StatusPaused,
			PositionMS:     2500,
			DurationMS:     &duration,
		},
	}
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test.mp3",
			},
		},
	}, backend, store, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if backend.position != 2500 {
		t.Fatalf("backend position = %d, want 2500", backend.position)
	}
	if snapshot.PositionMS != 2500 {
		t.Fatalf("snapshot position = %d, want 2500", snapshot.PositionMS)
	}
	if snapshot.Status != StatusPlaying {
		t.Fatalf("status = %q, want %q", snapshot.Status, StatusPlaying)
	}
}

