package playback

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

type memoryStore struct {
	snapshot SessionSnapshot
}

func (s *memoryStore) Load(context.Context) (SessionSnapshot, error) {
	return s.snapshot, nil
}

func (s *memoryStore) Save(_ context.Context, snapshot SessionSnapshot) error {
	s.snapshot = snapshot
	return nil
}

func (s *memoryStore) Clear(context.Context) error {
	s.snapshot = SessionSnapshot{}
	return nil
}

type mockBridge struct {
	results             map[string]apitypes.PlaybackResolveResult
	availability        map[string]apitypes.RecordingPlaybackAvailability
	preparations        map[string][]apitypes.PlaybackPreparationStatus
	recording           map[string]apitypes.RecordingListItem
	albumTracks         map[string][]apitypes.AlbumTrackItem
	playlistTracks      map[string][]apitypes.PlaylistTrackItem
	likedRecordings     []apitypes.LikedRecordingItem
	prepareCalls        map[string]int
	getPreparationCalls map[string]int
}

func (b *mockBridge) Close() error { return nil }

func (b *mockBridge) ListRecordings(context.Context, apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return apitypes.Page[apitypes.RecordingListItem]{}, nil
}

func (b *mockBridge) GetRecording(_ context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	if item, ok := b.recording[recordingID]; ok {
		return item, nil
	}
	return apitypes.RecordingListItem{}, fmt.Errorf("unexpected recording %s", recordingID)
}

func (b *mockBridge) ListAlbumTracks(_ context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return paginateTestItems(b.albumTracks[strings.TrimSpace(req.AlbumID)], req.PageRequest), nil
}

func (b *mockBridge) ListPlaylistTracks(_ context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return paginateTestItems(b.playlistTracks[strings.TrimSpace(req.PlaylistID)], req.PageRequest), nil
}

func (b *mockBridge) ListLikedRecordings(_ context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return paginateTestItems(b.likedRecordings, req.PageRequest), nil
}

func (b *mockBridge) ResolvePlaybackRecording(_ context.Context, recordingID, _ string) (apitypes.PlaybackResolveResult, error) {
	result, ok := b.results[recordingID]
	if !ok {
		return apitypes.PlaybackResolveResult{}, fmt.Errorf("unexpected recording %s", recordingID)
	}
	return result, nil
}

func (b *mockBridge) ResolveArtworkRef(context.Context, apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	return apitypes.ArtworkResolveResult{}, nil
}

func (b *mockBridge) ResolveAlbumArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, nil
}

func (b *mockBridge) InspectPlaybackRecording(_ context.Context, recordingID, _ string) (apitypes.PlaybackPreparationStatus, error) {
	return b.nextPreparationStatus(recordingID, false)
}

func (b *mockBridge) PreparePlaybackRecording(_ context.Context, recordingID, _ string, _ apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	if b.prepareCalls == nil {
		b.prepareCalls = make(map[string]int)
	}
	b.prepareCalls[recordingID]++
	return b.nextPreparationStatus(recordingID, true)
}

func (b *mockBridge) PreparePlaybackTarget(ctx context.Context, target PlaybackTargetRef, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return b.PreparePlaybackRecording(ctx, testPlaybackTargetInputID(target), preferredProfile, purpose)
}

func (b *mockBridge) GetPlaybackPreparation(_ context.Context, recordingID, _ string) (apitypes.PlaybackPreparationStatus, error) {
	if b.getPreparationCalls == nil {
		b.getPreparationCalls = make(map[string]int)
	}
	b.getPreparationCalls[recordingID]++
	return b.nextPreparationStatus(recordingID, true)
}

func (b *mockBridge) GetPlaybackTargetPreparation(ctx context.Context, target PlaybackTargetRef, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.GetPlaybackPreparation(ctx, testPlaybackTargetInputID(target), preferredProfile)
}

func (b *mockBridge) ResolveRecordingArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, nil
}

func (b *mockBridge) GetRecordingAvailability(_ context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	if item, ok := b.availability[recordingID]; ok {
		return item, nil
	}
	if result, ok := b.results[recordingID]; ok {
		return apitypes.RecordingPlaybackAvailability{
			RecordingID:      recordingID,
			PreferredProfile: preferredProfile,
			State:            result.State,
			SourceKind:       result.SourceKind,
			Reason:           result.Reason,
			LocalPath:        result.PlayableURI,
		}, nil
	}
	return apitypes.RecordingPlaybackAvailability{
		RecordingID:      recordingID,
		PreferredProfile: preferredProfile,
	}, nil
}

func (b *mockBridge) GetPlaybackTargetAvailability(ctx context.Context, target PlaybackTargetRef, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return b.GetRecordingAvailability(ctx, testPlaybackTargetInputID(target), preferredProfile)
}

