package playback

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
)

const (
	positionPollInterval = 500 * time.Millisecond
	pendingRetryInterval = time.Second
	pendingRetryTimeout  = 2 * time.Minute
	preloadListenedAfter = 45 * time.Second
	preloadRemainingLead = 45 * time.Second
	nextPreparationPoll  = 5 * time.Second
)

type Logger interface {
	Printf(format string, args ...any)
	Errorf(format string, args ...any)
}

type Session struct {
	mu sync.Mutex
	selectMu sync.Mutex

	core             PlaybackCore
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

	loadedEntryID       string
	loadedURI           string
	preloadedID         string
	preloadedURI        string
	backendPreloadArmed bool

	nextPreparationRetryEntryID string
	nextPreparationRetryAt      time.Time

	rng *rand.Rand

	pendingCancel context.CancelFunc
	pendingWG     sync.WaitGroup
	pendingEntry  string
	pendingToken  uint64
}

type playbackCandidate struct {
	Entry      SessionEntry
	Origin     EntryOrigin
	QueueIndex int
}

type skippedPlaybackEntry struct {
	Entry   SessionEntry
	Status  apitypes.PlaybackPreparationStatus
	Message string
}

type entryUnavailableError struct {
	entry  SessionEntry
	status apitypes.PlaybackPreparationStatus
}

func (e *entryUnavailableError) Error() string {
	if e == nil {
		return "recording is unavailable"
	}
	return fmt.Sprintf("recording %s is unavailable (%s)", e.entry.Item.RecordingID, e.status.Reason)
}

