package playback

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	apitypes "ben/core/api/types"
)

var errPendingPlayback = errors.New("playback is waiting for an available source")

const (
	positionPollInterval = 500 * time.Millisecond
	pendingRetryInterval = time.Second
	pendingRetryTimeout  = 2 * time.Minute
	maxHistoryEntries    = 128
	preloadListenedAfter = 45 * time.Second
	preloadRemainingLead = 45 * time.Second
)

type Logger interface {
	Printf(format string, args ...any)
	Errorf(format string, args ...any)
}

type Session struct {
	mu sync.Mutex

	bridge           CorePlaybackBridge
	backend          Backend
	store            SessionStore
	preferredProfile string
	logger           Logger

	snapshot SessionSnapshot

	emit func(SessionSnapshot)

	ctx    context.Context
	cancel context.CancelFunc

	eventsWG sync.WaitGroup
	tickerWG sync.WaitGroup
	tickStop chan struct{}

	loadedEntryID string
	loadedURI     string
	preloadedID   string
	preloadedURI  string

	rng *rand.Rand

	pendingCancel context.CancelFunc
	pendingWG     sync.WaitGroup
	pendingEntry  string
	pendingToken  uint64
}

func NewSession(bridge CorePlaybackBridge, backend Backend, store SessionStore, preferredProfile string, logger Logger) *Session {
	if backend == nil {
		backend = NewBackend()
	}
	return &Session{
		bridge:           bridge,
		backend:          backend,
		store:            store,
		preferredProfile: strings.TrimSpace(preferredProfile),
		logger:           logger,
		snapshot: SessionSnapshot{
			CurrentContextIndex: -1,
			RepeatMode:          RepeatOff,
			Volume:              DefaultVolume,
			Status:              StatusIdle,
			NextEntrySeq:        1,
		},
		rng: rand.New(rand.NewSource(time.Now().UTC().UnixNano())),
	}
}

func (s *Session) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	s.ctx = childCtx
	s.cancel = cancel
	s.mu.Unlock()

	if s.store != nil {
		snapshot, err := s.store.Load(childCtx)
		if err != nil {
			return err
		}
		if snapshot.Status == StatusPlaying {
			snapshot.Status = StatusPaused
		}
		s.mu.Lock()
		s.snapshot = normalizeSnapshot(snapshot)
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.preloadedID = ""
		s.preloadedURI = ""
		s.mu.Unlock()
	}

	if s.backend != nil {
		if err := s.backend.SetVolume(childCtx, s.snapshot.Volume); err != nil {
			s.logErrorf("playback: set initial volume failed: %v", err)
		}
		if events := s.backend.Events(); events != nil {
			s.eventsWG.Add(1)
			go s.runBackendEvents(childCtx, events)
		}
	}

	s.publishSnapshot(s.Snapshot())
	return nil
}

func (s *Session) Close() error {
	s.persistFinalSnapshot()

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.stopTickerLocked()
	s.cancelPendingRetryLocked()
	backend := s.backend
	s.backend = nil
	s.mu.Unlock()

	s.eventsWG.Wait()
	s.tickerWG.Wait()
	s.pendingWG.Wait()

	if backend != nil {
		return backend.Close()
	}
	return nil
}

func (s *Session) SetSnapshotEmitter(emitter func(SessionSnapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit = emitter
}

func (s *Session) Snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotCopyLocked(&s.snapshot)
}