func (b *mockBridge) ListRecordingPlaybackAvailability(_ context.Context, req apitypes.RecordingPlaybackAvailabilityListRequest) ([]apitypes.RecordingPlaybackAvailability, error) {
	out := make([]apitypes.RecordingPlaybackAvailability, 0, len(req.RecordingIDs))
	for _, recordingID := range req.RecordingIDs {
		item, err := b.GetRecordingAvailability(context.Background(), recordingID, req.PreferredProfile)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (b *mockBridge) ListPlaybackTargetAvailability(ctx context.Context, req TargetAvailabilityRequest) ([]TargetAvailability, error) {
	out := make([]TargetAvailability, 0, len(req.Targets))
	for _, target := range req.Targets {
		status, err := b.GetPlaybackTargetAvailability(ctx, target, req.PreferredProfile)
		if err != nil {
			return nil, err
		}
		out = append(out, TargetAvailability{
			Target: target,
			Status: status,
		})
	}
	return out, nil
}

func testPlaybackTargetInputID(target PlaybackTargetRef) string {
	switch target.ResolutionPolicy {
	case PlaybackTargetResolutionExact:
		return firstNonEmpty(target.ExactVariantRecordingID, target.LogicalRecordingID)
	default:
		return firstNonEmpty(target.LogicalRecordingID, target.ExactVariantRecordingID)
	}
}

func (b *mockBridge) nextPreparationStatus(recordingID string, advance bool) (apitypes.PlaybackPreparationStatus, error) {
	if rows := b.preparations[recordingID]; len(rows) > 0 {
		status := rows[0]
		if advance && len(rows) > 1 {
			b.preparations[recordingID] = rows[1:]
		}
		return status, nil
	}
	result, ok := b.results[recordingID]
	if !ok {
		return apitypes.PlaybackPreparationStatus{}, fmt.Errorf("unexpected recording %s", recordingID)
	}
	status := apitypes.PlaybackPreparationStatus{
		RecordingID: recordingID,
		SourceKind:  result.SourceKind,
		PlayableURI: result.PlayableURI,
		EncodingID:  result.EncodingID,
		BlobID:      result.BlobID,
	}
	switch result.State {
	case apitypes.AvailabilityPlayableLocalFile, apitypes.AvailabilityPlayableCachedOpt, apitypes.AvailabilityPlayableRemoteOpt:
		status.Phase = apitypes.PlaybackPreparationReady
	case apitypes.AvailabilityWaitingTranscode:
		status.Phase = apitypes.PlaybackPreparationPreparingTranscode
	default:
		status.Phase = apitypes.PlaybackPreparationUnavailable
		status.Reason = result.Reason
	}
	return status, nil
}

type testBackend struct {
	loadedURI       string
	loadCalls       int
	loadErr         error
	playErr         error
	preloadedURI    string
	preloadCalls    int
	position        int64
	duration        *int64
	volume          int
	stopCalls       int
	events          chan BackendEvent
	supportsPreload bool
}

type blockingLoadBackend struct {
	*testBackend
	blockURI string
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func newTestBackend() *testBackend {
	return &testBackend{
		events: make(chan BackendEvent, 8),
	}
}

func newBlockingLoadBackend(blockURI string) *blockingLoadBackend {
	return &blockingLoadBackend{
		testBackend: newTestBackend(),
		blockURI:    blockURI,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (b *blockingLoadBackend) Load(ctx context.Context, uri string) error {
	if uri == b.blockURI {
		b.once.Do(func() {
			close(b.entered)
		})
		<-b.release
	}
	return b.testBackend.Load(ctx, uri)
}

func (b *testBackend) Load(_ context.Context, uri string) error {
	b.loadedURI = uri
	b.loadCalls++
	b.position = 0
	return b.loadErr
}

func (b *testBackend) Play(context.Context) error  { return b.playErr }
func (b *testBackend) Pause(context.Context) error { return nil }
func (b *testBackend) Stop(context.Context) error {
	b.stopCalls++
	b.position = 0
	return nil
}

func (b *testBackend) SeekTo(_ context.Context, positionMS int64) error {
	b.position = positionMS
	return nil
}

func (b *testBackend) SetVolume(_ context.Context, volume int) error {
	b.volume = volume
	return nil
}

func (b *testBackend) PositionMS() (int64, error)  { return b.position, nil }
func (b *testBackend) DurationMS() (*int64, error) { return cloneInt64Ptr(b.duration), nil }
func (b *testBackend) Events() <-chan BackendEvent { return b.events }
func (b *testBackend) SupportsPreload() bool       { return b.supportsPreload }
func (b *testBackend) PreloadNext(_ context.Context, uri string) error {
	b.preloadedURI = uri
	b.preloadCalls++
	return nil
}

func (b *testBackend) ClearPreloaded(context.Context) error {
	b.preloadedURI = ""
	return nil
}

func (b *testBackend) Close() error {
	close(b.events)
	return nil
}

func paginateTestItems[T any](items []T, req apitypes.PageRequest) apitypes.Page[T] {
	limit := req.Limit
	if limit <= 0 {
		limit = len(items)
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return apitypes.Page[T]{
		Items: append([]T(nil), items[offset:end]...),
		Page: apitypes.PageInfo{
			Offset:     offset,
			Limit:      limit,
			Returned:   end - offset,
			Total:      len(items),
			HasMore:    end < len(items),
			NextOffset: end,
		},
	}
}

func TestSessionStartRestoresPausedState(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
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
			Status:         StatusPlaying,
			PositionMS:     2500,
			DurationMS:     &duration,
		},
	}
	session := NewSession(&mockBridge{}, newTestBackend(), store, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	snapshot := session.Snapshot()
	if snapshot.Status != StatusPaused {
		t.Fatalf("expected paused snapshot, got %q", snapshot.Status)
	}
	if snapshot.PositionMS != 2500 {
		t.Fatalf("expected restored position 2500, got %d", snapshot.PositionMS)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current entry rec-1, got %#v", snapshot.CurrentEntry)
	}
}

func TestSessionPlayLoadsResolvedURI(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if backend.loadedURI != "file:///tmp/test.mp3" {
		t.Fatalf("expected loaded URI file:///tmp/test.mp3, got %q", backend.loadedURI)
	}
	if snapshot.Status != StatusPlaying {
		t.Fatalf("expected playing status, got %q", snapshot.Status)
	}
}

func TestSessionSelectEntryIgnoresCurrentEntryWithoutResettingPosition(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test-1.mp3",
			},
			"rec-2": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test-2.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	snapshot, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "Track 1"},
			{RecordingID: "rec-2", Title: "Track 2"},
		},
	})
	if err != nil {
		t.Fatalf("set context: %v", err)
	}
	snapshot, err = session.Play(context.Background())
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.SeekTo(context.Background(), 12000); err != nil {
		t.Fatalf("seek: %v", err)
	}

	loadCallsBefore := backend.loadCalls
	selected, err := session.SelectEntry(context.Background(), snapshot.CurrentEntry.EntryID)
	if err != nil {
		t.Fatalf("select current entry: %v", err)
	}
	if backend.loadCalls != loadCallsBefore {
		t.Fatalf("expected current entry selection not to reload backend, load calls = %d want %d", backend.loadCalls, loadCallsBefore)
	}
	if selected.PositionMS != 12000 {
		t.Fatalf("expected current entry selection to preserve position, got %d", selected.PositionMS)
	}
	if selected.CurrentEntry == nil || selected.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected rec-1 to remain current, got %+v", selected.CurrentEntry)
	}
}

func TestSessionConcurrentSelectEntryAppliesLatestSelection(t *testing.T) {
	t.Parallel()

	backend := newBlockingLoadBackend("file:///tmp/test-2.mp3")
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test-1.mp3",
			},
			"rec-2": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test-2.mp3",
			},
			"rec-3": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test-3.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	snapshot, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "Track 1"},
			{RecordingID: "rec-2", Title: "Track 2"},
			{RecordingID: "rec-3", Title: "Track 3"},
		},
	})
	if err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	entry2ID := snapshot.ContextQueue.Entries[1].EntryID
	entry3ID := snapshot.ContextQueue.Entries[2].EntryID

	errs := make(chan error, 2)
	go func() {
		_, err := session.SelectEntry(context.Background(), entry2ID)
		errs <- err
	}()

	select {
	case <-backend.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first queued selection to reach backend load")
	}

	go func() {
		_, err := session.SelectEntry(context.Background(), entry3ID)
		errs <- err
	}()

	close(backend.release)

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("select entry: %v", err)
		}
	}

	final := session.Snapshot()
	if final.CurrentEntry == nil || final.CurrentEntry.Item.RecordingID != "rec-3" {
		t.Fatalf("expected latest selected entry rec-3 to win, got %+v", final.CurrentEntry)
	}
	if backend.loadedURI != "file:///tmp/test-3.mp3" {
		t.Fatalf("expected backend to finish on rec-3 URI, got %q", backend.loadedURI)
	}
}

func TestSessionClosePersistsFinalPosition(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
	store := &memoryStore{}
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

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1", DurationMS: duration}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.position = 4321

	if err := session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}

	if store.snapshot.PositionMS != 4321 {
		t.Fatalf("expected persisted position 4321, got %d", store.snapshot.PositionMS)
	}
	if store.snapshot.Status != StatusPaused {
		t.Fatalf("expected persisted status paused, got %q", store.snapshot.Status)
	}
}

