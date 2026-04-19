package playback

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
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
	results                   map[string]apitypes.PlaybackResolveResult
	availability              map[string]apitypes.RecordingPlaybackAvailability
	preparations              map[string][]apitypes.PlaybackPreparationStatus
	recordings                []apitypes.RecordingListItem
	recording                 map[string]apitypes.RecordingListItem
	albums                    map[string]apitypes.AlbumListItem
	playlists                 map[string]apitypes.PlaylistListItem
	albumTracks               map[string][]apitypes.AlbumTrackItem
	playlistTracks            map[string][]apitypes.PlaylistTrackItem
	likedRecordings           []apitypes.LikedRecordingItem
	prepareCalls              map[string]int
	getPreparationCalls       map[string]int
	recordingsCursorHook      func(context.Context, apitypes.RecordingCursorRequest) error
	playlistTracksCursorHook  func(context.Context, apitypes.PlaylistTrackCursorRequest) error
	likedRecordingsCursorHook func(context.Context, apitypes.LikedRecordingCursorRequest) error
}

func (b *mockBridge) Close() error { return nil }

func (b *mockBridge) ListRecordings(_ context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return paginateTestItems(b.recordings, req.PageRequest), nil
}

func (b *mockBridge) ListRecordingsCursor(ctx context.Context, req apitypes.RecordingCursorRequest) (apitypes.CursorPage[apitypes.RecordingListItem], error) {
	if b.recordingsCursorHook != nil {
		if err := b.recordingsCursorHook(ctx, req); err != nil {
			return apitypes.CursorPage[apitypes.RecordingListItem]{}, err
		}
	}
	return paginateCursorTestItems(b.recordings, req.CursorPageRequest), nil
}

func (b *mockBridge) GetRecording(_ context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	if item, ok := b.recording[recordingID]; ok {
		return item, nil
	}
	return apitypes.RecordingListItem{}, fmt.Errorf("unexpected recording %s", recordingID)
}

func (b *mockBridge) GetAlbum(_ context.Context, albumID string) (apitypes.AlbumListItem, error) {
	if item, ok := b.albums[albumID]; ok {
		return item, nil
	}
	return apitypes.AlbumListItem{}, fmt.Errorf("unexpected album %s", albumID)
}

func (b *mockBridge) ListAlbumTracks(_ context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return paginateTestItems(b.albumTracks[strings.TrimSpace(req.AlbumID)], req.PageRequest), nil
}

func (b *mockBridge) GetPlaylistSummary(_ context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	if item, ok := b.playlists[playlistID]; ok {
		return item, nil
	}
	return apitypes.PlaylistListItem{}, fmt.Errorf("unexpected playlist %s", playlistID)
}

func (b *mockBridge) ListPlaylistTracks(_ context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return paginateTestItems(b.playlistTracks[strings.TrimSpace(req.PlaylistID)], req.PageRequest), nil
}

func (b *mockBridge) ListPlaylistTracksCursor(ctx context.Context, req apitypes.PlaylistTrackCursorRequest) (apitypes.CursorPage[apitypes.PlaylistTrackItem], error) {
	if b.playlistTracksCursorHook != nil {
		if err := b.playlistTracksCursorHook(ctx, req); err != nil {
			return apitypes.CursorPage[apitypes.PlaylistTrackItem]{}, err
		}
	}
	return paginateCursorTestItems(b.playlistTracks[strings.TrimSpace(req.PlaylistID)], req.CursorPageRequest), nil
}

func (b *mockBridge) ListLikedRecordings(_ context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return paginateTestItems(b.likedRecordings, req.PageRequest), nil
}

func (b *mockBridge) ListLikedRecordingsCursor(ctx context.Context, req apitypes.LikedRecordingCursorRequest) (apitypes.CursorPage[apitypes.LikedRecordingItem], error) {
	if b.likedRecordingsCursorHook != nil {
		if err := b.likedRecordingsCursorHook(ctx, req); err != nil {
			return apitypes.CursorPage[apitypes.LikedRecordingItem]{}, err
		}
	}
	return paginateCursorTestItems(b.likedRecordings, req.CursorPageRequest), nil
}

func (b *mockBridge) SubscribeCatalogChanges(func(apitypes.CatalogChangeEvent)) func() {
	return func() {}
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
	activateCalls   int
	activateErr     error
	playErr         error
	preloadedURI    string
	preloadCalls    int
	preloadErr      error
	position        int64
	positionReads   []int64
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

func makeTrackCatalog(total int, duration int64) []apitypes.RecordingListItem {
	recordings := make([]apitypes.RecordingListItem, 0, total)
	for index := 0; index < total; index++ {
		recordingID := fmt.Sprintf("cluster-%04d", index)
		recordings = append(recordings, apitypes.RecordingListItem{
			LibraryRecordingID:          recordingID,
			PreferredVariantRecordingID: "variant-" + recordingID,
			TrackClusterID:              recordingID,
			RecordingID:                 recordingID,
			Title:                       fmt.Sprintf("Track %04d", index),
			DurationMS:                  duration,
			Artists:                     []string{"Artist"},
		})
	}
	return recordings
}

func makeSessionItems(total int, duration int64) []SessionItem {
	items := make([]SessionItem, 0, total)
	for index := 0; index < total; index++ {
		recordingID := fmt.Sprintf("rec-%04d", index)
		items = append(items, SessionItem{
			RecordingID: recordingID,
			Title:       fmt.Sprintf("Track %04d", index),
			DurationMS:  duration,
		})
	}
	return items
}

func makePlayableResult(recordingID string) apitypes.PlaybackResolveResult {
	return apitypes.PlaybackResolveResult{
		RecordingID: recordingID,
		State:       apitypes.AvailabilityPlayableLocalFile,
		SourceKind:  apitypes.PlaybackSourceLocalFile,
		PlayableURI: "file:///tmp/" + recordingID + ".mp3",
	}
}

func makePlayableAvailability(recordingID string) apitypes.RecordingPlaybackAvailability {
	return apitypes.RecordingPlaybackAvailability{
		RecordingID: recordingID,
		State:       apitypes.AvailabilityPlayableLocalFile,
		SourceKind:  apitypes.PlaybackSourceLocalFile,
		LocalPath:   "file:///tmp/" + recordingID + ".mp3",
	}
}

func makeUnavailableAvailability(recordingID string) apitypes.RecordingPlaybackAvailability {
	return apitypes.RecordingPlaybackAvailability{
		RecordingID: recordingID,
		State:       apitypes.AvailabilityUnavailableProvider,
		Reason:      apitypes.PlaybackUnavailableProviderOffline,
	}
}

func containsRecording(entries []SessionEntry, recordingID string) bool {
	for _, entry := range entries {
		if entry.Item.RecordingID == recordingID {
			return true
		}
	}
	return false
}

func equalIntSlices(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func waitForSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal(message)
	}
}