func (s *Session) SetContext(input PlaybackContextInput) (SessionSnapshot, error) {
	s.mu.Lock()
	contextData, startIndex, err := s.buildContextLocked(input)
	if err != nil {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, err
	}

	s.snapshot.Context = contextData
	s.snapshot.QueuedEntries = nil
	s.snapshot.History = nil
	s.snapshot.PositionMS = 0
	s.snapshot.DurationMS = nil
	s.snapshot.LastError = ""
	s.snapshot.CurrentSourceKind = ""
	s.snapshot.CurrentPreparation = nil
	s.snapshot.NextPreparation = nil
	s.snapshot.Status = StatusPaused
	s.snapshot.ShuffleCycle = nil
	s.cancelPendingRetryLocked()
	s.loadedEntryID = ""
	s.loadedURI = ""
	s.preloadedID = ""
	s.preloadedURI = ""

	if len(contextData.Entries) == 0 {
		s.clearCurrentLocked()
		s.snapshot.Status = StatusIdle
	} else {
		entry := contextData.Entries[startIndex]
		s.setCurrentLocked(entry, EntryOriginContext)
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = currentDuration(&entry.Item)
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	_ = s.stopBackend()
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) QueueItems(items []SessionItem, mode QueueInsertMode) (SessionSnapshot, error) {
	items = NormalizeItems(items)
	if len(items) == 0 {
		return s.Snapshot(), nil
	}

	s.mu.Lock()
	needsClearPreload := s.snapshot.NextPreparation != nil || s.preloadedID != ""
	entries := make([]SessionEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, SessionEntry{
			EntryID:      s.nextEntryIDLocked("queued"),
			Origin:       EntryOriginQueued,
			ContextIndex: -1,
			Item:         item,
		})
	}

	insertMode := mode
	if insertMode == "" {
		insertMode = QueueInsertLast
	}
	if insertMode == QueueInsertNext {
		s.snapshot.QueuedEntries = append(entries, s.snapshot.QueuedEntries...)
	} else {
		s.snapshot.QueuedEntries = append(s.snapshot.QueuedEntries, entries...)
	}

	if s.snapshot.CurrentEntry == nil && s.snapshot.Context == nil {
		entry := s.snapshot.QueuedEntries[0]
		s.snapshot.QueuedEntries = s.snapshot.QueuedEntries[1:]
		s.setCurrentLocked(entry, EntryOriginQueued)
		s.snapshot.Status = StatusPaused
		s.snapshot.DurationMS = currentDuration(&entry.Item)
	}

	s.clearNextPreparationStateLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	backend := s.backend
	s.mu.Unlock()

	if needsClearPreload && backend != nil {
		_ = backend.ClearPreloaded(context.Background())
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) RemoveQueuedEntry(entryID string) (SessionSnapshot, error) {
	s.mu.Lock()
	index := indexOfEntryID(s.snapshot.QueuedEntries, entryID)
	if index < 0 {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queued entry %s not found", strings.TrimSpace(entryID))
	}
	needsClearPreload := s.snapshot.NextPreparation != nil || s.preloadedID != ""
	s.snapshot.QueuedEntries = append(s.snapshot.QueuedEntries[:index], s.snapshot.QueuedEntries[index+1:]...)
	s.clearNextPreparationStateLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	backend := s.backend
	s.mu.Unlock()

	if needsClearPreload && backend != nil {
		_ = backend.ClearPreloaded(context.Background())
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) MoveQueuedEntry(entryID string, toIndex int) (SessionSnapshot, error) {
	s.mu.Lock()
	fromIndex := indexOfEntryID(s.snapshot.QueuedEntries, entryID)
	if fromIndex < 0 {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queued entry %s not found", strings.TrimSpace(entryID))
	}
	if toIndex < 0 || toIndex >= len(s.snapshot.QueuedEntries) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", toIndex)
	}
	if fromIndex == toIndex {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}

	needsClearPreload := s.snapshot.NextPreparation != nil || s.preloadedID != ""
	entry := s.snapshot.QueuedEntries[fromIndex]
	queue := append([]SessionEntry(nil), s.snapshot.QueuedEntries...)
	queue = append(queue[:fromIndex], queue[fromIndex+1:]...)
	queue = append(queue[:toIndex], append([]SessionEntry{entry}, queue[toIndex:]...)...)
	s.snapshot.QueuedEntries = queue
	s.clearNextPreparationStateLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	backend := s.backend
	s.mu.Unlock()

	if needsClearPreload && backend != nil {
		_ = backend.ClearPreloaded(context.Background())
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) SelectEntry(ctx context.Context, entryID string) (SessionSnapshot, error) {
	s.mu.Lock()
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("entry id is required")
	}

	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	target, origin, queueIndex, err := s.resolveEntrySelectionLocked(entryID)
	if err != nil {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, err
	}
	wasPlaying := s.snapshot.Status == StatusPlaying
	needsClearPreload := s.snapshot.NextPreparation != nil || s.preloadedID != ""
	s.switchCurrentLocked(target, origin, queueIndex, current, false, 0)
	s.clearNextPreparationStateLocked()
	state := snapshotCopyLocked(&s.snapshot)
	backend := s.backend
	s.mu.Unlock()

	if needsClearPreload && backend != nil {
		_ = backend.ClearPreloaded(context.Background())
	}
	if !wasPlaying {
		s.publishSnapshot(state)
		return state, nil
	}
	return s.playCurrent(ctx)
}

func (s *Session) ClearQueue() (SessionSnapshot, error) {
	s.mu.Lock()
	s.snapshot.Context = nil
	s.snapshot.QueuedEntries = nil
	s.snapshot.History = nil
	s.snapshot.ShuffleCycle = nil
	s.snapshot.PositionMS = 0
	s.snapshot.DurationMS = nil
	s.snapshot.LastError = ""
	s.snapshot.Status = StatusIdle
	s.snapshot.CurrentSourceKind = ""
	s.snapshot.CurrentPreparation = nil
	s.snapshot.NextPreparation = nil
	s.clearCurrentLocked()
	s.cancelPendingRetryLocked()
	s.loadedEntryID = ""
	s.loadedURI = ""
	s.preloadedID = ""
	s.preloadedURI = ""
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	_ = s.stopBackend()
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) Play(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	if !s.ensureCurrentLocked() {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, errors.New("queue is empty")
	}
	if s.loadedEntryID == s.snapshot.CurrentEntryID && s.loadedURI != "" && s.snapshot.Status == StatusPaused {
		backend := s.backend
		s.snapshot.Status = StatusPlaying
		s.snapshot.LastError = ""
		s.touchLocked()
		s.ensureTickerLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()

		if backend != nil {
			if err := backend.Play(ctx); err != nil {
				return s.failPlayback(err)
			}
		}
		s.publishSnapshot(state)
		return state, nil
	}
	s.mu.Unlock()
	return s.playCurrent(ctx)
}

func (s *Session) Pause(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	backend := s.backend
	if s.snapshot.Status != StatusPlaying {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}
	s.snapshot.Status = StatusPaused
	s.touchLocked()
	s.stopTickerLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if backend != nil {
		if err := backend.Pause(ctx); err != nil {
			return s.failPlayback(err)
		}
	}
	s.refreshPosition()
	s.publishSnapshot(s.Snapshot())
	return state, nil
}

func (s *Session) TogglePlayback(ctx context.Context) (SessionSnapshot, error) {
	state := s.Snapshot()
	if state.Status == StatusPlaying {
		return s.Pause(ctx)
	}
	return s.Play(ctx)
}

func (s *Session) Next(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	if current != nil && s.snapshot.RepeatMode == RepeatOne {
		s.mu.Unlock()
		return s.SeekTo(ctx, 0)
	}
	nextEntry, origin, queueIndex, ok := s.peekNextLocked(true)
	s.mu.Unlock()
	if !ok {
		return s.Snapshot(), nil
	}
	return s.playEntry(ctx, nextEntry, origin, queueIndex, current, false)
}

func (s *Session) Previous(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	if s.snapshot.PositionMS > 3000 {
		s.mu.Unlock()
		return s.SeekTo(ctx, 0)
	}
	if len(s.snapshot.History) == 0 {
		current := cloneEntryPtr(s.snapshot.CurrentEntry)
		s.mu.Unlock()
		if current == nil {
			return s.Snapshot(), nil
		}
		return s.playEntry(ctx, *current, current.Origin, -1, nil, false)
	}
	last := s.snapshot.History[len(s.snapshot.History)-1]
	s.snapshot.History = s.snapshot.History[:len(s.snapshot.History)-1]
	s.mu.Unlock()

	return s.playEntry(ctx, last.Entry, last.Entry.Origin, -1, nil, false)
}

func (s *Session) SeekTo(ctx context.Context, positionMS int64) (SessionSnapshot, error) {
	if positionMS < 0 {
		positionMS = 0
	}

	s.mu.Lock()
	if s.snapshot.CurrentEntry == nil {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}
	if s.snapshot.DurationMS != nil && positionMS > *s.snapshot.DurationMS {
		positionMS = *s.snapshot.DurationMS
	}
	s.snapshot.PositionMS = positionMS
	s.touchLocked()
	backend := s.backend
	loaded := s.loadedEntryID == s.snapshot.CurrentEntryID && s.loadedURI != ""
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if loaded && backend != nil {
		if err := backend.SeekTo(ctx, positionMS); err != nil {
			return s.failPlayback(err)
		}
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) SetVolume(ctx context.Context, volume int) (SessionSnapshot, error) {
	volume = clampVolume(volume)

	s.mu.Lock()
	s.snapshot.Volume = volume
	s.touchLocked()
	backend := s.backend
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if backend != nil {
		if err := backend.SetVolume(ctx, volume); err != nil {
			return s.failPlayback(err)
		}
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) SetRepeatMode(mode string) (SessionSnapshot, error) {
	repeatMode, ok := ParseRepeatMode(strings.TrimSpace(mode))
	if !ok {
		return s.Snapshot(), fmt.Errorf("invalid repeat mode %q", mode)
	}

	s.mu.Lock()
	s.snapshot.RepeatMode = repeatMode
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) SetShuffle(enabled bool) (SessionSnapshot, error) {
	s.mu.Lock()
	s.snapshot.Shuffle = enabled
	if enabled {
		s.rebuildShuffleCycleLocked()
	} else {
		s.snapshot.ShuffleCycle = nil
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) ReplaceQueue(items []SessionItem, startIndex int) (SessionSnapshot, error) {
	return s.SetContext(PlaybackContextInput{
		Kind:       ContextKindCustom,
		ID:         "custom",
		Items:      items,
		StartIndex: startIndex,
	})
}

func (s *Session) AppendToQueue(items []SessionItem) (SessionSnapshot, error) {
	return s.QueueItems(items, QueueInsertLast)
}

func (s *Session) RemoveQueueItem(index int) (SessionSnapshot, error) {
	s.mu.Lock()
	if index < 0 || index >= len(s.snapshot.QueuedEntries) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", index)
	}
	entryID := s.snapshot.QueuedEntries[index].EntryID
	s.mu.Unlock()
	return s.RemoveQueuedEntry(entryID)
}

func (s *Session) MoveQueueItem(fromIndex int, toIndex int) (SessionSnapshot, error) {
	s.mu.Lock()
	if fromIndex < 0 || fromIndex >= len(s.snapshot.QueuedEntries) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", fromIndex)
	}
	entryID := s.snapshot.QueuedEntries[fromIndex].EntryID
	s.mu.Unlock()
	return s.MoveQueuedEntry(entryID, toIndex)
}

func (s *Session) SelectQueueIndex(ctx context.Context, index int) (SessionSnapshot, error) {
	s.mu.Lock()
	entries := s.selectionEntriesLocked()
	if index < 0 || index >= len(entries) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", index)
	}
	entryID := entries[index].EntryID
	s.mu.Unlock()
	return s.SelectEntry(ctx, entryID)
}

func (s *Session) playCurrent(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	if !s.ensureCurrentLocked() {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, errors.New("queue is empty")
	}
	current := *s.snapshot.CurrentEntry
	s.mu.Unlock()

	return s.playEntry(ctx, current, current.Origin, -1, nil, false)
}

func (s *Session) playEntry(
	ctx context.Context,
	entry SessionEntry,
	origin EntryOrigin,
	queueIndex int,
	previous *SessionEntry,
	keepPosition bool,
) (SessionSnapshot, error) {
	restorePosition := int64(0)
	if keepPosition && previous != nil && previous.EntryID == entry.EntryID {
		restorePosition = s.Snapshot().PositionMS
	}
	backend := s.backend
	bridge := s.bridge
	if bridge == nil {
		return s.Snapshot(), errors.New("core playback bridge is not configured")
	}
	if backend == nil {
		return s.Snapshot(), errors.New("playback backend is not configured")
	}

	preparation, err := bridge.PreparePlaybackRecording(ctx, entry.Item.RecordingID, s.preferredProfile, apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		return s.Snapshot(), err
	}

	if preparation.Phase == apitypes.PlaybackPreparationPreparingFetch || preparation.Phase == apitypes.PlaybackPreparationPreparingTranscode {
		s.mu.Lock()
		s.switchCurrentLocked(entry, origin, queueIndex, previous, keepPosition, restorePosition)
		s.snapshot.Status = StatusPending
		s.snapshot.LastError = ""
		s.snapshot.CurrentSourceKind = preparation.SourceKind
		s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: preparation}
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.clearNextPreparationStateLocked()
		s.touchLocked()
		s.startPendingRetryLocked(entry.EntryID)
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		s.publishSnapshot(state)
		return state, errPendingPlayback
	}

	if preparation.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(preparation.PlayableURI) == "" {
		return s.Snapshot(), fmt.Errorf("recording %s is unavailable (%s)", entry.Item.RecordingID, preparation.Reason)
	}

	if err := backend.Load(ctx, preparation.PlayableURI); err != nil {
		return s.Snapshot(), err
	}
	if restorePosition > 0 {
		_ = backend.SeekTo(ctx, restorePosition)
	}
	if err := backend.Play(ctx); err != nil {
		return s.Snapshot(), err
	}

	s.mu.Lock()
	s.switchCurrentLocked(entry, origin, queueIndex, previous, keepPosition, restorePosition)
	s.snapshot.Status = StatusPlaying
	s.snapshot.LastError = ""
	s.snapshot.CurrentSourceKind = preparation.SourceKind
	s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: preparation}
	s.loadedEntryID = entry.EntryID
	s.loadedURI = preparation.PlayableURI
	s.clearNextPreparationStateLocked()
	s.cancelPendingRetryLocked()
	s.ensureTickerLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.refreshPosition()
	s.publishSnapshot(s.Snapshot())
	return state, nil
}