func TestQueueEntriesPlayBeforeRemainingContext(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1":  {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2":  {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"queued": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/queued.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindAlbum,
		ID:   "album-1",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One", Subtitle: "Artist A", AlbumID: "album-1"},
			{RecordingID: "ctx-2", Title: "Two", Subtitle: "Artist B", AlbumID: "album-1"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "queued", Title: "Queued"}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "queued" {
		t.Fatalf("expected queued item to play next, got %#v", snapshot.CurrentEntry)
	}
	if len(snapshot.UserQueue) != 0 {
		t.Fatalf("expected queued entries to be consumed, got %d", len(snapshot.UserQueue))
	}
}

func TestSessionQueuedPlaybackResumesRemainingContext(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"ctx-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"q-1":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q1.mp3"},
			"q-2":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q2.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindAlbum,
		ID:   "album-1",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One"},
			{RecordingID: "ctx-2", Title: "Two"},
			{RecordingID: "ctx-3", Title: "Three"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{
		{RecordingID: "q-1", Title: "Queue One"},
		{RecordingID: "q-2", Title: "Queue Two"},
	}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next to first queued: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "q-1" {
		t.Fatalf("expected first queued item, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.ContextQueue.ResumeIndex != 1 {
		t.Fatalf("resume context index = %d, want 1", snapshot.ContextQueue.ResumeIndex)
	}
	if len(snapshot.UpcomingEntries) < 3 {
		t.Fatalf("expected queued and remaining context in upcoming entries, got %+v", snapshot.UpcomingEntries)
	}
	if snapshot.UpcomingEntries[0].Item.RecordingID != "q-2" || snapshot.UpcomingEntries[1].Item.RecordingID != "ctx-2" {
		t.Fatalf("unexpected upcoming order while queued: %+v", snapshot.UpcomingEntries)
	}

	snapshot, err = session.Next(context.Background())
	if err != nil {
		t.Fatalf("next to second queued: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "q-2" {
		t.Fatalf("expected second queued item, got %+v", snapshot.CurrentEntry)
	}

	snapshot, err = session.Next(context.Background())
	if err != nil {
		t.Fatalf("next back to context: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "ctx-2" {
		t.Fatalf("expected resume to ctx-2, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionPreviousFromUserQueueReturnsToContextAndKeepsRemovedUserTrackGone(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"ctx-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"q-1":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q1.mp3"},
			"q-2":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q2.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindAlbum,
		ID:   "album-1",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One"},
			{RecordingID: "ctx-2", Title: "Two"},
			{RecordingID: "ctx-3", Title: "Three"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{
		{RecordingID: "q-1", Title: "Queue One"},
		{RecordingID: "q-2", Title: "Queue Two"},
	}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next to user queue: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "q-1" {
		t.Fatalf("expected q-1 current, got %+v", snapshot.CurrentEntry)
	}

	snapshot, err = session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous back to context: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "ctx-1" {
		t.Fatalf("expected previous to return to ctx-1, got %+v", snapshot.CurrentEntry)
	}
	if len(snapshot.UserQueue) != 1 || snapshot.UserQueue[0].Item.RecordingID != "q-2" {
		t.Fatalf("expected only q-2 to remain queued, got %+v", snapshot.UserQueue)
	}

	snapshot, err = session.Next(context.Background())
	if err != nil {
		t.Fatalf("next back to remaining user queue: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "q-2" {
		t.Fatalf("expected q-2 current after returning forward, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionSelectingQueuedEntryKeepsContextResume(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"ctx-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"q-1":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q1.mp3"},
			"q-2":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q2.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindAlbum,
		ID:   "album-1",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One"},
			{RecordingID: "ctx-2", Title: "Two"},
			{RecordingID: "ctx-3", Title: "Three"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	queuedSnapshot, err := session.QueueItems([]SessionItem{
		{RecordingID: "q-1", Title: "Queue One"},
		{RecordingID: "q-2", Title: "Queue Two"},
	}, QueueInsertLast)
	if err != nil {
		t.Fatalf("queue items: %v", err)
	}
	if len(queuedSnapshot.UserQueue) != 2 {
		t.Fatalf("expected 2 queued entries, got %d", len(queuedSnapshot.UserQueue))
	}

	selected, err := session.SelectEntry(context.Background(), queuedSnapshot.UserQueue[1].EntryID)
	if err != nil {
		t.Fatalf("select second queued entry: %v", err)
	}
	if selected.CurrentEntry == nil || selected.CurrentEntry.Item.RecordingID != "q-2" {
		t.Fatalf("expected q-2 to become current, got %+v", selected.CurrentEntry)
	}
	if selected.ContextQueue.ResumeIndex != 1 {
		t.Fatalf("resume context index = %d, want 1", selected.ContextQueue.ResumeIndex)
	}

	if _, err := session.RemoveQueuedEntry(queuedSnapshot.UserQueue[0].EntryID); err != nil {
		t.Fatalf("remove remaining queued entry: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next back to context: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "ctx-2" {
		t.Fatalf("expected resume to ctx-2 after manual queue actions, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionShuffleWhileQueuedResumesUsingNewShuffleOrder(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"ctx-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"ctx-4": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/four.mp3"},
			"q-1":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q1.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindAlbum,
		ID:   "album-1",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One", Subtitle: "Artist A", AlbumID: "album-a"},
			{RecordingID: "ctx-2", Title: "Two", Subtitle: "Artist B", AlbumID: "album-b"},
			{RecordingID: "ctx-3", Title: "Three", Subtitle: "Artist C", AlbumID: "album-c"},
			{RecordingID: "ctx-4", Title: "Four", Subtitle: "Artist D", AlbumID: "album-d"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "q-1", Title: "Queue One"}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next to queued: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	if len(shuffled.ContextQueue.ShuffleBag) < 2 {
		t.Fatalf("unexpected shuffle cycle for anchored context: %v", shuffled.ContextQueue.ShuffleBag)
	}
	if shuffled.ContextQueue.ShuffleBag[0] != 0 {
		t.Fatalf("expected shuffle cycle to anchor last context index 0, got %v", shuffled.ContextQueue.ShuffleBag)
	}
	expectedIndex := shuffled.ContextQueue.ShuffleBag[1]
	expectedRecordingID := shuffled.ContextQueue.Entries[expectedIndex].Item.RecordingID

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next back to shuffled context: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != expectedRecordingID {
		t.Fatalf("expected resume to %s based on shuffle cycle %v, got %+v", expectedRecordingID, shuffled.ContextQueue.ShuffleBag, snapshot.CurrentEntry)
	}
}

func TestSessionSetContextPreservesStartIndexBeforeFirstPlay(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"ctx-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:       ContextKindCustom,
		ID:         "custom",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One"},
			{RecordingID: "ctx-2", Title: "Two"},
			{RecordingID: "ctx-3", Title: "Three"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "ctx-2" {
		t.Fatalf("expected initial play to honor start index, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionReplaceContextAndPlayResetsPreviousToNewContext(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"album-1":    {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/album-1.mp3"},
			"album-2":    {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/album-2.mp3"},
			"playlist-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/playlist-1.mp3"},
			"playlist-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/playlist-2.mp3"},
			"playlist-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/playlist-3.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:       ContextKindAlbum,
		ID:         "album",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "album-1", Title: "Album 1"},
			{RecordingID: "album-2", Title: "Album 2"},
		},
	}); err != nil {
		t.Fatalf("set album context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play album: %v", err)
	}

	snapshot, err := session.ReplaceContextAndPlay(context.Background(), PlaybackContextInput{
		Kind:       ContextKindPlaylist,
		ID:         "playlist",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "playlist-1", Title: "Playlist 1"},
			{RecordingID: "playlist-2", Title: "Playlist 2"},
			{RecordingID: "playlist-3", Title: "Playlist 3"},
		},
	})
	if err != nil {
		t.Fatalf("replace context and play: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "playlist-2" {
		t.Fatalf("expected playlist-2 to become current, got %+v", snapshot.CurrentEntry)
	}
	previous, err := session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous in new context: %v", err)
	}
	if previous.CurrentEntry == nil || previous.CurrentEntry.Item.RecordingID != "playlist-1" {
		t.Fatalf("expected previous to stay within new context, got %+v", previous.CurrentEntry)
	}
}

func TestSessionPendingContextReplacementDoesNotLeakOldContextIntoHistory(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"album-1": {
				{RecordingID: "album-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/album-1.mp3"},
			},
			"album-2": {
				{RecordingID: "album-2", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/album-2.mp3"},
			},
			"playlist-1": {
				{RecordingID: "playlist-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/playlist-1.mp3"},
			},
			"playlist-2": {
				{RecordingID: "playlist-2", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:       ContextKindAlbum,
		ID:         "album",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "album-1", Title: "Album 1"},
			{RecordingID: "album-2", Title: "Album 2"},
		},
	}); err != nil {
		t.Fatalf("set album context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play album: %v", err)
	}

	pending, err := session.ReplaceContextAndPlay(context.Background(), PlaybackContextInput{
		Kind:       ContextKindPlaylist,
		ID:         "playlist",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "playlist-1", Title: "Playlist 1"},
			{RecordingID: "playlist-2", Title: "Playlist 2"},
		},
	})
	if err != nil {
		t.Fatalf("replace context and play: %v", err)
	}
	if pending.CurrentEntry == nil || pending.CurrentEntry.Item.RecordingID != "album-2" {
		t.Fatalf("expected old track to remain current while pending, got %+v", pending.CurrentEntry)
	}
	if pending.LoadingEntry == nil || pending.LoadingEntry.Item.RecordingID != "playlist-2" {
		t.Fatalf("expected pending playlist-2 entry, got %+v", pending.LoadingEntry)
	}

	if err := session.completePendingPlayback(context.Background(), *pending.LoadingEntry, apitypes.PlaybackPreparationStatus{
		RecordingID: "playlist-2",
		Phase:       apitypes.PlaybackPreparationReady,
		SourceKind:  apitypes.PlaybackSourceLocalFile,
		PlayableURI: "file:///tmp/playlist-2.mp3",
	}); err != nil {
		t.Fatalf("complete pending playback: %v", err)
	}

	resolved := session.Snapshot()
	if resolved.CurrentEntry == nil || resolved.CurrentEntry.Item.RecordingID != "playlist-2" {
		t.Fatalf("expected playlist-2 after pending completion, got %+v", resolved.CurrentEntry)
	}
	previous, err := session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous after pending replacement: %v", err)
	}
	if previous.CurrentEntry == nil || previous.CurrentEntry.Item.RecordingID != "playlist-1" {
		t.Fatalf("expected previous to resolve inside playlist context, got %+v", previous.CurrentEntry)
	}
}

func TestSessionPreviousFallsBackToPreviousShuffledContextEntry(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"rec-4": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/four.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", Subtitle: "Artist A", AlbumID: "album-a"},
			{RecordingID: "rec-2", Title: "Two", Subtitle: "Artist B", AlbumID: "album-b"},
			{RecordingID: "rec-3", Title: "Three", Subtitle: "Artist C", AlbumID: "album-c"},
			{RecordingID: "rec-4", Title: "Four", Subtitle: "Artist D", AlbumID: "album-d"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	if len(shuffled.ContextQueue.ShuffleBag) < 3 {
		t.Fatalf("expected shuffle cycle with at least 3 items, got %v", shuffled.ContextQueue.ShuffleBag)
	}

	targetIndex := shuffled.ContextQueue.ShuffleBag[2]
	target := shuffled.ContextQueue.Entries[targetIndex]
	if _, err := session.playEntry(context.Background(), target, EntryOriginContext, -1, nil, false, true); err != nil {
		t.Fatalf("play shuffled middle entry: %v", err)
	}

	previous, err := session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous in shuffled context: %v", err)
	}

	expectedIndex := shuffled.ContextQueue.ShuffleBag[1]
	expected := shuffled.ContextQueue.Entries[expectedIndex].Item.RecordingID
	if previous.CurrentEntry == nil || previous.CurrentEntry.Item.RecordingID != expected {
		t.Fatalf("expected shuffled previous %s, got %+v", expected, previous.CurrentEntry)
	}
}

func TestSessionPreviousDoesNotStepBeforeShuffleAnchor(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"rec-4": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/four.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:       ContextKindCustom,
		ID:         "custom",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", Subtitle: "Artist A", AlbumID: "album-a"},
			{RecordingID: "rec-2", Title: "Two", Subtitle: "Artist B", AlbumID: "album-b"},
			{RecordingID: "rec-3", Title: "Three", Subtitle: "Artist C", AlbumID: "album-c"},
			{RecordingID: "rec-4", Title: "Four", Subtitle: "Artist D", AlbumID: "album-d"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	if len(shuffled.ContextQueue.ShuffleBag) == 0 {
		t.Fatalf("expected non-empty shuffle cycle")
	}
	if shuffled.ContextQueue.ShuffleBag[0] != 1 {
		t.Fatalf("expected shuffle cycle to anchor current index 1, got %v", shuffled.ContextQueue.ShuffleBag)
	}

	previous, err := session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous at shuffle anchor: %v", err)
	}
	if previous.CurrentEntry == nil || previous.CurrentEntry.Item.RecordingID != "rec-2" {
		t.Fatalf("expected previous to stay on rec-2 at shuffle start, got %+v", previous.CurrentEntry)
	}
}

func TestSessionSetShuffleClearsAndRebuildsPreloadWithoutChangingCurrent(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"rec-4": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/four.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", Subtitle: "Artist A", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", Subtitle: "Artist B", DurationMS: duration},
			{RecordingID: "rec-3", Title: "Three", Subtitle: "Artist C", DurationMS: duration},
			{RecordingID: "rec-4", Title: "Four", Subtitle: "Artist D", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected linear preload for rec-2, got %q", backend.preloadedURI)
	}

	currentRecording := session.Snapshot().CurrentEntry.Item.RecordingID
	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	if shuffled.CurrentEntry == nil || shuffled.CurrentEntry.Item.RecordingID != currentRecording {
		t.Fatalf("expected current track to stay fixed on shuffle toggle, got %+v", shuffled.CurrentEntry)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected stale preload cleared on shuffle toggle, got %q", backend.preloadedURI)
	}

	shuffledUpcoming := buildUpcomingEntries(shuffled)
	if len(shuffledUpcoming) == 0 {
		t.Fatalf("expected shuffled upcoming entries")
	}
	session.preloadNext(context.Background())
	expectedShuffled := bridge.results[shuffledUpcoming[0].Item.RecordingID].PlayableURI
	if backend.preloadedURI != expectedShuffled {
		t.Fatalf("expected shuffled preload %q, got %q", expectedShuffled, backend.preloadedURI)
	}

	unshuffled, err := session.SetShuffle(false)
	if err != nil {
		t.Fatalf("unset shuffle: %v", err)
	}
	if unshuffled.CurrentEntry == nil || unshuffled.CurrentEntry.Item.RecordingID != currentRecording {
		t.Fatalf("expected current track to stay fixed after disabling shuffle, got %+v", unshuffled.CurrentEntry)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected shuffle-off to clear stale preload, got %q", backend.preloadedURI)
	}

	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected linear preload after disabling shuffle, got %q", backend.preloadedURI)
	}
}

func TestSessionSetRepeatModeClearsStalePreload(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected rec-2 preloaded, got %q", backend.preloadedURI)
	}

	snapshot, err := session.SetRepeatMode(string(RepeatOne))
	if err != nil {
		t.Fatalf("set repeat mode: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current track to remain rec-1, got %+v", snapshot.CurrentEntry)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected stale preload cleared on repeat change, got %q", backend.preloadedURI)
	}
	if snapshot.NextPreparation != nil {
		t.Fatalf("expected repeat change to invalidate next preparation, got %+v", snapshot.NextPreparation)
	}
}

func TestSessionEOFUsesPreloadedTrack(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	backend.supportsPreload = true
	duration := int64(120000)
	backend.duration = &duration
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", Subtitle: "Artist A", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", Subtitle: "Artist B", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected no immediate preload before thresholds, got %q", backend.preloadedURI)
	}

	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected preloaded URI file:///tmp/two.mp3, got %q", backend.preloadedURI)
	}

	backend.events <- BackendEvent{Type: BackendEventTrackEnd, Reason: TrackEndReasonEOF}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.CurrentEntry != nil && snapshot.CurrentEntry.Item.RecordingID == "rec-2" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected preloaded next track to become current, got %+v", session.Snapshot())
}