func makeAvailabilityCandidateEntry(entryID, recordingID string) SessionEntry {
	return SessionEntry{
		EntryID: entryID,
		Origin:  EntryOriginQueued,
		Item: SessionItem{
			RecordingID: recordingID,
			Title:       "Track " + recordingID,
			Target: PlaybackTargetRef{
				LogicalRecordingID: recordingID,
			},
		},
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

func (b *testBackend) ActivatePreloaded(_ context.Context, uri string) (BackendActivationRef, error) {
	b.activateCalls++
	if b.activateErr != nil {
		return BackendActivationRef{}, b.activateErr
	}
	if strings.TrimSpace(uri) == "" || strings.TrimSpace(uri) != strings.TrimSpace(b.preloadedURI) {
		return BackendActivationRef{}, ErrUnsupportedPreloadActivation
	}
	b.loadedURI = uri
	b.preloadedURI = ""
	b.position = 0
	attemptID := uint64(b.activateCalls)
	return BackendActivationRef{
		URI:             uri,
		PlaylistEntryID: int64(b.activateCalls),
		PlaylistPos:     int64(b.activateCalls - 1),
		AttemptID:       attemptID,
	}, nil
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

func (b *testBackend) PositionMS() (int64, error) {
	if len(b.positionReads) > 0 {
		b.position = b.positionReads[0]
		b.positionReads = b.positionReads[1:]
	}
	return b.position, nil
}

func (b *testBackend) DurationMS() (*int64, error) { return cloneInt64Ptr(b.duration), nil }
func (b *testBackend) Events() <-chan BackendEvent { return b.events }
func (b *testBackend) SupportsPreload() bool       { return b.supportsPreload }
func (b *testBackend) PreloadNext(_ context.Context, uri string) error {
	if b.preloadErr != nil {
		b.preloadCalls++
		return b.preloadErr
	}
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

func paginateCursorTestItems[T any](items []T, req apitypes.CursorPageRequest) apitypes.CursorPage[T] {
	limit := req.Limit
	if limit <= 0 {
		limit = len(items)
	}
	offset := 0
	cursor := strings.TrimSpace(req.Cursor)
	if cursor != "" {
		if parsed, err := strconv.Atoi(cursor); err == nil && parsed > 0 {
			offset = parsed
		}
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	nextCursor := ""
	if end < len(items) {
		nextCursor = strconv.Itoa(end)
	}
	return apitypes.CursorPage[T]{
		Items: append([]T(nil), items[offset:end]...),
		Page: apitypes.CursorPageInfo{
			Limit:      limit,
			Returned:   end - offset,
			HasMore:    end < len(items),
			NextCursor: nextCursor,
		},
	}
}

func TestSessionStartRestoresPausedState(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	store := &memoryStore{
		snapshot: SessionSnapshot{
			ContextQueue: &ContextQueue{
				Kind:         ContextKindAlbum,
				ID:           "album-1",
				StartIndex:   0,
				CurrentIndex: 0,
				ResumeIndex:  0,
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

func TestSessionPauseReturnsAndPublishesFinalAuthoritativePosition(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	var emitted []SessionSnapshot
	session.SetSnapshotEmitter(func(snapshot SessionSnapshot) {
		emitted = append(emitted, snapshot)
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

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
	snapshot, err := session.Pause(context.Background())
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if snapshot.Status != StatusPaused {
		t.Fatalf("status = %q, want %q", snapshot.Status, StatusPaused)
	}
	if snapshot.PositionMS != 4321 {
		t.Fatalf("position = %d, want 4321", snapshot.PositionMS)
	}
	if snapshot.PositionCapturedAtMS <= 0 {
		t.Fatalf("expected authoritative position capture timestamp, got %d", snapshot.PositionCapturedAtMS)
	}
	last := emitted[len(emitted)-1]
	if last.Status != StatusPaused || last.PositionMS != 4321 {
		t.Fatalf("last emitted snapshot = %+v, want paused position 4321", last)
	}
}

func TestSessionTogglePlaybackPausePublishesFinalAuthoritativePosition(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	var emitted []SessionSnapshot
	session.SetSnapshotEmitter(func(snapshot SessionSnapshot) {
		emitted = append(emitted, snapshot)
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

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

	backend.position = 2468
	snapshot, err := session.TogglePlayback(context.Background())
	if err != nil {
		t.Fatalf("toggle playback: %v", err)
	}
	if snapshot.Status != StatusPaused || snapshot.PositionMS != 2468 {
		t.Fatalf("toggle pause snapshot = %+v, want paused position 2468", snapshot)
	}
	last := emitted[len(emitted)-1]
	if last.Status != StatusPaused || last.PositionMS != 2468 {
		t.Fatalf("last emitted toggle snapshot = %+v, want paused position 2468", last)
	}
}

func TestSessionSeekToLoadedEntryPublishesFinalAuthoritativePosition(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	var emitted []SessionSnapshot
	session.SetSnapshotEmitter(func(snapshot SessionSnapshot) {
		emitted = append(emitted, snapshot)
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

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

	snapshot, err := session.SeekTo(context.Background(), 5000)
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	if backend.position != 5000 {
		t.Fatalf("backend position = %d, want 5000", backend.position)
	}
	if snapshot.PositionMS != 5000 {
		t.Fatalf("snapshot position = %d, want 5000", snapshot.PositionMS)
	}
	if snapshot.PositionCapturedAtMS <= 0 {
		t.Fatalf("expected authoritative position capture timestamp, got %d", snapshot.PositionCapturedAtMS)
	}
	last := emitted[len(emitted)-1]
	if last.PositionMS != 5000 {
		t.Fatalf("last emitted seek snapshot = %+v, want position 5000", last)
	}
}

func TestSessionSeekToLoadedEntryWaitsForAuthoritativeSeekReadback(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
	backend.position = 98000
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {
				State:       apitypes.AvailabilityPlayableLocalFile,
				SourceKind:  apitypes.PlaybackSourceLocalFile,
				PlayableURI: "file:///tmp/test.mp3",
			},
		},
	}, backend, &memoryStore{}, "desktop", nil)
	var emitted []SessionSnapshot
	session.SetSnapshotEmitter(func(snapshot SessionSnapshot) {
		emitted = append(emitted, snapshot)
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

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
	backend.positionReads = []int64{98000, 98000, 5000}

	snapshot, err := session.SeekTo(context.Background(), 5000)
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	if snapshot.PositionMS != 5000 {
		t.Fatalf("snapshot position = %d, want 5000", snapshot.PositionMS)
	}
	last := emitted[len(emitted)-1]
	if last.PositionMS != 5000 {
		t.Fatalf("last emitted seek snapshot = %+v, want position 5000", last)
	}
}

func TestSessionSeekToLoadedEntryDoesNotInventAuthoritativeTargetOnTimeout(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
	backend.position = 98000
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
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1", DurationMS: duration}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.position = 98000
	session.refreshPosition("test")
	before := session.Snapshot()
	if before.PositionMS != 98000 {
		t.Fatalf("pre-seek position = %d, want 98000", before.PositionMS)
	}
	if before.PositionCapturedAtMS <= 0 {
		t.Fatalf("expected pre-seek capture timestamp, got %d", before.PositionCapturedAtMS)
	}

	backend.positionReads = make([]int64, 64)
	for index := range backend.positionReads {
		backend.positionReads[index] = 98000
	}

	snapshot, err := session.SeekTo(context.Background(), 5000)
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	if snapshot.PositionMS != 98000 {
		t.Fatalf("snapshot position = %d, want unchanged 98000", snapshot.PositionMS)
	}
	if snapshot.PositionCapturedAtMS != before.PositionCapturedAtMS {
		t.Fatalf(
			"position capture timestamp changed from %d to %d without authoritative seek confirmation",
			before.PositionCapturedAtMS,
			snapshot.PositionCapturedAtMS,
		)
	}
}

func TestSessionSeekToUnloadedEntryPreservesRequestedPositionOnFirstPlay(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	store := &memoryStore{
		snapshot: SessionSnapshot{
			ContextQueue: &ContextQueue{
				Kind:         ContextKindCustom,
				ID:           "custom",
				StartIndex:   0,
				CurrentIndex: 0,
				ResumeIndex:  0,
				Entries: []SessionEntry{{
					EntryID:      "ctx-1",
					Origin:       EntryOriginContext,
					ContextIndex: 0,
					Item: SessionItem{
						RecordingID: "rec-1",
						Title:       "Track 1",
						DurationMS:  duration,
					},
				}},
			},
			CurrentEntryID: "ctx-1",
			CurrentEntry: &SessionEntry{
				EntryID:      "ctx-1",
				Origin:       EntryOriginContext,
				ContextIndex: 0,
				Item: SessionItem{
					RecordingID: "rec-1",
					Title:       "Track 1",
					DurationMS:  duration,
				},
			},
			Status:     StatusPaused,
			DurationMS: &duration,
		},
	}
	backend := newTestBackend()
	backend.duration = &duration
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

	seeked, err := session.SeekTo(context.Background(), 5000)
	if err != nil {
		t.Fatalf("seek before play: %v", err)
	}
	if seeked.PositionMS != 5000 {
		t.Fatalf("seeked position = %d, want 5000", seeked.PositionMS)
	}
	if seeked.PositionCapturedAtMS <= 0 {
		t.Fatalf("expected authoritative position capture timestamp, got %d", seeked.PositionCapturedAtMS)
	}

	played, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play after seek: %v", err)
	}
	if backend.position != 5000 {
		t.Fatalf("backend position = %d, want 5000", backend.position)
	}
	if played.PositionMS != 5000 {
		t.Fatalf("played position = %d, want 5000", played.PositionMS)
	}
}

func TestSessionPlayRefreshesCaptureTimestampWhenKeepingZeroPosition(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	staleCapture := currentTransportCaptureMS() - 120_000
	store := &memoryStore{
		snapshot: SessionSnapshot{
			ContextQueue: &ContextQueue{
				Kind:         ContextKindCustom,
				ID:           "custom",
				StartIndex:   0,
				CurrentIndex: 0,
				ResumeIndex:  0,
				Entries: []SessionEntry{{
					EntryID:      "ctx-1",
					Origin:       EntryOriginContext,
					ContextIndex: 0,
					Item: SessionItem{
						RecordingID: "rec-1",
						Title:       "Track 1",
						DurationMS:  duration,
					},
				}},
			},
			CurrentEntryID: "ctx-1",
			CurrentEntry: &SessionEntry{
				EntryID:      "ctx-1",
				Origin:       EntryOriginContext,
				ContextIndex: 0,
				Item: SessionItem{
					RecordingID: "rec-1",
					Title:       "Track 1",
					DurationMS:  duration,
				},
			},
			Status:               StatusPaused,
			PositionMS:           0,
			PositionCapturedAtMS: staleCapture,
			DurationMS:           &duration,
		},
	}
	backend := newTestBackend()
	backend.duration = &duration
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

	played, err := session.Play(context.Background())
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if played.PositionMS != 0 {
		t.Fatalf("played position = %d, want 0", played.PositionMS)
	}
	if played.PositionCapturedAtMS <= staleCapture {
		t.Fatalf(
			"expected refreshed capture timestamp, got %d (stale was %d)",
			played.PositionCapturedAtMS,
			staleCapture,
		)
	}
}

func TestSessionQueueOnlyChangesDoNotRewritePositionCaptureTimestamp(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
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
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1", DurationMS: duration}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.position = 3200
	session.refreshPosition("test")
	before := session.Snapshot().PositionCapturedAtMS
	time.Sleep(2 * time.Millisecond)

	if _, err := session.QueueItems([]SessionItem{{RecordingID: "rec-2", Title: "Queued", DurationMS: duration}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}

	after := session.Snapshot().PositionCapturedAtMS
	if after != before {
		t.Fatalf("queue-only position capture timestamp changed from %d to %d", before, after)
	}
}

func TestSessionRefreshPositionUpdatesAuthoritativeCaptureTimestamp(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.duration = &duration
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
		Items: []SessionItem{{RecordingID: "rec-1", Title: "Track 1", DurationMS: duration}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	before := session.Snapshot().PositionCapturedAtMS
	time.Sleep(2 * time.Millisecond)
	backend.position = 6400
	session.refreshPosition("test")

	snapshot := session.Snapshot()
	if snapshot.PositionMS != 6400 {
		t.Fatalf("position = %d, want 6400", snapshot.PositionMS)
	}
	if snapshot.PositionCapturedAtMS <= before {
		t.Fatalf("position capture timestamp = %d, want > %d", snapshot.PositionCapturedAtMS, before)
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

func TestSessionClosePersistsSourceBackedQueueWithoutWindowAndRestoresAbsoluteIndex(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	liked := make([]apitypes.LikedRecordingItem, 0, 200)
	for index := 0; index < 200; index++ {
		liked = append(liked, apitypes.LikedRecordingItem{
			LibraryRecordingID: fmt.Sprintf("cluster-%03d", index),
			RecordingID:        fmt.Sprintf("variant-%03d", index),
			Title:              fmt.Sprintf("Track %03d", index),
			Artists:            []string{"Artist"},
			DurationMS:         1000,
		})
	}
	bridge := &mockBridge{likedRecordings: liked}

	session := NewSession(bridge, newTestBackend(), store, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	initial, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedTrackSource("cluster-150"), false)
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	if initial.ContextQueue == nil || initial.ContextQueue.CurrentIndex != 150 {
		t.Fatalf("expected current index 150 before persistence, got %+v", initial.ContextQueue)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}

	if store.snapshot.ContextQueue == nil {
		t.Fatalf("expected persisted source-backed context queue")
	}
	if len(store.snapshot.ContextQueue.Entries) != 0 {
		t.Fatalf("expected persisted source-backed window to be stripped, got %d entries", len(store.snapshot.ContextQueue.Entries))
	}
	if store.snapshot.ContextQueue.CurrentIndex != 150 || store.snapshot.ContextQueue.ResumeIndex != 150 {
		t.Fatalf("expected persisted absolute indexes to survive, got current=%d resume=%d", store.snapshot.ContextQueue.CurrentIndex, store.snapshot.ContextQueue.ResumeIndex)
	}

	restored := NewSession(bridge, newTestBackend(), store, "desktop", nil)
	if err := restored.Start(context.Background()); err != nil {
		t.Fatalf("start restored session: %v", err)
	}
	defer restored.Close()

	snapshot := restored.Snapshot()
	if snapshot.ContextQueue == nil {
		t.Fatalf("expected restored source-backed context queue")
	}
	if snapshot.ContextQueue.CurrentIndex != 150 || snapshot.ContextQueue.ResumeIndex != 150 {
		t.Fatalf("expected restored absolute indexes current=150 resume=150, got %+v", snapshot.ContextQueue)
	}
	if len(contextAllEntries(snapshot)) != 200 {
		t.Fatalf("expected rebuilt source-backed entries, got %d", len(contextAllEntries(snapshot)))
	}
}

func TestSessionSetSourceEnumeratesPagedLikedSource(t *testing.T) {
	t.Parallel()

	liked := make([]apitypes.LikedRecordingItem, 0, 1205)
	for index := 0; index < 1205; index++ {
		recordingID := fmt.Sprintf("cluster-%04d", index)
		liked = append(liked, apitypes.LikedRecordingItem{
			LibraryRecordingID: recordingID,
			RecordingID:        recordingID,
			Title:              fmt.Sprintf("Track %d", index),
			Artists:            []string{"Artist"},
			DurationMS:         180000,
		})
	}
	bridge := &mockBridge{likedRecordings: liked}

	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	snapshot, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedTrackSource("cluster-1103"), false)
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	if snapshot.ContextQueue == nil {
		t.Fatalf("expected source-backed context queue")
	}
	if snapshot.ContextQueue.CurrentIndex != 1103 || snapshot.ContextQueue.ResumeIndex != 1103 {
		t.Fatalf("expected absolute indexes to survive paged enumeration, got %+v", snapshot.ContextQueue)
	}
	if len(contextAllEntries(snapshot)) != len(liked) {
		t.Fatalf("expected %d liked entries from paged source, got %d", len(liked), len(contextAllEntries(snapshot)))
	}
	if len(snapshot.ContextQueue.Entries) == len(liked) {
		t.Fatalf("expected public context queue to remain windowed for large source")
	}
}

func TestSessionNextScansPastSparseUnavailableSourceBackedTracksOutsideWindow(t *testing.T) {
	t.Parallel()

	const (
		total        = 1500
		currentIndex = 5
		skippedCount = 120
	)
	duration := int64(180000)
	recordings := makeTrackCatalog(total, duration)
	currentID := recordings[currentIndex].RecordingID
	targetIndex := currentIndex + skippedCount + 1
	targetID := recordings[targetIndex].RecordingID

	bridge := &mockBridge{
		recordings: recordings,
		results: map[string]apitypes.PlaybackResolveResult{
			currentID: makePlayableResult(currentID),
			targetID:  makePlayableResult(targetID),
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			currentID: makePlayableAvailability(currentID),
			targetID:  makePlayableAvailability(targetID),
		},
	}
	for index := currentIndex + 1; index < targetIndex; index++ {
		recordingID := recordings[index].RecordingID
		bridge.availability[recordingID] = makeUnavailableAvailability(recordingID)
	}

	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	initial, err := session.ReplaceSourceAndPlay(context.Background(), NewCatalogLoader(bridge).BuildTracksTrackSource(currentID), false)
	if err != nil {
		t.Fatalf("replace source and play: %v", err)
	}
	if initial.ContextQueue == nil {
		t.Fatalf("expected source-backed context queue")
	}
	if containsRecording(initial.ContextQueue.Entries, targetID) {
		t.Fatalf("expected target %s to sit outside the visible window", targetID)
	}

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.RecordingID != targetID {
		t.Fatalf("expected next playable target %s, got %+v", targetID, next.CurrentEntry)
	}
	if next.LastSkipEvent == nil {
		t.Fatalf("expected skip event after sparse availability traversal")
	}
	if next.LastSkipEvent.Count != skippedCount {
		t.Fatalf("skip count = %d, want %d", next.LastSkipEvent.Count, skippedCount)
	}
	if next.LastSkipEvent.FirstEntry == nil || next.LastSkipEvent.FirstEntry.Item.RecordingID != recordings[currentIndex+1].RecordingID {
		t.Fatalf("expected first skipped entry %s, got %+v", recordings[currentIndex+1].RecordingID, next.LastSkipEvent.FirstEntry)
	}
}

func TestSessionNextScansPastSparseUnavailableShuffledTracks(t *testing.T) {
	t.Parallel()

	const (
		total        = 1500
		currentIndex = 7
		skippedCount = 17
	)
	duration := int64(180000)
	recordings := makeTrackCatalog(total, duration)
	currentID := recordings[currentIndex].RecordingID

	bridge := &mockBridge{
		recordings: recordings,
		results: map[string]apitypes.PlaybackResolveResult{
			currentID: makePlayableResult(currentID),
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			currentID: makePlayableAvailability(currentID),
		},
	}

	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.ReplaceSourceAndPlay(context.Background(), NewCatalogLoader(bridge).BuildTracksTrackSource(currentID), false); err != nil {
		t.Fatalf("replace source and play: %v", err)
	}
	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("enable shuffle: %v", err)
	}
	upcoming := buildUpcomingEntries(shuffled)
	if len(upcoming) <= skippedCount {
		t.Fatalf("expected shuffled upcoming list longer than %d, got %d", skippedCount, len(upcoming))
	}

	for _, entry := range upcoming[:skippedCount] {
		bridge.availability[entry.Item.RecordingID] = makeUnavailableAvailability(entry.Item.RecordingID)
	}
	targetID := upcoming[skippedCount].Item.RecordingID
	bridge.results[targetID] = makePlayableResult(targetID)
	bridge.availability[targetID] = makePlayableAvailability(targetID)

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.RecordingID != targetID {
		t.Fatalf("expected shuffled next target %s, got %+v", targetID, next.CurrentEntry)
	}
	if next.LastSkipEvent == nil || next.LastSkipEvent.Count != skippedCount {
		t.Fatalf("expected skip count %d, got %+v", skippedCount, next.LastSkipEvent)
	}
	if next.LastSkipEvent.FirstEntry == nil || next.LastSkipEvent.FirstEntry.Item.RecordingID != upcoming[0].Item.RecordingID {
		t.Fatalf("expected first skipped shuffled entry %s, got %+v", upcoming[0].Item.RecordingID, next.LastSkipEvent.FirstEntry)
	}
}

func TestSessionPreloadScansPastSparseUnavailableSourceBackedTracks(t *testing.T) {
	t.Parallel()

	const (
		total        = 1500
		currentIndex = 10
		skippedCount = 90
	)
	duration := int64(180000)
	recordings := makeTrackCatalog(total, duration)
	currentID := recordings[currentIndex].RecordingID
	targetIndex := currentIndex + skippedCount + 1
	targetID := recordings[targetIndex].RecordingID

	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		recordings: recordings,
		results: map[string]apitypes.PlaybackResolveResult{
			currentID: makePlayableResult(currentID),
			targetID:  makePlayableResult(targetID),
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			currentID: makePlayableAvailability(currentID),
			targetID:  makePlayableAvailability(targetID),
		},
	}
	for index := currentIndex + 1; index < targetIndex; index++ {
		recordingID := recordings[index].RecordingID
		bridge.availability[recordingID] = makeUnavailableAvailability(recordingID)
	}

	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.ReplaceSourceAndPlay(context.Background(), NewCatalogLoader(bridge).BuildTracksTrackSource(currentID), false); err != nil {
		t.Fatalf("replace source and play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())

	if backend.preloadedURI != bridge.results[targetID].PlayableURI {
		t.Fatalf("expected preload target %q, got %q", bridge.results[targetID].PlayableURI, backend.preloadedURI)
	}
	if bridge.prepareCalls[targetID] != 1 {
		t.Fatalf("expected target %s to be prepared once, got %d", targetID, bridge.prepareCalls[targetID])
	}
	if bridge.prepareCalls[recordings[currentIndex+1].RecordingID] != 0 {
		t.Fatalf("expected unavailable track %s not to be prepared, got %d", recordings[currentIndex+1].RecordingID, bridge.prepareCalls[recordings[currentIndex+1].RecordingID])
	}
}

func TestSessionNextStopsAfterAllRemainingSparseTracksAreUnavailable(t *testing.T) {
	t.Parallel()

	const (
		total        = 40
		currentIndex = 5
	)
	duration := int64(180000)
	recordings := makeTrackCatalog(total, duration)
	currentID := recordings[currentIndex].RecordingID
	remaining := total - currentIndex - 1

	bridge := &mockBridge{
		recordings: recordings,
		results: map[string]apitypes.PlaybackResolveResult{
			currentID: makePlayableResult(currentID),
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			currentID: makePlayableAvailability(currentID),
		},
	}
	for index := currentIndex + 1; index < len(recordings); index++ {
		recordingID := recordings[index].RecordingID
		bridge.availability[recordingID] = makeUnavailableAvailability(recordingID)
	}

	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.ReplaceSourceAndPlay(context.Background(), NewCatalogLoader(bridge).BuildTracksTrackSource(currentID), false); err != nil {
		t.Fatalf("replace source and play: %v", err)
	}

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if next.Status != StatusPaused {
		t.Fatalf("expected paused status after exhausting remaining tracks, got %q", next.Status)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.RecordingID != currentID {
		t.Fatalf("expected current entry to remain on %s after exhaustion, got %+v", currentID, next.CurrentEntry)
	}
	if next.LastSkipEvent == nil || !next.LastSkipEvent.Stopped || next.LastSkipEvent.Count != remaining {
		t.Fatalf("expected stopped skip event count %d, got %+v", remaining, next.LastSkipEvent)
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
	upcoming := buildUpcomingEntries(snapshot)
	if len(upcoming) < 3 {
		t.Fatalf("expected queued and remaining context in upcoming entries, got %+v", upcoming)
	}
	if upcoming[0].Item.RecordingID != "q-2" || upcoming[1].Item.RecordingID != "ctx-2" {
		t.Fatalf("unexpected upcoming order while queued: %+v", upcoming)
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
	session.refreshPosition("test")
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

func TestSessionSetShuffleReenableGeneratesNewOrder(t *testing.T) {
	t.Parallel()

	items := []SessionItem{
		{RecordingID: "ctx-1", Title: "One", Subtitle: "Artist A"},
		{RecordingID: "ctx-2", Title: "Two", Subtitle: "Artist B"},
		{RecordingID: "ctx-3", Title: "Three", Subtitle: "Artist C"},
		{RecordingID: "ctx-4", Title: "Four", Subtitle: "Artist D"},
		{RecordingID: "ctx-5", Title: "Five", Subtitle: "Artist E"},
		{RecordingID: "ctx-6", Title: "Six", Subtitle: "Artist F"},
		{RecordingID: "ctx-7", Title: "Seven", Subtitle: "Artist G"},
		{RecordingID: "ctx-8", Title: "Eight", Subtitle: "Artist H"},
	}
	results := make(map[string]apitypes.PlaybackResolveResult, len(items))
	for _, item := range items {
		results[item.RecordingID] = makePlayableResult(item.RecordingID)
	}

	session := NewSession(&mockBridge{results: results}, newTestBackend(), &memoryStore{}, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:       ContextKindCustom,
		ID:         "custom",
		StartIndex: 2,
		Items:      items,
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("enable shuffle: %v", err)
	}
	if shuffled.ContextQueue == nil {
		t.Fatalf("expected context queue")
	}
	initialSeed := shuffled.ContextQueue.ShuffleSeed
	initialBag := append([]int(nil), shuffled.ContextQueue.ShuffleBag...)
	currentRecording := shuffled.CurrentEntry.Item.RecordingID

	unshuffled, err := session.SetShuffle(false)
	if err != nil {
		t.Fatalf("disable shuffle: %v", err)
	}
	if unshuffled.ContextQueue == nil {
		t.Fatalf("expected context queue after disabling shuffle")
	}
	if unshuffled.ContextQueue.ShuffleSeed != 0 {
		t.Fatalf("expected shuffle seed cleared when disabled, got %d", unshuffled.ContextQueue.ShuffleSeed)
	}
	if len(unshuffled.ContextQueue.ShuffleBag) != 0 {
		t.Fatalf("expected shuffle bag cleared when disabled, got %v", unshuffled.ContextQueue.ShuffleBag)
	}

	reshuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("re-enable shuffle: %v", err)
	}
	if reshuffled.CurrentEntry == nil || reshuffled.CurrentEntry.Item.RecordingID != currentRecording {
		t.Fatalf("expected current track to stay fixed, got %+v", reshuffled.CurrentEntry)
	}
	if reshuffled.ContextQueue == nil {
		t.Fatalf("expected reshuffled context queue")
	}
	if reshuffled.ContextQueue.ShuffleSeed == 0 || reshuffled.ContextQueue.ShuffleSeed == initialSeed {
		t.Fatalf("expected fresh shuffle seed after re-enable, got %d from initial %d", reshuffled.ContextQueue.ShuffleSeed, initialSeed)
	}
	if len(reshuffled.ContextQueue.ShuffleBag) != len(initialBag) {
		t.Fatalf("expected reshuffled bag length %d, got %d", len(initialBag), len(reshuffled.ContextQueue.ShuffleBag))
	}
	if reshuffled.ContextQueue.ShuffleBag[0] != reshuffled.ContextQueue.CurrentIndex {
		t.Fatalf("expected reshuffled bag anchored at current index %d, got %v", reshuffled.ContextQueue.CurrentIndex, reshuffled.ContextQueue.ShuffleBag)
	}
	if equalIntSlices(reshuffled.ContextQueue.ShuffleBag, initialBag) {
		t.Fatalf("expected reshuffled order to change, got %v", reshuffled.ContextQueue.ShuffleBag)
	}
}

func TestSessionSetContextRebuildsShuffleBagWhenShuffleEnabled(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	session := NewSession(&mockBridge{}, newTestBackend(), store, "desktop", nil)
	session.rng = rand.New(rand.NewSource(7))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "first",
		Items: []SessionItem{
			{RecordingID: "ctx-1", Title: "One", Subtitle: "Artist A"},
			{RecordingID: "ctx-2", Title: "Two", Subtitle: "Artist B"},
			{RecordingID: "ctx-3", Title: "Three", Subtitle: "Artist C"},
			{RecordingID: "ctx-4", Title: "Four", Subtitle: "Artist D"},
		},
	}); err != nil {
		t.Fatalf("set first context: %v", err)
	}

	if _, err := session.SetShuffle(true); err != nil {
		t.Fatalf("enable shuffle: %v", err)
	}

	snapshot, err := session.SetContext(PlaybackContextInput{
		Kind:       ContextKindCustom,
		ID:         "second",
		StartIndex: 1,
		Items: []SessionItem{
			{RecordingID: "next-1", Title: "Next One", Subtitle: "Artist E"},
			{RecordingID: "next-2", Title: "Next Two", Subtitle: "Artist F"},
			{RecordingID: "next-3", Title: "Next Three", Subtitle: "Artist G"},
		},
	})
	if err != nil {
		t.Fatalf("set second context: %v", err)
	}
	if snapshot.ContextQueue == nil {
		t.Fatalf("expected context queue")
	}
	if len(snapshot.ContextQueue.ShuffleBag) != len(snapshot.ContextQueue.Entries) {
		t.Fatalf("expected rebuilt shuffle bag for new context, got %v for %d entries", snapshot.ContextQueue.ShuffleBag, len(snapshot.ContextQueue.Entries))
	}
	if snapshot.ContextQueue.ShuffleBag[0] != 1 {
		t.Fatalf("expected rebuilt shuffle bag anchored at start index 1, got %v", snapshot.ContextQueue.ShuffleBag)
	}
	if store.snapshot.ContextQueue == nil {
		t.Fatalf("expected persisted context queue")
	}
	if len(store.snapshot.ContextQueue.ShuffleBag) != len(snapshot.ContextQueue.Entries) {
		t.Fatalf("expected persisted shuffle bag %v, got %v", snapshot.ContextQueue.ShuffleBag, store.snapshot.ContextQueue.ShuffleBag)
	}
}

func TestSessionDisabledShufflePersistenceClearsOrderAndRegeneratesOnEnable(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	session := NewSession(&mockBridge{}, newTestBackend(), store, "desktop", nil)
	session.rng = rand.New(rand.NewSource(11))
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "persisted",
		Items: []SessionItem{{RecordingID: "ctx-1", Title: "One"}, {RecordingID: "ctx-2", Title: "Two"}, {RecordingID: "ctx-3", Title: "Three"}, {RecordingID: "ctx-4", Title: "Four"}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	shuffled, err := session.SetShuffle(true)
	if err != nil {
		t.Fatalf("enable shuffle: %v", err)
	}
	if shuffled.ContextQueue == nil {
		t.Fatalf("expected context queue")
	}
	initialSeed := shuffled.ContextQueue.ShuffleSeed

	if _, err := session.SetShuffle(false); err != nil {
		t.Fatalf("disable shuffle: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}

	if store.snapshot.ContextQueue == nil {
		t.Fatalf("expected persisted context queue")
	}
	if store.snapshot.ContextQueue.ShuffleSeed != 0 {
		t.Fatalf("expected persisted disabled shuffle seed cleared, got %d", store.snapshot.ContextQueue.ShuffleSeed)
	}
	if len(store.snapshot.ContextQueue.ShuffleBag) != 0 {
		t.Fatalf("expected persisted disabled shuffle bag cleared, got %v", store.snapshot.ContextQueue.ShuffleBag)
	}

	restored := NewSession(&mockBridge{}, newTestBackend(), store, "desktop", nil)
	restored.rng = rand.New(rand.NewSource(19))
	if err := restored.Start(context.Background()); err != nil {
		t.Fatalf("start restored session: %v", err)
	}
	defer restored.Close()

	reenabled, err := restored.SetShuffle(true)
	if err != nil {
		t.Fatalf("re-enable shuffle: %v", err)
	}
	if reenabled.ContextQueue == nil {
		t.Fatalf("expected restored context queue")
	}
	if reenabled.ContextQueue.ShuffleSeed == 0 || reenabled.ContextQueue.ShuffleSeed == initialSeed {
		t.Fatalf("expected regenerated shuffle seed distinct from %d, got %d", initialSeed, reenabled.ContextQueue.ShuffleSeed)
	}
	if len(reenabled.ContextQueue.ShuffleBag) != len(reenabled.ContextQueue.Entries) {
		t.Fatalf("expected regenerated shuffle bag for %d entries, got %v", len(reenabled.ContextQueue.Entries), reenabled.ContextQueue.ShuffleBag)
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
	session.refreshPosition("test")
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
		t.Fatalf("expected no preload transport before the gapless window, got %q", backend.preloadedURI)
	}
	backend.position = 60000
	session.refreshPosition("test")
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

func TestSessionNextUsesEventDrivenPreloadedActivation(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	backend.supportsPreload = true
	duration := int64(120000)
	backend.duration = &duration
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
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
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if backend.preloadedURI != "" {
		t.Fatalf("expected no preload transport before the gapless window, got %q", backend.preloadedURI)
	}
	if bridge.prepareCalls["rec-2"] != 1 {
		t.Fatalf("expected one prepare call for rec-2 preload, got %d", bridge.prepareCalls["rec-2"])
	}
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())
	if backend.preloadedURI != "file:///tmp/two.mp3" {
		t.Fatalf("expected rec-2 preloaded in the gapless window, got %q", backend.preloadedURI)
	}

	loadCallsBeforeNext := backend.loadCalls
	prepareCallsBeforeNext := bridge.prepareCalls["rec-2"]
	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}

	if backend.activateCalls != 1 {
		t.Fatalf("expected one preloaded activation, got %d", backend.activateCalls)
	}
	if backend.loadCalls != loadCallsBeforeNext {
		t.Fatalf("expected no backend reload on preloaded next, got %d -> %d", loadCallsBeforeNext, backend.loadCalls)
	}
	if bridge.prepareCalls["rec-2"] != prepareCallsBeforeNext {
		t.Fatalf("expected no extra prepare call for rec-2 activation, got %d -> %d", prepareCallsBeforeNext, bridge.prepareCalls["rec-2"])
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current entry to stay rec-1 until backend confirmation, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.Item.RecordingID != "rec-2" {
		t.Fatalf("expected loading entry rec-2 while activation is pending, got %+v", snapshot.LoadingEntry)
	}

	session.mu.Lock()
	activeAttemptID := session.transportPending.activation.AttemptID
	session.mu.Unlock()

	backend.events <- BackendEvent{
		Type:            BackendEventFileLoaded,
		ActiveURI:       "/tmp/two.mp3",
		ActiveAttemptID: activeAttemptID,
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current := session.Snapshot()
		if current.CurrentEntry != nil &&
			current.CurrentEntry.Item.RecordingID == "rec-2" &&
			current.LoadingEntry == nil {
			if backend.preloadedURI != "" {
				t.Fatalf("expected no immediate preload transport for rec-3 right after activation, got %q", backend.preloadedURI)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected backend confirmation to promote rec-2 to current, got %+v", session.Snapshot())
}

func TestSessionNextCompletesPreloadedActivationDespiteFileLoadedWarning(t *testing.T) {
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
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())

	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next: %v", err)
	}
	session.mu.Lock()
	activeAttemptID := session.transportPending.activation.AttemptID
	session.mu.Unlock()
	backend.events <- BackendEvent{
		Type:            BackendEventFileLoaded,
		ActiveURI:       "file:///tmp/two.mp3",
		ActiveAttemptID: activeAttemptID,
		Err:             errors.New("seek apply failed"),
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.CurrentEntry != nil &&
			snapshot.CurrentEntry.Item.RecordingID == "rec-2" &&
			snapshot.LoadingEntry == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected warning-bearing file-loaded event to still complete transition, got %+v", session.Snapshot())
}

func TestSessionNextFallsBackToRegularLoadWhenPreloadedActivationFails(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	backend.supportsPreload = true
	backend.activateErr = errors.New("unknown error")
	duration := int64(120000)
	backend.duration = &duration
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"rec-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"rec-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
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
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())

	loadCallsBeforeNext := backend.loadCalls
	prepareCallsBeforeNext := bridge.prepareCalls["rec-2"]
	snapshot, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next should fall back instead of failing: %v", err)
	}

	if backend.activateCalls != 1 {
		t.Fatalf("expected one failed activation attempt before fallback, got %d", backend.activateCalls)
	}
	if backend.loadCalls != loadCallsBeforeNext+1 {
		t.Fatalf("expected fallback load after activation failure, got %d -> %d", loadCallsBeforeNext, backend.loadCalls)
	}
	if bridge.prepareCalls["rec-2"] != prepareCallsBeforeNext+1 {
		t.Fatalf("expected one extra prepare call for fallback load, got %d -> %d", prepareCallsBeforeNext, bridge.prepareCalls["rec-2"])
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-2" {
		t.Fatalf("expected fallback load to switch current entry to rec-2, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionNextSupersedesPendingActivationFromPendingTarget(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
	backend.supportsPreload = true
	duration := int64(120000)
	backend.duration = &duration
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
			{RecordingID: "rec-3", Title: "Three", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())

	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("first next: %v", err)
	}
	state, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("second next: %v", err)
	}

	if backend.activateCalls != 1 {
		t.Fatalf("expected second next to supersede pending target without reactivating the same preload, got %d", backend.activateCalls)
	}
	if state.CurrentEntry == nil || state.CurrentEntry.Item.RecordingID != "rec-3" {
		t.Fatalf("expected second next to advance to rec-3, got %+v", state.CurrentEntry)
	}
	if state.LoadingEntry != nil {
		t.Fatalf("expected superseding next to settle on rec-3 without lingering loading state, got %+v", state.LoadingEntry)
	}
}

func TestSessionLateSameURIActivationEventDoesNotCompleteSupersededPendingTransport(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	backend.duration = &duration
	sharedURI := "file:///tmp/shared.mp3"
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1":     makePlayableResult("rec-1"),
			"rec-dup-a": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: sharedURI},
			"rec-dup-b": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: sharedURI},
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
			{RecordingID: "rec-dup-a", Title: "Dup A", DurationMS: duration},
			{RecordingID: "rec-dup-b", Title: "Dup B", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	entries := contextAllEntries(session.Snapshot())
	entryA := entries[1]
	entryB := entries[2]
	previous := session.Snapshot().CurrentEntry
	statusA := apitypes.PlaybackPreparationStatus{
		RecordingID: "rec-dup-a",
		SourceKind:  apitypes.PlaybackSourceLocalFile,
		PlayableURI: sharedURI,
		Phase:       apitypes.PlaybackPreparationReady,
	}
	statusB := apitypes.PlaybackPreparationStatus{
		RecordingID: "rec-dup-b",
		SourceKind:  apitypes.PlaybackSourceLocalFile,
		PlayableURI: sharedURI,
		Phase:       apitypes.PlaybackPreparationReady,
	}

	backend.preloadedURI = sharedURI
	if _, activated, err := session.activatePreloadedEntry(context.Background(), backend, entryA, EntryOriginContext, -1, previous, sharedURI, statusA); err != nil || !activated {
		t.Fatalf("activate first duplicate: activated=%v err=%v", activated, err)
	}
	session.mu.Lock()
	firstActivation := session.transportPending.activation
	session.mu.Unlock()

	backend.preloadedURI = sharedURI
	if _, activated, err := session.activatePreloadedEntry(context.Background(), backend, entryB, EntryOriginContext, -1, previous, sharedURI, statusB); err != nil || !activated {
		t.Fatalf("activate second duplicate: activated=%v err=%v", activated, err)
	}
	session.mu.Lock()
	secondActivation := session.transportPending.activation
	session.mu.Unlock()

	backend.events <- BackendEvent{
		Type:                  BackendEventFileLoaded,
		ActiveURI:             sharedURI,
		ActivePlaylistEntryID: firstActivation.PlaylistEntryID,
		ActivePlaylistPos:     firstActivation.PlaylistPos,
		ActiveAttemptID:       firstActivation.AttemptID,
	}
	time.Sleep(100 * time.Millisecond)

	snapshot := session.Snapshot()
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected stale same-uri activation event to be ignored, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.Item.RecordingID != "rec-dup-b" {
		t.Fatalf("expected newer pending target to remain active, got %+v", snapshot.LoadingEntry)
	}

	backend.events <- BackendEvent{
		Type:                  BackendEventFileLoaded,
		ActiveURI:             sharedURI,
		ActivePlaylistEntryID: secondActivation.PlaylistEntryID,
		ActivePlaylistPos:     secondActivation.PlaylistPos,
		ActiveAttemptID:       secondActivation.AttemptID,
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current := session.Snapshot()
		if current.CurrentEntry != nil && current.CurrentEntry.Item.RecordingID == "rec-dup-b" && current.LoadingEntry == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected matching same-uri activation event to complete latest pending target, got %+v", session.Snapshot())
}

func TestSessionStaleFileLoadedEventDoesNotApplyAfterFutureOrderInvalidation(t *testing.T) {
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
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())
	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next: %v", err)
	}

	if _, err := session.SetShuffle(true); err != nil {
		t.Fatalf("set shuffle: %v", err)
	}

	backend.events <- BackendEvent{Type: BackendEventFileLoaded, ActiveURI: "file:///tmp/two.mp3", ActiveAttemptID: 1}
	time.Sleep(100 * time.Millisecond)

	state := session.Snapshot()
	if state.CurrentEntry == nil || state.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected stale file-loaded event to be ignored, got %+v", state.CurrentEntry)
	}
	if state.LoadingEntry != nil {
		t.Fatalf("expected invalidation to clear pending loading entry, got %+v", state.LoadingEntry)
	}
}

func TestSessionNextRetriesDirectLoadWhenPreloadedActivationDoesNotComplete(t *testing.T) {
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
			{RecordingID: "rec-1", Title: "One", DurationMS: duration},
			{RecordingID: "rec-2", Title: "Two", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())

	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next: %v", err)
	}

	deadline := time.Now().Add(transportRetryTimeout + time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.CurrentEntry != nil &&
			snapshot.CurrentEntry.Item.RecordingID == "rec-2" &&
			snapshot.LoadingEntry == nil &&
			backend.loadCalls >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected transport retry to recover direct load, got snapshot=%+v loadCalls=%d activateCalls=%d", session.Snapshot(), backend.loadCalls, backend.activateCalls)
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
	session.refreshPosition("test")
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
	session.refreshPosition("test")
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
	session.refreshPosition("test")
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
	session.refreshPosition("test")
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
	session.refreshPosition("test")

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
	session.refreshPosition("test")
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
	session.refreshPosition("test")
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
	session.refreshPosition("test")

	session.preloadNext(context.Background())
	session.preloadNext(context.Background())
	if backend.preloadCalls != 1 {
		t.Fatalf("expected ready preload to be sent to backend once, got %d", backend.preloadCalls)
	}
}

func TestSessionNextActionPlanStaysCachedAcrossLargeQueueSteadyState(t *testing.T) {
	t.Parallel()

	const totalTracks = 1500
	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	bridge := &mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-0000": makePlayableResult("rec-0000"),
			"rec-0001": makePlayableResult("rec-0001"),
		},
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-0001": makePlayableAvailability("rec-0001"),
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind:  ContextKindCustom,
		ID:    "large",
		Items: makeSessionItems(totalTracks, duration),
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	backend.duration = &duration
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())

	session.mu.Lock()
	initialBuilds := session.nextActionBuilds
	session.mu.Unlock()
	if initialBuilds == 0 {
		t.Fatal("expected preload planning to build next-action plan")
	}
	if bridge.prepareCalls["rec-0001"] != 1 {
		t.Fatalf("expected one preload prepare call for rec-0001, got %d", bridge.prepareCalls["rec-0001"])
	}

	for tick := 0; tick < 8; tick++ {
		backend.position = 60000 + int64(tick*250)
		session.refreshPosition("test")
		session.preloadNext(context.Background())
	}

	session.mu.Lock()
	finalBuilds := session.nextActionBuilds
	session.mu.Unlock()
	if finalBuilds != initialBuilds {
		t.Fatalf("expected steady-state playback ticks to reuse cached next-action plan, got %d -> %d", initialBuilds, finalBuilds)
	}
	if bridge.prepareCalls["rec-0001"] != 1 {
		t.Fatalf("expected unchanged next candidate to avoid repeated prepare calls, got %d", bridge.prepareCalls["rec-0001"])
	}
}

func TestSessionNextActionPlanInvalidatesOncePerMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(t *testing.T, session *Session)
	}{
		{
			name: "queue add",
			mutate: func(t *testing.T, session *Session) {
				t.Helper()
				if _, err := session.QueueItems([]SessionItem{{
					RecordingID: "rec-queue",
					Title:       "Queued",
					DurationMS:  120000,
				}}, QueueInsertLast); err != nil {
					t.Fatalf("queue items: %v", err)
				}
			},
		},
		{
			name: "shuffle toggle",
			mutate: func(t *testing.T, session *Session) {
				t.Helper()
				if _, err := session.SetShuffle(true); err != nil {
					t.Fatalf("set shuffle: %v", err)
				}
			},
		},
		{
			name: "repeat mode change",
			mutate: func(t *testing.T, session *Session) {
				t.Helper()
				if _, err := session.SetRepeatMode(string(RepeatAll)); err != nil {
					t.Fatalf("set repeat mode: %v", err)
				}
			},
		},
		{
			name: "selection change",
			mutate: func(t *testing.T, session *Session) {
				t.Helper()
				target := session.Snapshot().ContextQueue.Entries[1].EntryID
				if _, err := session.SelectEntry(context.Background(), target); err != nil {
					t.Fatalf("select entry: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			duration := int64(120000)
			backend := newTestBackend()
			bridge := &mockBridge{
				results: map[string]apitypes.PlaybackResolveResult{
					"rec-0000":  makePlayableResult("rec-0000"),
					"rec-0001":  makePlayableResult("rec-0001"),
					"rec-0002":  makePlayableResult("rec-0002"),
					"rec-queue": makePlayableResult("rec-queue"),
				},
				availability: map[string]apitypes.RecordingPlaybackAvailability{
					"rec-0001":  makePlayableAvailability("rec-0001"),
					"rec-0002":  makePlayableAvailability("rec-0002"),
					"rec-queue": makePlayableAvailability("rec-queue"),
				},
			}
			session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
			if err := session.Start(context.Background()); err != nil {
				t.Fatalf("start session: %v", err)
			}
			defer session.Close()

			if _, err := session.SetContext(PlaybackContextInput{
				Kind:  ContextKindCustom,
				ID:    "plan",
				Items: makeSessionItems(3, duration),
			}); err != nil {
				t.Fatalf("set context: %v", err)
			}
			if _, err := session.Play(context.Background()); err != nil {
				t.Fatalf("play: %v", err)
			}

			if _, _, err := session.resolveNextPlayableCandidate(context.Background(), nil); err != nil {
				t.Fatalf("resolve next playable candidate: %v", err)
			}

			session.mu.Lock()
			initialBuilds := session.nextActionBuilds
			session.mu.Unlock()

			tc.mutate(t, session)

			session.mu.Lock()
			afterMutationBuilds := session.nextActionBuilds
			session.mu.Unlock()
			if afterMutationBuilds != initialBuilds {
				t.Fatalf("expected invalidation to defer rebuild until planner is needed, got %d -> %d", initialBuilds, afterMutationBuilds)
			}

			if _, _, err := session.resolveNextPlayableCandidate(context.Background(), nil); err != nil {
				t.Fatalf("resolve next playable candidate after mutation: %v", err)
			}

			session.mu.Lock()
			rebuilt := session.nextActionBuilds
			session.mu.Unlock()
			if rebuilt != initialBuilds+1 {
				t.Fatalf("expected exactly one planner rebuild after mutation, got %d -> %d", initialBuilds, rebuilt)
			}
		})
	}
}

func TestSessionPreloadNextSuppressesRepeatedTransportFailureForSameCandidate(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	backend.preloadErr = errors.New("transport failed")
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
	session.refreshPosition("test")

	session.preloadNext(context.Background())
	session.preloadNext(context.Background())
	session.preloadNext(context.Background())

	if backend.preloadCalls != 1 {
		t.Fatalf("expected one transport preload attempt for the same failed candidate, got %d", backend.preloadCalls)
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
	session.refreshPosition("test")
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
	session.refreshPosition("test")
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

func TestSessionLikedLiveRebasePreservesCurrentTrackIndexForPrevious(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Current", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"cluster-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"cluster-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "New Top", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Current", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
	}
	session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})

	snapshot := session.Snapshot()
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "cluster-2" {
		t.Fatalf("expected current track to stay on cluster-2 after liked rebase, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.CurrentIndex != 1 {
		t.Fatalf("expected current index 1 after insert-before rebase, got %+v", snapshot.ContextQueue)
	}
	if snapshot.ContextQueue.ResumeIndex != 2 {
		t.Fatalf("expected resume index 2 after insert-before rebase, got %d", snapshot.ContextQueue.ResumeIndex)
	}

	previous, err := session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous after liked rebase: %v", err)
	}
	if previous.CurrentEntry == nil || previous.CurrentEntry.Item.RecordingID != "cluster-1" {
		t.Fatalf("expected previous to reach newly inserted liked track, got %+v", previous.CurrentEntry)
	}
}

func TestSessionLikedLiveRebaseNextAdvancesPastCurrentTrack(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Current", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"cluster-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"cluster-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "New Top", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Current", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
	}
	session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next after liked rebase: %v", err)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.RecordingID != "cluster-3" {
		t.Fatalf("expected next to advance to cluster-3 after liked rebase, got %+v", next.CurrentEntry)
	}
}

func TestSessionLikedLiveRebaseKeepsRemovedCurrentPlayingButDetachesItFromContext(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "Removed Current", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Next", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"cluster-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
			"cluster-3": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}

	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Next", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
	}
	session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})

	snapshot := session.Snapshot()
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "cluster-1" {
		t.Fatalf("expected removed current track to keep playing, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.ContextQueue == nil || len(contextAllEntries(snapshot)) != 2 {
		t.Fatalf("expected rebuilt liked context without removed track, got %+v", snapshot.ContextQueue)
	}
	if currentMatchesContext(snapshot) {
		t.Fatalf("expected removed current track to be detached from rebuilt context")
	}

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next after current removal: %v", err)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.RecordingID != "cluster-2" {
		t.Fatalf("expected next to advance to first surviving liked track, got %+v", next.CurrentEntry)
	}

	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("resume play after next: %v", err)
	}
	previous, err := session.Previous(context.Background())
	if err != nil {
		t.Fatalf("previous after leaving removed current: %v", err)
	}
	if previous.CurrentEntry == nil || previous.CurrentEntry.Item.RecordingID != "cluster-2" {
		t.Fatalf("expected previous near start of rebuilt context to stay on cluster-2, got %+v", previous.CurrentEntry)
	}
}

func TestSessionPlaylistLiveRebaseDoesNotReattachRemovedDuplicateByRecordingID(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		playlistTracks: map[string][]apitypes.PlaylistTrackItem{
			"playlist-1": {
				{ItemID: "item-1", LibraryRecordingID: "cluster-dup", RecordingID: "variant-dup-a", Title: "Duplicate A", Artists: []string{"Artist"}, DurationMS: duration},
				{ItemID: "item-2", LibraryRecordingID: "cluster-dup", RecordingID: "variant-dup-b", Title: "Duplicate B", Artists: []string{"Artist"}, DurationMS: duration},
				{ItemID: "item-3", LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
			},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-dup": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/dup.mp3"},
			"cluster-3":   {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/three.mp3"},
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	loader := NewCatalogLoader(bridge)
	if _, err := session.ReplaceSourceAndPlay(context.Background(), loader.BuildPlaylistTrackSource("playlist-1", "item-2"), false); err != nil {
		t.Fatalf("play duplicate playlist item: %v", err)
	}

	bridge.playlistTracks["playlist-1"] = []apitypes.PlaylistTrackItem{
		{ItemID: "item-1", LibraryRecordingID: "cluster-dup", RecordingID: "variant-dup-a", Title: "Duplicate A", Artists: []string{"Artist"}, DurationMS: duration},
		{ItemID: "item-3", LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Later", Artists: []string{"Artist"}, DurationMS: duration},
	}
	session.handleCatalogChange(apitypes.CatalogChangeEvent{
		Entity:   apitypes.CatalogChangeEntityPlaylistTracks,
		EntityID: "playlist-1",
	})

	snapshot := session.Snapshot()
	if snapshot.CurrentEntry == nil {
		t.Fatalf("expected current entry to remain detached after duplicate removal")
	}
	if snapshot.CurrentEntry.Item.SourceItemID != "item-2" {
		t.Fatalf("expected removed playlist item to keep playing, got %+v", snapshot.CurrentEntry)
	}
	if currentMatchesContext(snapshot) {
		t.Fatalf("expected removed duplicate playlist item to stay detached from rebuilt context")
	}

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next after duplicate removal: %v", err)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.SourceItemID != "item-3" {
		t.Fatalf("expected next to advance past removed duplicate, got %+v", next.CurrentEntry)
	}
}

func TestSessionSetContextPrunesStaleEntryAvailability(t *testing.T) {
	t.Parallel()

	session := NewSession(&mockBridge{}, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	session.mu.Lock()
	session.snapshot.EntryAvailability = map[string]apitypes.RecordingPlaybackAvailability{
		"stale-entry": {
			RecordingID: "stale-recording",
			State:       apitypes.AvailabilityUnavailableProvider,
		},
	}
	session.mu.Unlock()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{{
			RecordingID: "rec-new",
			Title:       "New Track",
			DurationMS:  1000,
		}},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}

	snapshot := session.Snapshot()
	if len(snapshot.EntryAvailability) != 0 {
		t.Fatalf("expected stale entry availability to be pruned, got %+v", snapshot.EntryAvailability)
	}
}

func TestSessionCloseCancelsBlockedLiveRebaseWorker(t *testing.T) {
	t.Parallel()

	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 120000},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 120000},
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}

	entered := make(chan struct{})
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-ctx.Done()
		return ctx.Err()
	}

	session.requestCatalogRebase(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live rebase to start")
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("close session: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close blocked while waiting for live rebase cancellation")
	}
}

func TestSessionLiveRebaseDropsStaleCommitAfterSelectionChange(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"cluster-2": makePlayableResult("cluster-2"),
			"cluster-3": makePlayableResult("cluster-3"),
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	before := session.Snapshot()
	targetEntryID := contextAllEntries(before)[1].EntryID

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
	}
	go func() {
		session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})
		close(done)
	}()

	waitForSignal(t, entered, 2*time.Second, "timed out waiting for live rebase to start")
	if _, err := session.SelectEntry(context.Background(), targetEntryID); err != nil {
		t.Fatalf("select entry during live rebase: %v", err)
	}
	close(release)
	waitForSignal(t, done, 2*time.Second, "timed out waiting for live rebase to finish")

	snapshot := session.Snapshot()
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.SourceVersion != before.ContextQueue.SourceVersion {
		t.Fatalf("expected stale live rebase to be dropped after selection change, got before=%+v after=%+v", before.ContextQueue, snapshot.ContextQueue)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "cluster-2" {
		t.Fatalf("expected selection change to remain authoritative, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionLiveRebaseDropsStaleCommitAfterShuffleToggle(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	session := NewSession(&mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"cluster-2": makePlayableResult("cluster-2"),
			"cluster-3": makePlayableResult("cluster-3"),
		},
	}, newTestBackend(), &memoryStore{}, "desktop", nil)
	bridge := session.core.(*mockBridge)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	before := session.Snapshot()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
	}
	go func() {
		session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})
		close(done)
	}()

	waitForSignal(t, entered, 2*time.Second, "timed out waiting for live rebase to start")
	if _, err := session.SetShuffle(true); err != nil {
		t.Fatalf("set shuffle during live rebase: %v", err)
	}
	close(release)
	waitForSignal(t, done, 2*time.Second, "timed out waiting for live rebase to finish")

	snapshot := session.Snapshot()
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.SourceVersion != before.ContextQueue.SourceVersion {
		t.Fatalf("expected stale live rebase to be dropped after shuffle toggle, got before=%+v after=%+v", before.ContextQueue, snapshot.ContextQueue)
	}
	if !snapshot.Shuffle {
		t.Fatalf("expected shuffle toggle to remain applied")
	}
}