func (s *Session) refreshPosition() {
	s.mu.Lock()
	backend := s.backend
	s.mu.Unlock()
	if backend == nil {
		return
	}

	positionMS, positionErr := backend.PositionMS()
	durationMS, durationErr := backend.DurationMS()

	s.mu.Lock()
	defer s.mu.Unlock()
	if positionErr == nil {
		s.snapshot.PositionMS = positionMS
	}
	if durationErr == nil {
		s.snapshot.DurationMS = cloneInt64Ptr(durationMS)
	}
	s.touchLocked()
}

func (s *Session) preloadNext(ctx context.Context) {
	s.mu.Lock()
	if s.backend == nil || !s.backend.SupportsPreload() {
		s.mu.Unlock()
		return
	}
	nextEntry, _, _, ok := s.peekNextLocked(false)
	if !ok || nextEntry.EntryID == "" || !s.shouldArmNextPreparationLocked() {
		backend := s.backend
		s.clearNextPreparationStateLocked()
		s.mu.Unlock()
		_ = backend.ClearPreloaded(ctx)
		return
	}
	currentNext := cloneEntryPreparation(s.snapshot.NextPreparation)
	backend := s.backend
	bridge := s.bridge
	s.mu.Unlock()

	if bridge == nil || backend == nil {
		return
	}

	if currentNext != nil && currentNext.EntryID == nextEntry.EntryID {
		switch currentNext.Status.Phase {
		case apitypes.PlaybackPreparationReady:
			if err := backend.PreloadNext(ctx, currentNext.Status.PlayableURI); err != nil {
				s.logErrorf("playback: preload next failed: %v", err)
				return
			}
			s.mu.Lock()
			s.preloadedID = nextEntry.EntryID
			s.preloadedURI = currentNext.Status.PlayableURI
			s.snapshot.NextPreparation = &EntryPreparation{EntryID: nextEntry.EntryID, Status: currentNext.Status}
			s.mu.Unlock()
			return
		case apitypes.PlaybackPreparationPreparingFetch, apitypes.PlaybackPreparationPreparingTranscode:
			status, err := bridge.GetPlaybackPreparation(ctx, nextEntry.Item.RecordingID, s.preferredProfile)
			if err != nil {
				return
			}
			s.applyNextPreparationStatus(ctx, nextEntry, status)
			return
		}
	}

	status, err := bridge.PreparePlaybackRecording(ctx, nextEntry.Item.RecordingID, s.preferredProfile, apitypes.PlaybackPreparationPreloadNext)
	if err != nil {
		return
	}
	s.applyNextPreparationStatus(ctx, nextEntry, status)
}