func TestSessionPlaySkipsLeadingUnavailableContextEntries(t *testing.T) {
	t.Parallel()

	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:  apitypes.AvailabilityUnavailableNoPath,
				Reason: apitypes.PlaybackUnavailableNoPath,
			},
			"rec-2": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/two.mp3",
			},
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": {
				RecordingID: "rec-1",
				State:       apitypes.AvailabilityUnavailableNoPath,
				Reason:      apitypes.PlaybackUnavailableNoPath,
			},
			"rec-2": {
				RecordingID: "rec-2",
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
			},
		},
	}, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One"},
			{RecordingID: "rec-2", Title: "Two"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-2" {
		t.Fatalf("expected current entry rec-2, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LastSkipEvent == nil || snapshot.LastSkipEvent.Count != 1 {
		t.Fatalf("expected one skipped item event, got %+v", snapshot.LastSkipEvent)
	}
}

func TestSessionQueueMutationClearsStalePreload(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected rec-2 preloaded, got %q", backend.preloadedURI)
	}

	if _, err := session.QueueItems([]SessionItem{{RecordingID: "rec-3", Title: "Three", DurationMS: duration}}, QueueInsertNext); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected stale preload cleared after queue mutation, got %q", backend.preloadedURI)
	}
}

func TestSessionClearQueueClearsStalePreload(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected rec-2 preloaded, got %q", backend.preloadedURI)
	}

	if _, err := session.ClearQueue(); err != nil {
		t.Fatalf("clear queue: %v", err)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected clear queue to remove stale preload, got %q", backend.preloadedURI)
	}
}