func TestSessionLiveRebaseCommitsAcrossUIOnlyChange(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"cluster-2": makePlayableResult("cluster-2"),
			"cluster-3": makePlayableResult("cluster-3"),
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	before := session.Snapshot()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
	}
	go func() {
		session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})
		close(done)
	}()

	waitForSignal(t, entered, 2*time.Second, "timed out waiting for live rebase to start")
	if _, err := session.SetVolume(context.Background(), 33); err != nil {
		t.Fatalf("set volume during live rebase: %v", err)
	}
	close(release)
	waitForSignal(t, done, 2*time.Second, "timed out waiting for live rebase to finish")

	snapshot := session.Snapshot()
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.SourceVersion <= before.ContextQueue.SourceVersion {
		t.Fatalf("expected live rebase to commit across UI-only change, got before=%+v after=%+v", before.ContextQueue, snapshot.ContextQueue)
	}
	if len(contextAllEntries(snapshot)) != 3 {
		t.Fatalf("expected rebuilt liked context to contain 3 entries, got %+v", snapshot.ContextQueue)
	}
	if snapshot.Volume != 33 {
		t.Fatalf("expected UI-only volume change to survive rebase, got %d", snapshot.Volume)
	}
}

func TestSessionLiveRebaseDropsStaleCommitAfterFutureQueueMutation(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"cluster-2": makePlayableResult("cluster-2"),
			"cluster-3": makePlayableResult("cluster-3"),
			"queue-1":   makePlayableResult("queue-1"),
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	before := session.Snapshot()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	bridge.likedRecordings = []apitypes.LikedRecordingItem{
		{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: duration},
		{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
	}
	go func() {
		session.handleCatalogChange(apitypes.CatalogChangeEvent{Entity: apitypes.CatalogChangeEntityLiked})
		close(done)
	}()

	waitForSignal(t, entered, 2*time.Second, "timed out waiting for live rebase to start")
	if _, err := session.QueueItems([]SessionItem{{RecordingID: "queue-1", Title: "Queued", DurationMS: duration}}, QueueInsertLast); err != nil {
		t.Fatalf("queue items during live rebase: %v", err)
	}
	close(release)
	waitForSignal(t, done, 2*time.Second, "timed out waiting for live rebase to finish")

	snapshot := session.Snapshot()
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.SourceVersion != before.ContextQueue.SourceVersion {
		t.Fatalf("expected future-order queue mutation to invalidate stale live rebase, got before=%+v after=%+v", before.ContextQueue, snapshot.ContextQueue)
	}
	if len(snapshot.UserQueue) != 1 || snapshot.UserQueue[0].Item.RecordingID != "queue-1" {
		t.Fatalf("expected queued item to remain applied, got %+v", snapshot.UserQueue)
	}
}

func TestSessionSetSourceDropsStaleEnumeratedResultAfterNewerIntent(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"rec-local": makePlayableResult("rec-local"),
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	go func() {
		_, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false)
		done <- err
	}()

	waitForSignal(t, entered, 2*time.Second, "timed out waiting for set source enumeration to start")
	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "custom",
		Items: []SessionItem{
			{RecordingID: "rec-local", Title: "Local", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set context during stale set source: %v", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale set source returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stale set source to finish")
	}

	snapshot := session.Snapshot()
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.Kind != ContextKindCustom {
		t.Fatalf("expected newer context intent to win, got %+v", snapshot.ContextQueue)
	}
	if len(contextAllEntries(snapshot)) != 1 || contextAllEntries(snapshot)[0].Item.RecordingID != "rec-local" {
		t.Fatalf("expected stale set source result to be dropped, got %+v", snapshot.ContextQueue)
	}
}