func (s *Session) applyNextPreparationStatus(ctx context.Context, entry SessionEntry, status apitypes.PlaybackPreparationStatus) {
	s.mu.Lock()
	backend := s.backend
	s.snapshot.NextPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: status}
	if status.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(status.PlayableURI) == "" {
		s.preloadedID = ""
		s.preloadedURI = ""
		s.touchLocked()
		s.mu.Unlock()
		if backend != nil {
			_ = backend.ClearPreloaded(ctx)
		}
		return
	}
	s.touchLocked()
	s.mu.Unlock()

	if backend != nil {
		if err := backend.PreloadNext(ctx, status.PlayableURI); err != nil {
			s.logErrorf("playback: preload next failed: %v", err)
			return
		}
	}
	s.mu.Lock()
	s.preloadedID = entry.EntryID
	s.preloadedURI = status.PlayableURI
	s.snapshot.NextPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: status}
	s.touchLocked()
	s.mu.Unlock()
}

func (s *Session) shouldArmNextPreparationLocked() bool {
	if s.snapshot.Status != StatusPlaying || s.snapshot.CurrentEntry == nil {
		return false
	}
	if s.snapshot.PositionMS >= preloadListenedAfter.Milliseconds() {
		return true
	}
	if s.snapshot.DurationMS == nil {
		return false
	}
	remaining := *s.snapshot.DurationMS - s.snapshot.PositionMS
	return remaining <= preloadRemainingLead.Milliseconds()
}

func (s *Session) clearNextPreparationStateLocked() {
	s.snapshot.NextPreparation = nil
	s.preloadedID = ""
	s.preloadedURI = ""
}

func (s *Session) runBackendEvents(ctx context.Context, events <-chan BackendEvent) {
	defer s.eventsWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			s.handleBackendEvent(event)
		}
	}
}

func (s *Session) handleBackendEvent(event BackendEvent) {
	switch event.Type {
	case BackendEventTrackEnd:
		if event.Reason != TrackEndReasonEOF {
			return
		}
		s.handleTrackEOF()
	case BackendEventShutdown:
		s.mu.Lock()
		s.snapshot.Status = StatusPaused
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		s.publishSnapshot(state)
	case BackendEventError:
		if event.Err != nil {
			_, _ = s.failPlayback(event.Err)
		}
	}
}

func (s *Session) handleTrackEOF() {
	s.mu.Lock()
	playing := s.snapshot.Status == StatusPlaying
	supportsPreload := s.backend != nil && s.backend.SupportsPreload()
	nextEntry, origin, queueIndex, ok := s.peekNextLocked(false)
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	preloadedMatches := supportsPreload && ok && s.preloadedID != "" && s.preloadedID == nextEntry.EntryID
	s.mu.Unlock()
	if !playing {
		return
	}

	if !ok {
		s.mu.Lock()
		s.snapshot.Status = StatusPaused
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = currentDuration(currentItemFromEntry(s.snapshot.CurrentEntry))
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.clearNextPreparationStateLocked()
		s.stopTickerLocked()
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		s.publishSnapshot(state)
		return
	}

	if preloadedMatches {
		s.mu.Lock()
		s.switchCurrentLocked(nextEntry, origin, queueIndex, current, false, 0)
		s.snapshot.Status = StatusPlaying
		s.snapshot.LastError = ""
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = currentDuration(&nextEntry.Item)
		if s.snapshot.NextPreparation != nil && s.snapshot.NextPreparation.EntryID == nextEntry.EntryID {
			s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: nextEntry.EntryID, Status: s.snapshot.NextPreparation.Status}
		}
		s.loadedEntryID = nextEntry.EntryID
		s.loadedURI = s.preloadedURI
		s.clearNextPreparationStateLocked()
		s.ensureTickerLocked()
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()

		s.preloadNext(context.Background())
		s.publishSnapshot(state)
		return
	}

	_, err := s.playEntry(context.Background(), nextEntry, origin, queueIndex, current, false)
	if err != nil {
		s.logErrorf("playback: auto-next failed: %v", err)
	}
}

func (s *Session) ensureTickerLocked() {
	if s.tickStop != nil {
		return
	}
	stop := make(chan struct{})
	s.tickStop = stop
	s.tickerWG.Add(1)
	go s.runTicker(stop)
}

func (s *Session) stopTickerLocked() {
	if s.tickStop == nil {
		return
	}
	close(s.tickStop)
	s.tickStop = nil
}

func (s *Session) runTicker(stop <-chan struct{}) {
	defer s.tickerWG.Done()
	ticker := time.NewTicker(positionPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			playing := s.snapshot.Status == StatusPlaying
			s.mu.Unlock()
			if !playing {
				continue
			}
			s.refreshPosition()
			s.preloadNext(context.Background())
			s.publishSnapshot(s.Snapshot())
		}
	}
}

func (s *Session) startPendingRetryLocked(entryID string) {
	if s.ctx == nil {
		return
	}
	s.cancelPendingRetryLocked()
	s.pendingToken++
	token := s.pendingToken
	ctx, cancel := context.WithTimeout(s.ctx, pendingRetryTimeout)
	s.pendingCancel = cancel
	s.pendingEntry = entryID
	s.pendingWG.Add(1)
	go s.runPendingRetry(ctx, token, entryID)
}