func NewSession(core PlaybackCore, backend Backend, store SessionStore, preferredProfile string, logger Logger) *Session {
	if backend == nil {
		backend = NewBackend()
	}
	return &Session{
		core:             core,
		backend:          backend,
		store:            store,
		preferredProfile: strings.TrimSpace(preferredProfile),
		logger:           logger,
		snapshot: SessionSnapshot{
			RepeatMode:   RepeatOff,
			Volume:       DefaultVolume,
			Status:       StatusIdle,
			NextEntrySeq: 1,
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
		s.backendPreloadArmed = false
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

	backend, needsClearPreload := s.invalidateFutureOrderLocked("set context", true)
	s.snapshot.ContextQueue = contextData
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.setCurrentContextIndexLocked(startIndex)
	s.setResumeContextIndexLocked(startIndex)

	if s.snapshot.CurrentEntry == nil {
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = nil
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
		s.loadedEntryID = ""
		s.loadedURI = ""
		if len(contextData.Entries) == 0 {
			s.clearCurrentLocked()
			s.snapshot.Status = StatusIdle
			s.setResumeContextIndexLocked(-1)
		} else {
			s.clearCurrentLocked()
			s.snapshot.Status = StatusPaused
		}
	} else if len(contextData.Entries) == 0 {
		s.setResumeContextIndexLocked(-1)
	}

	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	shouldStopBackend := s.snapshot.CurrentEntry == nil
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	if shouldStopBackend {
		_ = s.stopBackend()
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) ReplaceContextAndPlay(ctx context.Context, input PlaybackContextInput) (SessionSnapshot, error) {
	s.mu.Lock()
	contextData, startIndex, err := s.buildContextLocked(input)
	if err != nil {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, err
	}

	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	backend, needsClearPreload := s.invalidateFutureOrderLocked("replace context and play", true)

	s.snapshot.ContextQueue = contextData
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.setCurrentContextIndexLocked(startIndex)
	s.setResumeContextIndexLocked(startIndex)

	if current == nil {
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = nil
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.clearCurrentLocked()
		if len(contextData.Entries) == 0 {
			s.snapshot.Status = StatusIdle
			s.setResumeContextIndexLocked(-1)
		} else {
			s.snapshot.Status = StatusPaused
		}
	}

	var target SessionEntry
	hasTarget := len(contextData.Entries) > 0
	if hasTarget {
		target = contextData.Entries[startIndex]
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	if !hasTarget {
		s.publishSnapshot(state)
		return state, errors.New("queue is empty")
	}
	return s.playEntry(ctx, target, EntryOriginContext, -1, current, false, true)
}

func (s *Session) QueueItems(items []SessionItem, mode QueueInsertMode) (SessionSnapshot, error) {
	items = NormalizeItems(items)
	if len(items) == 0 {
		return s.Snapshot(), nil
	}

	s.mu.Lock()
	backend, needsClearPreload := s.invalidateFutureOrderLocked("queue items", true)
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
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
		s.snapshot.UserQueue = append(entries, s.snapshot.UserQueue...)
	} else {
		s.snapshot.UserQueue = append(s.snapshot.UserQueue, entries...)
	}

	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) RemoveQueuedEntry(entryID string) (SessionSnapshot, error) {
	s.mu.Lock()
	index := indexOfEntryID(s.snapshot.UserQueue, entryID)
	if index < 0 {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queued entry %s not found", strings.TrimSpace(entryID))
	}
	backend, needsClearPreload := s.invalidateFutureOrderLocked("remove queued entry", true)
	s.snapshot.UserQueue = append(s.snapshot.UserQueue[:index], s.snapshot.UserQueue[index+1:]...)
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) MoveQueuedEntry(entryID string, toIndex int) (SessionSnapshot, error) {
	s.mu.Lock()
	fromIndex := indexOfEntryID(s.snapshot.UserQueue, entryID)
	if fromIndex < 0 {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queued entry %s not found", strings.TrimSpace(entryID))
	}
	if toIndex < 0 || toIndex >= len(s.snapshot.UserQueue) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", toIndex)
	}
	if fromIndex == toIndex {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}

	backend, needsClearPreload := s.invalidateFutureOrderLocked("move queued entry", true)
	entry := s.snapshot.UserQueue[fromIndex]
	queue := append([]SessionEntry(nil), s.snapshot.UserQueue...)
	queue = append(queue[:fromIndex], queue[fromIndex+1:]...)
	queue = append(queue[:toIndex], append([]SessionEntry{entry}, queue[toIndex:]...)...)
	s.snapshot.UserQueue = queue
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) SelectEntry(ctx context.Context, entryID string) (SessionSnapshot, error) {
	s.selectMu.Lock()
	defer s.selectMu.Unlock()

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
	isCurrentSelection :=
		current != nil && current.EntryID == target.EntryID
	isLoadingSelection :=
		s.snapshot.LoadingEntry != nil &&
			s.snapshot.LoadingEntry.EntryID == target.EntryID
	if isCurrentSelection || isLoadingSelection {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}
	s.mu.Unlock()
	return s.playEntry(ctx, target, origin, queueIndex, current, false, true)
}

func (s *Session) ClearQueue() (SessionSnapshot, error) {
	s.mu.Lock()
	hasCurrent := s.snapshot.CurrentEntry != nil
	backend, needsClearPreload := s.invalidateFutureOrderLocked("clear queue", true)
	s.snapshot.ContextQueue = nil
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.setResumeContextIndexLocked(-1)
	shouldStopBackend := !hasCurrent
	if !hasCurrent {
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = nil
		s.snapshot.Status = StatusIdle
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
		s.clearCurrentLocked()
		s.loadedEntryID = ""
		s.loadedURI = ""
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	if shouldStopBackend {
		_ = s.stopBackend()
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) Play(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	if s.snapshot.LoadingEntry != nil && s.snapshot.CurrentEntry == nil {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	target, origin, queueIndex, ok := s.playTargetLocked()
	if !ok {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, errors.New("queue is empty")
	}
	if current == nil {
		s.mu.Unlock()
		state, err := s.playEntry(ctx, target, origin, queueIndex, nil, false, true)
		if err == nil {
			return state, nil
		}
		var unavailableErr *entryUnavailableError
		if !errors.As(err, &unavailableErr) {
			return state, err
		}
		return s.playNextAvailable(
			ctx,
			nil,
			false,
			true,
			map[string]struct{}{target.EntryID: struct{}{}},
			[]skippedPlaybackEntry{{
				Entry:   target,
				Status:  unavailableErr.status,
				Message: unavailableErr.Error(),
			}},
			true,
		)
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
	return s.playEntry(ctx, target, origin, queueIndex, current, current != nil, true)
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
	if state.Status == StatusPending && state.LoadingEntry != nil && state.CurrentEntry == nil {
		return state, nil
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
	s.mu.Unlock()
	return s.playNextAvailable(ctx, current, false, true, nil, nil, true)
}

func (s *Session) Previous(ctx context.Context) (SessionSnapshot, error) {
	s.mu.Lock()
	if s.snapshot.PositionMS > 3000 {
		s.mu.Unlock()
		return s.SeekTo(ctx, 0)
	}
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	if current != nil && current.Origin == EntryOriginQueued {
		if entry, ok := s.returnContextEntryLocked(); ok {
			s.mu.Unlock()
			return s.playEntry(ctx, entry, EntryOriginContext, -1, nil, false, true)
		}
	}
	if entry, ok := s.previousContextEntryLocked(); ok {
		s.mu.Unlock()
		return s.playEntry(ctx, entry, EntryOriginContext, -1, nil, false, true)
	}
	s.mu.Unlock()

	if current == nil {
		return s.Snapshot(), nil
	}
	return s.playEntry(ctx, *current, current.Origin, -1, nil, false, true)
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
	backend, needsClearPreload := s.invalidateFutureOrderLocked("set repeat mode", true)
	s.snapshot.RepeatMode = repeatMode
	s.recalculateResumeContextIndexLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	s.publishSnapshot(state)
	return state, nil
}

func (s *Session) SetShuffle(enabled bool) (SessionSnapshot, error) {
	s.mu.Lock()
	backend, needsClearPreload := s.invalidateFutureOrderLocked("set shuffle", true)
	s.snapshot.Shuffle = enabled
	if enabled {
		s.rebuildShuffleCycleLocked()
	} else {
		setShuffleBagLocked(&s.snapshot, nil)
	}
	s.recalculateResumeContextIndexLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
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
	if index < 0 || index >= len(s.snapshot.UserQueue) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", index)
	}
	entryID := s.snapshot.UserQueue[index].EntryID
	s.mu.Unlock()
	return s.RemoveQueuedEntry(entryID)
}

func (s *Session) MoveQueueItem(fromIndex int, toIndex int) (SessionSnapshot, error) {
	s.mu.Lock()
	if fromIndex < 0 || fromIndex >= len(s.snapshot.UserQueue) {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, fmt.Errorf("queue index %d out of range", fromIndex)
	}
	entryID := s.snapshot.UserQueue[fromIndex].EntryID
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
	target, origin, queueIndex, ok := s.playTargetLocked()
	if !ok {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, errors.New("queue is empty")
	}
	s.mu.Unlock()

	return s.playEntry(ctx, target, origin, queueIndex, nil, false, true)
}

func (s *Session) playNextAvailable(
	ctx context.Context,
	previous *SessionEntry,
	keepPosition bool,
	preserveCurrentOnPending bool,
	blocked map[string]struct{},
	initialSkipped []skippedPlaybackEntry,
	stopIfExhausted bool,
) (SessionSnapshot, error) {
	skipped := append([]skippedPlaybackEntry(nil), initialSkipped...)
	blockedEntries := make(map[string]struct{}, len(blocked))
	for entryID := range blocked {
		trimmed := strings.TrimSpace(entryID)
		if trimmed == "" {
			continue
		}
		blockedEntries[trimmed] = struct{}{}
	}

	for {
		candidate, unavailable, err := s.resolveNextPlayableCandidate(ctx, blockedEntries)
		if err != nil {
			return s.Snapshot(), err
		}
		skipped = append(skipped, unavailable...)
		if candidate == nil {
			if len(skipped) > 0 {
				if stopIfExhausted {
					return s.stopAfterSkipped(ctx, previous, skipped), nil
				}
				return s.settleAfterSkipped(skipped), nil
			}
			state := s.Snapshot()
			if previous == nil && state.CurrentEntry == nil && state.QueueLength == 0 {
				return state, errors.New("queue is empty")
			}
			return state, nil
		}

		state, err := s.playEntry(
			ctx,
			candidate.Entry,
			candidate.Origin,
			candidate.QueueIndex,
			previous,
			keepPosition,
			preserveCurrentOnPending,
		)
		if err == nil {
			state = s.consumeSkippedUserEntries(skipped, candidate, state)
			if len(skipped) > 0 {
				state = s.applySkipEvent(skipped, false)
			}
			return state, nil
		}

		var unavailableErr *entryUnavailableError
		if !errors.As(err, &unavailableErr) {
			return state, err
		}
		blockedEntries[candidate.Entry.EntryID] = struct{}{}
		skipped = append(skipped, skippedPlaybackEntry{
			Entry:   candidate.Entry,
			Status:  unavailableErr.status,
			Message: unavailableErr.Error(),
		})
	}
}

func (s *Session) resolveNextPlayableCandidate(
	ctx context.Context,
	blocked map[string]struct{},
) (*playbackCandidate, []skippedPlaybackEntry, error) {
	s.mu.Lock()
	candidates := s.upcomingCandidatesLocked(blocked)
	core := s.core
	s.mu.Unlock()

	if len(candidates) == 0 {
		return nil, nil, nil
	}
	if core == nil {
		candidate := candidates[0]
		return &candidate, nil, nil
	}

	targets := make([]PlaybackTargetRef, 0, len(candidates))
	seenTargets := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := playbackTargetKey(candidate.Entry.Item.Target)
		if key == "" {
			continue
		}
		if _, ok := seenTargets[key]; ok {
			continue
		}
		seenTargets[key] = struct{}{}
		targets = append(targets, candidate.Entry.Item.Target)
	}
	if len(targets) == 0 {
		candidate := candidates[0]
		return &candidate, nil, nil
	}

	items, err := core.ListPlaybackTargetAvailability(ctx, TargetAvailabilityRequest{
		Targets:          targets,
		PreferredProfile: s.preferredProfile,
	})
	if err != nil {
		candidate := candidates[0]
		return &candidate, nil, nil
	}

	availabilityByTargetKey := make(map[string]apitypes.RecordingPlaybackAvailability, len(items))
	for _, item := range items {
		key := playbackTargetKey(item.Target)
		if key == "" {
			continue
		}
		availabilityByTargetKey[key] = item.Status
	}
	s.mu.Lock()
	for _, candidate := range candidates {
		status, ok := availabilityByTargetKey[playbackTargetKey(candidate.Entry.Item.Target)]
		if !ok {
			continue
		}
		s.setEntryAvailabilityLocked(candidate.Entry.EntryID, status)
	}
	s.mu.Unlock()

	skipped := make([]skippedPlaybackEntry, 0, len(candidates))
	for _, candidate := range candidates {
		availability, ok := availabilityByTargetKey[playbackTargetKey(candidate.Entry.Item.Target)]
		if !ok || !isAvailabilityDefinitivelyUnavailable(availability.State) {
			next := candidate
			return &next, skipped, nil
		}
		status := apitypes.PlaybackPreparationStatus{
			RecordingID:      availability.RecordingID,
			PreferredProfile: availability.PreferredProfile,
			Purpose:          apitypes.PlaybackPreparationPlayNow,
			Phase:            apitypes.PlaybackPreparationUnavailable,
			SourceKind:       availability.SourceKind,
			Reason:           availability.Reason,
		}
		skipped = append(skipped, skippedPlaybackEntry{
			Entry:   candidate.Entry,
			Status:  status,
			Message: availabilityUnavailableMessage(candidate.Entry, status),
		})
	}
	return nil, skipped, nil
}

func (s *Session) upcomingCandidatesLocked(blocked map[string]struct{}) []playbackCandidate {
	upcoming := buildUpcomingEntries(s.snapshot)
	out := make([]playbackCandidate, 0, len(upcoming))
	for _, entry := range upcoming {
		if _, ok := blocked[entry.EntryID]; ok {
			continue
		}
		queueIndex := -1
		if entry.Origin == EntryOriginQueued {
			queueIndex = indexOfEntryID(s.snapshot.UserQueue, entry.EntryID)
		}
		out = append(out, playbackCandidate{
			Entry:      entry,
			Origin:     entry.Origin,
			QueueIndex: queueIndex,
		})
	}
	return out
}

func (s *Session) applySkipEvent(skipped []skippedPlaybackEntry, stopped bool) SessionSnapshot {
	if len(skipped) == 0 {
		return s.Snapshot()
	}
	s.mu.Lock()
	s.recordSkipEventLocked(skipped, stopped)
	if stopped {
		s.snapshot.LastError = s.snapshot.LastSkipEvent.Message
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()
	s.publishSnapshot(state)
	return state
}

func (s *Session) stopAfterSkipped(ctx context.Context, previous *SessionEntry, skipped []skippedPlaybackEntry) SessionSnapshot {
	s.mu.Lock()
	backend := s.backend
	if previous != nil && previous.EntryID != "" && s.snapshot.CurrentEntry == nil {
		s.setCurrentLocked(*previous, previous.Origin)
	}
	if s.snapshot.CurrentEntry == nil {
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
	}
	s.snapshot.Status = StatusPaused
	s.snapshot.PositionMS = 0
	s.snapshot.DurationMS = currentDuration(currentItemFromEntry(s.snapshot.CurrentEntry))
	s.loadedEntryID = ""
	s.loadedURI = ""
	s.clearLoadingStateLocked()
	s.clearNextPreparationStateLocked()
	s.backendPreloadArmed = false
	s.stopTickerLocked()
	s.recordSkipEventLocked(skipped, true)
	s.snapshot.LastError = s.snapshot.LastSkipEvent.Message
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	if backend != nil {
		_ = backend.Stop(ctx)
	}
	s.publishSnapshot(state)
	return state
}

func (s *Session) settleAfterSkipped(skipped []skippedPlaybackEntry) SessionSnapshot {
	s.mu.Lock()
	s.clearLoadingStateLocked()
	s.clearNextPreparationStateLocked()
	s.recordSkipEventLocked(skipped, false)
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()
	s.publishSnapshot(state)
	return state
}

func (s *Session) recordSkipEventLocked(skipped []skippedPlaybackEntry, stopped bool) {
	if len(skipped) == 0 {
		return
	}
	firstEntry := cloneEntryPtr(&skipped[0].Entry)
	s.snapshot.LastSkipEvent = &PlaybackSkipEvent{
		EventID:    fmt.Sprintf("skip:%d", time.Now().UTC().UnixNano()),
		Message:    playbackSkipMessage(skipped, stopped),
		Count:      len(skipped),
		Stopped:    stopped,
		FirstEntry: firstEntry,
		OccurredAt: formatTimestamp(time.Now().UTC()),
	}
}

func playbackSkipMessage(skipped []skippedPlaybackEntry, stopped bool) string {
	if len(skipped) == 0 {
		if stopped {
			return "All remaining tracks are unavailable."
		}
		return "Skipped unavailable track."
	}
	if stopped {
		if len(skipped) == 1 {
			return fmt.Sprintf("Skipped unavailable track: %s. No playable tracks remain.", strings.TrimSpace(skipped[0].Entry.Item.Title))
		}
		return fmt.Sprintf("Skipped %d unavailable tracks. No playable tracks remain.", len(skipped))
	}
	if len(skipped) == 1 {
		return fmt.Sprintf("Skipped unavailable track: %s.", strings.TrimSpace(skipped[0].Entry.Item.Title))
	}
	return fmt.Sprintf("Skipped %d unavailable tracks.", len(skipped))
}

func availabilityUnavailableMessage(entry SessionEntry, status apitypes.PlaybackPreparationStatus) string {
	return fmt.Sprintf("recording %s is unavailable (%s)", entry.Item.RecordingID, status.Reason)
}

func (s *Session) playEntry(
	ctx context.Context,
	entry SessionEntry,
	origin EntryOrigin,
	queueIndex int,
	previous *SessionEntry,
	keepPosition bool,
	preserveCurrentOnPending bool,
) (SessionSnapshot, error) {
	restorePosition := int64(0)
	if keepPosition && previous != nil && previous.EntryID == entry.EntryID {
		restorePosition = s.Snapshot().PositionMS
	}
	backend := s.backend
	core := s.core
	if core == nil {
		return s.Snapshot(), errors.New("playback core is not configured")
	}
	if backend == nil {
		return s.Snapshot(), errors.New("playback backend is not configured")
	}

	s.mu.Lock()
	needsClearPreload := s.shouldClearBackendPreloadLocked()
	s.mu.Unlock()
	if needsClearPreload {
		s.clearBackendPreload(ctx, backend)
	}

	preparation, err := core.PreparePlaybackTarget(ctx, entry.Item.Target, s.preferredProfile, apitypes.PlaybackPreparationPlayNow)
	if err != nil {
		return s.Snapshot(), err
	}

	if preparation.Phase == apitypes.PlaybackPreparationPreparingFetch || preparation.Phase == apitypes.PlaybackPreparationPreparingTranscode {
		s.mu.Lock()
		hasCurrent := s.snapshot.CurrentEntry != nil
		shouldReplaceCurrent := !hasCurrent || !preserveCurrentOnPending
		s.setLoadingLocked(entry, preparation)
		if shouldReplaceCurrent {
			s.clearCurrentLocked()
			s.snapshot.Status = StatusPending
			s.snapshot.PositionMS = 0
			s.snapshot.DurationMS = nil
			s.snapshot.CurrentSourceKind = ""
			s.snapshot.CurrentPreparation = nil
			s.loadedEntryID = ""
			s.loadedURI = ""
			s.stopTickerLocked()
		}
		s.snapshot.LastError = ""
		s.clearNextPreparationStateLocked()
		s.touchLocked()
		s.startPendingRetryLocked(entry.EntryID)
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		if shouldReplaceCurrent {
			_ = backend.Stop(context.Background())
		}
		s.publishSnapshot(state)
		return state, nil
	}

	if preparation.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(preparation.PlayableURI) == "" {
		return s.Snapshot(), &entryUnavailableError{entry: entry, status: preparation}
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
	s.clearLoadingStateLocked()
	s.switchCurrentLocked(entry, origin, queueIndex, previous, keepPosition, restorePosition)
	s.snapshot.Status = StatusPlaying
	s.snapshot.LastError = ""
	s.snapshot.CurrentSourceKind = preparation.SourceKind
	s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: preparation}
	s.loadedEntryID = entry.EntryID
	s.loadedURI = preparation.PlayableURI
	s.backendPreloadArmed = false
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
	now := time.Now()

	s.mu.Lock()
	if s.backend == nil || !s.backend.SupportsPreload() {
		s.mu.Unlock()
		return
	}
	currentEntryID := ""
	if s.snapshot.CurrentEntry != nil {
		currentEntryID = s.snapshot.CurrentEntry.EntryID
	}
	if !s.shouldArmNextPreparationLocked() {
		backend := s.backend
		s.clearNextPreparationStateLocked()
		s.mu.Unlock()
		s.clearBackendPreload(ctx, backend)
		return
	}
	currentNext := cloneEntryPreparation(s.snapshot.NextPreparation)
	backend := s.backend
	core := s.core
	preloadReady := false
	useCachedStatus := false
	s.mu.Unlock()

	if core == nil || backend == nil {
		return
	}

	nextCandidate, _, err := s.resolveNextPlayableCandidate(ctx, nil)
	if err != nil {
		return
	}
	if nextCandidate == nil {
		s.mu.Lock()
		backend = s.backend
		s.clearNextPreparationStateLocked()
		s.mu.Unlock()
		s.clearBackendPreload(ctx, backend)
		return
	}
	nextEntry := nextCandidate.Entry
	nextIsCurrent := currentEntryID != "" && nextEntry.EntryID == currentEntryID
	if nextEntry.EntryID == "" || nextIsCurrent {
		s.mu.Lock()
		backend = s.backend
		s.clearNextPreparationStateLocked()
		s.mu.Unlock()
		s.clearBackendPreload(ctx, backend)
		return
	}

	if currentNext != nil && currentNext.EntryID == nextEntry.EntryID {
		switch currentNext.Status.Phase {
		case apitypes.PlaybackPreparationReady:
			s.mu.Lock()
			alreadyPreloaded := s.preloadedID == nextEntry.EntryID && s.preloadedURI == currentNext.Status.PlayableURI
			s.mu.Unlock()
			if alreadyPreloaded {
				return
			}
			preloadReady = true
		case apitypes.PlaybackPreparationPreparingFetch, apitypes.PlaybackPreparationPreparingTranscode:
			s.mu.Lock()
			if !s.shouldPollNextPreparationLocked(nextEntry.EntryID, now) {
				s.mu.Unlock()
				return
			}
			s.scheduleNextPreparationPollLocked(nextEntry.EntryID, now.Add(nextPreparationPoll))
			s.mu.Unlock()
			useCachedStatus = true
		case apitypes.PlaybackPreparationUnavailable, apitypes.PlaybackPreparationFailed:
			s.mu.Lock()
			if !s.shouldPollNextPreparationLocked(nextEntry.EntryID, now) {
				s.mu.Unlock()
				return
			}
			s.scheduleNextPreparationPollLocked(nextEntry.EntryID, now.Add(nextPreparationPoll))
			s.mu.Unlock()
		}
	}

	if preloadReady {
		if err := backend.PreloadNext(ctx, currentNext.Status.PlayableURI); err != nil {
			s.logErrorf("playback: preload next failed: %v", err)
			return
		}
		s.mu.Lock()
		s.clearNextPreparationRetryLocked()
		s.preloadedID = nextEntry.EntryID
		s.preloadedURI = currentNext.Status.PlayableURI
		s.backendPreloadArmed = true
		s.snapshot.NextPreparation = &EntryPreparation{EntryID: nextEntry.EntryID, Status: currentNext.Status}
		s.mu.Unlock()
		return
	}

	if useCachedStatus {
		status, err := core.GetPlaybackTargetPreparation(ctx, nextEntry.Item.Target, s.preferredProfile)
		if err != nil {
			return
		}
		s.applyNextPreparationStatus(ctx, nextEntry, status)
		return
	}

	status, err := core.PreparePlaybackTarget(ctx, nextEntry.Item.Target, s.preferredProfile, apitypes.PlaybackPreparationPreloadNext)
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
		s.scheduleNextPreparationPollLocked(entry.EntryID, time.Now().Add(nextPreparationPoll))
		s.preloadedID = ""
		s.preloadedURI = ""
		s.touchLocked()
		s.mu.Unlock()
		s.clearBackendPreload(ctx, backend)
		return
	}
	s.clearNextPreparationRetryLocked()
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
	s.backendPreloadArmed = true
	s.snapshot.NextPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: status}
	s.touchLocked()
	s.mu.Unlock()
}

func (s *Session) shouldArmNextPreparationLocked() bool {
	if s.snapshot.LoadingEntry != nil {
		return false
	}
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
	s.clearNextPreparationRetryLocked()
}

func (s *Session) shouldClearBackendPreloadLocked() bool {
	return s.backendPreloadArmed || s.snapshot.NextPreparation != nil || s.preloadedID != "" || s.preloadedURI != ""
}

func (s *Session) clearBackendPreload(ctx context.Context, backend Backend) {
	if backend == nil {
		return
	}
	if err := backend.ClearPreloaded(ctx); err != nil {
		s.logErrorf("playback: clear preload failed: %v", err)
		return
	}
	s.mu.Lock()
	s.backendPreloadArmed = false
	s.mu.Unlock()
}

func (s *Session) shouldPollNextPreparationLocked(entryID string, now time.Time) bool {
	if strings.TrimSpace(entryID) == "" {
		return false
	}
	if s.nextPreparationRetryEntryID != entryID {
		return true
	}
	return s.nextPreparationRetryAt.IsZero() || !now.Before(s.nextPreparationRetryAt)
}

func (s *Session) scheduleNextPreparationPollLocked(entryID string, when time.Time) {
	s.nextPreparationRetryEntryID = strings.TrimSpace(entryID)
	s.nextPreparationRetryAt = when
}

func (s *Session) clearNextPreparationRetryLocked() {
	s.nextPreparationRetryEntryID = ""
	s.nextPreparationRetryAt = time.Time{}
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
		s.handleTrackEOF(event)
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

func (s *Session) handleTrackEOF(event BackendEvent) {
	s.mu.Lock()
	playing := s.snapshot.Status == StatusPlaying
	backend := s.backend
	supportsPreload := backend != nil && backend.SupportsPreload()
	hasLoading := s.snapshot.LoadingEntry != nil
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	loadedURI := s.loadedURI
	expectedPreloadedURI := s.preloadedURI
	s.mu.Unlock()
	if !playing {
		return
	}
	if event.EndedURI != "" && loadedURI != "" && event.EndedURI != loadedURI {
		return
	}

	if hasLoading {
		s.mu.Lock()
		s.snapshot.Status = StatusPending
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = nil
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.clearCurrentLocked()
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
		s.clearNextPreparationStateLocked()
		s.backendPreloadArmed = false
		s.stopTickerLocked()
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		if backend != nil {
			_ = backend.Stop(context.Background())
		}
		s.publishSnapshot(state)
		return
	}

	nextCandidate, skipped, err := s.resolveNextPlayableCandidate(context.Background(), nil)
	if err != nil {
		s.logErrorf("playback: resolve next candidate failed: %v", err)
		return
	}
	if nextCandidate == nil {
		if len(skipped) > 0 {
			s.stopAfterSkipped(context.Background(), current, skipped)
			return
		}
		s.mu.Lock()
		s.snapshot.Status = StatusPaused
		s.snapshot.PositionMS = 0
		s.snapshot.DurationMS = currentDuration(currentItemFromEntry(current))
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.clearNextPreparationStateLocked()
		s.backendPreloadArmed = false
		s.stopTickerLocked()
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		if backend != nil {
			_ = backend.Stop(context.Background())
		}
		s.publishSnapshot(state)
		return
	}
	nextEntry := nextCandidate.Entry
	origin := nextCandidate.Origin
	queueIndex := nextCandidate.QueueIndex
	preloadedMatches := supportsPreload &&
		s.preloadedID != "" &&
		s.preloadedID == nextEntry.EntryID &&
		(event.ActiveURI == "" || event.ActiveURI == expectedPreloadedURI)

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
		s.backendPreloadArmed = false
		s.clearNextPreparationStateLocked()
		s.ensureTickerLocked()
		if len(skipped) > 0 {
			s.recordSkipEventLocked(skipped, false)
		}
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()

		s.preloadNext(context.Background())
		s.publishSnapshot(state)
		return
	}

	_, err = s.playNextAvailable(context.Background(), current, false, false, nil, nil, true)
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
		s.snapshot.LastError = "waiting for provider transcode timed out"
	}
	if s.snapshot.CurrentEntry == nil {
		s.snapshot.Status = StatusPaused
	}
	s.clearLoadingStateLocked()
	s.pendingCancel = nil
	s.pendingEntry = ""
	s.pendingToken++
	s.touchLocked()
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
	entry := cloneEntryPtr(s.snapshot.LoadingEntry)
	core := s.core
	s.mu.Unlock()

	if entry == nil || entry.EntryID != entryID || core == nil {
		return true
	}

	status, err := core.GetPlaybackTargetPreparation(ctx, entry.Item.Target, s.preferredProfile)
	if err != nil {
		return false
	}
	s.mu.Lock()
	if token == s.pendingToken && entryID == s.pendingEntry {
		s.snapshot.LoadingPreparation = &EntryPreparation{EntryID: entryID, Status: status}
		s.touchLocked()
	}
	s.mu.Unlock()
	if status.Phase == apitypes.PlaybackPreparationPreparingFetch || status.Phase == apitypes.PlaybackPreparationPreparingTranscode {
		return false
	}
	if status.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(status.PlayableURI) == "" {
		s.mu.Lock()
		hasCurrent := s.snapshot.CurrentEntry != nil
		s.mu.Unlock()
		if _, advanceErr := s.playNextAvailable(
			ctx,
			nil,
			false,
			hasCurrent,
			nil,
			nil,
			!hasCurrent,
		); advanceErr != nil {
			_, _ = s.failPendingPlayback(entry, advanceErr)
		}
		s.mu.Lock()
		s.pendingCancel = nil
		s.pendingEntry = ""
		s.pendingToken++
		s.mu.Unlock()
		return true
	}

	playErr := s.completePendingPlayback(ctx, *entry, status)
	if playErr != nil {
		_, _ = s.failPendingPlayback(entry, playErr)
		s.mu.Lock()
		if token == s.pendingToken && entryID == s.pendingEntry {
			s.pendingCancel = nil
			s.pendingEntry = ""
			s.pendingToken++
		}
		s.mu.Unlock()
		return true
	}
	return true
}

func (s *Session) buildContextLocked(input PlaybackContextInput) (*ContextQueue, int, error) {
	items := NormalizeItems(input.Items)
	if len(items) == 0 {
		return &ContextQueue{
			Kind:         input.Kind,
			ID:           strings.TrimSpace(input.ID),
			Title:        strings.TrimSpace(input.Title),
			CurrentIndex: -1,
			ResumeIndex:  -1,
		}, 0, nil
	}

	startIndex := normalizeCurrentIndex(len(items), input.StartIndex)
	contextData := &ContextQueue{
		Kind:         input.Kind,
		ID:           strings.TrimSpace(input.ID),
		Title:        strings.TrimSpace(input.Title),
		StartIndex:   startIndex,
		CurrentIndex: startIndex,
		ResumeIndex:  startIndex,
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

func (s *Session) playTargetLocked() (SessionEntry, EntryOrigin, int, bool) {
	if s.snapshot.CurrentEntry != nil {
		return *s.snapshot.CurrentEntry, s.snapshot.CurrentEntry.Origin, -1, true
	}
	if len(s.snapshot.UserQueue) > 0 {
		return s.snapshot.UserQueue[0], EntryOriginQueued, 0, true
	}
	index := s.firstContextIndexLocked()
	if index < 0 || s.snapshot.ContextQueue == nil || index >= len(s.snapshot.ContextQueue.Entries) {
		return SessionEntry{}, "", -1, false
	}
	return s.snapshot.ContextQueue.Entries[index], EntryOriginContext, -1, true
}

func currentMatchesContext(snapshot SessionSnapshot) bool {
	if snapshot.CurrentEntry == nil || snapshot.ContextQueue == nil {
		return false
	}
	if snapshot.CurrentEntry.Origin != EntryOriginContext {
		return false
	}
	currentIndex := currentContextIndex(snapshot)
	if currentIndex < 0 || currentIndex >= len(snapshot.ContextQueue.Entries) {
		return false
	}
	return snapshot.ContextQueue.Entries[currentIndex].EntryID == snapshot.CurrentEntry.EntryID
}

func isValidContextIndex(snapshot SessionSnapshot, index int) bool {
	return snapshot.ContextQueue != nil && index >= 0 && index < len(snapshot.ContextQueue.Entries)
}

func defaultFirstContextIndex(snapshot SessionSnapshot) int {
	if snapshot.ContextQueue == nil || len(snapshot.ContextQueue.Entries) == 0 {
		return -1
	}
	if !snapshot.Shuffle {
		return 0
	}
	cycle := effectiveShuffleCycle(snapshot)
	if len(cycle) == 0 {
		return -1
	}
	return cycle[0]
}

func nextContextIndexFromAnchor(snapshot SessionSnapshot, anchorIndex int, ignoreRepeatOne bool) int {
	if !isValidContextIndex(snapshot, anchorIndex) {
		return defaultFirstContextIndex(snapshot)
	}

	if !snapshot.Shuffle {
		if !ignoreRepeatOne && snapshot.RepeatMode == RepeatOne {
			return anchorIndex
		}
		next := anchorIndex + 1
		if next < len(snapshot.ContextQueue.Entries) {
			return next
		}
		if snapshot.RepeatMode == RepeatAll {
			return 0
		}
		return -1
	}

	cycle := effectiveShuffleCycle(snapshot)
	if len(cycle) == 0 {
		return -1
	}
	if !ignoreRepeatOne && snapshot.RepeatMode == RepeatOne {
		return anchorIndex
	}
	position := indexOfInt(cycle, anchorIndex)
	if position < 0 {
		return cycle[0]
	}
	nextPosition := position + 1
	if nextPosition < len(cycle) {
		return cycle[nextPosition]
	}
	if snapshot.RepeatMode == RepeatAll {
		return cycle[0]
	}
	return -1
}

func normalizeResumeContextIndex(snapshot SessionSnapshot) int {
	if snapshot.ContextQueue == nil || len(snapshot.ContextQueue.Entries) == 0 {
		return -1
	}
	if currentMatchesContext(snapshot) {
		return nextContextIndexFromAnchor(snapshot, currentContextIndex(snapshot), false)
	}
	if snapshot.CurrentEntry != nil && snapshot.CurrentEntry.Origin != EntryOriginContext && isValidContextIndex(snapshot, currentContextIndex(snapshot)) {
		return nextContextIndexFromAnchor(snapshot, currentContextIndex(snapshot), false)
	}
	if isValidContextIndex(snapshot, resumeContextIndex(snapshot)) {
		return resumeContextIndex(snapshot)
	}
	if snapshot.CurrentEntry == nil && isValidContextIndex(snapshot, currentContextIndex(snapshot)) {
		return currentContextIndex(snapshot)
	}
	return defaultFirstContextIndex(snapshot)
}

func previousContextIndexFromAnchor(snapshot SessionSnapshot, anchorIndex int) int {
	if !isValidContextIndex(snapshot, anchorIndex) {
		return -1
	}
	if !snapshot.Shuffle {
		previous := anchorIndex - 1
		if previous >= 0 {
			return previous
		}
		if snapshot.RepeatMode == RepeatAll {
			return len(snapshot.ContextQueue.Entries) - 1
		}
		return -1
	}

	cycle := effectiveShuffleCycle(snapshot)
	if len(cycle) == 0 {
		return -1
	}
	position := indexOfInt(cycle, anchorIndex)
	if position < 0 {
		return -1
	}
	previousPosition := position - 1
	if previousPosition >= 0 {
		return cycle[previousPosition]
	}
	if snapshot.RepeatMode == RepeatAll {
		return cycle[len(cycle)-1]
	}
	return -1
}

func (s *Session) resolveEntrySelectionLocked(entryID string) (SessionEntry, EntryOrigin, int, error) {
	if current := s.snapshot.CurrentEntry; current != nil && current.EntryID == entryID {
		return *current, current.Origin, -1, nil
	}
	if index := indexOfEntryID(s.snapshot.UserQueue, entryID); index >= 0 {
		return s.snapshot.UserQueue[index], EntryOriginQueued, index, nil
	}
	if s.snapshot.ContextQueue != nil {
		for _, entry := range s.snapshot.ContextQueue.Entries {
			if entry.EntryID == entryID {
				return entry, EntryOriginContext, -1, nil
			}
		}
	}
	return SessionEntry{}, "", -1, fmt.Errorf("entry %s not found", entryID)
}

func (s *Session) switchCurrentLocked(entry SessionEntry, origin EntryOrigin, queueIndex int, previous *SessionEntry, keepPosition bool, restorePosition int64) {
	if origin == EntryOriginQueued && queueIndex >= 0 && queueIndex < len(s.snapshot.UserQueue) {
		s.snapshot.UserQueue = append(s.snapshot.UserQueue[:queueIndex], s.snapshot.UserQueue[queueIndex+1:]...)
	}
	s.setCurrentLocked(entry, origin)
	s.recalculateResumeContextIndexLocked()
	s.snapshot.CurrentPreparation = nil
	if !keepPosition {
		s.snapshot.PositionMS = 0
	}
	if restorePosition > 0 {
		s.snapshot.PositionMS = restorePosition
	}
	s.snapshot.DurationMS = currentDuration(&entry.Item)
}

func (s *Session) setLoadingLocked(entry SessionEntry, preparation apitypes.PlaybackPreparationStatus) {
	s.snapshot.LoadingEntry = cloneEntryPtr(&entry)
	item := entry.Item
	s.snapshot.LoadingItem = &item
	s.snapshot.LoadingPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: preparation}
}

func (s *Session) setCurrentLocked(entry SessionEntry, origin EntryOrigin) {
	entry.Origin = origin
	s.snapshot.CurrentEntryID = entry.EntryID
	s.snapshot.CurrentEntry = &entry
	item := entry.Item
	s.snapshot.CurrentItem = &item
	if origin == EntryOriginContext {
		s.setCurrentContextIndexLocked(entry.ContextIndex)
	}
}

func (s *Session) clearCurrentLocked() {
	s.snapshot.CurrentEntryID = ""
	s.snapshot.CurrentEntry = nil
	s.snapshot.CurrentItem = nil
}

func (s *Session) recalculateResumeContextIndexLocked() {
	s.setResumeContextIndexLocked(normalizeResumeContextIndex(s.snapshot))
}

func (s *Session) clearLoadingStateLocked() {
	s.snapshot.LoadingEntry = nil
	s.snapshot.LoadingItem = nil
	s.snapshot.LoadingPreparation = nil
}

func (s *Session) peekNextLocked(ignoreRepeatOne bool) (SessionEntry, EntryOrigin, int, bool) {
	if s.snapshot.CurrentEntry == nil {
		if len(s.snapshot.UserQueue) > 0 {
			return s.snapshot.UserQueue[0], EntryOriginQueued, 0, true
		}
		index := s.firstContextIndexLocked()
		if index < 0 || s.snapshot.ContextQueue == nil || index >= len(s.snapshot.ContextQueue.Entries) {
			return SessionEntry{}, "", -1, false
		}
		return s.snapshot.ContextQueue.Entries[index], EntryOriginContext, -1, true
	}
	if len(s.snapshot.UserQueue) > 0 {
		return s.snapshot.UserQueue[0], EntryOriginQueued, 0, true
	}
	index := s.nextContextIndexLocked(ignoreRepeatOne)
	if index < 0 || s.snapshot.ContextQueue == nil || index >= len(s.snapshot.ContextQueue.Entries) {
		return SessionEntry{}, "", -1, false
	}
	return s.snapshot.ContextQueue.Entries[index], EntryOriginContext, -1, true
}

func (s *Session) firstContextIndexLocked() int {
	if isValidContextIndex(s.snapshot, resumeContextIndex(s.snapshot)) {
		if !s.snapshot.Shuffle {
			return resumeContextIndex(s.snapshot)
		}
		s.ensureShuffleCycleLocked()
		if indexOfInt(shuffleBag(s.snapshot), resumeContextIndex(s.snapshot)) >= 0 {
			return resumeContextIndex(s.snapshot)
		}
	}
	return defaultFirstContextIndex(s.snapshot)
}

func (s *Session) nextContextIndexLocked(ignoreRepeatOne bool) int {
	if s.snapshot.ContextQueue == nil || len(s.snapshot.ContextQueue.Entries) == 0 {
		return -1
	}
	if !currentMatchesContext(s.snapshot) {
		return s.firstContextIndexLocked()
	}
	return nextContextIndexFromAnchor(s.snapshot, currentContextIndex(s.snapshot), ignoreRepeatOne)
}

func (s *Session) ensureShuffleCycleLocked() {
	if !s.snapshot.Shuffle || s.snapshot.ContextQueue == nil {
		return
	}
	if len(shuffleBag(s.snapshot)) == len(s.snapshot.ContextQueue.Entries) && len(shuffleBag(s.snapshot)) > 0 {
		return
	}
	s.rebuildShuffleCycleLocked()
}

func (s *Session) rebuildShuffleCycleLocked() {
	if !s.snapshot.Shuffle || s.snapshot.ContextQueue == nil {
		setShuffleBagLocked(&s.snapshot, nil)
		return
	}
	setShuffleBagLocked(&s.snapshot, buildAnchoredSmartShuffleCycle(
		s.snapshot.ContextQueue.Entries,
		shuffleAnchorIndex(s.snapshot),
		s.rng,
	))
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
	size := len(s.snapshot.UserQueue)
	if s.snapshot.CurrentEntry != nil {
		size++
	}
	if s.snapshot.ContextQueue != nil {
		size += len(s.snapshot.ContextQueue.Entries)
	}

	out := make([]SessionEntry, 0, size)
	if s.snapshot.CurrentEntry != nil {
		out = append(out, *s.snapshot.CurrentEntry)
	}
	out = append(out, s.snapshot.UserQueue...)
	if s.snapshot.ContextQueue != nil {
		out = append(out, s.snapshot.ContextQueue.Entries...)
	}
	return out
}

func (s *Session) stopBackend() error {
	s.mu.Lock()
	backend := s.backend
	s.stopTickerLocked()
	s.backendPreloadArmed = false
	s.mu.Unlock()
	if backend == nil {
		return nil
	}
	return backend.Stop(context.Background())
}

func (s *Session) completePendingPlayback(ctx context.Context, entry SessionEntry, status apitypes.PlaybackPreparationStatus) error {
	backend := s.backend
	if backend == nil {
		return errors.New("playback backend is not configured")
	}
	if status.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(status.PlayableURI) == "" {
		return &entryUnavailableError{entry: entry, status: status}
	}
	if err := backend.Load(ctx, status.PlayableURI); err != nil {
		return err
	}
	if err := backend.Play(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	previous := cloneEntryPtr(s.snapshot.CurrentEntry)
	queueIndex := -1
	if entry.Origin == EntryOriginQueued {
		queueIndex = indexOfEntryID(s.snapshot.UserQueue, entry.EntryID)
	}
	s.clearLoadingStateLocked()
	s.switchCurrentLocked(entry, entry.Origin, queueIndex, previous, false, 0)
	s.snapshot.Status = StatusPlaying
	s.snapshot.LastError = ""
	s.snapshot.CurrentSourceKind = status.SourceKind
	s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: status}
	s.loadedEntryID = entry.EntryID
	s.loadedURI = status.PlayableURI
	s.backendPreloadArmed = false
	s.pendingCancel = nil
	s.pendingEntry = ""
	s.pendingToken++
	s.ensureTickerLocked()
	s.touchLocked()
	s.mu.Unlock()

	s.refreshPosition()
	s.publishSnapshot(s.Snapshot())
	return nil
}

func (s *Session) invalidateFutureOrderLocked(reason string, forceRecovery bool) (Backend, bool) {
	backend := s.backend
	needsClearPreload := s.shouldClearBackendPreloadLocked()
	if !needsClearPreload && forceRecovery && backend != nil && backend.SupportsPreload() && s.snapshot.CurrentEntry != nil && s.loadedURI != "" {
		needsClearPreload = true
	}
	if needsClearPreload {
		s.logPrintf("playback: invalidating future order (%s)", reason)
	}
	s.clearNextPreparationStateLocked()
	return backend, needsClearPreload
}

func (s *Session) previousContextEntryLocked() (SessionEntry, bool) {
	if !currentMatchesContext(s.snapshot) {
		return SessionEntry{}, false
	}
	index := previousContextIndexFromAnchor(s.snapshot, currentContextIndex(s.snapshot))
	if index < 0 || s.snapshot.ContextQueue == nil || index >= len(s.snapshot.ContextQueue.Entries) {
		return SessionEntry{}, false
	}
	return s.snapshot.ContextQueue.Entries[index], true
}

func (s *Session) returnContextEntryLocked() (SessionEntry, bool) {
	if !isValidContextIndex(s.snapshot, currentContextIndex(s.snapshot)) || s.snapshot.ContextQueue == nil {
		return SessionEntry{}, false
	}
	return s.snapshot.ContextQueue.Entries[currentContextIndex(s.snapshot)], true
}

func (s *Session) entryInCurrentContextLocked(entry SessionEntry) bool {
	if entry.Origin != EntryOriginContext || s.snapshot.ContextQueue == nil {
		return false
	}
	if entry.ContextIndex >= 0 && entry.ContextIndex < len(s.snapshot.ContextQueue.Entries) {
		return s.snapshot.ContextQueue.Entries[entry.ContextIndex].EntryID == entry.EntryID
	}
	for _, contextEntry := range s.snapshot.ContextQueue.Entries {
		if contextEntry.EntryID == entry.EntryID {
			return true
		}
	}
	return false
}

func (s *Session) consumeSkippedUserEntries(skipped []skippedPlaybackEntry, candidate *playbackCandidate, state SessionSnapshot) SessionSnapshot {
	if candidate == nil || candidate.Origin != EntryOriginQueued || len(skipped) == 0 {
		return state
	}
	queuedSkipped := make(map[string]struct{}, len(skipped))
	for _, item := range skipped {
		if item.Entry.Origin != EntryOriginQueued {
			continue
		}
		queuedSkipped[item.Entry.EntryID] = struct{}{}
	}
	if len(queuedSkipped) == 0 {
		return state
	}

	s.mu.Lock()
	nextQueue := make([]SessionEntry, 0, len(s.snapshot.UserQueue))
	changed := false
	for _, entry := range s.snapshot.UserQueue {
		if _, ok := queuedSkipped[entry.EntryID]; ok {
			changed = true
			continue
		}
		nextQueue = append(nextQueue, entry)
	}
	if changed {
		s.snapshot.UserQueue = nextQueue
		s.touchLocked()
		state = snapshotCopyLocked(&s.snapshot)
	}
	s.mu.Unlock()
	if changed {
		s.publishSnapshot(state)
	}
	return state
}

func (s *Session) failPlayback(err error) (SessionSnapshot, error) {
	if err == nil {
		return s.Snapshot(), nil
	}
	s.mu.Lock()
	s.snapshot.Status = StatusPaused
	s.snapshot.LastError = err.Error()
	s.clearLoadingStateLocked()
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

func (s *Session) failPendingPlayback(entry *SessionEntry, err error) (SessionSnapshot, error) {
	if err == nil {
		return s.Snapshot(), nil
	}
	s.mu.Lock()
	if s.snapshot.CurrentEntry == nil {
		s.snapshot.Status = StatusPaused
	}
	s.snapshot.LastError = err.Error()
	s.clearLoadingStateLocked()
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

	if snapshot.ContextQueue != nil {
		contextCopy := *snapshot.ContextQueue
		contextCopy.Entries = cloneEntries(contextCopy.Entries)
		contextCopy.ShuffleBag = append([]int(nil), contextCopy.ShuffleBag...)
		for index := range contextCopy.Entries {
			contextCopy.Entries[index].Item = normalizeSessionItem(contextCopy.Entries[index].Item)
			contextCopy.Entries[index].Origin = EntryOriginContext
			contextCopy.Entries[index].ContextIndex = index
		}
		if len(contextCopy.Entries) == 0 {
			snapshot.ContextQueue = nil
		} else {
			if contextCopy.StartIndex < 0 || contextCopy.StartIndex >= len(contextCopy.Entries) {
				contextCopy.StartIndex = 0
			}
			if contextCopy.CurrentIndex < 0 || contextCopy.CurrentIndex >= len(contextCopy.Entries) {
				contextCopy.CurrentIndex = -1
			}
			if contextCopy.ResumeIndex < 0 || contextCopy.ResumeIndex >= len(contextCopy.Entries) {
				contextCopy.ResumeIndex = -1
			}
			snapshot.ContextQueue = &contextCopy
		}
	}

	snapshot.UserQueue = cloneEntries(snapshot.UserQueue)
	for index := range snapshot.UserQueue {
		snapshot.UserQueue[index].Item = normalizeSessionItem(snapshot.UserQueue[index].Item)
		snapshot.UserQueue[index].Origin = EntryOriginQueued
		snapshot.UserQueue[index].ContextIndex = -1
	}

	snapshot.CurrentPreparation = cloneEntryPreparation(snapshot.CurrentPreparation)
	snapshot.LoadingPreparation = cloneEntryPreparation(snapshot.LoadingPreparation)
	snapshot.NextPreparation = cloneEntryPreparation(snapshot.NextPreparation)
	snapshot.LastSkipEvent = clonePlaybackSkipEvent(snapshot.LastSkipEvent)

	if snapshot.CurrentEntry != nil {
		entry := *snapshot.CurrentEntry
		entry.Item = normalizeSessionItem(entry.Item)
		if snapshot.CurrentEntryID == "" {
			snapshot.CurrentEntryID = entry.EntryID
		}
		snapshot.CurrentEntry = &entry
		item := entry.Item
		snapshot.CurrentItem = &item
		if snapshot.DurationMS == nil {
			snapshot.DurationMS = currentDuration(&item)
		}
		if snapshot.ContextQueue != nil && entry.Origin == EntryOriginContext && entry.ContextIndex >= 0 && entry.ContextIndex < len(snapshot.ContextQueue.Entries) {
			snapshot.ContextQueue.CurrentIndex = entry.ContextIndex
		}
	} else {
		snapshot.CurrentEntryID = ""
		snapshot.CurrentItem = nil
		snapshot.CurrentPreparation = nil
		if snapshot.Status == StatusPlaying || snapshot.Status == StatusPending {
			snapshot.Status = StatusPaused
		}
	}

	if snapshot.LoadingEntry != nil {
		entry := *snapshot.LoadingEntry
		entry.Item = normalizeSessionItem(entry.Item)
		snapshot.LoadingEntry = &entry
		item := entry.Item
		snapshot.LoadingItem = &item
	} else {
		snapshot.LoadingItem = nil
		snapshot.LoadingPreparation = nil
	}

	if snapshot.ContextQueue == nil {
		if snapshot.CurrentEntry == nil {
			snapshot.CurrentLane = ""
		}
	} else {
		if !snapshot.Shuffle {
			snapshot.ContextQueue.ShuffleBag = nil
		}
		snapshot.ContextQueue.ResumeIndex = normalizeResumeContextIndex(snapshot)
	}
	if snapshot.DurationMS != nil && snapshot.PositionMS > *snapshot.DurationMS {
		snapshot.PositionMS = *snapshot.DurationMS
	}
	if snapshot.ContextQueue == nil && len(snapshot.UserQueue) == 0 && snapshot.CurrentEntry == nil {
		if snapshot.LoadingEntry == nil {
			snapshot.Status = StatusIdle
			snapshot.PositionMS = 0
			snapshot.DurationMS = nil
			snapshot.CurrentSourceKind = ""
			snapshot.NextPreparation = nil
		}
	}
	if snapshot.LoadingEntry != nil && snapshot.CurrentEntry == nil && snapshot.Status != StatusPending {
		snapshot.Status = StatusPending
	}

	snapshot.UpcomingEntries = buildUpcomingEntries(snapshot)
	snapshot.CurrentLane = deriveCurrentLane(snapshot)
	snapshot.NextPlanned = buildQueuePlan(snapshot.UpcomingEntries)
	if snapshot.PreloadedPlan == nil && snapshot.NextPreparation != nil {
		if entry, ok := findEntryByID(snapshot, snapshot.NextPreparation.EntryID); ok {
			snapshot.PreloadedPlan = &QueuePlan{
				Entry:   cloneEntryPtr(&entry),
				Lane:    laneFromOrigin(entry.Origin),
				Planned: true,
			}
		}
	}
	snapshot.QueueLength = len(snapshot.UpcomingEntries)
	if snapshot.CurrentEntry != nil {
		snapshot.QueueLength++
	}
	return snapshot
}

func snapshotCopyLocked(snapshot *SessionSnapshot) SessionSnapshot {
	copyState := normalizeSnapshot(*snapshot)
	copyState.UserQueue = cloneEntries(copyState.UserQueue)
	copyState.UpcomingEntries = cloneEntries(copyState.UpcomingEntries)
	copyState.ContextQueue = cloneContextQueue(copyState.ContextQueue)
	copyState.NextPlanned = cloneQueuePlan(copyState.NextPlanned)
	copyState.PreloadedPlan = cloneQueuePlan(copyState.PreloadedPlan)
	copyState.CurrentPreparation = cloneEntryPreparation(copyState.CurrentPreparation)
	copyState.LoadingPreparation = cloneEntryPreparation(copyState.LoadingPreparation)
	copyState.NextPreparation = cloneEntryPreparation(copyState.NextPreparation)
	copyState.LastSkipEvent = clonePlaybackSkipEvent(copyState.LastSkipEvent)
	copyState.EntryAvailability = cloneAvailabilityMap(copyState.EntryAvailability)
	if copyState.CurrentEntry != nil {
		entry := *copyState.CurrentEntry
		copyState.CurrentEntry = &entry
	}
	if copyState.CurrentItem != nil {
		item := *copyState.CurrentItem
		copyState.CurrentItem = &item
	}
	if copyState.LoadingEntry != nil {
		entry := *copyState.LoadingEntry
		copyState.LoadingEntry = &entry
	}
	if copyState.LoadingItem != nil {
		item := *copyState.LoadingItem
		copyState.LoadingItem = &item
	}
	copyState.DurationMS = cloneInt64Ptr(copyState.DurationMS)
	return copyState
}

func buildUpcomingEntries(snapshot SessionSnapshot) []SessionEntry {
	contextEntries := 0
	if snapshot.ContextQueue != nil {
		contextEntries = len(snapshot.ContextQueue.Entries)
	}
	out := make([]SessionEntry, 0, len(snapshot.UserQueue)+contextEntries)
	out = append(out, cloneEntries(snapshot.UserQueue)...)
	if snapshot.ContextQueue == nil || len(snapshot.ContextQueue.Entries) == 0 {
		return out
	}

	currentIsInContext := currentMatchesContext(snapshot)

	if snapshot.Shuffle {
		cycle := effectiveShuffleCycle(snapshot)
		resumeIndex := resumeContextIndex(snapshot)
		startAdded := false
		if currentIsInContext && currentContextIndex(snapshot) >= 0 {
			position := indexOfInt(cycle, currentContextIndex(snapshot))
			if position >= 0 {
				for index := position + 1; index < len(cycle); index++ {
					out = append(out, snapshot.ContextQueue.Entries[cycle[index]])
				}
				if snapshot.RepeatMode == RepeatAll {
					for index := 0; index < position; index++ {
						out = append(out, snapshot.ContextQueue.Entries[cycle[index]])
					}
				}
				startAdded = true
			}
		}
		if !startAdded && resumeIndex >= 0 && resumeIndex < len(snapshot.ContextQueue.Entries) {
			position := indexOfInt(cycle, resumeIndex)
			if position >= 0 {
				for index := position; index < len(cycle); index++ {
					out = append(out, snapshot.ContextQueue.Entries[cycle[index]])
				}
				if snapshot.RepeatMode == RepeatAll {
					for index := 0; index < position; index++ {
						out = append(out, snapshot.ContextQueue.Entries[cycle[index]])
					}
				}
				startAdded = true
			}
		}
		if !startAdded {
			for _, index := range cycle {
				out = append(out, snapshot.ContextQueue.Entries[index])
			}
		}
		return out
	}

	start := 0
	if currentIsInContext && currentContextIndex(snapshot) >= 0 {
		start = currentContextIndex(snapshot) + 1
	} else if resumeContextIndex(snapshot) >= 0 && resumeContextIndex(snapshot) < len(snapshot.ContextQueue.Entries) {
		start = resumeContextIndex(snapshot)
	}
	for index := start; index < len(snapshot.ContextQueue.Entries); index++ {
		out = append(out, snapshot.ContextQueue.Entries[index])
	}
	if snapshot.RepeatMode == RepeatAll && currentIsInContext && currentContextIndex(snapshot) >= 0 {
		for index := 0; index < currentContextIndex(snapshot); index++ {
			out = append(out, snapshot.ContextQueue.Entries[index])
		}
	} else if snapshot.RepeatMode == RepeatAll && !currentIsInContext && start > 0 {
		for index := 0; index < start; index++ {
			out = append(out, snapshot.ContextQueue.Entries[index])
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

func isAvailabilityDefinitivelyUnavailable(state apitypes.RecordingAvailabilityState) bool {
	switch state {
	case apitypes.AvailabilityUnavailableNoPath, apitypes.AvailabilityUnavailableProvider:
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

func clonePlaybackSkipEvent(value *PlaybackSkipEvent) *PlaybackSkipEvent {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.FirstEntry = cloneEntryPtr(copyValue.FirstEntry)
	copyValue.OccurredAt = normalizeTimestamp(copyValue.OccurredAt)
	return &copyValue
}

func cloneQueuePlan(value *QueuePlan) *QueuePlan {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.Entry = cloneEntryPtr(copyValue.Entry)
	return &copyValue
}

func cloneContextQueue(value *ContextQueue) *ContextQueue {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.Entries = cloneEntries(copyValue.Entries)
	copyValue.ShuffleBag = append([]int(nil), copyValue.ShuffleBag...)
	return &copyValue
}

func cloneAvailabilityMap(value map[string]apitypes.RecordingPlaybackAvailability) map[string]apitypes.RecordingPlaybackAvailability {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]apitypes.RecordingPlaybackAvailability, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func currentContextIndex(snapshot SessionSnapshot) int {
	if snapshot.ContextQueue == nil {
		return -1
	}
	return snapshot.ContextQueue.CurrentIndex
}

func resumeContextIndex(snapshot SessionSnapshot) int {
	if snapshot.ContextQueue == nil {
		return -1
	}
	return snapshot.ContextQueue.ResumeIndex
}

func shuffleBag(snapshot SessionSnapshot) []int {
	if snapshot.ContextQueue == nil {
		return nil
	}
	return snapshot.ContextQueue.ShuffleBag
}

func (s *Session) setCurrentContextIndexLocked(index int) {
	if s.snapshot.ContextQueue == nil {
		return
	}
	s.snapshot.ContextQueue.CurrentIndex = index
}

func (s *Session) setResumeContextIndexLocked(index int) {
	if s.snapshot.ContextQueue == nil {
		return
	}
	s.snapshot.ContextQueue.ResumeIndex = index
}

func setShuffleBagLocked(snapshot *SessionSnapshot, bag []int) {
	if snapshot == nil || snapshot.ContextQueue == nil {
		return
	}
	snapshot.ContextQueue.ShuffleBag = append([]int(nil), bag...)
}

func (s *Session) setEntryAvailabilityLocked(entryID string, status apitypes.RecordingPlaybackAvailability) {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return
	}
	if s.snapshot.EntryAvailability == nil {
		s.snapshot.EntryAvailability = make(map[string]apitypes.RecordingPlaybackAvailability)
	}
	s.snapshot.EntryAvailability[entryID] = status
}

func normalizeSessionItem(item SessionItem) SessionItem {
	items := NormalizeItems([]SessionItem{item})
	if len(items) == 0 {
		return item
	}
	return items[0]
}

func laneFromOrigin(origin EntryOrigin) CurrentLane {
	if origin == EntryOriginQueued {
		return CurrentLaneUser
	}
	return CurrentLaneContext
}

func deriveCurrentLane(snapshot SessionSnapshot) CurrentLane {
	if snapshot.CurrentEntry != nil {
		return laneFromOrigin(snapshot.CurrentEntry.Origin)
	}
	if snapshot.LoadingEntry != nil {
		return laneFromOrigin(snapshot.LoadingEntry.Origin)
	}
	return ""
}

func buildQueuePlan(upcoming []SessionEntry) *QueuePlan {
	if len(upcoming) == 0 {
		return nil
	}
	entry := upcoming[0]
	return &QueuePlan{
		Entry:   cloneEntryPtr(&entry),
		Lane:    laneFromOrigin(entry.Origin),
		Planned: true,
	}
}

func findEntryByID(snapshot SessionSnapshot, entryID string) (SessionEntry, bool) {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return SessionEntry{}, false
	}
	for _, entry := range snapshot.UserQueue {
		if entry.EntryID == entryID {
			return entry, true
		}
	}
	if snapshot.ContextQueue != nil {
		for _, entry := range snapshot.ContextQueue.Entries {
			if entry.EntryID == entryID {
				return entry, true
			}
		}
	}
	if snapshot.CurrentEntry != nil && snapshot.CurrentEntry.EntryID == entryID {
		return *snapshot.CurrentEntry, true
	}
	return SessionEntry{}, false
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

func playbackTargetKey(target PlaybackTargetRef) string {
	logical := strings.TrimSpace(target.LogicalRecordingID)
	exact := strings.TrimSpace(target.ExactVariantRecordingID)
	if logical == "" && exact == "" {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s", target.ResolutionPolicy, logical, exact)
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

func buildAnchoredSmartShuffleCycle(entries []SessionEntry, anchorIndex int, rng *rand.Rand) []int {
	if anchorIndex < 0 || anchorIndex >= len(entries) {
		return buildSmartShuffleCycle(entries, rng)
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	}

	remaining := make([]int, 0, len(entries)-1)
	for index := range entries {
		if index == anchorIndex {
			continue
		}
		remaining = append(remaining, index)
	}
	rng.Shuffle(len(remaining), func(left int, right int) {
		remaining[left], remaining[right] = remaining[right], remaining[left]
	})

	cycle := make([]int, 0, len(entries))
	cycle = append(cycle, anchorIndex)
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

func effectiveShuffleCycle(snapshot SessionSnapshot) []int {
	cycle := append([]int(nil), shuffleBag(snapshot)...)
	if len(cycle) > 0 {
		return cycle
	}
	if snapshot.ContextQueue == nil {
		return nil
	}
	return buildAnchoredSmartShuffleCycle(
		snapshot.ContextQueue.Entries,
		shuffleAnchorIndex(snapshot),
		rand.New(rand.NewSource(1)),
	)
}

func shuffleAnchorIndex(snapshot SessionSnapshot) int {
	if isValidContextIndex(snapshot, currentContextIndex(snapshot)) {
		return currentContextIndex(snapshot)
	}
	if isValidContextIndex(snapshot, resumeContextIndex(snapshot)) {
		return resumeContextIndex(snapshot)
	}
	return -1
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

func (s *Session) syncAuthorityStateLocked() {
	s.snapshot.CurrentLane = deriveCurrentLane(s.snapshot)
	s.snapshot.NextPlanned = buildQueuePlan(s.snapshot.UpcomingEntries)
	if s.preloadedID == "" {
		s.snapshot.PreloadedPlan = nil
		return
	}
	entry, ok := findEntryByID(s.snapshot, s.preloadedID)
	if !ok {
		s.snapshot.PreloadedPlan = nil
		return
	}
	s.snapshot.PreloadedPlan = &QueuePlan{
		Entry:   cloneEntryPtr(&entry),
		Lane:    laneFromOrigin(entry.Origin),
		Planned: true,
	}
}

func (s *Session) touchLocked() {
	s.snapshot = normalizeSnapshot(s.snapshot)
	s.syncAuthorityStateLocked()
	s.snapshot.UpdatedAt = formatTimestamp(time.Now().UTC())
}

func (s *Session) logErrorf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Errorf(format, args...)
	}
}

func (s *Session) logPrintf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}
