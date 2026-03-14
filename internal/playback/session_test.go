package playback

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	results         map[string]apitypes.PlaybackResolveResult
	preparations    map[string][]apitypes.PlaybackPreparationStatus
	recording       map[string]apitypes.RecordingListItem
	albumTracks     map[string][]apitypes.AlbumTrackItem
	playlistTracks  map[string][]apitypes.PlaylistTrackItem
	likedRecordings []apitypes.LikedRecordingItem
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
	return b.nextPreparationStatus(recordingID, true)
}

func (b *mockBridge) GetPlaybackPreparation(_ context.Context, recordingID, _ string) (apitypes.PlaybackPreparationStatus, error) {
	return b.nextPreparationStatus(recordingID, true)
}

func (b *mockBridge) ResolveRecordingArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, nil
}

func (b *mockBridge) GetRecordingAvailability(context.Context, string, string) (apitypes.RecordingPlaybackAvailability, error) {
	return apitypes.RecordingPlaybackAvailability{}, nil
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
	preloadedURI    string
	position        int64
	duration        *int64
	volume          int
	stopCalls       int
	events          chan BackendEvent
	supportsPreload bool
}

func newTestBackend() *testBackend {
	return &testBackend{
		events: make(chan BackendEvent, 8),
	}
}

func (b *testBackend) Load(_ context.Context, uri string) error {
	b.loadedURI = uri
	b.loadCalls++
	b.position = 0
	return nil
}

func (b *testBackend) Play(context.Context) error  { return nil }
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
			Context: &PlaybackContext{
				Kind: ContextKindAlbum,
				ID:   "album-1",
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
			CurrentEntryID:      "ctx-1",
			CurrentEntry:        &SessionEntry{EntryID: "ctx-1", Origin: EntryOriginContext, ContextIndex: 0, Item: SessionItem{RecordingID: "rec-1", Title: "Track 1", DurationMS: duration}},
			CurrentOrigin:       EntryOriginContext,
			CurrentContextIndex: 0,
			Volume:              60,
			Status:              StatusPlaying,
			PositionMS:          2500,
			DurationMS:          &duration,
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
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "queued", Title: "Queued"}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "queued" {
		t.Fatalf("expected queued item to play next, got %#v", snapshot.CurrentEntry)
	}
	if len(snapshot.QueuedEntries) != 0 {
		t.Fatalf("expected queued entries to be consumed, got %d", len(snapshot.QueuedEntries))
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

func TestSessionPlayPendingUsesPreparationStatus(t *testing.T) {
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
	if !errors.Is(err, errPendingPlayback) {
		t.Fatalf("expected pending playback error, got %v", err)
	}
	if snapshot.Status != StatusPending {
		t.Fatalf("expected pending status, got %q", snapshot.Status)
	}
	if snapshot.CurrentPreparation == nil || snapshot.CurrentPreparation.Status.Phase != apitypes.PlaybackPreparationPreparingTranscode {
		t.Fatalf("expected current preparation to show preparing transcode, got %+v", snapshot.CurrentPreparation)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if backend.loadedURI == "file:///tmp/ready.mp3" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected pending playback to resolve and load track, got %q", backend.loadedURI)
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
	if len(snapshot.ShuffleCycle) != 4 {
		t.Fatalf("expected shuffle cycle of 4, got %v", snapshot.ShuffleCycle)
	}

	cycle := snapshot.ShuffleCycle
	entries := snapshot.Context.Entries
	for index := 1; index < len(cycle); index++ {
		left := entries[cycle[index-1]].Item
		right := entries[cycle[index]].Item
		if left.Subtitle == right.Subtitle && len(entries) > 2 {
			t.Fatalf("expected smart shuffle to avoid adjacent artist repeats, got %v", cycle)
		}
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
