package playback

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
)

const (
	positionPollInterval  = 500 * time.Millisecond
	pendingRetryInterval  = time.Second
	pendingRetryTimeout   = 2 * time.Minute
	nextPreparationPoll   = 5 * time.Second
	transportRetryTimeout = 3 * time.Second
	seekObserveTimeout    = 250 * time.Millisecond
	seekObserveInterval   = 10 * time.Millisecond
	seekObserveTolerance  = int64(750)
	trackEndNearEOFWindow = int64(3000)
	availabilityBatchSize = 64
	availabilityCacheTTL  = 2 * time.Second
	preloadAfterPlayedMS  = int64(45_000)
	preloadRemainingMS    = int64(30_000)
)

type Logger interface {
	Printf(format string, args ...any)
	Errorf(format string, args ...any)
}

type Session struct {
	mu       sync.Mutex
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

	eventsWG   sync.WaitGroup
	tickerWG   sync.WaitGroup
	tickStop   chan struct{}
	queueDirty bool

	catalogChangeCancel func()
	catalogRebaseCh     chan struct{}
	catalogRebaseWG     sync.WaitGroup
	intentEpoch         uint64

	loadedEntryID       string
	loadedURI           string
	preloadedID         string
	preloadedURI        string
	backendPreloadArmed bool

	nextPreparationRetryEntryID string
	nextPreparationRetryAt      time.Time
	preloadFailureEntryID       string
	preloadFailureURI           string

	rng *rand.Rand

	pendingCancel context.CancelFunc
	pendingWG     sync.WaitGroup
	pendingEntry  string
	pendingToken  uint64
	transportWG   sync.WaitGroup

	transportToken   uint64
	transportPending *transportTransitionState

	availabilityCache map[string]cachedTargetAvailability
	nextActionPlan    *nextActionPlan
	nextActionBuilds  int64
}

type cachedTargetAvailability struct {
	status    apitypes.RecordingPlaybackAvailability
	expiresAt time.Time
}

type transportTransitionState struct {
	token          uint64
	entry          SessionEntry
	origin         EntryOrigin
	queueIndex     int
	previous       *SessionEntry
	playableURI    string
	activation     BackendActivationRef
	status         apitypes.PlaybackPreparationStatus
	sourceIdentity sourceIdentity
	kind           transportTransitionKind
}

type transportTransitionKind string

const (
	transportTransitionDirectLoad transportTransitionKind = "direct_load"
	transportTransitionPreloaded  transportTransitionKind = "activate_preloaded"
)

type sourceIdentity struct {
	Kind         ContextKind
	ID           string
	RebasePolicy ContextRebasePolicy
	Live         bool
	AnchorMode   string
}

type playbackCandidate struct {
	Entry      SessionEntry
	Origin     EntryOrigin
	QueueIndex int
}

type plannedCandidate struct {
	EntryID      string
	Origin       EntryOrigin
	QueueIndex   int
	ContextIndex int
	Target       PlaybackTargetRef
	RecordingID  string
}

type nextActionPlan struct {
	Version         int64
	QueueVersion    int64
	SourceVersion   int64
	CurrentEntryID  string
	RepeatMode      RepeatMode
	Shuffle         bool
	TransportToken  uint64
	PendingEntryID  string
	PendingOrigin   EntryOrigin
	PendingQueueIdx int
	Candidates      []plannedCandidate
}

type nextActionIterator struct {
	snapshot         SessionSnapshot
	blocked          map[string]struct{}
	emittedRepeatOne bool
	userIndex        int
	contextIndex     int
	contextWrapIndex int
	contextWrapLimit int
	contextWrapping  bool
	shuffleCycle     []int
	shufflePos       int
	shuffleWrapLimit int
	shuffleWrapping  bool
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
		rng:               rand.New(rand.NewSource(time.Now().UTC().UnixNano())),
		availabilityCache: make(map[string]cachedTargetAvailability),
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
		if s.snapshot.CurrentEntry != nil && s.snapshot.PositionCapturedAtMS <= 0 {
			s.snapshot.PositionCapturedAtMS = currentTransportCaptureMS()
		}
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.preloadedID = ""
		s.preloadedURI = ""
		s.backendPreloadArmed = false
		s.mu.Unlock()
	}

	if err := s.restoreSourceBackedContext(childCtx); err != nil {
		s.logErrorf("playback: restore source-backed context failed: %v", err)
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
	if s.core != nil {
		s.mu.Lock()
		if s.catalogRebaseCh == nil {
			s.catalogRebaseCh = make(chan struct{}, 1)
			s.catalogRebaseWG.Add(1)
			go s.runCatalogRebaseWorker(childCtx)
		}
		s.mu.Unlock()
	}
	s.startCatalogSubscription()

	s.publishSnapshot(s.Snapshot())
	return nil
}

func (s *Session) Close() error {
	s.persistFinalSnapshot()

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.catalogChangeCancel != nil {
		s.catalogChangeCancel()
		s.catalogChangeCancel = nil
	}
	s.stopTickerLocked()
	s.cancelPendingRetryLocked()
	backend := s.backend
	s.backend = nil
	s.mu.Unlock()

	s.eventsWG.Wait()
	s.tickerWG.Wait()
	s.pendingWG.Wait()
	s.transportWG.Wait()
	s.catalogRebaseWG.Wait()

	if backend != nil {
		return backend.Close()
	}
	return nil
}

func (s *Session) startCatalogSubscription() {
	if s.core == nil {
		return
	}
	s.mu.Lock()
	if s.catalogChangeCancel != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	cancel := s.core.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		if event.Kind == apitypes.CatalogChangeInvalidateAvailability {
			s.handleAvailabilityChange(event)
			return
		}
		s.requestCatalogRebase(event)
	})
	s.mu.Lock()
	if s.catalogChangeCancel != nil {
		s.mu.Unlock()
		cancel()
		return
	}
	s.catalogChangeCancel = cancel
	s.mu.Unlock()
}

func (s *Session) requestCatalogRebase(event apitypes.CatalogChangeEvent) {
	s.mu.Lock()
	relevant := s.catalogChangeRelevantLocked(event)
	ch := s.catalogRebaseCh
	s.mu.Unlock()
	if !relevant || ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (s *Session) runCatalogRebaseWorker(ctx context.Context) {
	defer s.catalogRebaseWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-s.catalogRebaseCh:
			if !ok {
				return
			}
			s.performCatalogRebase(ctx)
		}
	}
}

func (s *Session) restoreSourceBackedContext(ctx context.Context) error {
	s.mu.Lock()
	queue := cloneContextQueue(s.snapshot.ContextQueue)
	s.mu.Unlock()

	if queue == nil || queue.Source == nil {
		return nil
	}

	resolved, candidates, startIndex, _, err := s.enumerateSourceForContextRebuild(ctx, queue, s.Snapshot().CurrentEntry)
	if err != nil {
		return err
	}
	rebuiltIdentity := sourceIdentityFromRequest(resolved)

	s.mu.Lock()
	if !sourceIdentityEqual(s.currentSourceIdentityLocked(), rebuiltIdentity) {
		s.mu.Unlock()
		return nil
	}
	rebuilt := s.buildSourceContextLocked(resolved, candidates, startIndex)
	if queue.ShuffleSeed != 0 {
		rebuilt.ShuffleSeed = queue.ShuffleSeed
	}
	_, _, _ = s.applyRebuiltSourceContextLocked(rebuilt, rebuiltIdentity, "restore source-backed context", false)
	s.mu.Unlock()
	return nil
}

func (s *Session) handleCatalogChange(event apitypes.CatalogChangeEvent) {
	if event.Kind == apitypes.CatalogChangeInvalidateAvailability {
		s.handleAvailabilityChange(event)
		return
	}
	s.mu.Lock()
	if !s.catalogChangeRelevantLocked(event) {
		s.mu.Unlock()
		return
	}
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	s.performCatalogRebase(ctx)
}

func (s *Session) performCatalogRebase(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	queue := cloneContextQueue(s.snapshot.ContextQueue)
	intentEpoch := s.intentEpoch
	identity := s.currentSourceIdentityLocked()
	s.mu.Unlock()

	if queue == nil || queue.Source == nil || queue.Source.RebasePolicy != ContextRebaseLive {
		return
	}
	current := s.Snapshot().CurrentEntry
	resolved, candidates, startIndex, _, err := s.enumerateSourceForContextRebuild(ctx, queue, current)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logErrorf("playback: live rebase failed: %v", err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	rebuiltIdentity := sourceIdentityFromRequest(resolved)

	s.mu.Lock()
	currentQueue := s.snapshot.ContextQueue
	if currentQueue == nil || currentQueue.Source == nil || currentQueue.Source.RebasePolicy != ContextRebaseLive {
		s.mu.Unlock()
		return
	}
	if s.intentEpoch != intentEpoch {
		s.mu.Unlock()
		return
	}
	if !sourceIdentityEqual(s.currentSourceIdentityLocked(), identity) || !sourceIdentityEqual(identity, rebuiltIdentity) {
		s.mu.Unlock()
		return
	}
	rebuilt := s.buildSourceContextLocked(resolved, candidates, startIndex)
	if queue.ShuffleSeed != 0 {
		rebuilt.ShuffleSeed = queue.ShuffleSeed
	}
	rebuilt.SourceVersion = queue.SourceVersion + 1
	backend, needsClearPreload, state := s.applyRebuiltSourceContextLocked(rebuilt, rebuiltIdentity, "live rebase", true)
	s.mu.Unlock()

	if ctx.Err() != nil {
		return
	}
	if needsClearPreload {
		s.clearBackendPreload(ctx, backend)
	}
	s.publishSnapshot(state)
}

func restoreContextEntryForAnchor(entries []SessionEntry, anchor *PlaybackSourceAnchor) (SessionEntry, bool) {
	if anchor == nil {
		return SessionEntry{}, false
	}
	entryKey := strings.TrimSpace(anchor.EntryKey)
	sourceItemID := strings.TrimSpace(anchor.SourceItemID)
	recordingID := strings.TrimSpace(anchor.RecordingID)
	for _, entry := range entries {
		switch {
		case entryKey != "" && strings.TrimSpace(entry.EntryID) == entryKey:
			return entry, true
		case sourceItemID != "" && strings.TrimSpace(entry.Item.SourceItemID) == sourceItemID:
			return entry, true
		case recordingID != "" && strings.TrimSpace(entry.Item.RecordingID) == recordingID:
			return entry, true
		}
	}
	return SessionEntry{}, false
}

func (s *Session) applyRebuiltSourceContextLocked(
	rebuilt *ContextQueue,
	rebuiltIdentity sourceIdentity,
	reason string,
	forceRecovery bool,
) (Backend, bool, SessionSnapshot) {
	if rebuilt == nil {
		return s.backend, false, snapshotCopyLocked(&s.snapshot)
	}

	previousQueue := s.snapshot.ContextQueue
	previousResumeIndex := -1
	if previousQueue != nil {
		previousResumeIndex = previousQueue.ResumeIndex
	}
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	loading := cloneEntryPtr(s.snapshot.LoadingEntry)
	loadingPreparation := cloneEntryPreparation(s.snapshot.LoadingPreparation)
	currentPreparation := cloneEntryPreparation(s.snapshot.CurrentPreparation)
	backend, needsClearPreload := s.invalidatePreloadPlanLocked(reason, forceRecovery)

	var restoredPending *SessionEntry
	if pending := s.transportPending; pending != nil {
		if pending.origin == EntryOriginContext && sourceIdentityEqual(pending.sourceIdentity, rebuiltIdentity) {
			if entry, ok := restoreContextEntry(rebuilt.allEntries, &pending.entry); ok {
				pending.entry = entry
				pending.sourceIdentity = rebuiltIdentity
				restoredPending = cloneEntryPtr(&entry)
			} else {
				s.cancelTransportTransitionLocked(reason + ": pending target invalid")
			}
		}
	}

	var restoredCurrent *SessionEntry
	if current != nil && current.Origin == EntryOriginContext {
		if entry, ok := restoreContextEntry(rebuilt.allEntries, current); ok {
			restoredCurrent = cloneEntryPtr(&entry)
		}
	}

	var restoredLoading *SessionEntry
	if restoredPending == nil && loading != nil && loading.Origin == EntryOriginContext {
		if entry, ok := restoreContextEntry(rebuilt.allEntries, loading); ok {
			restoredLoading = cloneEntryPtr(&entry)
		}
	}

	s.snapshot.ContextQueue = rebuilt
	if s.snapshot.Shuffle {
		if s.snapshot.ContextQueue.ShuffleSeed == 0 {
			if previousQueue != nil && previousQueue.ShuffleSeed != 0 {
				s.snapshot.ContextQueue.ShuffleSeed = previousQueue.ShuffleSeed
			} else {
				s.snapshot.ContextQueue.ShuffleSeed = s.rng.Uint64()
			}
		}
		s.rebuildShuffleCycleLocked()
	} else {
		clearContextShuffleStateLocked(&s.snapshot)
	}

	currentIndex := -1
	switch {
	case restoredPending != nil:
		currentIndex = restoredPending.ContextIndex
	case restoredCurrent != nil:
		currentIndex = restoredCurrent.ContextIndex
	case restoredLoading != nil:
		currentIndex = restoredLoading.ContextIndex
	case previousResumeIndex >= 0 && previousResumeIndex < len(rebuilt.allEntries):
		currentIndex = previousResumeIndex
	default:
		if anchorEntry, ok := restoreContextEntryForAnchor(rebuilt.allEntries, rebuilt.Anchor); ok {
			currentIndex = anchorEntry.ContextIndex
		}
	}
	if currentIndex < 0 || currentIndex >= len(rebuilt.allEntries) {
		currentIndex = defaultFirstContextIndex(s.snapshot)
	}
	s.setCurrentContextIndexLocked(currentIndex)

	if restoredPending != nil && s.transportPending != nil {
		s.setLoadingLocked(*restoredPending, s.transportPending.status)
	} else if restoredLoading != nil && loadingPreparation != nil {
		s.setLoadingLocked(*restoredLoading, loadingPreparation.Status)
	} else if loading != nil && loading.Origin == EntryOriginContext {
		s.clearLoadingStateLocked()
	}

	if restoredCurrent != nil {
		s.snapshot.CurrentEntryID = restoredCurrent.EntryID
		s.snapshot.CurrentEntry = cloneEntryPtr(restoredCurrent)
		item := restoredCurrent.Item
		s.snapshot.CurrentItem = &item
		if currentPreparation != nil {
			s.snapshot.CurrentPreparation = &EntryPreparation{
				EntryID: restoredCurrent.EntryID,
				Status:  currentPreparation.Status,
			}
		}
	}

	s.recalculateResumeContextIndexLocked()
	s.refreshCurrentContextWindowLocked()
	s.markQueueDirtyLocked()
	s.touchLocked()
	return backend, needsClearPreload, snapshotCopyLocked(&s.snapshot)
}

func (s *Session) enumerateSourceForContextRebuild(
	ctx context.Context,
	queue *ContextQueue,
	current *SessionEntry,
) (PlaybackSourceRequest, []PlaybackSourceCandidate, int, bool, error) {
	if queue == nil || queue.Source == nil {
		return PlaybackSourceRequest{}, nil, 0, false, fmt.Errorf("playback source is not available")
	}

	req := PlaybackSourceRequest{Descriptor: *queue.Source}
	if queue.Anchor != nil {
		req.Anchor = *queue.Anchor
	}
	if current != nil && current.Origin == EntryOriginContext {
		req.Anchor = anchorFromEntry(*current)
	}
	resolved, candidates, startIndex, err := s.enumerateSource(ctx, req)
	if err == nil {
		return resolved, candidates, startIndex, false, nil
	}
	if current == nil || current.Origin != EntryOriginContext || !isMissingSourceAnchorError(err) {
		return PlaybackSourceRequest{}, nil, 0, false, err
	}

	fallbackReq := PlaybackSourceRequest{Descriptor: *queue.Source}
	resolved, candidates, startIndex, err = s.enumerateSource(ctx, fallbackReq)
	if err != nil {
		return PlaybackSourceRequest{}, nil, 0, false, err
	}
	return resolved, candidates, startIndex, true, nil
}

func isMissingSourceAnchorError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "selected item is not present in playback source")
}