func TestSessionClearQueueClearsStalePreloadWhenSessionCacheIsLost(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected rec-2 preloaded, got %q", backend.preloadedURI)
	}

	session.mu.Lock()
	session.snapshot.NextPreparation = nil
	session.preloadedID = ""
	session.preloadedURI = ""
	session.backendPreloadArmed = false
	session.mu.Unlock()

	if _, err := session.ClearQueue(); err != nil {
		t.Fatalf("clear queue: %v", err)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected clear queue to flush backend preload after session cache loss, got %q", backend.preloadedURI)
	}
}

func TestSessionNextSkipsUnavailableQueuedEntriesAndRemovesPlayedSkips(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/one.mp3",
			},
			"rec-3": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/three.mp3",
			},
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": {RecordingID: "rec-1", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
			"rec-2": {RecordingID: "rec-2", State: apitypes.AvailabilityUnavailableProvider, Reason: apitypes.PlaybackUnavailableProviderOffline},
			"rec-3": {RecordingID: "rec-3", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
		},
	}, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-4", Title: "Four", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{
		{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
	}, QueueInsertNext); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-3" {
		t.Fatalf("expected current entry rec-3, got %+v", snapshot.CurrentEntry)
	}
	if len(snapshot.UserQueue) != 0 {
		t.Fatalf("expected skipped queued entry rec-2 to be removed after later queued playback, got %+v", snapshot.UserQueue)
	}
	if snapshot.LastSkipEvent == nil || snapshot.LastSkipEvent.Count != 1 {
		t.Fatalf("expected one skipped item event, got %+v", snapshot.LastSkipEvent)
	}
}

func TestSessionNextKeepsUnavailableUserQueueWhenNoQueuedFallbackExists(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": {RecordingID: "rec-1", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
			"rec-2": {RecordingID: "rec-2", State: apitypes.AvailabilityUnavailableProvider, Reason: apitypes.PlaybackUnavailableProviderOffline},
			"rec-3": {RecordingID: "rec-3", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
		},
	}, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{
		{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
	}, QueueInsertNext); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-3" {
		t.Fatalf("expected context to continue to rec-3, got %+v", snapshot.CurrentEntry)
	}
	if len(snapshot.UserQueue) != 1 || snapshot.UserQueue[0].Item.RecordingID != "rec-2" {
		t.Fatalf("expected unavailable queued entry rec-2 to remain queued, got %+v", snapshot.UserQueue)
	}
}