func (s *Session) cancelPendingRetryLocked() {
	if s.pendingCancel != nil {
		s.pendingCancel()
	}
	s.pendingCancel = nil
	s.pendingEntry = ""
	s.pendingToken++
}

func (s *Session) runPendingRetry(ctx context.Context, token uint64, entryID string) {
	defer s.pendingWG.Done()
	ticker := time.NewTicker(pendingRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.finishPendingRetry(token, entryID, ctx.Err())
			return
		case <-ticker.C:
			if s.tryPendingPlayback(ctx, token, entryID) {
				return
			}
		}
	}
}

func (s *Session) finishPendingRetry(token uint64, entryID string, err error) {
	s.mu.Lock()
	if token != s.pendingToken || entryID != s.pendingEntry {
		s.mu.Unlock()
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.snapshot.Status = StatusPaused
		s.snapshot.LastError = "waiting for provider transcode timed out"
		s.touchLocked()
	}
	s.pendingCancel = nil
	s.pendingEntry = ""
	s.pendingToken++
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()
	s.publishSnapshot(state)
}

func (s *Session) tryPendingPlayback(ctx context.Context, token uint64, entryID string) bool {
	s.mu.Lock()
	if token != s.pendingToken || entryID != s.pendingEntry {
		s.mu.Unlock()
		return true
	}
	entry := cloneEntryPtr(s.snapshot.CurrentEntry)
	bridge := s.bridge
	s.mu.Unlock()

	if entry == nil || entry.EntryID != entryID || bridge == nil {
		return true
	}

	status, err := bridge.GetPlaybackPreparation(ctx, entry.Item.RecordingID, s.preferredProfile)
	if err != nil {
		return false
	}
	s.mu.Lock()
	if token == s.pendingToken && entryID == s.pendingEntry {
		s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: entryID, Status: status}
		s.touchLocked()
	}
	s.mu.Unlock()
	if status.Phase == apitypes.PlaybackPreparationPreparingFetch || status.Phase == apitypes.PlaybackPreparationPreparingTranscode {
		return false
	}
	if status.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(status.PlayableURI) == "" {
		_, _ = s.failPlayback(fmt.Errorf("recording %s is unavailable (%s)", entry.Item.RecordingID, status.Reason))
		s.mu.Lock()
		s.pendingCancel = nil
		s.pendingEntry = ""
		s.pendingToken++
		s.mu.Unlock()
		return true
	}

	_, playErr := s.playCurrent(ctx)
	return playErr == nil
}

func (s *Session) buildContextLocked(input PlaybackContextInput) (*PlaybackContext, int, error) {
	items := NormalizeItems(input.Items)
	if len(items) == 0 {
		return &PlaybackContext{
			Kind:  input.Kind,
			ID:    strings.TrimSpace(input.ID),
			Title: strings.TrimSpace(input.Title),
		}, 0, nil
	}

	startIndex := normalizeCurrentIndex(len(items), input.StartIndex)
	contextData := &PlaybackContext{
		Kind:  input.Kind,
		ID:    strings.TrimSpace(input.ID),
		Title: strings.TrimSpace(input.Title),
	}
	for index, item := range items {
		contextData.Entries = append(contextData.Entries, SessionEntry{
			EntryID:      s.nextEntryIDLocked("ctx"),
			Origin:       EntryOriginContext,
			ContextIndex: index,
			Item:         item,
		})
	}
	return contextData, startIndex, nil
}

func (s *Session) ensureCurrentLocked() bool {
	if s.snapshot.CurrentEntry != nil {
		return true
	}
	if len(s.snapshot.QueuedEntries) > 0 {
		entry := s.snapshot.QueuedEntries[0]
		s.snapshot.QueuedEntries = s.snapshot.QueuedEntries[1:]
		s.setCurrentLocked(entry, EntryOriginQueued)
		s.snapshot.DurationMS = currentDuration(&entry.Item)
		s.snapshot.Status = StatusPaused
		s.touchLocked()
		return true
	}
	index := s.firstContextIndexLocked()
	if index < 0 || s.snapshot.Context == nil || index >= len(s.snapshot.Context.Entries) {
		return false
	}
	entry := s.snapshot.Context.Entries[index]
	s.setCurrentLocked(entry, EntryOriginContext)
	s.snapshot.DurationMS = currentDuration(&entry.Item)
	s.snapshot.Status = StatusPaused
	s.touchLocked()
	return true
}

func (s *Session) resolveEntrySelectionLocked(entryID string) (SessionEntry, EntryOrigin, int, error) {
	if current := s.snapshot.CurrentEntry; current != nil && current.EntryID == entryID {
		return *current, current.Origin, -1, nil
	}
	if index := indexOfEntryID(s.snapshot.QueuedEntries, entryID); index >= 0 {
		return s.snapshot.QueuedEntries[index], EntryOriginQueued, index, nil
	}
	if s.snapshot.Context != nil {
		for _, entry := range s.snapshot.Context.Entries {
			if entry.EntryID == entryID {
				return entry, EntryOriginContext, -1, nil
			}
		}
	}
	for _, item := range s.snapshot.History {
		if item.Entry.EntryID == entryID {
			return item.Entry, item.Entry.Origin, -1, nil
		}
	}
	return SessionEntry{}, "", -1, fmt.Errorf("entry %s not found", entryID)
}

func (s *Session) switchCurrentLocked(entry SessionEntry, origin EntryOrigin, queueIndex int, previous *SessionEntry, keepPosition bool, restorePosition int64) {
	if previous != nil && previous.EntryID != "" && previous.EntryID != entry.EntryID {
		s.pushHistoryLocked(*previous)
	}
	if origin == EntryOriginQueued && queueIndex >= 0 && queueIndex < len(s.snapshot.QueuedEntries) {
		s.snapshot.QueuedEntries = append(s.snapshot.QueuedEntries[:queueIndex], s.snapshot.QueuedEntries[queueIndex+1:]...)
	}
	s.setCurrentLocked(entry, origin)
	s.snapshot.CurrentPreparation = nil
	if !keepPosition {
		s.snapshot.PositionMS = 0
	}
	if restorePosition > 0 {
		s.snapshot.PositionMS = restorePosition
	}
	s.snapshot.DurationMS = currentDuration(&entry.Item)
}