func (s *Session) handleAvailabilityChange(event apitypes.CatalogChangeEvent) {
	s.mu.Lock()
	if !s.availabilityChangeRelevantLocked(event) {
		s.mu.Unlock()
		return
	}
	ctx := s.ctx
	backend := s.backend
	needsClearPreload := s.shouldClearBackendPreloadLocked()
	hadEntryAvailability := len(s.snapshot.EntryAvailability) > 0
	hadPreparationState := s.snapshot.NextPreparation != nil ||
		s.preloadedID != "" ||
		s.preloadedURI != "" ||
		s.backendPreloadArmed

	s.availabilityCache = make(map[string]cachedTargetAvailability)
	if hadEntryAvailability {
		s.snapshot.EntryAvailability = nil
		s.markQueueDirtyLocked()
	}
	if hadPreparationState {
		s.clearNextPreparationStateLocked()
	}

	shouldPublish := hadEntryAvailability || hadPreparationState
	var state SessionSnapshot
	if shouldPublish {
		s.touchLocked()
		state = snapshotCopyLocked(&s.snapshot)
	}
	s.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	if needsClearPreload {
		s.clearBackendPreload(ctx, backend)
	}
	if shouldPublish {
		s.publishSnapshot(state)
	}
	s.preloadNext(ctx)
}

func (s *Session) catalogChangeRelevantLocked(event apitypes.CatalogChangeEvent) bool {
	queue := s.snapshot.ContextQueue
	if queue == nil || queue.Source == nil || queue.Source.RebasePolicy != ContextRebaseLive {
		return false
	}
	if event.Kind == apitypes.CatalogChangeInvalidateAvailability {
		return false
	}
	if event.InvalidateAll {
		return true
	}
	switch queue.Source.Kind {
	case ContextKindTracks:
		return event.Entity == apitypes.CatalogChangeEntityTracks
	case ContextKindPlaylist:
		return event.Entity == apitypes.CatalogChangeEntityPlaylistTracks && strings.TrimSpace(event.EntityID) == strings.TrimSpace(queue.Source.ID)
	case ContextKindLiked:
		return event.Entity == apitypes.CatalogChangeEntityLiked
	default:
		return false
	}
}

func (s *Session) availabilityChangeRelevantLocked(event apitypes.CatalogChangeEvent) bool {
	if event.Kind != apitypes.CatalogChangeInvalidateAvailability {
		return false
	}
	if event.InvalidateAll {
		return s.hasAvailabilitySensitiveStateLocked()
	}
	recordingIDs := recordingIDSet(event.RecordingIDs)
	if len(recordingIDs) == 0 {
		return false
	}
	if s.snapshot.CurrentEntry != nil && sessionItemMatchesRecordingIDSet(s.snapshot.CurrentEntry.Item, recordingIDs) {
		return true
	}
	if s.snapshot.LoadingEntry != nil && sessionItemMatchesRecordingIDSet(s.snapshot.LoadingEntry.Item, recordingIDs) {
		return true
	}
	for _, entry := range s.snapshot.UserQueue {
		if sessionItemMatchesRecordingIDSet(entry.Item, recordingIDs) {
			return true
		}
	}
	for _, entry := range contextAllEntries(s.snapshot) {
		if sessionItemMatchesRecordingIDSet(entry.Item, recordingIDs) {
			return true
		}
	}
	return false
}

func (s *Session) hasAvailabilitySensitiveStateLocked() bool {
	return s.snapshot.CurrentEntry != nil ||
		s.snapshot.LoadingEntry != nil ||
		len(s.snapshot.UserQueue) > 0 ||
		len(contextAllEntries(s.snapshot)) > 0 ||
		len(s.snapshot.EntryAvailability) > 0 ||
		s.snapshot.NextPreparation != nil ||
		s.preloadedID != "" ||
		s.preloadedURI != "" ||
		s.backendPreloadArmed
}

func sourceIdentityFromDescriptor(descriptor PlaybackSourceDescriptor, anchor PlaybackSourceAnchor) sourceIdentity {
	anchorMode := ""
	switch {
	case strings.TrimSpace(anchor.EntryKey) != "":
		anchorMode = "entry"
	case strings.TrimSpace(anchor.SourceItemID) != "":
		anchorMode = "source_item"
	case strings.TrimSpace(anchor.RecordingID) != "":
		anchorMode = "recording"
	}
	return sourceIdentity{
		Kind:         descriptor.Kind,
		ID:           strings.TrimSpace(descriptor.ID),
		RebasePolicy: descriptor.RebasePolicy,
		Live:         descriptor.Live,
		AnchorMode:   anchorMode,
	}
}

func sourceIdentityFromRequest(req PlaybackSourceRequest) sourceIdentity {
	return sourceIdentityFromDescriptor(req.Descriptor, req.Anchor)
}

func sourceIdentityFromContextQueue(queue *ContextQueue) sourceIdentity {
	if queue == nil || queue.Source == nil {
		return sourceIdentity{}
	}
	anchor := PlaybackSourceAnchor{}
	if queue.Anchor != nil {
		anchor = *queue.Anchor
	}
	return sourceIdentityFromDescriptor(*queue.Source, anchor)
}

func sourceIdentityEqual(left, right sourceIdentity) bool {
	return left.Kind == right.Kind &&
		left.RebasePolicy == right.RebasePolicy &&
		left.Live == right.Live &&
		left.AnchorMode == right.AnchorMode &&
		strings.TrimSpace(left.ID) == strings.TrimSpace(right.ID)
}

func (s *Session) currentSourceIdentityLocked() sourceIdentity {
	return sourceIdentityFromContextQueue(s.snapshot.ContextQueue)
}

func (s *Session) sourceIdentityForEntryLocked(entry SessionEntry) sourceIdentity {
	if entry.Origin != EntryOriginContext {
		return sourceIdentity{}
	}
	return s.currentSourceIdentityLocked()
}

func (s *Session) advanceIntentEpochLocked(reason string) {
	s.intentEpoch++
	if reason != "" {
		s.logPrintf("playback: advancing intent epoch (%s) -> %d", reason, s.intentEpoch)
	}
}

func (s *Session) cancelTransportTransitionLocked(reason string) bool {
	if s.transportPending == nil {
		return false
	}
	if reason != "" {
		s.logPrintf("playback: cancelling transport transition (%s)", reason)
	}
	s.transportPending = nil
	s.transportToken++
	s.clearLoadingStateLocked()
	s.invalidateNextActionPlanLocked(reason)
	return true
}

func (s *Session) invalidatePreloadPlanLocked(reason string, forceRecovery bool) (Backend, bool) {
	backend := s.backend
	needsClearPreload := s.shouldClearBackendPreloadLocked()
	if !needsClearPreload && forceRecovery && backend != nil && backend.SupportsPreload() && s.snapshot.CurrentEntry != nil && s.loadedURI != "" {
		needsClearPreload = true
	}
	if needsClearPreload && reason != "" {
		s.logPrintf("playback: invalidating preload plan (%s)", reason)
	}
	s.clearNextPreparationStateLocked()
	s.availabilityCache = make(map[string]cachedTargetAvailability)
	s.invalidateNextActionPlanLocked(reason)
	return backend, needsClearPreload
}

func (s *Session) transportTransitionInvalidatedByQueueMutationLocked(
	changedEntryIDs map[string]struct{},
	clearContext bool,
	clearUserQueue bool,
	nextSource sourceIdentity,
	invalidateQueueIndex bool,
) bool {
	pending := s.transportPending
	if pending == nil {
		return false
	}
	if len(changedEntryIDs) > 0 {
		if _, ok := changedEntryIDs[pending.entry.EntryID]; ok {
			return true
		}
	}
	if clearUserQueue && pending.origin == EntryOriginQueued {
		return true
	}
	if clearContext && pending.origin == EntryOriginContext {
		return true
	}
	if invalidateQueueIndex && pending.origin == EntryOriginQueued {
		return true
	}
	if pending.origin == EntryOriginContext && nextSource != (sourceIdentity{}) && !sourceIdentityEqual(pending.sourceIdentity, nextSource) {
		return true
	}
	return false
}

func anchorFromEntry(entry SessionEntry) PlaybackSourceAnchor {
	return PlaybackSourceAnchor{
		EntryKey:     strings.TrimSpace(entry.EntryID),
		SourceItemID: strings.TrimSpace(entry.Item.SourceItemID),
	}
}

func restoreContextIndex(entries []SessionEntry, preferred *SessionEntry, fallbackIndex int, defaultIndex int) int {
	if entry, ok := restoreContextEntry(entries, preferred); ok {
		return entry.ContextIndex
	}
	if fallbackIndex >= 0 && fallbackIndex < len(entries) {
		return fallbackIndex
	}
	if defaultIndex >= 0 && defaultIndex < len(entries) {
		return defaultIndex
	}
	if len(entries) == 0 {
		return -1
	}
	return 0
}