func TestSessionEOFToPendingQueuedEntryClearsCurrentAndStopsBackend(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-1": {
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			},
			"rec-2": {
				{RecordingID: "rec-2", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "One"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "rec-2", Title: "Two"}}, QueueInsertNext); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.events <- BackendEvent{Type: BackendEventTrackEnd, Reason: TrackEndReasonEOF}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.Status == StatusPending && snapshot.LoadingEntry != nil {
			if snapshot.LoadingEntry.Item.RecordingID != "rec-2" {
				t.Fatalf("expected queued pending entry rec-2, got %+v", snapshot.LoadingEntry)
			}
			if snapshot.CurrentEntry != nil {
				t.Fatalf("expected eof-to-pending to clear current entry, got %+v", snapshot.CurrentEntry)
			}
			if backend.stopCalls == 0 {
				t.Fatalf("expected backend stop when eof hits a pending next entry")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected session to enter pending queued state, got %+v", session.Snapshot())
}

func TestSessionEOFPendingNextStopsStalePreloadedPlayback(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-1": {
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			},
			"rec-2": {
				{RecordingID: "rec-2", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			},
			"rec-3": {
				{RecordingID: "rec-3", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected rec-2 preloaded, got %q", backend.preloadedURI)
	}

	if _, err := session.QueueItems([]SessionItem{{RecordingID: "rec-3", Title: "Three"}}, QueueInsertNext); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	backend.preloadedURI = "file:///tmp/two.mp3"

	backend.events <- BackendEvent{Type: BackendEventTrackEnd, Reason: TrackEndReasonEOF}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.Status == StatusPending && snapshot.LoadingEntry != nil {
			if snapshot.LoadingEntry.Item.RecordingID != "rec-3" {
				t.Fatalf("expected queued pending entry rec-3, got %+v", snapshot.LoadingEntry)
			}
			if backend.stopCalls == 0 {
				t.Fatalf("expected backend stop to cut off stale preloaded playback")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected session to enter pending state for rec-3, got %+v", session.Snapshot())
}

func TestSessionPreloadNextBacksOffUnavailablePreparation(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-2": {
				{
					RecordingID: "rec-2",
					Phase:       apitypes.PlaybackPreparationUnavailable,
					Reason:      apitypes.PlaybackUnavailableProviderOffline,
					SourceKind:  apitypes.PlaybackSourceRemoteOpt,
				},
				{
					RecordingID: "rec-2",
					Phase:       apitypes.PlaybackPreparationReady,
					SourceKind:  apitypes.PlaybackSourceRemoteOpt,
					PlayableURI: "file:///tmp/two.mp3",
				},
			},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()

	session.preloadNext(context.Background())
	if bridge.prepareCalls["rec-2"] != 1 {
		t.Fatalf("expected initial preload preparation call, got %d", bridge.prepareCalls["rec-2"])
	}

	session.preloadNext(context.Background())
	session.preloadNext(context.Background())
	if bridge.prepareCalls["rec-2"] != 1 {
		t.Fatalf("expected unavailable preload to back off, got %d prepare calls", bridge.prepareCalls["rec-2"])
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected no preloaded URI while track unavailable, got %q", backend.preloadedURI)
	}

	session.mu.Lock()
	session.nextPreparationRetryAt = time.Now().Add(-time.Second)
	session.mu.Unlock()

	session.preloadNext(context.Background())
	if bridge.prepareCalls["rec-2"] != 2 {
		t.Fatalf("expected preload retry after backoff, got %d prepare calls", bridge.prepareCalls["rec-2"])
	}
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected track to preload after retry, got %q", backend.preloadedURI)
	}
}

func TestSessionPreloadNextSkipsUnavailableStructuralNext(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": {RecordingID: "rec-1", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
			"rec-2": {RecordingID: "rec-2", State: apitypes.AvailabilityUnavailableNoPath, Reason: apitypes.PlaybackUnavailableNoPath},
			"rec-3": {RecordingID: "rec-3", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())

	if backend.preloadedURI != "file:///tmp/three.mp3" {
		t.Fatalf("expected rec-3 preloaded, got %q", backend.preloadedURI)
	}
}

func TestSessionPreloadNextSkipsUnavailableUsingShuffledUpcomingOrder(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"rec-4": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/four.mp3"},
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
			{RecordingID: "rec-4", Title: "Four", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.SetRepeatMode(string(RepeatAll)); err != nil {
		t.Fatalf("set repeat mode: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	upcoming := buildUpcomingEntries(shuffled)
	if len(upcoming) < 2 {
		t.Fatalf("expected at least two shuffled upcoming entries, got %+v", upcoming)
	}

	skipped := upcoming[0]
	expected := upcoming[1]
	bridge.availability[skipped.Item.RecordingID] = apitypes.RecordingPlaybackAvailability{
		RecordingID: skipped.Item.RecordingID,
		State:       apitypes.AvailabilityUnavailableNoPath,
		Reason:      apitypes.PlaybackUnavailableNoPath,
	}
	delete(bridge.results, skipped.Item.RecordingID)

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())

	expectedResult, ok := bridge.results[expected.Item.RecordingID]
	if !ok {
		t.Fatalf("missing playable result for expected shuffled preload target %s", expected.Item.RecordingID)
	}
	if backend.preloadedURI != expectedResult.PlayableURI {
		t.Fatalf(
			"expected preload to follow shuffled upcoming order and skip unavailable %s for %s, got %q",
			skipped.Item.RecordingID,
			expected.Item.RecordingID,
			backend.preloadedURI,
		)
	}
	if bridge.prepareCalls[skipped.Item.RecordingID] != 0 {
		t.Fatalf("expected unavailable shuffled candidate %s not to be prepared, got %d calls", skipped.Item.RecordingID, bridge.prepareCalls[skipped.Item.RecordingID])
	}
	if bridge.prepareCalls[expected.Item.RecordingID] != 1 {
		t.Fatalf("expected playable shuffled candidate %s to be prepared once, got %d", expected.Item.RecordingID, bridge.prepareCalls[expected.Item.RecordingID])
	}
}

func TestSessionPendingUnavailableFallsThroughToNextPlayableEntry(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-1": {
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			},
			"rec-2": {
				{RecordingID: "rec-2", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
				{RecordingID: "rec-2", Phase: apitypes.PlaybackPreparationUnavailable, Reason: apitypes.PlaybackUnavailableProviderOffline},
			},
			"rec-3": {
				{RecordingID: "rec-3", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			},
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": {RecordingID: "rec-1", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
			"rec-2": {RecordingID: "rec-2", State: apitypes.AvailabilityWaitingProviderTranscode, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			"rec-3": {RecordingID: "rec-3", State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{
		{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
	}, QueueInsertNext); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next: %v", err)
	}

	snapshot := session.Snapshot()
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.Item.RecordingID != "rec-2" {
		t.Fatalf("expected rec-2 to be loading first, got %+v", snapshot.LoadingEntry)
	}

	token := session.pendingToken
	entryID := session.pendingEntry
	if !session.tryPendingPlayback(context.Background(), token, entryID) {
		t.Fatalf("expected pending retry to settle")
	}

	snapshot = session.Snapshot()
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-3" {
		t.Fatalf("expected fallback current entry rec-3, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LastSkipEvent == nil || snapshot.LastSkipEvent.Count != 1 {
		t.Fatalf("expected one skipped pending entry, got %+v", snapshot.LastSkipEvent)
	}
}

func TestSessionPreloadNextDoesNotRepeatReadyBackendPreload(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()

	session.preloadNext(context.Background())
	session.preloadNext(context.Background())
	if backend.preloadCalls != 1 {
		t.Fatalf("expected ready preload to be sent to backend once, got %d", backend.preloadCalls)
	}
}

func TestSessionPreloadNextSkipsCurrentEntryWhenSingleTrackRepeatsAll(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.SetRepeatMode(string(RepeatAll)); err != nil {
		t.Fatalf("set repeat all: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())

	if backend.preloadCalls != 0 {
		t.Fatalf("expected no backend preload for current-entry repeat-all loop, got %d", backend.preloadCalls)
	}
	if bridge.prepareCalls["rec-1"] != 1 {
		t.Fatalf("expected no extra prepare call for current-entry repeat-all loop, got %d", bridge.prepareCalls["rec-1"])
	}
	if session.Snapshot().NextPreparation != nil {
		t.Fatalf("expected no next preparation for current-entry repeat-all loop, got %+v", session.Snapshot().NextPreparation)
	}
}

func TestSessionPreloadNextSkipsCurrentEntryWhenSingleTrackRepeatsOne(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.SetRepeatMode(string(RepeatOne)); err != nil {
		t.Fatalf("set repeat one: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition()
	session.preloadNext(context.Background())

	if backend.preloadCalls != 0 {
		t.Fatalf("expected no backend preload for current-entry repeat-one loop, got %d", backend.preloadCalls)
	}
	if bridge.prepareCalls["rec-1"] != 1 {
		t.Fatalf("expected no extra prepare call for current-entry repeat-one loop, got %d", bridge.prepareCalls["rec-1"])
	}
	if session.Snapshot().NextPreparation != nil {
		t.Fatalf("expected no next preparation for current-entry repeat-one loop, got %+v", session.Snapshot().NextPreparation)
	}
}

func TestSessionPlayPendingUsesLoadingStateWhenPlayerEmpty(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-1": {
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationPreparingTranscode, SourceKind: apitypes.PlaybackSourceRemoteOpt},
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceRemoteOpt, PlayableURI: "file:///tmp/ready.mp3"},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play pending track: %v", err)
	}
	if snapshot.Status != StatusPending {
		t.Fatalf("expected pending status, got %q", snapshot.Status)
	}
	if snapshot.CurrentEntry != nil {
		t.Fatalf("expected no current entry while loading, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected loading entry rec-1, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.LoadingPreparation == nil || snapshot.LoadingPreparation.Status.Phase != apitypes.PlaybackPreparationPreparingTranscode {
		t.Fatalf("expected loading preparation to show preparing transcode, got %+v", snapshot.LoadingPreparation)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if backend.loadedURI == "file:///tmp/ready.mp3" {
			resolved := session.Snapshot()
			if resolved.CurrentEntry == nil || resolved.CurrentEntry.Item.RecordingID != "rec-1" {
				t.Fatalf("expected current entry rec-1 after load, got %+v", resolved.CurrentEntry)
			}
			if resolved.LoadingEntry != nil {
				t.Fatalf("expected loading state cleared after load, got %+v", resolved.LoadingEntry)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected pending playback to resolve and load track, got %q", backend.loadedURI)
}

func TestSessionPendingReadyBackendFailureClearsLoadingState(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	backend.loadErr = errors.New("backend load failed")
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-1": {
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationPreparingTranscode, SourceKind: apitypes.PlaybackSourceRemoteOpt},
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceRemoteOpt, PlayableURI: "file:///tmp/ready.mp3"},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play pending track: %v", err)
	}
	if snapshot.LoadingEntry == nil {
		t.Fatalf("expected loading entry after pending playback")
	}

	session.mu.Lock()
	token := session.pendingToken
	entryID := session.pendingEntry
	session.mu.Unlock()

	if !session.tryPendingPlayback(context.Background(), token, entryID) {
		t.Fatalf("expected backend failure to stop pending retry loop")
	}

	resolved := session.Snapshot()
	if resolved.LoadingEntry != nil {
		t.Fatalf("expected loading state cleared after backend failure, got %+v", resolved.LoadingEntry)
	}
	if resolved.Status != StatusPaused {
		t.Fatalf("expected paused status after backend failure, got %q", resolved.Status)
	}
	if resolved.LastError != "backend load failed" {
		t.Fatalf("expected backend load failure in last error, got %q", resolved.LastError)
	}
}

func TestSessionPendingRequestPreservesCurrentPlayerState(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-local": {
				{RecordingID: "rec-local", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/local.mp3"},
			},
			"rec-remote": {
				{RecordingID: "rec-remote", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "local",
		Items: []SessionItem{{RecordingID: "rec-local", Title: "Local"}},
	}); err != nil {
		t.Fatalf("set local context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play local: %v", err)
	}

	snapshot, err := session.ReplaceContextAndPlay(context.Background(), PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "remote",
		Items: []SessionItem{{RecordingID: "rec-remote", Title: "Remote"}},
	})
	if err != nil {
		t.Fatalf("replace context with pending track: %v", err)
	}
	if snapshot.Status != StatusPlaying {
		t.Fatalf("expected playing status to remain, got %q", snapshot.Status)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-local" {
		t.Fatalf("expected current entry rec-local, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.Item.RecordingID != "rec-remote" {
		t.Fatalf("expected loading entry rec-remote, got %+v", snapshot.LoadingEntry)
	}
	if backend.loadedURI != "file:///tmp/local.mp3" {
		t.Fatalf("expected backend to keep local track loaded, got %q", backend.loadedURI)
	}
}

func TestSessionPendingRetryTimeoutClearsLoadingAndPreservesCurrent(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-local": {
				{RecordingID: "rec-local", Phase: apitypes.PlaybackPreparationReady, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/local.mp3"},
			},
			"rec-remote": {
				{RecordingID: "rec-remote", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "local",
		Items: []SessionItem{{RecordingID: "rec-local", Title: "Local"}},
	}); err != nil {
		t.Fatalf("set local context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play local: %v", err)
	}
	if _, err := session.ReplaceContextAndPlay(context.Background(), PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "remote",
		Items: []SessionItem{{RecordingID: "rec-remote", Title: "Remote"}},
	}); err != nil {
		t.Fatalf("replace context with pending track: %v", err)
	}

	session.mu.Lock()
	token := session.pendingToken
	entryID := session.pendingEntry
	session.mu.Unlock()
	session.finishPendingRetry(token, entryID, context.DeadlineExceeded)

	snapshot := session.Snapshot()
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-local" {
		t.Fatalf("expected current entry rec-local after timeout, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LoadingEntry != nil {
		t.Fatalf("expected loading entry cleared after timeout, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.Status != StatusPlaying {
		t.Fatalf("expected current player status to remain playing, got %q", snapshot.Status)
	}
	if snapshot.LastError == "" {
		t.Fatalf("expected timeout error to be recorded")
	}
}

func TestSessionQueueMutationClearsPendingLoading(t *testing.T) {
	t.Parallel()

	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"rec-1": {
				{RecordingID: "rec-1", Phase: apitypes.PlaybackPreparationPreparingFetch, SourceKind: apitypes.PlaybackSourceRemoteOpt},
			},
		},
	}, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play pending track: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "rec-2", Title: "Track 2"}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot := session.Snapshot()
	if snapshot.LoadingEntry != nil {
		t.Fatalf("expected loading state cleared by queue mutation, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.Status != StatusPaused {
		t.Fatalf("expected paused status after clearing pending load, got %q", snapshot.Status)
	}
}

func TestSmartShuffleSpreadsAdjacentArtists(t *testing.T) {
	t.Parallel()

	session := NewSession(&mockBridge{}, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	_, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "a1", Title: "A1", Subtitle: "Artist A", AlbumID: "album-a"},
			{RecordingID: "a2", Title: "A2", Subtitle: "Artist A", AlbumID: "album-a"},
			{RecordingID: "b1", Title: "B1", Subtitle: "Artist B", AlbumID: "album-b"},
			{RecordingID: "c1", Title: "C1", Subtitle: "Artist C", AlbumID: "album-c"},
		},
	})
	if err != nil {
		t.Fatalf("set context: %v", err)
	}
	snapshot, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	if len(snapshot.ContextQueue.ShuffleBag) != 4 {
		t.Fatalf("expected shuffle cycle of 4, got %v", snapshot.ContextQueue.ShuffleBag)
	}

	cycle := snapshot.ContextQueue.ShuffleBag
	entries := snapshot.ContextQueue.Entries
	for index := 1; index < len(cycle); index++ {
		left := entries[cycle[index-1]].Item
		right := entries[cycle[index]].Item
		if left.Subtitle == right.Subtitle && len(entries) > 2 {
			t.Fatalf("expected smart shuffle to avoid adjacent artist repeats, got %v", cycle)
		}
	}
}

func TestSessionShuffleKeepsQueuedEntriesOutsideShuffleCycle(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"ctx-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"ctx-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"ctx-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
			"ctx-4": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/four.mp3"},
			"q-1":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q1.mp3"},
			"q-2":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/q2.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindAlbum,
		ID:   "album-1",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One", Subtitle: "Artist A", AlbumID: "album-a"},
			{RecordingID: "ctx-2", Title: "Two", Subtitle: "Artist B", AlbumID: "album-b"},
			{RecordingID: "ctx-3", Title: "Three", Subtitle: "Artist C", AlbumID: "album-c"},
			{RecordingID: "ctx-4", Title: "Four", Subtitle: "Artist D", AlbumID: "album-d"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{
		{RecordingID: "q-1", Title: "Queue One"},
		{RecordingID: "q-2", Title: "Queue Two"},
	}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("set shuffle: %v", err)
	}
	if len(shuffled.ContextQueue.ShuffleBag) != len(shuffled.ContextQueue.Entries) {
		t.Fatalf("expected shuffle cycle to cover only context entries, got cycle %v for %d context entries", shuffled.ContextQueue.ShuffleBag, len(shuffled.ContextQueue.Entries))
	}
	if len(shuffled.UpcomingEntries) < 3 {
		t.Fatalf("expected queued entries plus shuffled context in upcoming list, got %+v", shuffled.UpcomingEntries)
	}
	if shuffled.UpcomingEntries[0].Item.RecordingID != "q-1" || shuffled.UpcomingEntries[1].Item.RecordingID != "q-2" {
		t.Fatalf("expected queued entries to stay ahead of shuffle order, got %+v", shuffled.UpcomingEntries)
	}
	expectedContext := shuffled.ContextQueue.Entries[shuffled.ContextQueue.ShuffleBag[1]].Item.RecordingID
	if shuffled.UpcomingEntries[2].Item.RecordingID != expectedContext {
		t.Fatalf("expected first shuffled context entry %s after queued items, got %+v", expectedContext, shuffled.UpcomingEntries)
	}
}

func TestCatalogLoaderLoadAlbumTrackContextStartsAtSelectedTrack(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		albumTracks: map[string][]apitypes.AlbumTrackItem{
			"album-1": {
				{RecordingID: "rec-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
				{RecordingID: "rec-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
				{RecordingID: "rec-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: 1000},
			},
		},
	})

	contextInput, err := loader.LoadAlbumTrackContext(context.Background(), "album-1", "rec-2")
	if err != nil {
		t.Fatalf("load album track context: %v", err)
	}
	if contextInput.Kind != ContextKindAlbum || contextInput.ID != "album-1" {
		t.Fatalf("unexpected context: %+v", contextInput)
	}
	if contextInput.StartIndex != 1 {
		t.Fatalf("start index = %d, want 1", contextInput.StartIndex)
	}
	if len(contextInput.Items) != 3 || contextInput.Items[1].RecordingID != "rec-2" {
		t.Fatalf("unexpected items: %+v", contextInput.Items)
	}
}

func TestCatalogLoaderLoadPlaylistTrackContextStartsAtSelectedItem(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		playlistTracks: map[string][]apitypes.PlaylistTrackItem{
			"playlist-1": {
				{ItemID: "item-1", RecordingID: "rec-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
				{ItemID: "item-2", RecordingID: "rec-1", Title: "One again", Artists: []string{"Artist"}, DurationMS: 1000},
				{ItemID: "item-3", RecordingID: "rec-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
			},
		},
	})

	contextInput, err := loader.LoadPlaylistTrackContext(context.Background(), "playlist-1", "item-2")
	if err != nil {
		t.Fatalf("load playlist track context: %v", err)
	}
	if contextInput.Kind != ContextKindPlaylist || contextInput.ID != "playlist-1" {
		t.Fatalf("unexpected context: %+v", contextInput)
	}
	if contextInput.StartIndex != 1 {
		t.Fatalf("start index = %d, want 1", contextInput.StartIndex)
	}
	if len(contextInput.Items) != 3 || contextInput.Items[1].SourceItemID != "item-2" {
		t.Fatalf("unexpected items: %+v", contextInput.Items)
	}
}

func TestItemsFromPlaylistTracksUsePreferredResolutionTarget(t *testing.T) {
	t.Parallel()

	items := ItemsFromPlaylistTracks("playlist-1", []apitypes.PlaylistTrackItem{
		{
			ItemID:             "item-1",
			LibraryRecordingID: "cluster-1",
			RecordingID:        "variant-2",
			Title:              "Track",
			DurationMS:         1000,
			Artists:            []string{"Artist"},
		},
	})

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	if items[0].LibraryRecordingID != "cluster-1" {
		t.Fatalf("library recording id = %q, want cluster-1", items[0].LibraryRecordingID)
	}
	if items[0].VariantRecordingID != "variant-2" {
		t.Fatalf("variant recording id = %q, want variant-2", items[0].VariantRecordingID)
	}
	if items[0].RecordingID != "cluster-1" {
		t.Fatalf("recording id = %q, want cluster-1", items[0].RecordingID)
	}
	if items[0].ResolutionMode != ResolutionModeLibrary {
		t.Fatalf("resolution mode = %q, want %q", items[0].ResolutionMode, ResolutionModeLibrary)
	}
	if items[0].Target.ResolutionPolicy != PlaybackTargetResolutionPreferred {
		t.Fatalf("resolution policy = %q, want %q", items[0].Target.ResolutionPolicy, PlaybackTargetResolutionPreferred)
	}
}

func TestCatalogLoaderLoadLikedTrackContextStartsAtSelectedTrack(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{RecordingID: "rec-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
			{RecordingID: "rec-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
		},
	})

	contextInput, err := loader.LoadLikedTrackContext(context.Background(), "rec-2")
	if err != nil {
		t.Fatalf("load liked track context: %v", err)
	}
	if contextInput.Kind != ContextKindLiked || contextInput.ID != "liked" {
		t.Fatalf("unexpected context: %+v", contextInput)
	}
	if contextInput.StartIndex != 1 {
		t.Fatalf("start index = %d, want 1", contextInput.StartIndex)
	}
	if len(contextInput.Items) != 2 || contextInput.Items[1].RecordingID != "rec-2" {
		t.Fatalf("unexpected items: %+v", contextInput.Items)
	}
}

func TestCatalogLoaderLoadRecordingContextPreservesRequestedVariantTarget(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		recording: map[string]apitypes.RecordingListItem{
			"variant-2": {
				LibraryRecordingID:          "cluster-1",
				PreferredVariantRecordingID: "variant-1",
				RecordingID:                 "cluster-1",
				Title:                       "Track",
				Artists:                     []string{"Artist"},
				DurationMS:                  1000,
			},
		},
	})

	contextInput, err := loader.LoadRecordingContext(context.Background(), "variant-2")
	if err != nil {
		t.Fatalf("load recording context: %v", err)
	}
	if len(contextInput.Items) != 1 {
		t.Fatalf("items length = %d, want 1", len(contextInput.Items))
	}
	item := contextInput.Items[0]
	if item.RecordingID != "cluster-1" {
		t.Fatalf("recording id = %q, want cluster-1", item.RecordingID)
	}
	if item.VariantRecordingID != "variant-2" {
		t.Fatalf("variant recording id = %q, want variant-2", item.VariantRecordingID)
	}
	if item.Target.LogicalRecordingID != "cluster-1" {
		t.Fatalf("logical target = %q, want cluster-1", item.Target.LogicalRecordingID)
	}
	if item.Target.ExactVariantRecordingID != "variant-2" {
		t.Fatalf("exact target = %q, want variant-2", item.Target.ExactVariantRecordingID)
	}
	if item.Target.ResolutionPolicy != PlaybackTargetResolutionExact {
		t.Fatalf("resolution policy = %q, want %q", item.Target.ResolutionPolicy, PlaybackTargetResolutionExact)
	}
}

func TestSessionQueuedRecordingContextUsesRequestedVariantTarget(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		recording: map[string]apitypes.RecordingListItem{
			"variant-2": {
				LibraryRecordingID:          "cluster-1",
				PreferredVariantRecordingID: "variant-1",
				RecordingID:                 "cluster-1",
				Title:                       "Track",
				Artists:                     []string{"Artist"},
				DurationMS:                  1000,
			},
		},
	})

	contextInput, err := loader.LoadRecordingContext(context.Background(), "variant-2")
	if err != nil {
		t.Fatalf("load recording context: %v", err)
	}

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		preparations: map[string][]apitypes.PlaybackPreparationStatus{
			"variant-2": {{
				RecordingID:      "variant-2",
				PreferredProfile: "desktop",
				Phase:            apitypes.PlaybackPreparationPreparingFetch,
				SourceKind:       apitypes.PlaybackSourceRemoteOpt,
			}},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.QueueItems(contextInput.Items, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	snapshot, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play queued recording: %v", err)
	}
	if snapshot.LoadingEntry == nil {
		t.Fatalf("expected loading entry, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.LoadingEntry.Item.Target.ExactVariantRecordingID != "variant-2" {
		t.Fatalf("loading exact target = %q, want variant-2", snapshot.LoadingEntry.Item.Target.ExactVariantRecordingID)
	}
	if snapshot.LoadingPreparation == nil || snapshot.LoadingPreparation.Status.RecordingID != "variant-2" {
		t.Fatalf("loading preparation = %+v, want recording variant-2", snapshot.LoadingPreparation)
	}
}

func TestSessionNextAtEndWithRepeatOffIsNoOp(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "One"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	stopCallsBeforeNext := backend.stopCalls

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if snapshot.Status != StatusPlaying {
		t.Fatalf("status = %q, want playing", snapshot.Status)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current entry rec-1, got %+v", snapshot.CurrentEntry)
	}
	if backend.stopCalls != stopCallsBeforeNext {
		t.Fatalf("stop calls after next = %d, want %d", backend.stopCalls, stopCallsBeforeNext)
	}
}

func TestSessionClearQueuePreservesCurrentPlayback(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/one.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "rec-3", Title: "Three", DurationMS: duration}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	stopCallsBeforeClear := backend.stopCalls

	snapshot, err := session.ClearQueue()
	if err != nil {
		t.Fatalf("clear queue: %v", err)
	}

	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current entry rec-1, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.Status != StatusPlaying {
		t.Fatalf("status = %q, want playing", snapshot.Status)
	}
	if snapshot.ContextQueue != nil {
		t.Fatalf("expected context to be cleared, got %+v", snapshot.ContextQueue)
	}
	if len(snapshot.UserQueue) != 0 {
		t.Fatalf("expected queued entries to be cleared, got %d", len(snapshot.UserQueue))
	}
	if len(snapshot.UpcomingEntries) != 0 {
		t.Fatalf("expected upcoming entries to be cleared, got %d", len(snapshot.UpcomingEntries))
	}
	if snapshot.QueueLength != 1 {
		t.Fatalf("queue length = %d, want 1 for the current track", snapshot.QueueLength)
	}
	if snapshot.CurrentPreparation == nil {
		t.Fatalf("expected current preparation to be preserved")
	}
	if backend.stopCalls != stopCallsBeforeClear {
		t.Fatalf("stop calls after clear = %d, want %d", backend.stopCalls, stopCallsBeforeClear)
	}
}