func TestSessionSetSourceKeepsRequestedAnchorWhileDetachedCurrentContinues(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-3", RecordingID: "variant-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-current": makePlayableResult("rec-current"),
			"cluster-1":   makePlayableResult("cluster-1"),
			"cluster-2":   makePlayableResult("cluster-2"),
			"cluster-3":   makePlayableResult("cluster-3"),
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetContext(PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "context-a",
		Items: []SessionItem{
			{RecordingID: "rec-current", Title: "Current", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("set initial context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play current context: %v", err)
	}

	snapshot, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedTrackSource("cluster-2"), false)
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-current" {
		t.Fatalf("expected detached current track to keep playing, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.CurrentEntry.ContextIndex != -1 {
		t.Fatalf("expected detached current track to clear stale context index, got %+v", snapshot.CurrentEntry)
	}
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.CurrentIndex != 1 || snapshot.ContextQueue.ResumeIndex != 1 {
		t.Fatalf("expected staged source anchor at index 1, got %+v", snapshot.ContextQueue)
	}
	if currentMatchesContext(snapshot) {
		t.Fatalf("expected current track to remain detached from the staged source")
	}

	next, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("next after staged source: %v", err)
	}
	if next.CurrentEntry == nil || next.CurrentEntry.Item.RecordingID != "cluster-2" {
		t.Fatalf("expected next to start staged source at cluster-2, got %+v", next.CurrentEntry)
	}
}