func restoreContextEntry(entries []SessionEntry, preferred *SessionEntry) (SessionEntry, bool) {
	if preferred != nil {
		for index, entry := range entries {
			if entry.EntryID == preferred.EntryID {
				return entries[index], true
			}
			if entry.Item.SourceItemID != "" && entry.Item.SourceItemID == preferred.Item.SourceItemID {
				return entries[index], true
			}
		}
	}
	return SessionEntry{}, false
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

func (s *Session) planningSnapshotLocked() SessionSnapshot {
	snapshot := SessionSnapshot{
		ContextQueue: s.snapshot.ContextQueue,
		UserQueue:    s.snapshot.UserQueue,
		CurrentEntry: s.snapshot.CurrentEntry,
		RepeatMode:   s.snapshot.RepeatMode,
		Shuffle:      s.snapshot.Shuffle,
	}
	if snapshot.CurrentEntry != nil {
		snapshot.CurrentEntryID = snapshot.CurrentEntry.EntryID
	}
	if pending := s.transportPending; pending != nil {
		snapshot.CurrentEntry = cloneEntryPtr(&pending.entry)
		snapshot.CurrentEntryID = pending.entry.EntryID
		if pending.origin == EntryOriginContext && snapshot.ContextQueue != nil {
			snapshot.ContextQueue.CurrentIndex = pending.entry.ContextIndex
		}
		if pending.origin == EntryOriginQueued &&
			pending.queueIndex >= 0 &&
			pending.queueIndex < len(snapshot.UserQueue) &&
			snapshot.UserQueue[pending.queueIndex].EntryID == pending.entry.EntryID {
			snapshot.UserQueue = append(snapshot.UserQueue[:pending.queueIndex], snapshot.UserQueue[pending.queueIndex+1:]...)
		}
	}
	return snapshot
}

func (s *Session) invalidateNextActionPlanLocked(reason string) {
	if s.nextActionPlan == nil {
		return
	}
	if reason != "" {
		s.logPrintf("playback: invalidating next-action plan (%s)", reason)
	}
	s.nextActionPlan = nil
}

func (s *Session) currentNextActionPlanKeyLocked() (int64, int64, string, RepeatMode, bool, uint64, string, EntryOrigin, int) {
	sourceVersion := int64(0)
	if s.snapshot.ContextQueue != nil {
		sourceVersion = s.snapshot.ContextQueue.SourceVersion
	}
	queueVersion := s.snapshot.QueueVersion
	if s.queueDirty {
		queueVersion++
	}
	currentEntryID := s.snapshot.CurrentEntryID
	pendingEntryID := ""
	pendingOrigin := EntryOrigin("")
	pendingQueueIdx := -1
	if pending := s.transportPending; pending != nil {
		currentEntryID = pending.entry.EntryID
		pendingEntryID = pending.entry.EntryID
		pendingOrigin = pending.origin
		pendingQueueIdx = pending.queueIndex
	}
	return queueVersion, sourceVersion, currentEntryID, s.snapshot.RepeatMode, s.snapshot.Shuffle, s.transportToken, pendingEntryID, pendingOrigin, pendingQueueIdx
}

func (s *Session) ensureNextActionPlanLocked() *nextActionPlan {
	queueVersion, sourceVersion, currentEntryID, repeatMode, shuffle, transportToken, pendingEntryID, pendingOrigin, pendingQueueIdx := s.currentNextActionPlanKeyLocked()
	if plan := s.nextActionPlan; plan != nil &&
		plan.QueueVersion == queueVersion &&
		plan.SourceVersion == sourceVersion &&
		plan.CurrentEntryID == currentEntryID &&
		plan.RepeatMode == repeatMode &&
		plan.Shuffle == shuffle &&
		plan.TransportToken == transportToken &&
		plan.PendingEntryID == pendingEntryID &&
		plan.PendingOrigin == pendingOrigin &&
		plan.PendingQueueIdx == pendingQueueIdx {
		return plan
	}

	planningSnapshot := s.planningSnapshotLocked()
	iterator := newNextActionIterator(planningSnapshot, nil)
	candidates := make([]plannedCandidate, 0, len(planningSnapshot.UserQueue)+len(contextAllEntries(planningSnapshot))+1)
	for {
		candidate, ok := iterator.Next()
		if !ok {
			break
		}
		candidates = append(candidates, plannedCandidate{
			EntryID:      candidate.Entry.EntryID,
			Origin:       candidate.Origin,
			QueueIndex:   candidate.QueueIndex,
			ContextIndex: candidate.Entry.ContextIndex,
			Target:       candidate.Entry.Item.Target,
			RecordingID:  candidate.Entry.Item.RecordingID,
		})
	}

	s.nextActionBuilds++
	s.nextActionPlan = &nextActionPlan{
		Version:         s.nextActionBuilds,
		QueueVersion:    queueVersion,
		SourceVersion:   sourceVersion,
		CurrentEntryID:  currentEntryID,
		RepeatMode:      repeatMode,
		Shuffle:         shuffle,
		TransportToken:  transportToken,
		PendingEntryID:  pendingEntryID,
		PendingOrigin:   pendingOrigin,
		PendingQueueIdx: pendingQueueIdx,
		Candidates:      candidates,
	}
	return s.nextActionPlan
}

func (s *Session) playbackCandidateFromPlannedLocked(candidate plannedCandidate) (playbackCandidate, bool) {
	if strings.TrimSpace(candidate.EntryID) == "" {
		return playbackCandidate{}, false
	}

	if s.snapshot.CurrentEntry != nil && s.snapshot.CurrentEntry.EntryID == candidate.EntryID {
		return playbackCandidate{
			Entry:      *cloneEntryPtr(s.snapshot.CurrentEntry),
			Origin:     s.snapshot.CurrentEntry.Origin,
			QueueIndex: -1,
		}, true
	}
	if pending := s.transportPending; pending != nil && pending.entry.EntryID == candidate.EntryID {
		return playbackCandidate{
			Entry:      pending.entry,
			Origin:     pending.origin,
			QueueIndex: pending.queueIndex,
		}, true
	}

	switch candidate.Origin {
	case EntryOriginQueued:
		if candidate.QueueIndex >= 0 &&
			candidate.QueueIndex < len(s.snapshot.UserQueue) &&
			s.snapshot.UserQueue[candidate.QueueIndex].EntryID == candidate.EntryID {
			return playbackCandidate{
				Entry:      s.snapshot.UserQueue[candidate.QueueIndex],
				Origin:     EntryOriginQueued,
				QueueIndex: candidate.QueueIndex,
			}, true
		}
	case EntryOriginContext:
		allEntries := contextAllEntries(s.snapshot)
		if candidate.ContextIndex >= 0 &&
			candidate.ContextIndex < len(allEntries) &&
			allEntries[candidate.ContextIndex].EntryID == candidate.EntryID {
			return playbackCandidate{
				Entry:      allEntries[candidate.ContextIndex],
				Origin:     EntryOriginContext,
				QueueIndex: -1,
			}, true
		}
	}

	entry, ok := findEntryByID(s.snapshot, candidate.EntryID)
	if !ok {
		return playbackCandidate{}, false
	}
	origin := entry.Origin
	queueIndex := -1
	if origin == EntryOriginQueued {
		queueIndex = indexOfEntryID(s.snapshot.UserQueue, entry.EntryID)
	}
	return playbackCandidate{
		Entry:      entry,
		Origin:     origin,
		QueueIndex: queueIndex,
	}, true
}

func (s *Session) SetSource(ctx context.Context, req PlaybackSourceRequest, shuffle bool) (SessionSnapshot, error) {
	s.mu.Lock()
	s.advanceIntentEpochLocked("set source")
	intentEpoch := s.intentEpoch
	s.mu.Unlock()

	resolved, candidates, startIndex, err := s.enumerateSource(ctx, req)
	if err != nil {
		return s.Snapshot(), err
	}

	s.mu.Lock()
	if s.intentEpoch != intentEpoch {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}
	contextData := s.buildSourceContextLocked(resolved, candidates, startIndex)
	s.cancelTransportTransitionLocked("set source")
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("set source", true)
	s.snapshot.ContextQueue = contextData
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.detachCurrentContextLocked()
	s.snapshot.Shuffle = shuffle
	s.setCurrentContextIndexLocked(startIndex)
	s.setResumeContextIndexLocked(startIndex)
	if s.snapshot.Shuffle {
		s.ensureContextShuffleSeedLocked()
		s.rebuildShuffleCycleLocked()
	} else {
		clearContextShuffleStateLocked(&s.snapshot)
	}
	s.markQueueDirtyLocked()

	if s.snapshot.CurrentEntry == nil {
		s.clearAuthoritativePositionLocked()
		s.snapshot.DurationMS = nil
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
		s.loadedEntryID = ""
		s.loadedURI = ""
		if len(contextAllEntries(s.snapshot)) == 0 {
			s.clearCurrentLocked()
			s.snapshot.Status = StatusIdle
			s.setResumeContextIndexLocked(-1)
		} else {
			s.clearCurrentLocked()
			s.snapshot.Status = StatusPaused
		}
	} else if len(contextAllEntries(s.snapshot)) == 0 {
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

func (s *Session) ReplaceSourceAndPlay(ctx context.Context, req PlaybackSourceRequest, shuffle bool) (SessionSnapshot, error) {
	s.mu.Lock()
	s.advanceIntentEpochLocked("replace source and play")
	intentEpoch := s.intentEpoch
	s.mu.Unlock()

	resolved, candidates, startIndex, err := s.enumerateSource(ctx, req)
	if err != nil {
		return s.Snapshot(), err
	}

	s.mu.Lock()
	if s.intentEpoch != intentEpoch {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, nil
	}
	contextData := s.buildSourceContextLocked(resolved, candidates, startIndex)
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	s.cancelTransportTransitionLocked("replace source and play")
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("replace source and play", true)

	s.snapshot.ContextQueue = contextData
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.snapshot.Shuffle = shuffle
	s.setCurrentContextIndexLocked(startIndex)
	s.setResumeContextIndexLocked(startIndex)
	if s.snapshot.Shuffle {
		s.ensureContextShuffleSeedLocked()
		s.rebuildShuffleCycleLocked()
	} else {
		clearContextShuffleStateLocked(&s.snapshot)
	}
	s.markQueueDirtyLocked()

	if current == nil {
		s.clearAuthoritativePositionLocked()
		s.snapshot.DurationMS = nil
		s.snapshot.CurrentSourceKind = ""
		s.snapshot.CurrentPreparation = nil
		s.loadedEntryID = ""
		s.loadedURI = ""
		s.clearCurrentLocked()
		if len(contextAllEntries(s.snapshot)) == 0 {
			s.snapshot.Status = StatusIdle
			s.setResumeContextIndexLocked(-1)
		} else {
			s.snapshot.Status = StatusPaused
		}
	}

	var target SessionEntry
	hasTarget := len(contextAllEntries(s.snapshot)) > 0
	if hasTarget {
		target = contextEntryAt(s.snapshot, startIndex)
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

func (s *Session) SetContext(input PlaybackContextInput) (SessionSnapshot, error) {
	s.mu.Lock()
	contextData, startIndex, err := s.buildContextLocked(input)
	if err != nil {
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		return state, err
	}

	s.advanceIntentEpochLocked("set context")
	s.cancelTransportTransitionLocked("set context")
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("set context", true)
	s.snapshot.ContextQueue = contextData
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.detachCurrentContextLocked()
	s.setCurrentContextIndexLocked(startIndex)
	s.setResumeContextIndexLocked(startIndex)
	if s.snapshot.Shuffle {
		s.rebuildShuffleCycleLocked()
	}
	s.markQueueDirtyLocked()

	if s.snapshot.CurrentEntry == nil {
		s.clearAuthoritativePositionLocked()
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

	s.advanceIntentEpochLocked("replace context and play")
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	s.cancelTransportTransitionLocked("replace context and play")
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("replace context and play", true)

	s.snapshot.ContextQueue = contextData
	s.snapshot.UserQueue = nil
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.setCurrentContextIndexLocked(startIndex)
	s.setResumeContextIndexLocked(startIndex)
	if s.snapshot.Shuffle {
		s.rebuildShuffleCycleLocked()
	}

	if current == nil {
		s.clearAuthoritativePositionLocked()
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
	s.advanceIntentEpochLocked("queue items")
	insertMode := mode
	if insertMode == "" {
		insertMode = QueueInsertLast
	}
	cancelTransport := s.transportTransitionInvalidatedByQueueMutationLocked(
		nil,
		false,
		false,
		sourceIdentity{},
		insertMode == QueueInsertNext,
	)
	if cancelTransport {
		s.cancelTransportTransitionLocked("queue items")
	}
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("queue items", true)
	entries := make([]SessionEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, SessionEntry{
			EntryID:      s.nextEntryIDLocked("queued"),
			Origin:       EntryOriginQueued,
			ContextIndex: -1,
			Item:         item,
		})
	}

	if insertMode == QueueInsertNext {
		s.snapshot.UserQueue = append(entries, s.snapshot.UserQueue...)
	} else {
		s.snapshot.UserQueue = append(s.snapshot.UserQueue, entries...)
	}
	s.markQueueDirtyLocked()

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
	s.advanceIntentEpochLocked("remove queued entry")
	pendingQueueIndexInvalid := s.transportPending != nil &&
		s.transportPending.origin == EntryOriginQueued &&
		index >= 0 &&
		index <= s.transportPending.queueIndex
	if s.transportTransitionInvalidatedByQueueMutationLocked(
		map[string]struct{}{strings.TrimSpace(entryID): struct{}{}},
		false,
		false,
		sourceIdentity{},
		pendingQueueIndexInvalid,
	) {
		s.cancelTransportTransitionLocked("remove queued entry")
	}
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("remove queued entry", true)
	s.snapshot.UserQueue = append(s.snapshot.UserQueue[:index], s.snapshot.UserQueue[index+1:]...)
	s.markQueueDirtyLocked()
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

	s.advanceIntentEpochLocked("move queued entry")
	pendingQueueIndexInvalid := false
	if pending := s.transportPending; pending != nil && pending.origin == EntryOriginQueued && pending.queueIndex >= 0 {
		switch {
		case pending.entry.EntryID == strings.TrimSpace(entryID):
			pendingQueueIndexInvalid = true
		case fromIndex < pending.queueIndex && toIndex >= pending.queueIndex:
			pendingQueueIndexInvalid = true
		case fromIndex > pending.queueIndex && toIndex <= pending.queueIndex:
			pendingQueueIndexInvalid = true
		}
	}
	if s.transportTransitionInvalidatedByQueueMutationLocked(
		map[string]struct{}{strings.TrimSpace(entryID): struct{}{}},
		false,
		false,
		sourceIdentity{},
		pendingQueueIndexInvalid,
	) {
		s.cancelTransportTransitionLocked("move queued entry")
	}
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("move queued entry", true)
	entry := s.snapshot.UserQueue[fromIndex]
	queue := append([]SessionEntry(nil), s.snapshot.UserQueue...)
	queue = append(queue[:fromIndex], queue[fromIndex+1:]...)
	queue = append(queue[:toIndex], append([]SessionEntry{entry}, queue[toIndex:]...)...)
	s.snapshot.UserQueue = queue
	s.markQueueDirtyLocked()
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
	s.advanceIntentEpochLocked("clear queue")
	if s.transportTransitionInvalidatedByQueueMutationLocked(nil, true, true, sourceIdentity{}, true) {
		s.cancelTransportTransitionLocked("clear queue")
	}
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("clear queue", true)
	s.snapshot.ContextQueue = nil
	s.snapshot.UserQueue = nil
	s.markQueueDirtyLocked()
	s.snapshot.LastError = ""
	s.clearLoadingStateLocked()
	s.cancelPendingRetryLocked()
	s.setResumeContextIndexLocked(-1)
	shouldStopBackend := !hasCurrent
	if !hasCurrent {
		s.clearAuthoritativePositionLocked()
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
		s.mu.Unlock()

		if backend != nil {
			if err := backend.Play(ctx); err != nil {
				return s.failPlayback(err)
			}
			s.refreshPosition("play_resume")
		}

		s.mu.Lock()
		s.snapshot.Status = StatusPlaying
		s.snapshot.LastError = ""
		if s.snapshot.PositionCapturedAtMS <= 0 {
			s.snapshot.PositionCapturedAtMS = currentTransportCaptureMS()
		}
		s.touchLocked()
		s.ensureTickerLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()

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
	s.stopTickerLocked()
	s.mu.Unlock()

	if backend != nil {
		if err := backend.Pause(ctx); err != nil {
			return s.failPlayback(err)
		}
		s.refreshPosition("pause")
	}

	s.mu.Lock()
	s.snapshot.Status = StatusPaused
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.publishSnapshot(state)
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
	s.selectMu.Lock()
	defer s.selectMu.Unlock()

	s.mu.Lock()
	pending := s.transportPending
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	if pending != nil {
		current = cloneEntryPtr(&pending.entry)
	}
	if current != nil && s.snapshot.RepeatMode == RepeatOne {
		s.mu.Unlock()
		return s.SeekTo(ctx, 0)
	}
	s.mu.Unlock()
	return s.playNextAvailable(ctx, current, false, true, nil, nil, true)
}

func (s *Session) Previous(ctx context.Context) (SessionSnapshot, error) {
	s.selectMu.Lock()
	defer s.selectMu.Unlock()

	s.mu.Lock()
	pending := s.transportPending
	if s.snapshot.PositionMS > 3000 {
		s.mu.Unlock()
		return s.SeekTo(ctx, 0)
	}
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	if pending != nil {
		current = cloneEntryPtr(&pending.entry)
	}
	s.mu.Unlock()

	if current == nil {
		return s.Snapshot(), nil
	}
	return s.playPreviousAvailable(ctx, current, false, true)
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
	backend := s.backend
	loaded := s.loadedEntryID == s.snapshot.CurrentEntryID && s.loadedURI != ""
	stateBefore := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	trace := NewDebugTraceEntry("session.seek.called", &stateBefore)
	trace.TargetPositionMS = &positionMS
	if loaded {
		trace.Reason = "loaded"
	} else {
		trace.Reason = "local_only"
	}
	RecordDebugTrace(trace)

	if loaded && backend != nil {
		if err := backend.SeekTo(ctx, positionMS); err != nil {
			return s.failPlayback(err)
		}
		completed := NewDebugTraceEntry("session.seek.backend_complete", &stateBefore)
		completed.TargetPositionMS = &positionMS
		RecordDebugTrace(completed)
		s.refreshPositionAfterSeek(ctx, positionMS)
		s.mu.Lock()
		s.touchLocked()
		state := snapshotCopyLocked(&s.snapshot)
		s.mu.Unlock()
		s.publishSnapshot(state)
		return state, nil
	}

	s.mu.Lock()
	s.setAuthoritativePositionLocked(positionMS)
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

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
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("set repeat mode", true)
	previousResumeIndex := -1
	if s.snapshot.ContextQueue != nil {
		previousResumeIndex = s.snapshot.ContextQueue.ResumeIndex
	}
	s.snapshot.RepeatMode = repeatMode
	s.recalculateResumeContextIndexLocked()
	if s.snapshot.ContextQueue != nil && s.snapshot.ContextQueue.ResumeIndex != previousResumeIndex {
		s.markQueueDirtyLocked()
	}
	s.invalidateNextActionPlanLocked("set repeat mode")
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
	wasShuffled := s.snapshot.Shuffle
	s.advanceIntentEpochLocked("set shuffle")
	if s.transportPending != nil {
		s.cancelTransportTransitionLocked("set shuffle")
	}
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("set shuffle", true)
	s.snapshot.Shuffle = enabled
	if enabled {
		if !wasShuffled && s.snapshot.ContextQueue != nil {
			s.snapshot.ContextQueue.ShuffleSeed = s.rng.Uint64()
		}
		s.rebuildShuffleCycleLocked()
	} else {
		clearContextShuffleStateLocked(&s.snapshot)
	}
	s.recalculateResumeContextIndexLocked()
	s.markQueueDirtyLocked()
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

func (s *Session) playPreviousAvailable(
	ctx context.Context,
	previous *SessionEntry,
	keepPosition bool,
	preserveCurrentOnPending bool,
) (SessionSnapshot, error) {
	var skipped []skippedPlaybackEntry
	blockedEntries := make(map[string]struct{})

	for {
		candidate, unavailable, err := s.resolvePreviousPlayableCandidate(ctx, blockedEntries)
		if err != nil {
			return s.Snapshot(), err
		}
		skipped = append(skipped, unavailable...)
		if candidate == nil {
			if len(skipped) > 0 {
				return s.settleAfterSkipped(skipped), nil
			}
			if previous == nil {
				return s.Snapshot(), nil
			}
			return s.playEntry(ctx, *previous, previous.Origin, -1, nil, keepPosition, preserveCurrentOnPending)
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
	return s.resolvePlayableFromPlan(ctx, blocked)
}

func (s *Session) resolvePreviousPlayableCandidate(
	ctx context.Context,
	blocked map[string]struct{},
) (*playbackCandidate, []skippedPlaybackEntry, error) {
	s.mu.Lock()
	snapshot := s.planningSnapshotLocked()
	core := s.core
	sourceVersion := int64(0)
	if snapshot.ContextQueue != nil {
		sourceVersion = snapshot.ContextQueue.SourceVersion
	}
	candidates := buildPreviousActionPlan(snapshot)
	s.mu.Unlock()

	if len(candidates) == 0 {
		return nil, nil, nil
	}

	skipped := make([]skippedPlaybackEntry, 0, len(candidates))
	now := time.Now()
	for _, candidate := range candidates {
		if len(blocked) > 0 {
			if _, ok := blocked[candidate.EntryID]; ok {
				continue
			}
		}

		var availability apitypes.RecordingPlaybackAvailability
		knownAvailability := false
		if core != nil {
			if cached, ok := s.cachedAvailabilityLocked(candidate.Target, now, sourceVersion); ok {
				availability = cached
				knownAvailability = true
			} else if playbackTargetKey(candidate.Target) != "" {
				status, err := core.GetPlaybackTargetAvailability(ctx, candidate.Target, s.preferredProfile)
				if err != nil {
					return nil, skipped, err
				}
				availability = status
				knownAvailability = true
				s.cacheAvailabilityLocked(candidate.Target, status, now.Add(availabilityCacheTTL), sourceVersion)
			}
		}

		s.mu.Lock()
		resolved, ok := s.playbackCandidateFromPlannedLocked(candidate)
		if ok && knownAvailability {
			s.setEntryAvailabilityLocked(candidate.EntryID, availability)
		}
		s.mu.Unlock()
		if !ok {
			continue
		}
		if knownAvailability && isAvailabilityDefinitivelyUnavailable(availability.State) {
			skipped = append(skipped, skippedPlaybackEntryFromAvailability(resolved.Entry, availability))
			continue
		}
		return &resolved, skipped, nil
	}
	return nil, skipped, nil
}

func (s *Session) resolvePlayableFromPlan(
	ctx context.Context,
	blocked map[string]struct{},
) (*playbackCandidate, []skippedPlaybackEntry, error) {
	for attempt := 0; attempt < 2; attempt++ {
		s.mu.Lock()
		plan := s.ensureNextActionPlanLocked()
		core := s.core
		sourceVersion := plan.SourceVersion
		planned := append([]plannedCandidate(nil), plan.Candidates...)
		s.mu.Unlock()

		if core == nil {
			for _, candidate := range planned {
				if len(blocked) > 0 {
					if _, ok := blocked[candidate.EntryID]; ok {
						continue
					}
				}
				s.mu.Lock()
				resolved, ok := s.playbackCandidateFromPlannedLocked(candidate)
				s.mu.Unlock()
				if ok {
					return &resolved, nil, nil
				}
			}
			return nil, nil, nil
		}

		skipped := make([]skippedPlaybackEntry, 0, availabilityBatchSize)
		stalePlan := false
		batch := make([]plannedCandidate, 0, availabilityBatchSize)
		for _, candidate := range planned {
			if len(blocked) > 0 {
				if _, ok := blocked[candidate.EntryID]; ok {
					continue
				}
			}
			batch = append(batch, candidate)
			if len(batch) < availabilityBatchSize {
				continue
			}
			resolved, batchSkipped, stale := s.resolvePlayableCandidateBatch(ctx, core, batch, sourceVersion)
			skipped = append(skipped, batchSkipped...)
			if resolved != nil {
				return resolved, skipped, nil
			}
			if stale {
				stalePlan = true
				break
			}
			batch = batch[:0]
		}
		if stalePlan {
			s.mu.Lock()
			s.invalidateNextActionPlanLocked("stale candidate resolution")
			s.mu.Unlock()
			continue
		}
		if len(batch) > 0 {
			resolved, batchSkipped, stale := s.resolvePlayableCandidateBatch(ctx, core, batch, sourceVersion)
			skipped = append(skipped, batchSkipped...)
			if resolved != nil {
				return resolved, skipped, nil
			}
			if stale {
				s.mu.Lock()
				s.invalidateNextActionPlanLocked("stale candidate resolution")
				s.mu.Unlock()
				continue
			}
		}
		return nil, skipped, nil
	}
	return nil, nil, nil
}

func (s *Session) resolvePlayableCandidateBatch(
	ctx context.Context,
	core PlaybackCore,
	candidates []plannedCandidate,
	sourceVersion int64,
) (*playbackCandidate, []skippedPlaybackEntry, bool) {
	if len(candidates) == 0 {
		return nil, nil, false
	}

	now := time.Now()
	targets := make([]PlaybackTargetRef, 0, len(candidates))
	seenTargets := make(map[string]struct{}, len(candidates))
	availabilityByTargetKey := make(map[string]apitypes.RecordingPlaybackAvailability, len(candidates))
	for _, candidate := range candidates {
		key := playbackTargetKey(candidate.Target)
		if key == "" {
			continue
		}
		if cached, ok := s.cachedAvailabilityLocked(candidate.Target, now, sourceVersion); ok {
			availabilityByTargetKey[key] = cached
			continue
		}
		if _, ok := seenTargets[key]; ok {
			continue
		}
		seenTargets[key] = struct{}{}
		targets = append(targets, candidate.Target)
	}

	if len(targets) > 0 {
		items, err := core.ListPlaybackTargetAvailability(ctx, TargetAvailabilityRequest{
			Targets:          targets,
			PreferredProfile: s.preferredProfile,
		})
		if err != nil {
			return firstUncheckedCandidate(s, candidates, availabilityByTargetKey)
		}

		for _, item := range items {
			key := playbackTargetKey(item.Target)
			if key == "" {
				continue
			}
			availabilityByTargetKey[key] = item.Status
			s.cacheAvailabilityLocked(item.Target, item.Status, now.Add(availabilityCacheTTL), sourceVersion)
		}
	}

	s.mu.Lock()
	for _, candidate := range candidates {
		status, ok := availabilityByTargetKey[playbackTargetKey(candidate.Target)]
		if !ok {
			continue
		}
		s.setEntryAvailabilityLocked(candidate.EntryID, status)
	}
	s.mu.Unlock()

	return firstUncheckedCandidate(s, candidates, availabilityByTargetKey)
}

func newNextActionIterator(snapshot SessionSnapshot, blocked map[string]struct{}) *nextActionIterator {
	iterator := &nextActionIterator{
		snapshot: snapshot,
		blocked:  blocked,
	}
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 {
		return iterator
	}
	if snapshot.Shuffle {
		cycle := effectiveShuffleCycle(snapshot)
		iterator.shuffleCycle = cycle
		if len(cycle) == 0 {
			return iterator
		}
		position := 0
		wrapLimit := 0
		if currentMatchesContext(snapshot) && currentContextIndex(snapshot) >= 0 {
			currentPosition := indexOfInt(cycle, currentContextIndex(snapshot))
			if currentPosition >= 0 {
				position = currentPosition + 1
				if position > len(cycle) {
					position = len(cycle)
				}
				if snapshot.RepeatMode == RepeatAll {
					wrapLimit = currentPosition
				}
			}
		} else if resumeIndex := resumeContextIndex(snapshot); resumeIndex >= 0 && resumeIndex < len(allEntries) {
			resumePosition := indexOfInt(cycle, resumeIndex)
			if resumePosition >= 0 {
				position = resumePosition
				if snapshot.RepeatMode == RepeatAll {
					wrapLimit = resumePosition
				}
			}
		}
		iterator.shufflePos = position
		iterator.shuffleWrapLimit = wrapLimit
		return iterator
	}

	start := firstUpcomingContextIndex(snapshot)
	if start < 0 {
		start = len(allEntries)
	}
	iterator.contextIndex = start
	if snapshot.RepeatMode == RepeatAll {
		if currentMatchesContext(snapshot) && currentContextIndex(snapshot) >= 0 {
			iterator.contextWrapLimit = currentContextIndex(snapshot)
		} else if !currentMatchesContext(snapshot) && start > 0 {
			iterator.contextWrapLimit = start
		}
	}
	return iterator
}

func (i *nextActionIterator) NextBatch(limit int) []playbackCandidate {
	if limit <= 0 {
		return nil
	}
	out := make([]playbackCandidate, 0, limit)
	for len(out) < limit {
		candidate, ok := i.Next()
		if !ok {
			break
		}
		out = append(out, *candidate)
	}
	return out
}

func (i *nextActionIterator) Next() (*playbackCandidate, bool) {
	for {
		if !i.emittedRepeatOne {
			i.emittedRepeatOne = true
			if i.snapshot.RepeatMode == RepeatOne && i.snapshot.CurrentEntry != nil {
				if i.isBlocked(i.snapshot.CurrentEntry.EntryID) {
					continue
				}
				entry := *cloneEntryPtr(i.snapshot.CurrentEntry)
				return &playbackCandidate{
					Entry:      entry,
					Origin:     entry.Origin,
					QueueIndex: -1,
				}, true
			}
		}

		if i.userIndex < len(i.snapshot.UserQueue) {
			entry := i.snapshot.UserQueue[i.userIndex]
			queueIndex := i.userIndex
			i.userIndex++
			if i.isBlocked(entry.EntryID) {
				continue
			}
			return &playbackCandidate{
				Entry:      entry,
				Origin:     EntryOriginQueued,
				QueueIndex: queueIndex,
			}, true
		}

		if candidate, ok := i.nextContextCandidate(); ok {
			if i.isBlocked(candidate.Entry.EntryID) {
				continue
			}
			return candidate, true
		}
		return nil, false
	}
}

func (i *nextActionIterator) nextContextCandidate() (*playbackCandidate, bool) {
	allEntries := contextAllEntries(i.snapshot)
	if len(allEntries) == 0 {
		return nil, false
	}
	if i.snapshot.Shuffle {
		if !i.shuffleWrapping {
			if i.shufflePos < len(i.shuffleCycle) {
				entry := allEntries[i.shuffleCycle[i.shufflePos]]
				i.shufflePos++
				return &playbackCandidate{Entry: entry, Origin: EntryOriginContext, QueueIndex: -1}, true
			}
			if i.shuffleWrapLimit <= 0 {
				return nil, false
			}
			i.shuffleWrapping = true
			i.shufflePos = 0
		}
		if i.shufflePos >= i.shuffleWrapLimit {
			return nil, false
		}
		entry := allEntries[i.shuffleCycle[i.shufflePos]]
		i.shufflePos++
		return &playbackCandidate{Entry: entry, Origin: EntryOriginContext, QueueIndex: -1}, true
	}

	if !i.contextWrapping {
		if i.contextIndex < len(allEntries) {
			entry := allEntries[i.contextIndex]
			i.contextIndex++
			return &playbackCandidate{Entry: entry, Origin: EntryOriginContext, QueueIndex: -1}, true
		}
		if i.contextWrapLimit <= 0 {
			return nil, false
		}
		i.contextWrapping = true
		i.contextWrapIndex = 0
	}
	if i.contextWrapIndex >= i.contextWrapLimit {
		return nil, false
	}
	entry := allEntries[i.contextWrapIndex]
	i.contextWrapIndex++
	return &playbackCandidate{Entry: entry, Origin: EntryOriginContext, QueueIndex: -1}, true
}

func (i *nextActionIterator) isBlocked(entryID string) bool {
	if len(i.blocked) == 0 {
		return false
	}
	_, ok := i.blocked[entryID]
	return ok
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
	s.setAuthoritativePositionLocked(0)
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
	preloadedReady, preloadedURI, preloadedStatus := s.preloadedActivationLocked(entry)
	s.advanceIntentEpochLocked("play entry")
	s.cancelPendingRetryLocked()
	s.cancelTransportTransitionLocked("play entry")
	clearPreloadBackend := s.backend
	needsClearPreload := false
	if !preloadedReady {
		clearPreloadBackend, needsClearPreload = s.invalidatePreloadPlanLocked("play entry", true)
	}
	s.mu.Unlock()
	if needsClearPreload {
		s.clearBackendPreload(ctx, clearPreloadBackend)
	}
	if preloadedReady {
		state, activated, err := s.activatePreloadedEntry(
			ctx,
			backend,
			entry,
			origin,
			queueIndex,
			previous,
			preloadedURI,
			preloadedStatus,
		)
		if activated {
			return state, nil
		}
		if err != nil {
			s.logErrorf("playback: preloaded activation failed, falling back to regular load: %v", err)
			s.mu.Lock()
			clearPreloadBackend, needsClearPreload = s.invalidatePreloadPlanLocked("play entry preload fallback", true)
			s.mu.Unlock()
			if needsClearPreload {
				s.clearBackendPreload(ctx, clearPreloadBackend)
			}
		}
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
			s.clearAuthoritativePositionLocked()
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

	s.publishSnapshot(state)
	s.preloadNext(context.Background())
	return state, nil
}

func currentTransportCaptureMS() int64 {
	return time.Now().UTC().UnixMilli()
}

func midpointTransportCaptureMS(startedAtMS, completedAtMS int64) int64 {
	if completedAtMS >= startedAtMS {
		return startedAtMS + (completedAtMS-startedAtMS)/2
	}
	return startedAtMS
}

func isWithinSeekObserveTolerance(observedPositionMS, targetPositionMS int64) bool {
	deltaMS := observedPositionMS - targetPositionMS
	if deltaMS < 0 {
		deltaMS = -deltaMS
	}
	return deltaMS <= seekObserveTolerance
}

func (s *Session) setAuthoritativePositionLocked(positionMS int64) {
	s.setAuthoritativePositionAtLocked(positionMS, currentTransportCaptureMS())
}

func (s *Session) setAuthoritativePositionAtLocked(positionMS int64, capturedAtMS int64) {
	s.snapshot.PositionMS = positionMS
	if capturedAtMS < 0 {
		capturedAtMS = 0
	}
	s.snapshot.PositionCapturedAtMS = capturedAtMS
}

func (s *Session) clearAuthoritativePositionLocked() {
	s.snapshot.PositionMS = 0
	s.snapshot.PositionCapturedAtMS = 0
}

func (s *Session) refreshPosition(reason string) {
	s.mu.Lock()
	backend := s.backend
	s.mu.Unlock()
	if backend == nil {
		return
	}

	positionSampleStartedAtMS := currentTransportCaptureMS()
	positionMS, positionErr := backend.PositionMS()
	positionSampleCompletedAtMS := currentTransportCaptureMS()
	durationMS, durationErr := backend.DurationMS()
	positionCapturedAtMS := positionSampleStartedAtMS
	if positionErr == nil && positionSampleCompletedAtMS >= positionSampleStartedAtMS {
		positionCapturedAtMS = positionSampleStartedAtMS + (positionSampleCompletedAtMS-positionSampleStartedAtMS)/2
	}

	s.mu.Lock()
	if positionErr == nil {
		s.setAuthoritativePositionAtLocked(positionMS, positionCapturedAtMS)
	}
	if durationErr == nil {
		s.snapshot.DurationMS = cloneInt64Ptr(durationMS)
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	trace := NewDebugTraceEntry("session.refresh", &state)
	trace.Reason = reason
	if positionErr == nil {
		trace.ObservedPositionMS = &positionMS
	} else {
		trace.Message = positionErr.Error()
	}
	RecordDebugTrace(trace)
}

func (s *Session) refreshPositionAfterSeek(ctx context.Context, targetPositionMS int64) {
	s.mu.Lock()
	backend := s.backend
	s.mu.Unlock()
	if backend == nil {
		return
	}

	deadline := time.Now().Add(seekObserveTimeout)
	observedPositionMS := targetPositionMS
	observedCapturedAtMS := currentTransportCaptureMS()
	positionErr := error(nil)
	matchedTarget := false

	for {
		positionSampleStartedAtMS := currentTransportCaptureMS()
		positionMS, err := backend.PositionMS()
		positionSampleCompletedAtMS := currentTransportCaptureMS()
		if err == nil {
			observedPositionMS = positionMS
			observedCapturedAtMS = midpointTransportCaptureMS(
				positionSampleStartedAtMS,
				positionSampleCompletedAtMS,
			)
			positionErr = nil
			if isWithinSeekObserveTolerance(positionMS, targetPositionMS) {
				matchedTarget = true
				break
			}
		} else if positionErr == nil {
			positionErr = err
		}

		if ctx != nil && ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(seekObserveInterval)
	}

	durationMS, durationErr := backend.DurationMS()

	s.mu.Lock()
	if matchedTarget {
		s.setAuthoritativePositionAtLocked(
			observedPositionMS,
			observedCapturedAtMS,
		)
	}
	if durationErr == nil {
		s.snapshot.DurationMS = cloneInt64Ptr(durationMS)
	}
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	trace := NewDebugTraceEntry("session.refresh", &state)
	trace.Reason = "seek"
	if matchedTarget {
		trace.ObservedPositionMS = &observedPositionMS
	} else {
		trace.ObservedPositionMS = &observedPositionMS
		if positionErr != nil {
			trace.Message = fmt.Sprintf(
				"seek observe fallback target=%d position_err=%v",
				targetPositionMS,
				positionErr,
			)
		} else {
			trace.Message = fmt.Sprintf(
				"seek observe fallback target=%d last_observed=%d",
				targetPositionMS,
				observedPositionMS,
			)
		}
	}
	RecordDebugTrace(trace)
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
	shouldAttemptTransportPreload := s.shouldAttemptBackendPreloadLocked()
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
		if !shouldAttemptTransportPreload {
			return
		}
		s.mu.Lock()
		if s.shouldSuppressBackendPreloadLocked(nextEntry.EntryID, currentNext.Status.PlayableURI) {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		if err := backend.PreloadNext(ctx, currentNext.Status.PlayableURI); err != nil {
			s.mu.Lock()
			s.recordBackendPreloadFailureLocked(nextEntry.EntryID, currentNext.Status.PlayableURI)
			s.mu.Unlock()
			s.logErrorf("playback: preload next failed: %v", err)
			return
		}
		s.mu.Lock()
		s.clearNextPreparationRetryLocked()
		s.clearBackendPreloadFailureLocked()
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
	s.setNextPreparationLocked(entry, status)
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
	shouldAttemptTransportPreload := s.shouldAttemptBackendPreloadLocked()
	if !shouldAttemptTransportPreload {
		s.mu.Unlock()
		return
	}
	if s.shouldSuppressBackendPreloadLocked(entry.EntryID, status.PlayableURI) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	if backend != nil {
		if err := backend.PreloadNext(ctx, status.PlayableURI); err != nil {
			s.mu.Lock()
			s.recordBackendPreloadFailureLocked(entry.EntryID, status.PlayableURI)
			s.mu.Unlock()
			s.logErrorf("playback: preload next failed: %v", err)
			return
		}
	}
	s.mu.Lock()
	s.clearBackendPreloadFailureLocked()
	s.preloadedID = entry.EntryID
	s.preloadedURI = status.PlayableURI
	s.backendPreloadArmed = true
	s.setNextPreparationLocked(entry, status)
	s.touchLocked()
	s.mu.Unlock()
}

func (s *Session) preloadedActivationLocked(entry SessionEntry) (bool, string, apitypes.PlaybackPreparationStatus) {
	if s.backend == nil || !s.backend.SupportsPreload() {
		return false, "", apitypes.PlaybackPreparationStatus{}
	}
	if strings.TrimSpace(entry.EntryID) == "" || strings.TrimSpace(entry.EntryID) != strings.TrimSpace(s.preloadedID) {
		return false, "", apitypes.PlaybackPreparationStatus{}
	}
	if s.snapshot.NextPreparation == nil || s.snapshot.NextPreparation.EntryID != entry.EntryID {
		return false, "", apitypes.PlaybackPreparationStatus{}
	}
	status := s.snapshot.NextPreparation.Status
	if status.Phase != apitypes.PlaybackPreparationReady || strings.TrimSpace(status.PlayableURI) == "" {
		return false, "", apitypes.PlaybackPreparationStatus{}
	}
	if strings.TrimSpace(s.preloadedURI) != strings.TrimSpace(status.PlayableURI) {
		return false, "", apitypes.PlaybackPreparationStatus{}
	}
	return true, status.PlayableURI, status
}

func (s *Session) activatePreloadedEntry(
	ctx context.Context,
	backend Backend,
	entry SessionEntry,
	origin EntryOrigin,
	queueIndex int,
	previous *SessionEntry,
	preloadedURI string,
	preloadedStatus apitypes.PlaybackPreparationStatus,
) (SessionSnapshot, bool, error) {
	if backend == nil || strings.TrimSpace(preloadedURI) == "" {
		return SessionSnapshot{}, false, nil
	}

	activation, err := backend.ActivatePreloaded(ctx, preloadedURI)
	if err != nil {
		return SessionSnapshot{}, false, err
	}

	s.mu.Lock()
	token := s.transportToken + 1
	s.transportToken = token
	s.transportPending = &transportTransitionState{
		token:          token,
		entry:          entry,
		origin:         origin,
		queueIndex:     queueIndex,
		previous:       cloneEntryPtr(previous),
		playableURI:    preloadedURI,
		activation:     activation,
		status:         preloadedStatus,
		sourceIdentity: s.sourceIdentityForEntryLocked(entry),
		kind:           transportTransitionPreloaded,
	}
	s.setLoadingLocked(entry, preloadedStatus)
	s.cancelPendingRetryLocked()
	s.snapshot.LastError = ""
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.startTransportRetry(token, entry, origin, queueIndex, previous)
	s.publishSnapshot(state)
	return state, true, nil
}

func (s *Session) shouldArmNextPreparationLocked() bool {
	if s.transportPending != nil {
		return false
	}
	if s.snapshot.LoadingEntry != nil {
		return false
	}
	return s.snapshot.Status == StatusPlaying && s.snapshot.CurrentEntry != nil
}

func (s *Session) shouldAttemptBackendPreloadLocked() bool {
	if !s.shouldArmNextPreparationLocked() {
		return false
	}
	if s.snapshot.PositionMS >= preloadAfterPlayedMS {
		return true
	}
	if s.snapshot.DurationMS == nil {
		return false
	}
	return (*s.snapshot.DurationMS - s.snapshot.PositionMS) <= preloadRemainingMS
}

func (s *Session) clearNextPreparationStateLocked() {
	s.snapshot.NextPreparation = nil
	s.preloadedID = ""
	s.preloadedURI = ""
	s.clearNextPreparationRetryLocked()
	s.clearBackendPreloadFailureLocked()
}

func (s *Session) startTransportRetry(token uint64, entry SessionEntry, origin EntryOrigin, queueIndex int, previous *SessionEntry) {
	if s.ctx == nil {
		return
	}
	s.transportWG.Add(1)
	go func() {
		defer s.transportWG.Done()
		timer := time.NewTimer(transportRetryTimeout)
		defer timer.Stop()

		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}

		s.mu.Lock()
		pending := s.transportPending
		if pending == nil || pending.token != token {
			s.mu.Unlock()
			return
		}
		backend, needsClearPreload := s.invalidatePreloadPlanLocked("transport timeout fallback", true)
		s.cancelTransportTransitionLocked("transport timeout fallback")
		s.touchLocked()
		s.mu.Unlock()

		if needsClearPreload {
			s.clearBackendPreload(context.Background(), backend)
		}
		s.logPrintf("playback: transport transition timed out for %s, retrying direct load", entry.EntryID)
		if _, err := s.playEntry(context.Background(), entry, origin, queueIndex, previous, false, true); err != nil {
			s.logErrorf("playback: transport retry failed: %v", err)
		}
	}()
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

func (s *Session) shouldSuppressBackendPreloadLocked(entryID string, playableURI string) bool {
	return strings.TrimSpace(entryID) != "" &&
		strings.TrimSpace(entryID) == strings.TrimSpace(s.preloadFailureEntryID) &&
		normalizePlaybackURI(playableURI) != "" &&
		normalizePlaybackURI(playableURI) == normalizePlaybackURI(s.preloadFailureURI)
}

func (s *Session) recordBackendPreloadFailureLocked(entryID string, playableURI string) {
	s.preloadFailureEntryID = strings.TrimSpace(entryID)
	s.preloadFailureURI = normalizePlaybackURI(playableURI)
}

func (s *Session) clearBackendPreloadFailureLocked() {
	s.preloadFailureEntryID = ""
	s.preloadFailureURI = ""
}

func (s *Session) setNextPreparationLocked(entry SessionEntry, status apitypes.PlaybackPreparationStatus) {
	if !s.shouldSuppressBackendPreloadLocked(entry.EntryID, status.PlayableURI) {
		s.clearBackendPreloadFailureLocked()
	}
	s.snapshot.NextPreparation = &EntryPreparation{EntryID: entry.EntryID, Status: status}
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
		switch event.Reason {
		case TrackEndReasonEOF:
			if s.shouldTreatTrackEndAsInterrupted(event) {
				s.handleInterruptedTrackEnd(event.Err)
				return
			}
			s.handleTrackEOF(event)
		case TrackEndReasonError:
			s.handleInterruptedTrackEnd(event.Err)
		default:
			return
		}
	case BackendEventFileLoaded:
		s.handleFileLoaded(event)
	case BackendEventShutdown:
		s.mu.Lock()
		s.cancelTransportTransitionLocked("backend shutdown")
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

func (s *Session) shouldTreatTrackEndAsInterrupted(event BackendEvent) bool {
	if event.Err != nil {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.Status != StatusPlaying || s.snapshot.CurrentEntry == nil {
		return false
	}
	if s.snapshot.DurationMS == nil || *s.snapshot.DurationMS <= 0 {
		return false
	}
	return s.snapshot.PositionMS+trackEndNearEOFWindow < *s.snapshot.DurationMS
}

func (s *Session) handleInterruptedTrackEnd(err error) {
	if err == nil {
		err = errors.New("playback ended unexpectedly")
	}

	s.mu.Lock()
	backend, needsClearPreload := s.invalidatePreloadPlanLocked("track end interrupted", true)
	s.cancelPendingRetryLocked()
	s.cancelTransportTransitionLocked("track end interrupted")
	s.snapshot.Status = StatusPaused
	s.snapshot.LastError = err.Error()
	s.clearLoadingStateLocked()
	s.loadedEntryID = ""
	s.loadedURI = ""
	s.snapshot.CurrentSourceKind = ""
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

	if needsClearPreload {
		s.clearBackendPreload(context.Background(), backend)
	}
	s.logErrorf("playback: interrupted track end: %v", err)
	s.publishSnapshot(state)
}

func (s *Session) handleFileLoaded(event BackendEvent) {
	activeURI := normalizePlaybackURI(event.ActiveURI)
	if activeURI == "" {
		if event.Err != nil {
			s.logErrorf("playback: backend file-loaded transition failed: %v", event.Err)
			_, _ = s.failPlayback(event.Err)
		}
		return
	}

	s.mu.Lock()
	pending := s.transportPending
	if pending == nil || !transportTransitionMatchesEvent(pending, activeURI, event.ActivePlaylistEntryID, event.ActivePlaylistPos, event.ActiveAttemptID) {
		s.mu.Unlock()
		if event.Err != nil {
			s.logErrorf("playback: backend file-loaded state warning: %v", event.Err)
		}
		return
	}

	s.clearLoadingStateLocked()
	s.switchCurrentLocked(
		pending.entry,
		pending.origin,
		pending.queueIndex,
		pending.previous,
		false,
		0,
	)
	s.snapshot.Status = StatusPlaying
	s.snapshot.LastError = ""
	s.setAuthoritativePositionLocked(0)
	s.snapshot.DurationMS = currentDuration(&pending.entry.Item)
	s.snapshot.CurrentSourceKind = pending.status.SourceKind
	s.snapshot.CurrentPreparation = &EntryPreparation{EntryID: pending.entry.EntryID, Status: pending.status}
	s.loadedEntryID = pending.entry.EntryID
	s.loadedURI = activeURI
	s.backendPreloadArmed = false
	s.clearNextPreparationStateLocked()
	s.cancelPendingRetryLocked()
	s.transportPending = nil
	s.transportToken++
	s.ensureTickerLocked()
	s.touchLocked()
	state := snapshotCopyLocked(&s.snapshot)
	s.mu.Unlock()

	s.publishSnapshot(state)
	if event.Err != nil {
		s.logErrorf("playback: backend file-loaded completed with warning: %v", event.Err)
	}
	s.preloadNext(context.Background())
}

func transportTransitionMatchesEvent(
	pending *transportTransitionState,
	activeURI string,
	activePlaylistEntryID int64,
	activePlaylistPos int64,
	activeAttemptID uint64,
) bool {
	if pending == nil {
		return false
	}
	if pending.activation.AttemptID != 0 {
		return pending.activation.AttemptID == activeAttemptID
	}
	if pending.kind == transportTransitionPreloaded &&
		(pending.activation.PlaylistEntryID != 0 || pending.activation.PlaylistPos >= 0) {
		if pending.activation.PlaylistEntryID != 0 && activePlaylistEntryID != 0 {
			return pending.activation.PlaylistEntryID == activePlaylistEntryID
		}
		if pending.activation.PlaylistPos >= 0 && activePlaylistPos >= 0 {
			return pending.activation.PlaylistPos == activePlaylistPos
		}
		return false
	}
	if pending.activation.PlaylistEntryID != 0 && activePlaylistEntryID != 0 &&
		pending.activation.PlaylistEntryID == activePlaylistEntryID {
		return true
	}
	if pending.activation.PlaylistPos >= 0 && activePlaylistPos >= 0 &&
		pending.activation.PlaylistPos == activePlaylistPos {
		return true
	}
	if uri := normalizePlaybackURI(pending.activation.URI); uri != "" && uri == activeURI {
		return true
	}
	return normalizePlaybackURI(pending.playableURI) == activeURI
}

func (s *Session) handleTrackEOF(event BackendEvent) {
	s.mu.Lock()
	playing := s.snapshot.Status == StatusPlaying
	backend := s.backend
	supportsPreload := backend != nil && backend.SupportsPreload()
	hasLoading := s.snapshot.LoadingEntry != nil
	hasTransportPending := s.transportPending != nil
	current := cloneEntryPtr(s.snapshot.CurrentEntry)
	loadedURI := normalizePlaybackURI(s.loadedURI)
	expectedPreloadedURI := normalizePlaybackURI(s.preloadedURI)
	s.mu.Unlock()
	if event.Err != nil {
		s.logErrorf("playback: backend eof state warning: %v", event.Err)
	}
	if !playing {
		return
	}
	if hasTransportPending {
		return
	}
	if endedURI := normalizePlaybackURI(event.EndedURI); endedURI != "" && loadedURI != "" && endedURI != loadedURI {
		return
	}

	if hasLoading {
		s.mu.Lock()
		s.snapshot.Status = StatusPending
		s.clearAuthoritativePositionLocked()
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
		s.setAuthoritativePositionLocked(0)
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
		(normalizePlaybackURI(event.ActiveURI) == "" || normalizePlaybackURI(event.ActiveURI) == expectedPreloadedURI)

	if preloadedMatches {
		s.mu.Lock()
		s.switchCurrentLocked(nextEntry, origin, queueIndex, current, false, 0)
		s.snapshot.Status = StatusPlaying
		s.snapshot.LastError = ""
		s.setAuthoritativePositionLocked(0)
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
			s.refreshPosition("ticker")
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

func (s *Session) enumerateSource(ctx context.Context, req PlaybackSourceRequest) (PlaybackSourceRequest, []PlaybackSourceCandidate, int, error) {
	return NewCatalogLoader(s.core).EnumerateSource(ctx, req)
}

func (s *Session) buildSourceContextLocked(req PlaybackSourceRequest, candidates []PlaybackSourceCandidate, startIndex int) *ContextQueue {
	entries := make([]SessionEntry, 0, len(candidates))
	for index, candidate := range candidates {
		entries = append(entries, SessionEntry{
			EntryID:      candidate.Key,
			Origin:       EntryOriginContext,
			ContextIndex: index,
			Item:         normalizeSessionItem(candidate.Item),
		})
	}
	contextData := &ContextQueue{
		Kind:          req.Descriptor.Kind,
		ID:            strings.TrimSpace(req.Descriptor.ID),
		Title:         strings.TrimSpace(req.Descriptor.Title),
		StartIndex:    startIndex,
		CurrentIndex:  startIndex,
		ResumeIndex:   startIndex,
		Live:          req.Descriptor.Live,
		ShuffleSeed:   s.rng.Uint64(),
		Source:        &req.Descriptor,
		Anchor:        &req.Anchor,
		allEntries:    entries,
		SourceVersion: time.Now().UTC().UnixNano(),
	}
	s.refreshContextWindowLocked(contextData)
	return contextData
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
	contextData.allEntries = cloneEntries(contextData.Entries)
	return contextData, startIndex, nil
}

func contextAllEntries(snapshot SessionSnapshot) []SessionEntry {
	if snapshot.ContextQueue == nil {
		return nil
	}
	if len(snapshot.ContextQueue.allEntries) > 0 {
		return snapshot.ContextQueue.allEntries
	}
	return snapshot.ContextQueue.Entries
}

func contextEntryAt(snapshot SessionSnapshot, index int) SessionEntry {
	return contextAllEntries(snapshot)[index]
}

func (s *Session) contextEntryAtLocked(index int) SessionEntry {
	return contextEntryAt(s.snapshot, index)
}

func (s *Session) contextEntryCountLocked() int {
	return len(contextAllEntries(s.snapshot))
}

func isSourceBackedContext(queue *ContextQueue) bool {
	return queue != nil && queue.Source != nil
}

func (s *Session) refreshContextWindowLocked(queue *ContextQueue) {
	if queue == nil {
		return
	}
	allEntries := queue.allEntries
	if len(allEntries) == 0 {
		allEntries = queue.Entries
		queue.allEntries = cloneEntries(allEntries)
	}
	queue.TotalCount = len(allEntries)
	if queue.TotalCount == 0 {
		queue.Entries = nil
		queue.WindowStart = 0
		queue.WindowCount = 0
		queue.HasBefore = false
		queue.HasAfter = false
		return
	}
	if !isSourceBackedContext(queue) {
		queue.Entries = cloneEntries(allEntries)
		queue.WindowStart = 0
		queue.WindowCount = len(queue.Entries)
		queue.HasBefore = false
		queue.HasAfter = false
		return
	}
	windowAnchor := queue.ResumeIndex
	if queue.CurrentIndex >= 0 {
		windowAnchor = queue.CurrentIndex
	}
	if windowAnchor < 0 || windowAnchor >= len(allEntries) {
		windowAnchor = 0
	}
	const previousWindow = 20
	const nextWindow = 100
	start := windowAnchor - previousWindow
	if start < 0 {
		start = 0
	}
	end := windowAnchor + nextWindow + 1
	if end > len(allEntries) {
		end = len(allEntries)
	}
	queue.Entries = cloneEntries(allEntries[start:end])
	queue.WindowStart = start
	queue.WindowCount = len(queue.Entries)
	queue.HasBefore = start > 0
	queue.HasAfter = end < len(allEntries)
}

func (s *Session) refreshCurrentContextWindowLocked() {
	s.refreshContextWindowLocked(s.snapshot.ContextQueue)
}

func (s *Session) playTargetLocked() (SessionEntry, EntryOrigin, int, bool) {
	if s.snapshot.CurrentEntry != nil {
		return *s.snapshot.CurrentEntry, s.snapshot.CurrentEntry.Origin, -1, true
	}
	if len(s.snapshot.UserQueue) > 0 {
		return s.snapshot.UserQueue[0], EntryOriginQueued, 0, true
	}
	index := s.firstContextIndexLocked()
	if index < 0 || s.snapshot.ContextQueue == nil || index >= len(contextAllEntries(s.snapshot)) {
		return SessionEntry{}, "", -1, false
	}
	return s.contextEntryAtLocked(index), EntryOriginContext, -1, true
}

func currentMatchesContext(snapshot SessionSnapshot) bool {
	if snapshot.CurrentEntry == nil || snapshot.ContextQueue == nil {
		return false
	}
	if snapshot.CurrentEntry.Origin != EntryOriginContext {
		return false
	}
	return entryMatchesContextIndex(snapshot, *snapshot.CurrentEntry, currentContextIndex(snapshot))
}

func entryMatchesContextIndex(snapshot SessionSnapshot, entry SessionEntry, index int) bool {
	if entry.Origin != EntryOriginContext || snapshot.ContextQueue == nil {
		return false
	}
	if index < 0 || index >= len(contextAllEntries(snapshot)) {
		return false
	}
	return contextEntryAt(snapshot, index).EntryID == entry.EntryID
}

func isValidContextIndex(snapshot SessionSnapshot, index int) bool {
	return snapshot.ContextQueue != nil && index >= 0 && index < len(contextAllEntries(snapshot))
}

func defaultFirstContextIndex(snapshot SessionSnapshot) int {
	if snapshot.ContextQueue == nil || len(contextAllEntries(snapshot)) == 0 {
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
		if next < len(contextAllEntries(snapshot)) {
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
	if snapshot.ContextQueue == nil || len(contextAllEntries(snapshot)) == 0 {
		return -1
	}
	if currentMatchesContext(snapshot) {
		return nextContextIndexFromAnchor(snapshot, currentContextIndex(snapshot), false)
	}
	if detachedIndex := detachedCurrentContextIndex(snapshot); detachedIndex >= 0 {
		return nextDetachedContextResumeIndex(snapshot, detachedIndex)
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
			return len(contextAllEntries(snapshot)) - 1
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

func detachedCurrentContextIndex(snapshot SessionSnapshot) int {
	if snapshot.CurrentEntry == nil || snapshot.ContextQueue == nil {
		return -1
	}
	if snapshot.CurrentEntry.Origin != EntryOriginContext || currentMatchesContext(snapshot) {
		return -1
	}
	return snapshot.CurrentEntry.ContextIndex
}

func nextDetachedContextResumeIndex(snapshot SessionSnapshot, detachedIndex int) int {
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 {
		return -1
	}
	if snapshot.Shuffle {
		if isValidContextIndex(snapshot, resumeContextIndex(snapshot)) {
			return resumeContextIndex(snapshot)
		}
		return defaultFirstContextIndex(snapshot)
	}
	if detachedIndex < 0 {
		return defaultFirstContextIndex(snapshot)
	}
	if detachedIndex >= len(allEntries) {
		if snapshot.RepeatMode == RepeatAll {
			return 0
		}
		return -1
	}
	return detachedIndex
}

func previousDetachedContextIndex(snapshot SessionSnapshot, detachedIndex int) int {
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 {
		return -1
	}
	if snapshot.Shuffle {
		if isValidContextIndex(snapshot, currentContextIndex(snapshot)) {
			return previousContextIndexFromAnchor(snapshot, currentContextIndex(snapshot))
		}
		return -1
	}
	if detachedIndex <= 0 {
		if snapshot.RepeatMode == RepeatAll {
			return len(allEntries) - 1
		}
		return -1
	}
	if detachedIndex > len(allEntries) {
		return len(allEntries) - 1
	}
	return detachedIndex - 1
}

func (s *Session) resolveEntrySelectionLocked(entryID string) (SessionEntry, EntryOrigin, int, error) {
	if current := s.snapshot.CurrentEntry; current != nil && current.EntryID == entryID {
		return *current, current.Origin, -1, nil
	}
	if index := indexOfEntryID(s.snapshot.UserQueue, entryID); index >= 0 {
		return s.snapshot.UserQueue[index], EntryOriginQueued, index, nil
	}
	if s.snapshot.ContextQueue != nil {
		for _, entry := range contextAllEntries(s.snapshot) {
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
		s.markQueueDirtyLocked()
	}
	s.setCurrentLocked(entry, origin)
	s.recalculateResumeContextIndexLocked()
	s.snapshot.CurrentPreparation = nil
	if keepPosition {
		s.setAuthoritativePositionLocked(restorePosition)
	} else {
		s.setAuthoritativePositionLocked(0)
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
	s.markQueueDirtyLocked()
	if origin == EntryOriginContext {
		s.setCurrentContextIndexLocked(entry.ContextIndex)
	}
}

func (s *Session) detachCurrentContextLocked() {
	if s.snapshot.CurrentEntry == nil || s.snapshot.CurrentEntry.Origin != EntryOriginContext {
		return
	}
	entry := *s.snapshot.CurrentEntry
	entry.ContextIndex = -1
	s.snapshot.CurrentEntry = &entry
	item := entry.Item
	s.snapshot.CurrentItem = &item
}

func (s *Session) clearCurrentLocked() {
	s.snapshot.CurrentEntryID = ""
	s.snapshot.CurrentEntry = nil
	s.snapshot.CurrentItem = nil
	s.markQueueDirtyLocked()
}

func (s *Session) recalculateResumeContextIndexLocked() {
	s.setResumeContextIndexLocked(normalizeResumeContextIndex(s.snapshot))
}

func (s *Session) clearLoadingStateLocked() {
	s.snapshot.LoadingEntry = nil
	s.snapshot.LoadingItem = nil
	s.snapshot.LoadingPreparation = nil
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

func (s *Session) ensureShuffleCycleLocked() {
	if !s.snapshot.Shuffle || s.snapshot.ContextQueue == nil {
		return
	}
	if len(sanitizeShuffleBag(shuffleBag(s.snapshot), len(contextAllEntries(s.snapshot)))) > 0 {
		return
	}
	s.rebuildShuffleCycleLocked()
}

func (s *Session) rebuildShuffleCycleLocked() {
	if !s.snapshot.Shuffle || s.snapshot.ContextQueue == nil {
		clearContextShuffleStateLocked(&s.snapshot)
		return
	}
	seed := int64(s.snapshot.ContextQueue.ShuffleSeed)
	if seed == 0 {
		s.ensureContextShuffleSeedLocked()
		seed = int64(s.snapshot.ContextQueue.ShuffleSeed)
	}
	setShuffleBagLocked(&s.snapshot, buildAnchoredSmartShuffleCycle(
		contextAllEntries(s.snapshot),
		shuffleAnchorIndex(s.snapshot),
		rand.New(rand.NewSource(seed)),
	))
}

func (s *Session) ensureContextShuffleSeedLocked() {
	if s.snapshot.ContextQueue == nil {
		return
	}
	if s.snapshot.ContextQueue.ShuffleSeed != 0 {
		return
	}
	s.snapshot.ContextQueue.ShuffleSeed = s.rng.Uint64()
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
		size += len(contextAllEntries(s.snapshot))
	}

	out := make([]SessionEntry, 0, size)
	if s.snapshot.CurrentEntry != nil {
		out = append(out, *s.snapshot.CurrentEntry)
	}
	out = append(out, s.snapshot.UserQueue...)
	if s.snapshot.ContextQueue != nil {
		out = append(out, contextAllEntries(s.snapshot)...)
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
	s.transportPending = nil
	s.transportToken++
	s.ensureTickerLocked()
	s.touchLocked()
	s.mu.Unlock()

	s.refreshPosition("transport_ready")
	s.publishSnapshot(s.Snapshot())
	return nil
}

func (s *Session) previousContextEntryLocked() (SessionEntry, bool) {
	if currentMatchesContext(s.snapshot) {
		index := previousContextIndexFromAnchor(s.snapshot, currentContextIndex(s.snapshot))
		if index < 0 || s.snapshot.ContextQueue == nil || index >= len(contextAllEntries(s.snapshot)) {
			return SessionEntry{}, false
		}
		return s.contextEntryAtLocked(index), true
	}
	detachedIndex := detachedCurrentContextIndex(s.snapshot)
	if detachedIndex < 0 {
		return SessionEntry{}, false
	}
	index := previousDetachedContextIndex(s.snapshot, detachedIndex)
	if index < 0 || s.snapshot.ContextQueue == nil || index >= len(contextAllEntries(s.snapshot)) {
		return SessionEntry{}, false
	}
	return s.contextEntryAtLocked(index), true
}

func (s *Session) returnContextEntryLocked() (SessionEntry, bool) {
	if !isValidContextIndex(s.snapshot, currentContextIndex(s.snapshot)) || s.snapshot.ContextQueue == nil {
		return SessionEntry{}, false
	}
	return s.contextEntryAtLocked(currentContextIndex(s.snapshot)), true
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
		s.markQueueDirtyLocked()
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
	s.cancelTransportTransitionLocked("playback failure")
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
	s.cancelTransportTransitionLocked("pending playback failure")
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
		s.refreshPosition("persist_final")
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
		persisted := prepareSnapshotForPersistence(snapshot)
		if err := s.store.Save(context.Background(), persisted); err != nil {
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

func prepareSnapshotForPersistence(snapshot SessionSnapshot) SessionSnapshot {
	snapshot = normalizeSnapshot(snapshot)
	if queue := snapshot.ContextQueue; queue != nil && isSourceBackedContext(queue) {
		queue = cloneContextQueue(queue)
		queue.Entries = nil
		queue.allEntries = nil
		queue.ShuffleBag = nil
		queue.WindowStart = 0
		queue.WindowCount = 0
		queue.HasBefore = false
		queue.HasAfter = false
		queue.Loading = false
		snapshot.ContextQueue = queue
	}
	return snapshot
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
	if snapshot.PositionCapturedAtMS < 0 {
		snapshot.PositionCapturedAtMS = 0
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
			if contextCopy.Entries[index].ContextIndex < 0 {
				contextCopy.Entries[index].ContextIndex = contextCopy.WindowStart + index
			}
		}
		descriptorOnlySource := isSourceBackedContext(&contextCopy) &&
			len(contextCopy.allEntries) == 0 &&
			len(contextCopy.Entries) == 0
		if len(contextCopy.allEntries) == 0 && !descriptorOnlySource {
			contextCopy.allEntries = cloneEntries(contextCopy.Entries)
		}
		totalEntries := len(contextCopy.allEntries)
		if totalEntries == 0 && !descriptorOnlySource {
			snapshot.ContextQueue = nil
		} else {
			if totalEntries > 0 {
				contextCopy.ShuffleBag = sanitizeShuffleBag(contextCopy.ShuffleBag, totalEntries)
			} else {
				contextCopy.ShuffleBag = nil
			}
			if contextCopy.StartIndex < 0 || (totalEntries > 0 && contextCopy.StartIndex >= totalEntries) {
				contextCopy.StartIndex = 0
			}
			if contextCopy.CurrentIndex < -1 || (totalEntries > 0 && contextCopy.CurrentIndex >= totalEntries) {
				contextCopy.CurrentIndex = -1
			}
			if contextCopy.ResumeIndex < -1 || (totalEntries > 0 && contextCopy.ResumeIndex >= totalEntries) {
				contextCopy.ResumeIndex = -1
			}
			if contextCopy.TotalCount <= 0 && totalEntries > 0 {
				contextCopy.TotalCount = totalEntries
			}
			if totalEntries == 0 {
				contextCopy.Entries = nil
				contextCopy.WindowStart = 0
				contextCopy.WindowCount = 0
				contextCopy.HasBefore = false
				contextCopy.HasAfter = false
			} else if contextCopy.WindowStart < 0 || contextCopy.WindowStart >= totalEntries {
				contextCopy.WindowStart = 0
			}
			if totalEntries > 0 && contextCopy.WindowCount <= 0 {
				contextCopy.WindowCount = len(contextCopy.Entries)
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
		if snapshot.ContextQueue != nil && entryMatchesContextIndex(snapshot, entry, entry.ContextIndex) {
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
			clearContextShuffleStateLocked(&snapshot)
		}
		if snapshot.ContextQueue.TotalCount <= 0 {
			snapshot.ContextQueue.TotalCount = len(contextAllEntries(snapshot))
		}
		if !isSourceBackedContext(snapshot.ContextQueue) || len(contextAllEntries(snapshot)) > 0 {
			snapshot.ContextQueue.ResumeIndex = normalizeResumeContextIndex(snapshot)
		}
	}
	if snapshot.DurationMS != nil && snapshot.PositionMS > *snapshot.DurationMS {
		snapshot.PositionMS = *snapshot.DurationMS
	}
	if snapshot.ContextQueue == nil && len(snapshot.UserQueue) == 0 && snapshot.CurrentEntry == nil {
		if snapshot.LoadingEntry == nil {
			snapshot.Status = StatusIdle
			snapshot.PositionMS = 0
			snapshot.PositionCapturedAtMS = 0
			snapshot.DurationMS = nil
			snapshot.CurrentSourceKind = ""
			snapshot.NextPreparation = nil
		}
	}
	if snapshot.LoadingEntry != nil && snapshot.CurrentEntry == nil && snapshot.Status != StatusPending {
		snapshot.Status = StatusPending
	}

	snapshot.UpcomingEntries = nil
	snapshot.CurrentLane = deriveCurrentLane(snapshot)
	snapshot.NextPlanned = buildQueuePlanFromSnapshot(snapshot)
	if snapshot.PreloadedPlan == nil && snapshot.NextPreparation != nil {
		if entry, ok := findEntryByID(snapshot, snapshot.NextPreparation.EntryID); ok {
			snapshot.PreloadedPlan = &QueuePlan{
				Entry:   cloneEntryPtr(&entry),
				Lane:    laneFromOrigin(entry.Origin),
				Planned: true,
			}
		}
	}
	snapshot.QueueLength = queueLength(snapshot)
	return snapshot
}

func snapshotCopyLocked(snapshot *SessionSnapshot) SessionSnapshot {
	copyState := normalizeSnapshot(*snapshot)
	copyState.UserQueue = cloneEntries(copyState.UserQueue)
	copyState.ContextQueue = copyContextQueueForSnapshot(copyState.ContextQueue)
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
	contextEntries := len(contextAllEntries(snapshot))
	out := make([]SessionEntry, 0, len(snapshot.UserQueue)+contextEntries)
	out = append(out, cloneEntries(snapshot.UserQueue)...)
	if snapshot.ContextQueue == nil || len(contextAllEntries(snapshot)) == 0 {
		return out
	}

	currentIsInContext := currentMatchesContext(snapshot)
	allEntries := contextAllEntries(snapshot)

	if snapshot.Shuffle {
		cycle := effectiveShuffleCycle(snapshot)
		resumeIndex := resumeContextIndex(snapshot)
		startAdded := false
		if currentIsInContext && currentContextIndex(snapshot) >= 0 {
			position := indexOfInt(cycle, currentContextIndex(snapshot))
			if position >= 0 {
				for index := position + 1; index < len(cycle); index++ {
					out = append(out, allEntries[cycle[index]])
				}
				if snapshot.RepeatMode == RepeatAll {
					for index := 0; index < position; index++ {
						out = append(out, allEntries[cycle[index]])
					}
				}
				startAdded = true
			}
		}
		if !startAdded && resumeIndex >= 0 && resumeIndex < len(allEntries) {
			position := indexOfInt(cycle, resumeIndex)
			if position >= 0 {
				for index := position; index < len(cycle); index++ {
					out = append(out, allEntries[cycle[index]])
				}
				if snapshot.RepeatMode == RepeatAll {
					for index := 0; index < position; index++ {
						out = append(out, allEntries[cycle[index]])
					}
				}
				startAdded = true
			}
		}
		if !startAdded {
			for _, index := range cycle {
				out = append(out, allEntries[index])
			}
		}
		return out
	}

	start := 0
	if currentIsInContext && currentContextIndex(snapshot) >= 0 {
		start = currentContextIndex(snapshot) + 1
	} else if detachedIndex := detachedCurrentContextIndex(snapshot); detachedIndex >= 0 {
		start = nextDetachedContextResumeIndex(snapshot, detachedIndex)
		if start < 0 {
			start = len(allEntries)
		}
	} else if resumeContextIndex(snapshot) >= 0 && resumeContextIndex(snapshot) < len(allEntries) {
		start = resumeContextIndex(snapshot)
	}
	for index := start; index < len(allEntries); index++ {
		out = append(out, allEntries[index])
	}
	if snapshot.RepeatMode == RepeatAll && currentIsInContext && currentContextIndex(snapshot) >= 0 {
		for index := 0; index < currentContextIndex(snapshot); index++ {
			out = append(out, allEntries[index])
		}
	} else if snapshot.RepeatMode == RepeatAll && !currentIsInContext && start > 0 {
		for index := 0; index < start; index++ {
			out = append(out, allEntries[index])
		}
	}
	return out
}

func buildNextActionEntries(snapshot SessionSnapshot) []SessionEntry {
	upcoming := buildUpcomingEntries(snapshot)
	if snapshot.RepeatMode != RepeatOne || snapshot.CurrentEntry == nil {
		return upcoming
	}

	out := make([]SessionEntry, 0, len(upcoming)+1)
	out = append(out, *cloneEntryPtr(snapshot.CurrentEntry))
	out = append(out, upcoming...)
	return out
}

func buildPreviousActionPlan(snapshot SessionSnapshot) []plannedCandidate {
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 || snapshot.CurrentEntry == nil {
		return nil
	}

	indexes := make([]int, 0, len(allEntries))
	seen := make(map[int]struct{}, len(allEntries))
	appendIndex := func(index int) bool {
		if index < 0 || index >= len(allEntries) {
			return false
		}
		if _, ok := seen[index]; ok {
			return false
		}
		seen[index] = struct{}{}
		indexes = append(indexes, index)
		return true
	}

	switch {
	case snapshot.CurrentEntry.Origin == EntryOriginQueued:
		anchor := currentContextIndex(snapshot)
		if !appendIndex(anchor) {
			return nil
		}
		for index := previousContextIndexFromAnchor(snapshot, anchor); appendIndex(index); index = previousContextIndexFromAnchor(snapshot, index) {
		}
	case currentMatchesContext(snapshot):
		for index := previousContextIndexFromAnchor(snapshot, currentContextIndex(snapshot)); appendIndex(index); index = previousContextIndexFromAnchor(snapshot, index) {
		}
	default:
		detachedIndex := detachedCurrentContextIndex(snapshot)
		if detachedIndex < 0 {
			return nil
		}
		for index := previousDetachedContextIndex(snapshot, detachedIndex); appendIndex(index); index = previousContextIndexFromAnchor(snapshot, index) {
		}
	}

	candidates := make([]plannedCandidate, 0, len(indexes))
	for _, index := range indexes {
		entry := allEntries[index]
		candidates = append(candidates, plannedCandidate{
			EntryID:      entry.EntryID,
			Origin:       EntryOriginContext,
			QueueIndex:   -1,
			ContextIndex: entry.ContextIndex,
			Target:       entry.Item.Target,
			RecordingID:  entry.Item.RecordingID,
		})
	}
	return candidates
}

func HasNextAction(snapshot SessionSnapshot) bool {
	return nextActionCount(snapshot) > 0
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

func recordingIDSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sessionItemMatchesRecordingIDSet(item SessionItem, recordingIDs map[string]struct{}) bool {
	if len(recordingIDs) == 0 {
		return false
	}
	for _, candidate := range []string{item.RecordingID, item.LibraryRecordingID, item.VariantRecordingID} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := recordingIDs[candidate]; ok {
			return true
		}
	}
	return false
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
	out = append(out, entries...)
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
	copyValue.allEntries = cloneEntries(copyValue.allEntries)
	copyValue.ShuffleBag = append([]int(nil), copyValue.ShuffleBag...)
	return &copyValue
}

func copyContextQueueForSnapshot(value *ContextQueue) *ContextQueue {
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
	s.markQueueDirtyLocked()
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

func clearContextShuffleStateLocked(snapshot *SessionSnapshot) {
	if snapshot == nil || snapshot.ContextQueue == nil {
		return
	}
	snapshot.ContextQueue.ShuffleBag = nil
	snapshot.ContextQueue.ShuffleSeed = 0
}

func (s *Session) setEntryAvailabilityLocked(entryID string, status apitypes.RecordingPlaybackAvailability) bool {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return false
	}
	existing, ok := s.snapshot.EntryAvailability[entryID]
	if ok && reflect.DeepEqual(existing, status) {
		return false
	}
	if s.snapshot.EntryAvailability == nil {
		s.snapshot.EntryAvailability = make(map[string]apitypes.RecordingPlaybackAvailability)
	}
	s.snapshot.EntryAvailability[entryID] = status
	s.markQueueDirtyLocked()
	return true
}

func (s *Session) clearEntryAvailabilityLocked(entryID string) bool {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" || len(s.snapshot.EntryAvailability) == 0 {
		return false
	}
	if _, ok := s.snapshot.EntryAvailability[entryID]; !ok {
		return false
	}
	delete(s.snapshot.EntryAvailability, entryID)
	if len(s.snapshot.EntryAvailability) == 0 {
		s.snapshot.EntryAvailability = nil
	}
	s.markQueueDirtyLocked()
	return true
}

func (s *Session) pruneEntryAvailabilityLocked() {
	if len(s.snapshot.EntryAvailability) == 0 {
		return
	}

	allowed := make(map[string]struct{}, len(s.snapshot.UserQueue)+len(contextAllEntries(s.snapshot)))
	for _, entry := range s.snapshot.UserQueue {
		entryID := strings.TrimSpace(entry.EntryID)
		if entryID == "" {
			continue
		}
		allowed[entryID] = struct{}{}
	}
	for _, entry := range contextAllEntries(s.snapshot) {
		entryID := strings.TrimSpace(entry.EntryID)
		if entryID == "" {
			continue
		}
		allowed[entryID] = struct{}{}
	}
	if len(allowed) == 0 {
		s.snapshot.EntryAvailability = nil
		return
	}

	next := make(map[string]apitypes.RecordingPlaybackAvailability, len(allowed))
	for entryID, status := range s.snapshot.EntryAvailability {
		if _, ok := allowed[strings.TrimSpace(entryID)]; !ok {
			continue
		}
		next[entryID] = status
	}
	if len(next) == 0 {
		s.snapshot.EntryAvailability = nil
		return
	}
	s.snapshot.EntryAvailability = next
}

func (s *Session) cachedAvailabilityLocked(target PlaybackTargetRef, now time.Time, sourceVersion int64) (apitypes.RecordingPlaybackAvailability, bool) {
	key := availabilityCacheKey(target, s.preferredProfile, sourceVersion)
	if key == "" {
		return apitypes.RecordingPlaybackAvailability{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.availabilityCache[key]
	if !ok || now.After(item.expiresAt) {
		if ok {
			delete(s.availabilityCache, key)
		}
		return apitypes.RecordingPlaybackAvailability{}, false
	}
	return item.status, true
}

func (s *Session) cacheAvailabilityLocked(target PlaybackTargetRef, status apitypes.RecordingPlaybackAvailability, expiresAt time.Time, sourceVersion int64) {
	key := availabilityCacheKey(target, s.preferredProfile, sourceVersion)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.availabilityCache == nil {
		s.availabilityCache = make(map[string]cachedTargetAvailability)
	}
	s.availabilityCache[key] = cachedTargetAvailability{status: status, expiresAt: expiresAt}
}

func availabilityCacheKey(target PlaybackTargetRef, preferredProfile string, sourceVersion int64) string {
	targetKey := playbackTargetKey(target)
	if targetKey == "" {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d", targetKey, preferredProfile, sourceVersion)
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

func buildQueuePlanFromSnapshot(snapshot SessionSnapshot) *QueuePlan {
	entry, ok := firstUpcomingEntry(snapshot)
	if !ok {
		return nil
	}
	return &QueuePlan{
		Entry:   cloneEntryPtr(&entry),
		Lane:    laneFromOrigin(entry.Origin),
		Planned: true,
	}
}

func firstUpcomingEntry(snapshot SessionSnapshot) (SessionEntry, bool) {
	if len(snapshot.UserQueue) > 0 {
		return snapshot.UserQueue[0], true
	}
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 {
		return SessionEntry{}, false
	}

	if snapshot.Shuffle {
		cycle := effectiveShuffleCycle(snapshot)
		if len(cycle) == 0 {
			return SessionEntry{}, false
		}
		if currentMatchesContext(snapshot) && currentContextIndex(snapshot) >= 0 {
			position := indexOfInt(cycle, currentContextIndex(snapshot))
			if position >= 0 {
				if position+1 < len(cycle) {
					return allEntries[cycle[position+1]], true
				}
				if snapshot.RepeatMode == RepeatAll {
					return allEntries[cycle[0]], true
				}
				return SessionEntry{}, false
			}
		}
		resumeIndex := resumeContextIndex(snapshot)
		if resumeIndex >= 0 && resumeIndex < len(allEntries) {
			position := indexOfInt(cycle, resumeIndex)
			if position >= 0 {
				return allEntries[cycle[position]], true
			}
		}
		return allEntries[cycle[0]], true
	}

	start := firstUpcomingContextIndex(snapshot)
	if start >= 0 && start < len(allEntries) {
		return allEntries[start], true
	}
	if snapshot.RepeatMode != RepeatAll {
		return SessionEntry{}, false
	}
	if currentMatchesContext(snapshot) && currentContextIndex(snapshot) > 0 {
		return allEntries[0], true
	}
	if !currentMatchesContext(snapshot) && start > 0 {
		return allEntries[0], true
	}
	return SessionEntry{}, false
}

func firstUpcomingContextIndex(snapshot SessionSnapshot) int {
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 {
		return -1
	}
	if currentMatchesContext(snapshot) && currentContextIndex(snapshot) >= 0 {
		index := currentContextIndex(snapshot) + 1
		if index < len(allEntries) {
			return index
		}
		return len(allEntries)
	}
	if detachedIndex := detachedCurrentContextIndex(snapshot); detachedIndex >= 0 {
		index := nextDetachedContextResumeIndex(snapshot, detachedIndex)
		if index < 0 {
			return len(allEntries)
		}
		return index
	}
	resumeIndex := resumeContextIndex(snapshot)
	if resumeIndex >= 0 && resumeIndex < len(allEntries) {
		return resumeIndex
	}
	return 0
}

func upcomingEntryCount(snapshot SessionSnapshot) int {
	count := len(snapshot.UserQueue)
	allEntries := contextAllEntries(snapshot)
	if len(allEntries) == 0 {
		return count
	}

	if snapshot.Shuffle {
		cycle := effectiveShuffleCycle(snapshot)
		if len(cycle) == 0 {
			return count
		}
		if currentMatchesContext(snapshot) && currentContextIndex(snapshot) >= 0 {
			position := indexOfInt(cycle, currentContextIndex(snapshot))
			if position >= 0 {
				count += len(cycle) - position - 1
				if snapshot.RepeatMode == RepeatAll {
					count += position
				}
				return count
			}
		}
		resumeIndex := resumeContextIndex(snapshot)
		if resumeIndex >= 0 && resumeIndex < len(allEntries) {
			position := indexOfInt(cycle, resumeIndex)
			if position >= 0 {
				count += len(cycle) - position
				if snapshot.RepeatMode == RepeatAll {
					count += position
				}
				return count
			}
		}
		return count + len(cycle)
	}

	start := firstUpcomingContextIndex(snapshot)
	if start >= 0 && start < len(allEntries) {
		count += len(allEntries) - start
	}
	if snapshot.RepeatMode == RepeatAll {
		if currentMatchesContext(snapshot) && currentContextIndex(snapshot) >= 0 {
			count += currentContextIndex(snapshot)
		} else if !currentMatchesContext(snapshot) && start > 0 {
			count += start
		}
	}
	return count
}

func nextActionCount(snapshot SessionSnapshot) int {
	count := upcomingEntryCount(snapshot)
	if snapshot.RepeatMode == RepeatOne && snapshot.CurrentEntry != nil {
		count++
	}
	return count
}

func queueLength(snapshot SessionSnapshot) int {
	count := upcomingEntryCount(snapshot)
	if snapshot.CurrentEntry != nil {
		count++
	}
	return count
}

func firstUncheckedCandidate(
	session *Session,
	candidates []plannedCandidate,
	availabilityByTargetKey map[string]apitypes.RecordingPlaybackAvailability,
) (*playbackCandidate, []skippedPlaybackEntry, bool) {
	skipped := make([]skippedPlaybackEntry, 0, len(candidates))
	for _, candidate := range candidates {
		availability, ok := availabilityByTargetKey[playbackTargetKey(candidate.Target)]
		if !ok || !isAvailabilityDefinitivelyUnavailable(availability.State) {
			session.mu.Lock()
			next, resolved := session.playbackCandidateFromPlannedLocked(candidate)
			session.mu.Unlock()
			if resolved {
				return &next, skipped, false
			}
			return nil, skipped, true
		}
		session.mu.Lock()
		next, resolved := session.playbackCandidateFromPlannedLocked(candidate)
		session.mu.Unlock()
		if !resolved {
			return nil, skipped, true
		}
		skipped = append(skipped, skippedPlaybackEntryFromAvailability(next.Entry, availability))
	}
	return nil, skipped, false
}

func skippedPlaybackEntryFromAvailability(
	entry SessionEntry,
	availability apitypes.RecordingPlaybackAvailability,
) skippedPlaybackEntry {
	status := apitypes.PlaybackPreparationStatus{
		RecordingID:      availability.RecordingID,
		PreferredProfile: availability.PreferredProfile,
		Purpose:          apitypes.PlaybackPreparationPlayNow,
		Phase:            apitypes.PlaybackPreparationUnavailable,
		SourceKind:       availability.SourceKind,
		Reason:           availability.Reason,
	}
	return skippedPlaybackEntry{
		Entry:   entry,
		Status:  status,
		Message: availabilityUnavailableMessage(entry, status),
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
		for _, entry := range contextAllEntries(snapshot) {
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
	cycle := sanitizeShuffleBag(shuffleBag(snapshot), len(contextAllEntries(snapshot)))
	if len(cycle) > 0 {
		return cycle
	}
	if snapshot.ContextQueue == nil {
		return nil
	}
	seed := int64(snapshot.ContextQueue.ShuffleSeed)
	if seed == 0 {
		seed = 1
	}
	return buildAnchoredSmartShuffleCycle(
		contextAllEntries(snapshot),
		shuffleAnchorIndex(snapshot),
		rand.New(rand.NewSource(seed)),
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

func sanitizeShuffleBag(bag []int, total int) []int {
	if total <= 0 || len(bag) != total {
		return nil
	}
	seen := make([]bool, total)
	out := make([]int, 0, total)
	for _, index := range bag {
		if index < 0 || index >= total || seen[index] {
			return nil
		}
		seen[index] = true
		out = append(out, index)
	}
	return out
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
	s.snapshot.NextPlanned = buildQueuePlanFromSnapshot(s.snapshot)
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
	s.refreshCurrentContextWindowLocked()
	s.snapshot = normalizeSnapshot(s.snapshot)
	s.syncAuthorityStateLocked()
	if s.queueDirty {
		s.pruneEntryAvailabilityLocked()
		s.snapshot.QueueVersion++
		s.queueDirty = false
	}
	s.snapshot.UpdatedAt = formatTimestamp(time.Now().UTC())
}

func (s *Session) markQueueDirtyLocked() {
	s.queueDirty = true
	s.invalidateNextActionPlanLocked("queue dirty")
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