func (s *Session) setCurrentLocked(entry SessionEntry, origin EntryOrigin) {
	entry.Origin = origin
	s.snapshot.CurrentEntryID = entry.EntryID
	s.snapshot.CurrentOrigin = origin
	s.snapshot.CurrentEntry = &entry
	item := entry.Item
	s.snapshot.CurrentItem = &item
	if origin == EntryOriginContext {
		s.snapshot.CurrentContextIndex = entry.ContextIndex
	}
}

func (s *Session) clearCurrentLocked() {
	s.snapshot.CurrentEntryID = ""
	s.snapshot.CurrentEntry = nil
	s.snapshot.CurrentItem = nil
	s.snapshot.CurrentOrigin = ""
	s.snapshot.CurrentContextIndex = -1
}

func (s *Session) pushHistoryLocked(entry SessionEntry) {
	s.snapshot.History = append(s.snapshot.History, HistoryEntry{
		Entry:    entry,
		PlayedAt: formatTimestamp(time.Now().UTC()),
	})
	if len(s.snapshot.History) > maxHistoryEntries {
		s.snapshot.History = s.snapshot.History[len(s.snapshot.History)-maxHistoryEntries:]
	}
}

func (s *Session) peekNextLocked(ignoreRepeatOne bool) (SessionEntry, EntryOrigin, int, bool) {
	if s.snapshot.CurrentEntry == nil {
		if len(s.snapshot.QueuedEntries) > 0 {
			return s.snapshot.QueuedEntries[0], EntryOriginQueued, 0, true
		}
		index := s.firstContextIndexLocked()
		if index < 0 || s.snapshot.Context == nil || index >= len(s.snapshot.Context.Entries) {
			return SessionEntry{}, "", -1, false
		}
		return s.snapshot.Context.Entries[index], EntryOriginContext, -1, true
	}
	if len(s.snapshot.QueuedEntries) > 0 {
		return s.snapshot.QueuedEntries[0], EntryOriginQueued, 0, true
	}
	index := s.nextContextIndexLocked(ignoreRepeatOne)
	if index < 0 || s.snapshot.Context == nil || index >= len(s.snapshot.Context.Entries) {
		return SessionEntry{}, "", -1, false
	}
	return s.snapshot.Context.Entries[index], EntryOriginContext, -1, true
}

func (s *Session) firstContextIndexLocked() int {
	if s.snapshot.Context == nil || len(s.snapshot.Context.Entries) == 0 {
		return -1
	}
	if !s.snapshot.Shuffle {
		return 0
	}
	s.ensureShuffleCycleLocked()
	if len(s.snapshot.ShuffleCycle) == 0 {
		return -1
	}
	return s.snapshot.ShuffleCycle[0]
}

func (s *Session) nextContextIndexLocked(ignoreRepeatOne bool) int {
	if s.snapshot.Context == nil || len(s.snapshot.Context.Entries) == 0 {
		return -1
	}
	currentIndex := s.snapshot.CurrentContextIndex
	if currentIndex < 0 || currentIndex >= len(s.snapshot.Context.Entries) {
		return s.firstContextIndexLocked()
	}

	if !s.snapshot.Shuffle {
		if s.snapshot.CurrentOrigin == EntryOriginContext && !ignoreRepeatOne && s.snapshot.RepeatMode == RepeatOne {
			return currentIndex
		}
		next := currentIndex + 1
		if next < len(s.snapshot.Context.Entries) {
			return next
		}
		if s.snapshot.RepeatMode == RepeatAll {
			return 0
		}
		return -1
	}

	s.ensureShuffleCycleLocked()
	if len(s.snapshot.ShuffleCycle) == 0 {
		return -1
	}
	if s.snapshot.CurrentOrigin == EntryOriginContext && !ignoreRepeatOne && s.snapshot.RepeatMode == RepeatOne {
		return currentIndex
	}
	position := indexOfInt(s.snapshot.ShuffleCycle, currentIndex)
	if position < 0 {
		return s.snapshot.ShuffleCycle[0]
	}
	nextPosition := position + 1
	if nextPosition < len(s.snapshot.ShuffleCycle) {
		return s.snapshot.ShuffleCycle[nextPosition]
	}
	if s.snapshot.RepeatMode == RepeatAll {
		return s.snapshot.ShuffleCycle[0]
	}
	return -1
}

func (s *Session) ensureShuffleCycleLocked() {
	if !s.snapshot.Shuffle || s.snapshot.Context == nil {
		return
	}
	if len(s.snapshot.ShuffleCycle) == len(s.snapshot.Context.Entries) && len(s.snapshot.ShuffleCycle) > 0 {
		return
	}
	s.rebuildShuffleCycleLocked()
}

func (s *Session) rebuildShuffleCycleLocked() {
	if !s.snapshot.Shuffle || s.snapshot.Context == nil {
		s.snapshot.ShuffleCycle = nil
		return
	}
	s.snapshot.ShuffleCycle = buildSmartShuffleCycle(s.snapshot.Context.Entries, s.rng)
}

func (s *Session) nextEntryIDLocked(prefix string) string {
	if s.snapshot.NextEntrySeq <= 0 {
		s.snapshot.NextEntrySeq = 1
	}
	value := s.snapshot.NextEntrySeq
	s.snapshot.NextEntrySeq++
	return fmt.Sprintf("%s-%d", strings.TrimSpace(prefix), value)
}

func (s *Session) selectionEntriesLocked() []SessionEntry {
	size := len(s.snapshot.QueuedEntries)
	if s.snapshot.CurrentEntry != nil {
		size++
	}
	if s.snapshot.Context != nil {
		size += len(s.snapshot.Context.Entries)
	}

	out := make([]SessionEntry, 0, size)
	if s.snapshot.CurrentEntry != nil {
		out = append(out, *s.snapshot.CurrentEntry)
	}
	out = append(out, s.snapshot.QueuedEntries...)
	if s.snapshot.Context != nil {
		out = append(out, s.snapshot.Context.Entries...)
	}
	return out
}

func (s *Session) stopBackend() error {
	s.mu.Lock()
	backend := s.backend
	s.stopTickerLocked()
	s.mu.Unlock()
	if backend == nil {
		return nil
	}
	return backend.Stop(context.Background())
}