func TestSessionReplaceSourceAndPlayDropsStaleEnumeratedResultAfterNewerIntent(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"rec-old":   makePlayableResult("rec-old"),
			"rec-new":   makePlayableResult("rec-new"),
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.ReplaceContextAndPlay(context.Background(), PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "initial",
		Items: []SessionItem{
			{RecordingID: "rec-old", Title: "Old", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("seed current playback: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	bridge.likedRecordingsCursorHook = func(ctx context.Context, _ apitypes.LikedRecordingCursorRequest) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	go func() {
		_, err := session.ReplaceSourceAndPlay(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false)
		done <- err
	}()

	waitForSignal(t, entered, 2*time.Second, "timed out waiting for replace source enumeration to start")
	if _, err := session.ReplaceContextAndPlay(context.Background(), PlaybackContextInput{
		Kind: ContextKindCustom,
		ID:   "replacement",
		Items: []SessionItem{
			{RecordingID: "rec-new", Title: "New", DurationMS: duration},
		},
	}); err != nil {
		t.Fatalf("replace context and play during stale source replacement: %v", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale replace source and play returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stale replace source and play to finish")
	}

	snapshot := session.Snapshot()
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.Kind != ContextKindCustom {
		t.Fatalf("expected newer replace-context intent to win, got %+v", snapshot.ContextQueue)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-new" {
		t.Fatalf("expected stale replace source and play result to be dropped, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionUnrelatedQueuedEditDoesNotCancelPendingTransport(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	backend.duration = &duration
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": makePlayableResult("rec-1"),
			"rec-2": makePlayableResult("rec-2"),
			"rec-3": makePlayableResult("rec-3"),
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
	}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	queued := session.Snapshot().UserQueue
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())
	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := session.RemoveQueuedEntry(queued[1].EntryID); err != nil {
		t.Fatalf("remove unrelated queued entry: %v", err)
	}

	snapshot := session.Snapshot()
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.EntryID != queued[0].EntryID {
		t.Fatalf("expected pending transport target to remain active, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected current playback to stay on rec-1 while transport pending, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionRemovingPendingQueuedTargetCancelsTransport(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	backend := newTestBackend()
	backend.supportsPreload = true
	backend.duration = &duration
	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": makePlayableResult("rec-1"),
			"rec-2": makePlayableResult("rec-2"),
			"rec-3": makePlayableResult("rec-3"),
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
	}, QueueInsertLast); err != nil {
		t.Fatalf("queue items: %v", err)
	}
	targetEntryID := session.Snapshot().UserQueue[0].EntryID
	backend.position = 60000
	session.refreshPosition("test")
	session.preloadNext(context.Background())
	if _, err := session.Next(context.Background()); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := session.RemoveQueuedEntry(targetEntryID); err != nil {
		t.Fatalf("remove pending queued target: %v", err)
	}

	snapshot := session.Snapshot()
	if snapshot.LoadingEntry != nil {
		t.Fatalf("expected removing pending queued target to cancel transport, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.CurrentEntry == nil || snapshot.CurrentEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected stable current playback after target removal, got %+v", snapshot.CurrentEntry)
	}
}

func TestSessionAvailabilityInvalidationClearsAvailabilityWithoutLiveRebase(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	likedCursorCalls := 0
	backend := newTestBackend()
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
			"cluster-2": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/two.mp3"},
		},
		likedRecordingsCursorHook: func(context.Context, apitypes.LikedRecordingCursorRequest) error {
			likedCursorCalls++
			return nil
		},
	}
	session := NewSession(bridge, backend, &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.SetSource(context.Background(), NewCatalogLoader(bridge).BuildLikedSource(), false); err != nil {
		t.Fatalf("set source: %v", err)
	}
	initialCursorCalls := likedCursorCalls

	session.mu.Lock()
	firstEntry := contextAllEntries(session.snapshot)[0]
	nextEntry := contextAllEntries(session.snapshot)[1]
	session.snapshot.EntryAvailability = map[string]apitypes.RecordingPlaybackAvailability{
		firstEntry.EntryID: {
			RecordingID: "cluster-1",
			State:       apitypes.AvailabilityUnavailableProvider,
		},
	}
	session.snapshot.NextPreparation = &EntryPreparation{
		EntryID: nextEntry.EntryID,
		Status: apitypes.PlaybackPreparationStatus{
			RecordingID: "cluster-2",
			Phase:       apitypes.PlaybackPreparationReady,
			PlayableURI: "file:///tmp/two.mp3",
		},
	}
	session.preloadedID = nextEntry.EntryID
	session.preloadedURI = "file:///tmp/two.mp3"
	session.backendPreloadArmed = true
	session.touchLocked()
	before := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	session.handleCatalogChange(apitypes.CatalogChangeEvent{
		Kind:         apitypes.CatalogChangeInvalidateAvailability,
		Entity:       apitypes.CatalogChangeEntityLiked,
		RecordingIDs: []string{"cluster-1"},
	})

	snapshot := session.Snapshot()
	if likedCursorCalls != initialCursorCalls {
		t.Fatalf("expected availability invalidation to avoid live rebase, cursor calls %d -> %d", initialCursorCalls, likedCursorCalls)
	}
	if snapshot.ContextQueue == nil || snapshot.ContextQueue.SourceVersion != before.ContextQueue.SourceVersion {
		t.Fatalf("expected source version to stay stable, got before=%+v after=%+v", before.ContextQueue, snapshot.ContextQueue)
	}
	if len(snapshot.EntryAvailability) != 0 {
		t.Fatalf("expected entry availability cleared, got %+v", snapshot.EntryAvailability)
	}
	if snapshot.NextPreparation != nil {
		t.Fatalf("expected next preparation cleared, got %+v", snapshot.NextPreparation)
	}
	if snapshot.QueueVersion <= before.QueueVersion {
		t.Fatalf("expected queue version to advance, got %d -> %d", before.QueueVersion, snapshot.QueueVersion)
	}
}

func TestSessionResolvePlayableCandidateBatchDoesNotAdvanceQueueVersionForIdenticalAvailability(t *testing.T) {
	t.Parallel()

	entry := makeAvailabilityCandidateEntry("queue-1", "rec-1")
	status := makePlayableAvailability("rec-1")
	bridge := &mockBridge{
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": status,
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)

	session.mu.Lock()
	session.snapshot.UserQueue = []SessionEntry{entry}
	session.snapshot.EntryAvailability = map[string]apitypes.RecordingPlaybackAvailability{
		entry.EntryID: status,
	}
	session.touchLocked()
	before := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	candidate, skipped, stale := session.resolvePlayableCandidateBatch(context.Background(), bridge, []plannedCandidate{{
		EntryID:     entry.EntryID,
		Origin:      EntryOriginQueued,
		QueueIndex:  0,
		Target:      entry.Item.Target,
		RecordingID: entry.Item.RecordingID,
	}}, 0)
	if stale {
		t.Fatal("expected candidate batch to remain valid")
	}
	if candidate == nil || candidate.Entry.EntryID != entry.EntryID {
		t.Fatalf("expected candidate %s, got %+v", entry.EntryID, candidate)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped entries, got %+v", skipped)
	}

	session.mu.Lock()
	session.touchLocked()
	after := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	if after.QueueVersion != before.QueueVersion {
		t.Fatalf("expected queue version to stay stable, got %d -> %d", before.QueueVersion, after.QueueVersion)
	}
	if !reflect.DeepEqual(after.EntryAvailability, before.EntryAvailability) {
		t.Fatalf("expected identical availability snapshot, got before=%+v after=%+v", before.EntryAvailability, after.EntryAvailability)
	}
}

func TestSessionResolvePlayableCandidateBatchAdvancesQueueVersionWhenAvailabilityChanges(t *testing.T) {
	t.Parallel()

	entry := makeAvailabilityCandidateEntry("queue-1", "rec-1")
	initial := makeUnavailableAvailability("rec-1")
	updated := makePlayableAvailability("rec-1")
	bridge := &mockBridge{
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": updated,
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)

	session.mu.Lock()
	session.snapshot.UserQueue = []SessionEntry{entry}
	session.snapshot.EntryAvailability = map[string]apitypes.RecordingPlaybackAvailability{
		entry.EntryID: initial,
	}
	session.touchLocked()
	before := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	candidate, skipped, stale := session.resolvePlayableCandidateBatch(context.Background(), bridge, []plannedCandidate{{
		EntryID:     entry.EntryID,
		Origin:      EntryOriginQueued,
		QueueIndex:  0,
		Target:      entry.Item.Target,
		RecordingID: entry.Item.RecordingID,
	}}, 0)
	if stale {
		t.Fatal("expected candidate batch to remain valid")
	}
	if candidate == nil || candidate.Entry.EntryID != entry.EntryID {
		t.Fatalf("expected candidate %s, got %+v", entry.EntryID, candidate)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped entries, got %+v", skipped)
	}

	session.mu.Lock()
	session.touchLocked()
	after := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	if after.QueueVersion <= before.QueueVersion {
		t.Fatalf("expected queue version to advance, got %d -> %d", before.QueueVersion, after.QueueVersion)
	}
	if !reflect.DeepEqual(after.EntryAvailability[entry.EntryID], updated) {
		t.Fatalf("expected updated availability %+v, got %+v", updated, after.EntryAvailability[entry.EntryID])
	}
}

func TestSessionSetRepeatModeAdvancesQueueVersionWhenResumeIndexChanges(t *testing.T) {
	t.Parallel()

	duration := int64(120000)
	bridge := &mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: duration},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: duration},
		},
		results: map[string]apitypes.PlaybackResolveResult{
			"cluster-1": makePlayableResult("cluster-1"),
			"cluster-2": makePlayableResult("cluster-2"),
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	if _, err := session.ReplaceSourceAndPlay(context.Background(), NewCatalogLoader(bridge).BuildLikedTrackSource("cluster-2"), false); err != nil {
		t.Fatalf("replace source and play: %v", err)
	}

	var emitted []SessionSnapshot
	session.SetSnapshotEmitter(func(snapshot SessionSnapshot) {
		emitted = append(emitted, snapshot)
	})
	before := session.Snapshot()
	if before.ContextQueue == nil || before.ContextQueue.ResumeIndex != -1 {
		t.Fatalf("expected repeat-off queue to end without resume target, got %+v", before.ContextQueue)
	}

	after, err := session.SetRepeatMode(string(RepeatAll))
	if err != nil {
		t.Fatalf("set repeat mode: %v", err)
	}
	if after.ContextQueue == nil || after.ContextQueue.ResumeIndex != 0 {
		t.Fatalf("expected repeat-all to wrap resume index to 0, got %+v", after.ContextQueue)
	}
	if after.QueueVersion <= before.QueueVersion {
		t.Fatalf("expected queue version to advance, got %d -> %d", before.QueueVersion, after.QueueVersion)
	}
	if len(emitted) == 0 {
		t.Fatal("expected repeat change to publish a snapshot")
	}
	last := emitted[len(emitted)-1]
	if last.QueueVersion != after.QueueVersion {
		t.Fatalf("expected emitted snapshot queue version %d, got %d", after.QueueVersion, last.QueueVersion)
	}
	if last.ContextQueue == nil || last.ContextQueue.ResumeIndex != 0 {
		t.Fatalf("expected emitted snapshot resume index 0, got %+v", last.ContextQueue)
	}
}

func TestSessionResolvePlayableCandidateBatchAddsNewAvailabilityOnce(t *testing.T) {
	t.Parallel()

	entry := makeAvailabilityCandidateEntry("queue-1", "rec-1")
	status := makePlayableAvailability("rec-1")
	bridge := &mockBridge{
		availability: map[string]apitypes.RecordingPlaybackAvailability{
			"rec-1": status,
		},
	}
	session := NewSession(bridge, newTestBackend(), &memoryStore{}, "desktop", nil)

	session.mu.Lock()
	session.snapshot.UserQueue = []SessionEntry{entry}
	session.touchLocked()
	initial := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	firstCandidate, skipped, stale := session.resolvePlayableCandidateBatch(context.Background(), bridge, []plannedCandidate{{
		EntryID:     entry.EntryID,
		Origin:      EntryOriginQueued,
		QueueIndex:  0,
		Target:      entry.Item.Target,
		RecordingID: entry.Item.RecordingID,
	}}, 0)
	if stale {
		t.Fatal("expected candidate batch to remain valid")
	}
	if firstCandidate == nil || firstCandidate.Entry.EntryID != entry.EntryID {
		t.Fatalf("expected candidate %s, got %+v", entry.EntryID, firstCandidate)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped entries, got %+v", skipped)
	}

	session.mu.Lock()
	session.touchLocked()
	afterFirst := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	if afterFirst.QueueVersion <= initial.QueueVersion {
		t.Fatalf("expected first availability write to advance queue version, got %d -> %d", initial.QueueVersion, afterFirst.QueueVersion)
	}
	if !reflect.DeepEqual(afterFirst.EntryAvailability[entry.EntryID], status) {
		t.Fatalf("expected stored availability %+v, got %+v", status, afterFirst.EntryAvailability[entry.EntryID])
	}

	secondCandidate, skipped, stale := session.resolvePlayableCandidateBatch(context.Background(), bridge, []plannedCandidate{{
		EntryID:     entry.EntryID,
		Origin:      EntryOriginQueued,
		QueueIndex:  0,
		Target:      entry.Item.Target,
		RecordingID: entry.Item.RecordingID,
	}}, 0)
	if stale {
		t.Fatal("expected candidate batch to remain valid")
	}
	if secondCandidate == nil || secondCandidate.Entry.EntryID != entry.EntryID {
		t.Fatalf("expected candidate %s, got %+v", entry.EntryID, secondCandidate)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped entries, got %+v", skipped)
	}

	session.mu.Lock()
	session.touchLocked()
	afterSecond := snapshotCopyLocked(&session.snapshot)
	session.mu.Unlock()

	if afterSecond.QueueVersion != afterFirst.QueueVersion {
		t.Fatalf("expected repeated identical availability to keep queue version stable, got %d -> %d", afterFirst.QueueVersion, afterSecond.QueueVersion)
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

func TestSessionUnrelatedQueueMutationPreservesPendingLoading(t *testing.T) {
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
	if snapshot.LoadingEntry == nil || snapshot.LoadingEntry.Item.RecordingID != "rec-1" {
		t.Fatalf("expected queue mutation to preserve pending loading target, got %+v", snapshot.LoadingEntry)
	}
	if snapshot.Status != StatusPending {
		t.Fatalf("expected pending status to remain stable during unrelated queue mutation, got %q", snapshot.Status)
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
	upcoming := buildUpcomingEntries(shuffled)
	if len(upcoming) < 3 {
		t.Fatalf("expected queued entries plus shuffled context in upcoming list, got %+v", upcoming)
	}
	if upcoming[0].Item.RecordingID != "q-1" || upcoming[1].Item.RecordingID != "q-2" {
		t.Fatalf("expected queued entries to stay ahead of shuffle order, got %+v", upcoming)
	}
	expectedContext := shuffled.ContextQueue.Entries[shuffled.ContextQueue.ShuffleBag[1]].Item.RecordingID
	if upcoming[2].Item.RecordingID != expectedContext {
		t.Fatalf("expected first shuffled context entry %s after queued items, got %+v", expectedContext, upcoming)
	}
}

func TestCatalogLoaderMaterializeSourceStartsAlbumAtSelectedTrack(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		albums: map[string]apitypes.AlbumListItem{
			"album-1": {AlbumID: "album-1", Title: "Album One"},
		},
		albumTracks: map[string][]apitypes.AlbumTrackItem{
			"album-1": {
				{RecordingID: "rec-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
				{RecordingID: "rec-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
				{RecordingID: "rec-3", Title: "Three", Artists: []string{"Artist"}, DurationMS: 1000},
			},
		},
	})

	contextInput, err := loader.MaterializeSource(context.Background(), loader.BuildAlbumTrackSource("album-1", "rec-2"))
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
	if contextInput.Title != "Album One" {
		t.Fatalf("title = %q, want Album One", contextInput.Title)
	}
}

func TestCatalogLoaderMaterializeSourceStartsPlaylistAtSelectedItem(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		playlists: map[string]apitypes.PlaylistListItem{
			"playlist-1": {PlaylistID: "playlist-1", Name: "Roadtrip Mix"},
		},
		playlistTracks: map[string][]apitypes.PlaylistTrackItem{
			"playlist-1": {
				{ItemID: "item-1", RecordingID: "rec-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
				{ItemID: "item-2", RecordingID: "rec-1", Title: "One again", Artists: []string{"Artist"}, DurationMS: 1000},
				{ItemID: "item-3", RecordingID: "rec-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
			},
		},
	})

	contextInput, err := loader.MaterializeSource(context.Background(), loader.BuildPlaylistTrackSource("playlist-1", "item-2"))
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
	if contextInput.Title != "Roadtrip Mix" {
		t.Fatalf("title = %q, want Roadtrip Mix", contextInput.Title)
	}
}

func TestCatalogLoaderResolveSourceItemReturnsSelectedPlaylistItem(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		playlistTracks: map[string][]apitypes.PlaylistTrackItem{
			"playlist-1": {
				{ItemID: "item-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
				{ItemID: "item-2", LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
			},
		},
	})

	item, err := loader.ResolveSourceItem(context.Background(), loader.BuildPlaylistTrackSource("playlist-1", "item-2"))
	if err != nil {
		t.Fatalf("load playlist item: %v", err)
	}
	if item.SourceKind != SourceKindPlaylist || item.SourceID != "playlist-1" || item.SourceItemID != "item-2" {
		t.Fatalf("unexpected playlist item source: %+v", item)
	}
	if item.Target.ResolutionPolicy != PlaybackTargetResolutionPreferred {
		t.Fatalf("resolution policy = %q, want %q", item.Target.ResolutionPolicy, PlaybackTargetResolutionPreferred)
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
	if items[0].Target.ExactVariantRecordingID != "variant-2" {
		t.Fatalf("exact variant id = %q, want variant-2", items[0].Target.ExactVariantRecordingID)
	}
	if items[0].Target.ResolutionPolicy != PlaybackTargetResolutionPreferred {
		t.Fatalf("resolution policy = %q, want %q", items[0].Target.ResolutionPolicy, PlaybackTargetResolutionPreferred)
	}
}

func TestCatalogLoaderMaterializeSourceStartsLikedAtSelectedTrack(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
		},
	})

	contextInput, err := loader.MaterializeSource(context.Background(), loader.BuildLikedTrackSource("cluster-2"))
	if err != nil {
		t.Fatalf("load liked track context: %v", err)
	}
	if contextInput.Kind != ContextKindLiked || contextInput.ID != "liked" {
		t.Fatalf("unexpected context: %+v", contextInput)
	}
	if contextInput.StartIndex != 1 {
		t.Fatalf("start index = %d, want 1", contextInput.StartIndex)
	}
	if len(contextInput.Items) != 2 || contextInput.Items[1].RecordingID != "cluster-2" {
		t.Fatalf("unexpected items: %+v", contextInput.Items)
	}
	if contextInput.Items[1].Target.ExactVariantRecordingID != "variant-2" {
		t.Fatalf("exact variant target = %q, want variant-2", contextInput.Items[1].Target.ExactVariantRecordingID)
	}
	if contextInput.Items[1].Target.ResolutionPolicy != PlaybackTargetResolutionPreferred {
		t.Fatalf("resolution policy = %q, want %q", contextInput.Items[1].Target.ResolutionPolicy, PlaybackTargetResolutionPreferred)
	}
}

func TestItemsFromLikedRecordingsUsePreferredResolutionTarget(t *testing.T) {
	t.Parallel()

	items := ItemsFromLikedRecordings([]apitypes.LikedRecordingItem{
		{
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
	if items[0].RecordingID != "cluster-1" {
		t.Fatalf("recording id = %q, want cluster-1", items[0].RecordingID)
	}
	if items[0].Target.ExactVariantRecordingID != "variant-2" {
		t.Fatalf("exact variant id = %q, want variant-2", items[0].Target.ExactVariantRecordingID)
	}
	if items[0].ResolutionMode != ResolutionModeLibrary {
		t.Fatalf("resolution mode = %q, want %q", items[0].ResolutionMode, ResolutionModeLibrary)
	}
	if items[0].Target.ResolutionPolicy != PlaybackTargetResolutionPreferred {
		t.Fatalf("resolution policy = %q, want %q", items[0].Target.ResolutionPolicy, PlaybackTargetResolutionPreferred)
	}
}

func TestCatalogLoaderResolveSourceItemReturnsSelectedLikedTrack(t *testing.T) {
	t.Parallel()

	loader := NewCatalogLoader(&mockBridge{
		likedRecordings: []apitypes.LikedRecordingItem{
			{LibraryRecordingID: "cluster-1", RecordingID: "variant-1", Title: "One", Artists: []string{"Artist"}, DurationMS: 1000},
			{LibraryRecordingID: "cluster-2", RecordingID: "variant-2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 1000},
		},
	})

	item, err := loader.ResolveSourceItem(context.Background(), loader.BuildLikedTrackSource("cluster-2"))
	if err != nil {
		t.Fatalf("load liked item: %v", err)
	}
	if item.SourceKind != SourceKindLiked || item.RecordingID != "cluster-2" {
		t.Fatalf("unexpected liked item: %+v", item)
	}
	if item.Target.ResolutionPolicy != PlaybackTargetResolutionPreferred {
		t.Fatalf("resolution policy = %q, want %q", item.Target.ResolutionPolicy, PlaybackTargetResolutionPreferred)
	}
}

func TestCatalogLoaderMaterializeSourcePreservesRequestedRecordingVariantTarget(t *testing.T) {
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

	contextInput, err := loader.MaterializeSource(context.Background(), loader.BuildRecordingSource("variant-2"))
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

	contextInput, err := loader.MaterializeSource(context.Background(), loader.BuildRecordingSource("variant-2"))
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
	if len(buildUpcomingEntries(snapshot)) != 0 {
		t.Fatalf("expected upcoming entries to be cleared, got %d", len(buildUpcomingEntries(snapshot)))
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

func TestHasNextActionMatchesRepeatOneAndQueueState(t *testing.T) {
	t.Parallel()

	session := NewSession(&mockBridge{
		results: map[string]apitypes.PlaybackResolveResult{
			"rec-1": {State: apitypes.AvailabilityPlayableLocalFile, SourceKind: apitypes.PlaybackSourceLocalFile, PlayableURI: "file:///tmp/one.mp3"},
		},
	}, newTestBackend(), &memoryStore{}, "desktop", nil)
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

	if HasNextAction(session.Snapshot()) {
		t.Fatalf("expected no next action for a single-track session without repeat")
	}

	if _, err := session.SetRepeatMode(string(RepeatOne)); err != nil {
		t.Fatalf("set repeat mode: %v", err)
	}

	if !HasNextAction(session.Snapshot()) {
		t.Fatalf("expected repeat-one to expose a next action for the current track")
	}
}

func TestNormalizeSnapshotDropsInvalidPersistedShuffleBag(t *testing.T) {
	t.Parallel()

	snapshot := normalizeSnapshot(SessionSnapshot{
		Shuffle: true,
		ContextQueue: &ContextQueue{
			Kind:         ContextKindTracks,
			ID:           "tracks",
			ShuffleBag:   []int{0, 775},
			CurrentIndex: 0,
			ResumeIndex:  0,
			allEntries: []SessionEntry{
				{
					EntryID:      "ctx-1",
					Origin:       EntryOriginContext,
					ContextIndex: 0,
					Item:         SessionItem{RecordingID: "rec-1", Title: "One"},
				},
				{
					EntryID:      "ctx-2",
					Origin:       EntryOriginContext,
					ContextIndex: 1,
					Item:         SessionItem{RecordingID: "rec-2", Title: "Two"},
				},
			},
			Entries: []SessionEntry{
				{
					EntryID:      "ctx-1",
					Origin:       EntryOriginContext,
					ContextIndex: 0,
					Item:         SessionItem{RecordingID: "rec-1", Title: "One"},
				},
				{
					EntryID:      "ctx-2",
					Origin:       EntryOriginContext,
					ContextIndex: 1,
					Item:         SessionItem{RecordingID: "rec-2", Title: "Two"},
				},
			},
		},
	})

	if snapshot.ContextQueue == nil {
		t.Fatalf("expected context queue to survive normalization")
	}
	if len(snapshot.ContextQueue.ShuffleBag) != 0 {
		t.Fatalf("expected invalid shuffle bag to be dropped, got %+v", snapshot.ContextQueue.ShuffleBag)
	}
	if len(buildUpcomingEntries(snapshot)) != 2 {
		t.Fatalf("expected rebuilt upcoming entries, got %d", len(buildUpcomingEntries(snapshot)))
	}
}

func TestSessionEOFWithRepeatOneRestartsCurrentTrack(t *testing.T) {
	t.Parallel()

	backend := newTestBackend()
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
			{RecordingID: "rec-1", Title: "One"},
			{RecordingID: "rec-2", Title: "Two"},
		},
	}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	if _, err := session.Play(context.Background()); err != nil {
		t.Fatalf("play: %v", err)
	}
	if _, err := session.SetRepeatMode(string(RepeatOne)); err != nil {
		t.Fatalf("set repeat mode: %v", err)
	}

	loadCallsBeforeEOF := backend.loadCalls
	backend.events <- BackendEvent{Type: BackendEventTrackEnd, Reason: TrackEndReasonEOF}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := session.Snapshot()
		if snapshot.CurrentEntry != nil &&
			snapshot.CurrentEntry.Item.RecordingID == "rec-1" &&
			snapshot.Status == StatusPlaying &&
			backend.loadCalls > loadCallsBeforeEOF {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected repeat-one EOF to restart rec-1, got %+v", session.Snapshot())
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