func TestSessionNextWithRepeatOneRestartsTrack(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "One"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.SetRepeatMode(string(RepeatOne)); err != nil {
		t.Fatalf("set repeat mode: %v", err)
	}
	backend.position = 5000

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if snapshot.PositionMS != 0 {
		t.Fatalf("position = %d, want 0", snapshot.PositionMS)
	}
	if backend.position != 0 {
		t.Fatalf("backend position = %d, want 0", backend.position)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current entry rec-1, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionEOFAtEndPreservesContextAndReloadsOnPlay(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "custom",
		Items: []SessionItem{{RecordingID: "rec-1", Title: "One"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.events <- BackendEvent{Type: BackendEventTrackEnd, Reason: TrackEndReasonEOF}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.Status == StatusPaused {
			if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
				t.Fatalf("expected current entry rec-1, got %+v", snapshot.CurrentEntry)
			}
			if snapshot.PositionMS != 0 {
				t.Fatalf("position = %d, want 0", snapshot.PositionMS)
			}
			if _, err := session.Play(context.Background()); err != nil {
				t.Fatalf("replay after eof: %v", err)
			}
			if backend.loadCalls != 2 {
				t.Fatalf("load calls = %d, want 2", backend.loadCalls)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected session to settle into paused state after eof, got %+v", session.Snapshot())
}