func (s *Session) failPlayback(err error) (SessionSnapshot, error) {
	if err == nil {
		return s.Snapshot(), nil
	}
	s.mu.Lock()
	s.snapshot.Status = StatusPaused
	s.snapshot.LastError = err.Error()
	if s.snapshot.CurrentEntry != nil {
		failed := apitypes.PlaybackPreparationStatus{
			RecordingID:      s.snapshot.CurrentEntry.Item.RecordingID,
			PreferredProfile: s.preferredProfile,
			Purpose:          apitypes.PlaybackPreparationPlayNow,
			Phase:            apitypes.PlaybackPreparationFailed,
		}
		s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: s.snapshot.CurrentEntry.EntryID, Status: failed}
	}
	s.stopTickerLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.logErrorf("playback: %v", err)
	s.publishSnapshot(state)
	return state, err
}

func (s *Session) persistFinalSnapshot() {
	s.mu.Lock()
	backend := s.backend
	hasCurrent := s.snapshot.CurrentEntry != nil
	s.mu.Unlock()

	if backend != nil && hasCurrent {
		s.refreshPosition()
	}

	snapshot := s.Snapshot()
	if snapshot.Status == StatusPlaying || snapshot.Status == StatusPending {
		snapshot.Status = StatusPaused
		snapshot.UpdatedAt = formatTimestamp(time.Now().UTC())
	}
	s.persistSnapshot(snapshot)
}

func (s *Session) persistSnapshot(snapshot SessionSnapshot) {
	if s.store != nil {
		if err := s.store.Save(context.Background(), snapshot); err != nil {
			s.logErrorf("playback: save session failed: %v", err)
		}
	}
}

func (s *Session) publishSnapshot(snapshot SessionSnapshot) {
	s.persistSnapshot(snapshot)
	s.mu.Lock()
	emitter := s.emit
	s.mu.Unlock()
	if emitter != nil {
		emitter(snapshot)
	}
}

func normalizeSnapshot(snapshot SessionSnapshot) SessionSnapshot {
	snapshot.Volume = clampVolume(snapshot.Volume)
	snapshot.UpdatedAt = normalizeTimestamp(snapshot.UpdatedAt)
	switch snapshot.RepeatMode {
	case RepeatOff, RepeatAll, RepeatOne:
	default:
		snapshot.RepeatMode = RepeatOff
	}
	switch snapshot.Status {
	case StatusIdle, StatusPaused, StatusPlaying, StatusPending:
	default:
		snapshot.Status = StatusIdle
	}
	if snapshot.NextEntrySeq <= 0 {
		snapshot.NextEntrySeq = 1
	}

	if snapshot.Context != nil {
		contextCopy := *snapshot.Context
		contextCopy.Entries = cloneEntries(contextCopy.Entries)
		for index := range contextCopy.Entries {
			contextCopy.Entries[index].Origin = EntryOriginContext
			contextCopy.Entries[index].ContextIndex = index
		}
		if len(contextCopy.Entries) == 0 {
			snapshot.Context = nil
		} else {
			snapshot.Context = &contextCopy
		}
	}

	snapshot.QueuedEntries = cloneEntries(snapshot.QueuedEntries)
	for index := range snapshot.QueuedEntries {
		snapshot.QueuedEntries[index].Origin = EntryOriginQueued
		snapshot.QueuedEntries[index].ContextIndex = -1
	}

	snapshot.History = cloneHistory(snapshot.History)
	snapshot.CurrentPreparation = cloneEntryPreparation(snapshot.CurrentPreparation)
	snapshot.NextPreparation = cloneEntryPreparation(snapshot.NextPreparation)

	if snapshot.CurrentEntry != nil {
		entry := *snapshot.CurrentEntry
		if snapshot.CurrentEntryID == "" {
			snapshot.CurrentEntryID = entry.EntryID
		}
		if snapshot.CurrentOrigin == "" {
			snapshot.CurrentOrigin = entry.Origin
		} else {
			entry.Origin = snapshot.CurrentOrigin
		}
		snapshot.CurrentEntry = &entry
		item := entry.Item
		snapshot.CurrentItem = &item
		if snapshot.DurationMS == nil {
			snapshot.DurationMS = currentDuration(&item)
		}
		if snapshot.CurrentOrigin == EntryOriginContext && entry.ContextIndex >= 0 {
			snapshot.CurrentContextIndex = entry.ContextIndex
		}
	} else {
		snapshot.CurrentEntryID = ""
		snapshot.CurrentItem = nil
		snapshot.CurrentPreparation = nil
		if snapshot.Status == StatusPlaying || snapshot.Status == StatusPending {
			snapshot.Status = StatusPaused
		}
	}

	if snapshot.Context == nil {
		snapshot.ShuffleCycle = nil
		if snapshot.CurrentOrigin == EntryOriginContext {
			snapshot.CurrentContextIndex = -1
		}
	}
	if snapshot.CurrentContextIndex >= 0 && snapshot.Context != nil && snapshot.CurrentContextIndex >= len(snapshot.Context.Entries) {
		snapshot.CurrentContextIndex = -1
	}
	if snapshot.DurationMS != nil && snapshot.PositionMS > *snapshot.DurationMS {
		snapshot.PositionMS = *snapshot.DurationMS
	}
	if snapshot.Context == nil && len(snapshot.QueuedEntries) == 0 && snapshot.CurrentEntry == nil {
		snapshot.Status = StatusIdle
		snapshot.PositionMS = 0
		snapshot.DurationMS = nil
		snapshot.CurrentSourceKind = ""
		snapshot.NextPreparation = nil
	}

	snapshot.UpcomingEntries = buildUpcomingEntries(snapshot)
	snapshot.QueueLength = len(snapshot.UpcomingEntries)
	if snapshot.CurrentEntry != nil {
		snapshot.QueueLength++
	}
	return snapshot
}

func snapshotCopyLocked(snapshot *SessionSnapshot) SessionSnapshot {
	copyState := normalizeSnapshot(*snapshot)
	if copyState.Context != nil {
		contextCopy := *copyState.Context
		contextCopy.Entries = cloneEntries(contextCopy.Entries)
		copyState.Context = &contextCopy
	}
	copyState.QueuedEntries = cloneEntries(copyState.QueuedEntries)
	copyState.History = cloneHistory(copyState.History)
	copyState.UpcomingEntries = cloneEntries(copyState.UpcomingEntries)
	copyState.CurrentPreparation = cloneEntryPreparation(copyState.CurrentPreparation)
	copyState.NextPreparation = cloneEntryPreparation(copyState.NextPreparation)
	copyState.ShuffleCycle = append([]int(nil), copyState.ShuffleCycle...)
	if copyState.CurrentEntry != nil {
		entry := *copyState.CurrentEntry
		copyState.CurrentEntry = &entry
	}
	if copyState.CurrentItem != nil {
		item := *copyState.CurrentItem
		copyState.CurrentItem = &item
	}
	copyState.DurationMS = cloneInt64Ptr(copyState.DurationMS)
	return copyState
}

func buildUpcomingEntries(snapshot SessionSnapshot) []SessionEntry {
	contextEntries := 0
	if snapshot.Context != nil {
		contextEntries = len(snapshot.Context.Entries)
	}
	out := make([]SessionEntry, 0, len(snapshot.QueuedEntries)+contextEntries)
	out = append(out, cloneEntries(snapshot.QueuedEntries)...)
	if snapshot.Context == nil || len(snapshot.Context.Entries) == 0 {
		return out
	}

	if snapshot.Shuffle {
		cycle := append([]int(nil), snapshot.ShuffleCycle...)
		if len(cycle) == 0 {
			cycle = buildSmartShuffleCycle(snapshot.Context.Entries, rand.New(rand.NewSource(1)))
		}
		startAdded := false
		if snapshot.CurrentContextIndex >= 0 {
			position := indexOfInt(cycle, snapshot.CurrentContextIndex)
			if position >= 0 {
				for index := position + 1; index < len(cycle); index++ {
					out = append(out, snapshot.Context.Entries[cycle[index]])
				}
				if snapshot.RepeatMode == RepeatAll {
					for index := 0; index < position; index++ {
						out = append(out, snapshot.Context.Entries[cycle[index]])
					}
				}
				startAdded = true
			}
		}
		if !startAdded {
			for _, index := range cycle {
				out = append(out, snapshot.Context.Entries[index])
			}
		}
		return out
	}

	start := 0
	if snapshot.CurrentContextIndex >= 0 {
		start = snapshot.CurrentContextIndex + 1
	}
	for index := start; index < len(snapshot.Context.Entries); index++ {
		out = append(out, snapshot.Context.Entries[index])
	}
	if snapshot.RepeatMode == RepeatAll && snapshot.CurrentContextIndex >= 0 {
		for index := 0; index < snapshot.CurrentContextIndex; index++ {
			out = append(out, snapshot.Context.Entries[index])
		}
	}
	return out
}

func currentDuration(item *SessionItem) *int64 {
	if item == nil || item.DurationMS <= 0 {
		return nil
	}
	duration := item.DurationMS
	return &duration
}

func normalizeCurrentIndex(total int, index int) int {
	if total == 0 {
		return -1
	}
	if index < 0 || index >= total {
		return 0
	}
	return index
}

func clampVolume(volume int) int {
	if volume < 0 {
		return 0
	}
	if volume > 100 {
		return 100
	}
	return volume
}

func isPlayableState(state apitypes.RecordingAvailabilityState) bool {
	switch state {
	case apitypes.AvailabilityPlayableLocalFile, apitypes.AvailabilityPlayableCachedOpt, apitypes.AvailabilityPlayableRemoteOpt:
		return true
	default:
		return false
	}
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneEntries(entries []SessionEntry) []SessionEntry {
	out := make([]SessionEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	return out
}

func cloneHistory(history []HistoryEntry) []HistoryEntry {
	out := make([]HistoryEntry, 0, len(history))
	for _, item := range history {
		entry := item
		entry.PlayedAt = normalizeTimestamp(entry.PlayedAt)
		out = append(out, entry)
	}
	return out
}

func cloneEntryPtr(entry *SessionEntry) *SessionEntry {
	if entry == nil {
		return nil
	}
	copyEntry := *entry
	return &copyEntry
}

func cloneEntryPreparation(value *EntryPreparation) *EntryPreparation {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func currentItemFromEntry(entry *SessionEntry) *SessionItem {
	if entry == nil {
		return nil
	}
	item := entry.Item
	return &item
}

func indexOfInt(values []int, needle int) int {
	for index, value := range values {
		if value == needle {
			return index
		}
	}
	return -1
}

func indexOfEntryID(entries []SessionEntry, entryID string) int {
	entryID = strings.TrimSpace(entryID)
	for index, entry := range entries {
		if entry.EntryID == entryID {
			return index
		}
	}
	return -1
}

func buildSmartShuffleCycle(entries []SessionEntry, rng *rand.Rand) []int {
	if len(entries) == 0 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	}

	remaining := make([]int, len(entries))
	for index := range entries {
		remaining[index] = index
	}
	rng.Shuffle(len(remaining), func(left int, right int) {
		remaining[left], remaining[right] = remaining[right], remaining[left]
	})

	cycle := make([]int, 0, len(entries))
	for len(remaining) > 0 {
		bestScore := -1 << 30
		bestIndexes := make([]int, 0, len(remaining))
		for remainingIndex, entryIndex := range remaining {
			score := smartShuffleScore(entries, cycle, entryIndex)
			if score > bestScore {
				bestScore = score
				bestIndexes = bestIndexes[:0]
				bestIndexes = append(bestIndexes, remainingIndex)
				continue
			}
			if score == bestScore {
				bestIndexes = append(bestIndexes, remainingIndex)
			}
		}

		chosenRemainingIndex := bestIndexes[rng.Intn(len(bestIndexes))]
		chosenEntryIndex := remaining[chosenRemainingIndex]
		cycle = append(cycle, chosenEntryIndex)
		remaining = append(remaining[:chosenRemainingIndex], remaining[chosenRemainingIndex+1:]...)
	}
	return cycle
}

func smartShuffleScore(entries []SessionEntry, cycle []int, candidate int) int {
	score := 0
	candidateItem := entries[candidate].Item

	for distance := 1; distance <= 3 && distance <= len(cycle); distance++ {
		previousItem := entries[cycle[len(cycle)-distance]].Item
		weight := 4 - distance
		if candidateItem.RecordingID == previousItem.RecordingID {
			score -= 1000
		}
		if sameLabel(candidateItem.Subtitle, previousItem.Subtitle) {
			score -= 240 * weight
		}
		if candidateItem.AlbumID != "" && candidateItem.AlbumID == previousItem.AlbumID {
			score -= 160 * weight
		}
		if candidateItem.SourceKind == SourceKindAlbum && candidateItem.SourceID != "" && candidateItem.SourceID == previousItem.SourceID {
			score -= 120 * weight
		}
	}

	score += len(cycle) * 10
	return score
}

func sameLabel(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && strings.EqualFold(left, right)
}

func (s *Session) touchLocked() {
	s.snapshot = normalizeSnapshot(s.snapshot)
	s.snapshot.UpdatedAt = formatTimestamp(time.Now().UTC())
}

func (s *Session) logErrorf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Errorf(format, args...)
	}
}
