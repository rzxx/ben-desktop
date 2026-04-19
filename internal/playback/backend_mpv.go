//go:build !nompv

package playback

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	mpv "github.com/gen2brain/go-mpv"
)

type mpvPlaylistEntry struct {
	Index    int64
	EntryID  int64
	Filename string
	Current  bool
	Playing  bool
}

type mpvPlaylistSnapshot struct {
	Count      int64
	CurrentPos int64
	PlayingPos int64
	ActivePath string
	Entries    []mpvPlaylistEntry
}

type mpvPreloadState struct {
	URI      string
	EntryID  int64
	Index    int64
	Verified bool
}

type mpvPendingActivationState struct {
	AttemptID uint64
	URI       string
	EntryID   int64
	Index     int64
}

type mpvCommandError struct {
	Op           string
	Command      []string
	RequestedURI string
	Snapshot     mpvPlaylistSnapshot
	Cause        error
}

var errMPVPreloadRecovered = errors.New("mpv preload state was recovered")

func (e *mpvCommandError) Error() string {
	if e == nil {
		return "mpv command failed"
	}
	return fmt.Sprintf(
		"mpv transport op=%s cmd=%s uri=%q cause=%v playlist_count=%d current_pos=%d playing_pos=%d active_path=%q",
		e.Op,
		strings.Join(e.Command, " "),
		e.RequestedURI,
		e.Cause,
		e.Snapshot.Count,
		e.Snapshot.CurrentPos,
		e.Snapshot.PlayingPos,
		e.Snapshot.ActivePath,
	)
}

func (e *mpvCommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type mpvBackend struct {
	client *mpv.Mpv
	once   sync.Once
	events sync.Once
	done   chan struct{}
	out    chan BackendEvent
	wg     sync.WaitGroup
	mu     sync.Mutex
	opsMu  sync.Mutex

	activeURI   string
	pendingURI  string
	preload     mpvPreloadState
	activation  mpvPendingActivationState
	loading     bool
	pendingSeek int64
	hasSeek     bool
	nextAttempt uint64
}

func newBackend() Backend {
	client := mpv.New()
	_ = client.SetOptionString("terminal", "no")
	_ = client.SetOptionString("video", "no")
	_ = client.SetOptionString("audio-display", "no")
	_ = client.SetOptionString("gapless-audio", "yes")
	_ = client.SetOptionString("prefetch-playlist", "yes")
	if err := client.Initialize(); err != nil {
		return &mpvErrorBackend{err: err}
	}
	return &mpvBackend{
		client: client,
		done:   make(chan struct{}),
		out:    make(chan BackendEvent, 32),
	}
}

func (b *mpvBackend) Load(_ context.Context, uri string) error {
	b.ensureEventLoop()

	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	if err := b.client.Command([]string{"loadfile", uri, "replace"}); err != nil {
		return b.wrapCommandErrorLocked("load_replace", []string{"loadfile", uri, "replace"}, uri, err)
	}

	b.mu.Lock()
	b.pendingURI = normalizePlaybackURI(uri)
	b.preload = mpvPreloadState{}
	b.activation = mpvPendingActivationState{}
	b.loading = true
	b.pendingSeek = 0
	b.hasSeek = false
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) ActivatePreloaded(_ context.Context, uri string) (BackendActivationRef, error) {
	b.ensureEventLoop()

	canonicalURI := normalizePlaybackURI(uri)

	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	b.mu.Lock()
	preload := b.preload
	b.mu.Unlock()
	if !preload.Verified || canonicalURI == "" || canonicalURI != preload.URI {
		return BackendActivationRef{}, ErrUnsupportedPreloadActivation
	}

	snapshot, err := b.readPlaylistSnapshotLocked()
	if err != nil {
		b.clearPreloadState()
		return BackendActivationRef{}, b.wrapCommandErrorWithSnapshot("activate_snapshot", []string{"playlist-play-index"}, uri, snapshot, err)
	}
	entry, ok := snapshot.entryForPreload(preload)
	if !ok {
		b.clearPreloadState()
		return BackendActivationRef{}, ErrUnsupportedPreloadActivation
	}

	command := []string{"playlist-play-index", strconv.FormatInt(entry.Index, 10)}
	if err := b.client.Command(command); err != nil {
		b.clearPreloadState()
		return BackendActivationRef{}, b.wrapCommandErrorWithSnapshot("activate_preloaded", command, uri, snapshot, err)
	}

	b.mu.Lock()
	b.nextAttempt++
	attemptID := b.nextAttempt
	b.pendingURI = canonicalURI
	b.activation = mpvPendingActivationState{
		AttemptID: attemptID,
		URI:       canonicalURI,
		EntryID:   entry.EntryID,
		Index:     entry.Index,
	}
	b.loading = true
	b.pendingSeek = 0
	b.hasSeek = false
	b.mu.Unlock()
	return BackendActivationRef{
		URI:             canonicalURI,
		PlaylistEntryID: entry.EntryID,
		PlaylistPos:     entry.Index,
		AttemptID:       attemptID,
	}, nil
}

func (b *mpvBackend) Play(_ context.Context) error {
	b.ensureEventLoop()
	b.opsMu.Lock()
	defer b.opsMu.Unlock()
	return b.client.SetProperty("pause", mpv.FormatFlag, false)
}

func (b *mpvBackend) Pause(_ context.Context) error {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()
	return b.client.SetProperty("pause", mpv.FormatFlag, true)
}

func (b *mpvBackend) Stop(_ context.Context) error {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	if err := b.client.Command([]string{"stop"}); err != nil {
		return b.wrapCommandErrorLocked("stop", []string{"stop"}, "", err)
	}
	b.mu.Lock()
	b.activeURI = ""
	b.pendingURI = ""
	b.preload = mpvPreloadState{}
	b.activation = mpvPendingActivationState{}
	b.loading = false
	b.pendingSeek = 0
	b.hasSeek = false
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) SeekTo(_ context.Context, positionMS int64) error {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	b.mu.Lock()
	if b.loading {
		b.pendingSeek = positionMS
		b.hasSeek = true
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	seconds := float64(positionMS) / 1000.0
	return b.client.SetProperty("time-pos", mpv.FormatDouble, seconds)
}

func (b *mpvBackend) SetVolume(_ context.Context, volume int) error {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()
	return b.client.SetProperty("volume", mpv.FormatDouble, float64(volume))
}

func (b *mpvBackend) PositionMS() (int64, error) {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	value, err := b.client.GetProperty("time-pos", mpv.FormatDouble)
	if err != nil {
		return 0, err
	}
	floatValue, ok := value.(float64)
	if !ok || math.IsNaN(floatValue) || floatValue < 0 {
		return 0, nil
	}
	return int64(math.Round(floatValue * 1000)), nil
}

func (b *mpvBackend) DurationMS() (*int64, error) {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	value, err := b.client.GetProperty("duration", mpv.FormatDouble)
	if err != nil {
		if err == mpv.ErrPropertyUnavailable || err == mpv.ErrPropertyNotFound {
			return nil, nil
		}
		return nil, err
	}
	floatValue, ok := value.(float64)
	if !ok || math.IsNaN(floatValue) || floatValue < 0 {
		return nil, nil
	}
	result := int64(math.Round(floatValue * 1000))
	return &result, nil
}

func (b *mpvBackend) Events() <-chan BackendEvent {
	return b.out
}

func (b *mpvBackend) SupportsPreload() bool {
	return true
}

func (b *mpvBackend) PreloadNext(_ context.Context, uri string) error {
	b.ensureEventLoop()

	canonicalURI := normalizePlaybackURI(uri)
	if canonicalURI == "" {
		return nil
	}

	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	snapshot, err := b.readPlaylistSnapshotLocked()
	if err != nil {
		return b.wrapCommandErrorWithSnapshot("preload_snapshot", []string{"loadfile", uri, "append"}, uri, snapshot, err)
	}
	if canonicalURI == snapshot.ActivePath {
		return nil
	}

	b.mu.Lock()
	preload := b.preload
	b.mu.Unlock()
	if preload.Verified && preload.URI == canonicalURI {
		return nil
	}

	if preload.Verified {
		if tracked, ok := snapshot.entryForPreload(preload); ok {
			if err := b.removePreloadEntryLocked(snapshot, tracked, "preload_remove_existing", preload.URI); err != nil {
				return err
			}
		}
		b.clearPreloadState()
	}

	appendCommand := []string{"loadfile", uri, "append"}
	if err := b.client.Command(appendCommand); err != nil {
		b.clearPreloadState()
		return b.wrapCommandErrorWithSnapshot("preload_append", appendCommand, uri, snapshot, err)
	}

	snapshotAfter, err := b.readPlaylistSnapshotLocked()
	if err != nil {
		b.clearPreloadState()
		return b.wrapCommandErrorWithSnapshot("preload_verify_snapshot", appendCommand, uri, snapshotAfter, err)
	}
	entry, ok := snapshotAfter.newestEntryByURI(canonicalURI)
	if !ok {
		b.clearPreloadState()
		return b.wrapCommandErrorWithSnapshot(
			"preload_verify_append",
			appendCommand,
			uri,
			snapshotAfter,
			errors.New("appended playlist entry was not found"),
		)
	}

	b.mu.Lock()
	b.preload = mpvPreloadState{
		URI:      canonicalURI,
		EntryID:  entry.EntryID,
		Index:    entry.Index,
		Verified: true,
	}
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) ClearPreloaded(context.Context) error {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	b.mu.Lock()
	preload := b.preload
	b.mu.Unlock()
	if !preload.Verified {
		return nil
	}

	snapshot, err := b.readPlaylistSnapshotLocked()
	if err != nil {
		b.clearPreloadState()
		return b.wrapCommandErrorWithSnapshot("clear_preload_snapshot", []string{"playlist-remove"}, preload.URI, snapshot, err)
	}
	entry, ok := snapshot.entryForPreload(preload)
	if !ok {
		b.clearPreloadState()
		return nil
	}

	if err := b.removePreloadEntryLocked(snapshot, entry, "clear_preload_remove", preload.URI); err != nil {
		return err
	}
	b.clearPreloadState()
	return nil
}

func (b *mpvBackend) removePreloadEntryLocked(snapshot mpvPlaylistSnapshot, entry mpvPlaylistEntry, op string, requestedURI string) error {
	command := []string{"playlist-remove", strconv.FormatInt(entry.Index, 10)}
	if err := b.client.Command(command); err != nil {
		wrapped := b.wrapCommandErrorWithSnapshot(op, command, requestedURI, snapshot, err)
		if recoverErr := b.recoverPlaylistAfterRemovalFailureLocked(snapshot, requestedURI); recoverErr != nil {
			b.clearPreloadState()
			return errors.Join(wrapped, recoverErr)
		}
		b.clearPreloadState()
		return errors.Join(errMPVPreloadRecovered, wrapped)
	}
	return nil
}

func (b *mpvBackend) recoverPlaylistAfterRemovalFailureLocked(snapshot mpvPlaylistSnapshot, requestedURI string) error {
	activeURI := snapshot.ActivePath
	if activeURI == "" {
		activePos := snapshot.PlayingPos
		if activePos < 0 {
			activePos = snapshot.CurrentPos
		}
		if entry, ok := snapshot.entryAt(activePos); ok {
			activeURI = entry.Filename
		}
	}
	if activeURI == "" {
		return b.wrapCommandErrorWithSnapshot(
			"preload_recover_missing_active",
			nil,
			requestedURI,
			snapshot,
			errors.New("mpv active track is unavailable for preload recovery"),
		)
	}

	paused, _, pauseErr := b.getFlagPropertyLocked("pause")
	if pauseErr != nil {
		return b.wrapCommandErrorWithSnapshot("preload_recover_pause", nil, requestedURI, snapshot, pauseErr)
	}
	positionSeconds, _, positionErr := b.getDoublePropertyLocked("time-pos")
	if positionErr != nil && positionErr != mpv.ErrPropertyUnavailable && positionErr != mpv.ErrPropertyNotFound {
		return b.wrapCommandErrorWithSnapshot("preload_recover_position", nil, requestedURI, snapshot, positionErr)
	}

	reloadCommand := []string{"loadfile", activeURI, "replace"}
	if err := b.client.Command(reloadCommand); err != nil {
		return b.wrapCommandErrorWithSnapshot("preload_recover_replace", reloadCommand, requestedURI, snapshot, err)
	}
	if positionSeconds > 0 {
		if err := b.client.SetProperty("time-pos", mpv.FormatDouble, positionSeconds); err != nil {
			return b.wrapCommandErrorWithSnapshot("preload_recover_seek", reloadCommand, requestedURI, snapshot, err)
		}
	}
	if err := b.client.SetProperty("pause", mpv.FormatFlag, paused); err != nil {
		return b.wrapCommandErrorWithSnapshot("preload_recover_pause_restore", reloadCommand, requestedURI, snapshot, err)
	}

	b.mu.Lock()
	b.pendingURI = normalizePlaybackURI(activeURI)
	b.preload = mpvPreloadState{}
	b.activation = mpvPendingActivationState{}
	b.loading = true
	b.pendingSeek = 0
	b.hasSeek = false
	b.mu.Unlock()
	return nil
}

func (b *mpvBackend) Close() error {
	b.once.Do(func() {
		if b.client != nil {
			close(b.done)
			b.client.Wakeup()
			b.wg.Wait()
			b.client.TerminateDestroy()
		}
		close(b.out)
	})
	return nil
}

func (b *mpvBackend) ensureEventLoop() {
	b.events.Do(func() {
		b.wg.Add(1)
		go b.runEvents()
	})
}

func (b *mpvBackend) runEvents() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		default:
		}

		ev := b.client.WaitEvent(0.5)
		if ev == nil {
			continue
		}
		if ev.Error != nil {
			b.pushEvent(BackendEvent{Type: BackendEventError, Err: ev.Error})
		}
		switch ev.EventID {
		case mpv.EventFileLoaded:
			event, err := b.handleFileLoadedEvent()
			event.Err = errors.Join(event.Err, err)
			b.pushEvent(event)
		case mpv.EventEnd:
			end := ev.EndFile()
			if end.Reason != mpv.EndFileEOF {
				b.pushEvent(BackendEvent{
					Type:   BackendEventTrackEnd,
					Reason: mapEndReason(end.Reason),
					Err:    end.Error,
				})
				continue
			}
			event, err := b.handleEOFEvent(end.Error)
			event.Err = errors.Join(event.Err, err)
			b.pushEvent(event)
		case mpv.EventShutdown:
			b.pushEvent(BackendEvent{Type: BackendEventShutdown})
			return
		}
	}
}

func (b *mpvBackend) pushEvent(event BackendEvent) {
	select {
	case <-b.done:
		return
	case b.out <- event:
	default:
	}
}

func (b *mpvBackend) handleFileLoadedEvent() (BackendEvent, error) {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	snapshot, stateErr := b.readPlaylistSnapshotLocked()
	activePos := snapshot.PlayingPos
	if activePos < 0 {
		activePos = snapshot.CurrentPos
	}
	activeEntryID := int64(0)
	if entry, ok := snapshot.entryAt(activePos); ok {
		activeEntryID = entry.EntryID
	}
	activeURI := snapshot.ActivePath

	var positionMS int64
	shouldSeek := false
	activeAttemptID := uint64(0)

	b.mu.Lock()
	if activeURI == "" && b.pendingURI != "" {
		activeURI = b.pendingURI
	}
	b.activeURI = activeURI
	b.loading = false
	if b.hasSeek {
		positionMS = b.pendingSeek
		b.pendingSeek = 0
		b.hasSeek = false
		shouldSeek = true
	}
	if b.preload.Verified {
		if activeEntryID != 0 && b.preload.EntryID == activeEntryID {
			b.preload = mpvPreloadState{}
		} else if activeURI != "" && b.preload.URI == activeURI {
			b.preload = mpvPreloadState{}
		}
	}
	if b.activation.AttemptID != 0 {
		if attemptID, ok := snapshot.matchActivationAttempt(b.activation, activeURI, activePos, activeEntryID); ok {
			activeAttemptID = attemptID
			b.activation = mpvPendingActivationState{}
		}
	}
	b.pendingURI = ""
	b.mu.Unlock()

	if shouldSeek {
		seconds := float64(positionMS) / 1000.0
		if err := b.client.SetProperty("time-pos", mpv.FormatDouble, seconds); err != nil {
			return BackendEvent{
				Type:                  BackendEventFileLoaded,
				ActiveURI:             activeURI,
				ActivePlaylistEntryID: activeEntryID,
				ActivePlaylistPos:     activePos,
				ActiveAttemptID:       activeAttemptID,
			}, err
		}
	}

	return BackendEvent{
		Type:                  BackendEventFileLoaded,
		ActiveURI:             activeURI,
		ActivePlaylistEntryID: activeEntryID,
		ActivePlaylistPos:     activePos,
		ActiveAttemptID:       activeAttemptID,
	}, stateErr
}

func (b *mpvBackend) handleEOFEvent(endErr error) (BackendEvent, error) {
	b.opsMu.Lock()
	defer b.opsMu.Unlock()

	var stateErr error
	activeURI, activePos, activeEntryID, readErr := b.readActivePlaybackStateLocked()
	if readErr != nil && readErr != mpv.ErrPropertyUnavailable && readErr != mpv.ErrPropertyNotFound {
		stateErr = b.wrapCommandErrorLocked("event_eof_state", nil, "", readErr)
	}

	b.mu.Lock()
	endedURI := b.activeURI
	b.loading = false
	b.pendingSeek = 0
	b.hasSeek = false
	b.pendingURI = ""
	b.activation = mpvPendingActivationState{}
	if activeURI == "" && b.preload.Verified {
		activeURI = b.preload.URI
	}
	b.activeURI = activeURI
	if b.preload.Verified {
		if activeEntryID != 0 && b.preload.EntryID == activeEntryID {
			b.preload = mpvPreloadState{}
		} else if activeURI != "" && b.preload.URI == activeURI {
			b.preload = mpvPreloadState{}
		}
	}
	b.mu.Unlock()

	return BackendEvent{
		Type:                  BackendEventTrackEnd,
		Reason:                TrackEndReasonEOF,
		Err:                   endErr,
		EndedURI:              endedURI,
		ActiveURI:             activeURI,
		ActivePlaylistEntryID: activeEntryID,
		ActivePlaylistPos:     activePos,
	}, stateErr
}

func (b *mpvBackend) readActivePlaybackStateLocked() (string, int64, int64, error) {
	snapshot, err := b.readPlaylistSnapshotLocked()
	if err != nil {
		return "", -1, 0, err
	}
	activePos := snapshot.PlayingPos
	if activePos < 0 {
		activePos = snapshot.CurrentPos
	}
	activeEntryID := int64(0)
	if entry, ok := snapshot.entryAt(activePos); ok {
		activeEntryID = entry.EntryID
	}
	return snapshot.ActivePath, activePos, activeEntryID, nil
}

func (b *mpvBackend) readPlaylistSnapshotLocked() (mpvPlaylistSnapshot, error) {
	snapshot := mpvPlaylistSnapshot{
		CurrentPos: -1,
		PlayingPos: -1,
	}

	count, ok, err := b.getInt64PropertyLocked("playlist-count")
	if err != nil {
		return snapshot, err
	}
	if ok {
		snapshot.Count = count
	}
	currentPos, ok, err := b.getInt64PropertyLocked("playlist-current-pos")
	if err != nil {
		return snapshot, err
	}
	if !ok {
		currentPos, ok, err = b.getInt64PropertyLocked("playlist-pos")
		if err != nil {
			return snapshot, err
		}
	}
	if ok {
		snapshot.CurrentPos = currentPos
	}
	playingPos, ok, err := b.getInt64PropertyLocked("playlist-playing-pos")
	if err != nil {
		return snapshot, err
	}
	if ok {
		snapshot.PlayingPos = playingPos
	}
	activePath, _, err := b.getStringPropertyLocked("path")
	if err != nil && err != mpv.ErrPropertyUnavailable && err != mpv.ErrPropertyNotFound {
		return snapshot, err
	}
	snapshot.ActivePath = activePath

	if snapshot.Count <= 0 {
		return snapshot, nil
	}

	entries := make([]mpvPlaylistEntry, 0, snapshot.Count)
	for index := int64(0); index < snapshot.Count; index++ {
		entry := mpvPlaylistEntry{Index: index}
		if entryID, ok, err := b.getInt64PropertyLocked(fmt.Sprintf("playlist/%d/id", index)); err == nil {
			if ok {
				entry.EntryID = entryID
			}
		} else {
			// Some mpv builds expose playlist indexes but fail individual item ids.
			// Treat entry id as optional and fall back to URI/index matching later.
		}
		if filename, ok, err := b.getStringPropertyLocked(fmt.Sprintf("playlist/%d/filename", index)); err == nil {
			if ok {
				entry.Filename = filename
			}
		} else if snapshot.CurrentPos == index || snapshot.PlayingPos == index {
			// Keep the active entry addressable even if filename probing fails.
			entry.Filename = snapshot.ActivePath
		}
		if current, ok, err := b.getFlagPropertyLocked(fmt.Sprintf("playlist/%d/current", index)); err == nil {
			if ok {
				entry.Current = current
			} else {
				entry.Current = snapshot.CurrentPos == index
			}
		} else {
			entry.Current = snapshot.CurrentPos == index
		}
		if playing, ok, err := b.getFlagPropertyLocked(fmt.Sprintf("playlist/%d/playing", index)); err == nil {
			if ok {
				entry.Playing = playing
			} else {
				entry.Playing = snapshot.PlayingPos == index
			}
		} else {
			entry.Playing = snapshot.PlayingPos == index
		}
		entries = append(entries, entry)
	}
	snapshot.Entries = entries
	return snapshot, nil
}

func (b *mpvBackend) getInt64PropertyLocked(name string) (int64, bool, error) {
	value, err := b.client.GetProperty(name, mpv.FormatInt64)
	if err != nil {
		if err == mpv.ErrPropertyUnavailable || err == mpv.ErrPropertyNotFound {
			return 0, false, nil
		}
		return 0, false, err
	}
	typed, ok := value.(int64)
	if !ok {
		return 0, false, nil
	}
	return typed, true, nil
}

func (b *mpvBackend) getStringPropertyLocked(name string) (string, bool, error) {
	value, err := b.client.GetProperty(name, mpv.FormatString)
	if err != nil {
		if err == mpv.ErrPropertyUnavailable || err == mpv.ErrPropertyNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	typed, ok := value.(string)
	if !ok {
		return "", false, nil
	}
	return normalizePlaybackURI(typed), true, nil
}

func (b *mpvBackend) getDoublePropertyLocked(name string) (float64, bool, error) {
	value, err := b.client.GetProperty(name, mpv.FormatDouble)
	if err != nil {
		if err == mpv.ErrPropertyUnavailable || err == mpv.ErrPropertyNotFound {
			return 0, false, nil
		}
		return 0, false, err
	}
	typed, ok := value.(float64)
	if !ok {
		return 0, false, nil
	}
	return typed, true, nil
}

func (b *mpvBackend) getFlagPropertyLocked(name string) (bool, bool, error) {
	value, err := b.client.GetProperty(name, mpv.FormatFlag)
	if err != nil {
		if err == mpv.ErrPropertyUnavailable || err == mpv.ErrPropertyNotFound {
			return false, false, nil
		}
		return false, false, err
	}
	typed, ok := value.(bool)
	if !ok {
		return false, false, nil
	}
	return typed, true, nil
}

func (b *mpvBackend) clearPreloadState() {
	b.mu.Lock()
	b.preload = mpvPreloadState{}
	b.mu.Unlock()
}

func (b *mpvBackend) wrapCommandErrorLocked(op string, command []string, requestedURI string, cause error) error {
	snapshot, _ := b.readPlaylistSnapshotLocked()
	return b.wrapCommandErrorWithSnapshot(op, command, requestedURI, snapshot, cause)
}

func (b *mpvBackend) wrapCommandErrorWithSnapshot(op string, command []string, requestedURI string, snapshot mpvPlaylistSnapshot, cause error) error {
	b.mu.Lock()
	preload := b.preload
	b.mu.Unlock()

	err := &mpvCommandError{
		Op:           op,
		Command:      append([]string(nil), command...),
		RequestedURI: requestedURI,
		Snapshot:     snapshot,
		Cause:        cause,
	}
	if preload.Verified {
		return fmt.Errorf("%w preload_entry_id=%d preload_index=%d preload_uri=%q", err, preload.EntryID, preload.Index, preload.URI)
	}
	return err
}

func (s mpvPlaylistSnapshot) entryByID(entryID int64) (mpvPlaylistEntry, bool) {
	if entryID == 0 {
		return mpvPlaylistEntry{}, false
	}
	for _, entry := range s.Entries {
		if entry.EntryID == entryID {
			return entry, true
		}
	}
	return mpvPlaylistEntry{}, false
}

func (s mpvPlaylistSnapshot) entryForPreload(preload mpvPreloadState) (mpvPlaylistEntry, bool) {
	if preload.EntryID != 0 {
		if entry, ok := s.entryByID(preload.EntryID); ok {
			return entry, true
		}
	}
	if preload.Index >= 0 {
		if entry, ok := s.entryAt(preload.Index); ok {
			if preload.URI == "" || entry.Filename == "" || entry.Filename == preload.URI {
				return entry, true
			}
		}
	}
	if preload.URI != "" {
		return s.newestEntryByURI(preload.URI)
	}
	return mpvPlaylistEntry{}, false
}

func (s mpvPlaylistSnapshot) entryAt(index int64) (mpvPlaylistEntry, bool) {
	for _, entry := range s.Entries {
		if entry.Index == index {
			return entry, true
		}
	}
	return mpvPlaylistEntry{}, false
}

func (s mpvPlaylistSnapshot) newestEntryByURI(uri string) (mpvPlaylistEntry, bool) {
	uri = normalizePlaybackURI(uri)
	if uri == "" {
		return mpvPlaylistEntry{}, false
	}
	var (
		best  mpvPlaylistEntry
		found bool
	)
	for _, entry := range s.Entries {
		if entry.Filename != uri {
			continue
		}
		if !found {
			best = entry
			found = true
			continue
		}
		if best.Current || best.Playing {
			if !entry.Current && !entry.Playing {
				best = entry
				continue
			}
		}
		if entry.Index > best.Index {
			best = entry
		}
	}
	return best, found
}

func (s mpvPlaylistSnapshot) hasUniqueURI(uri string) bool {
	uri = normalizePlaybackURI(uri)
	if uri == "" {
		return false
	}
	count := 0
	for _, entry := range s.Entries {
		if entry.Filename != uri {
			continue
		}
		count++
		if count > 1 {
			return false
		}
	}
	return count == 1
}

func (s mpvPlaylistSnapshot) matchActivationAttempt(
	activation mpvPendingActivationState,
	activeURI string,
	activePos int64,
	activeEntryID int64,
) (uint64, bool) {
	activeURI = normalizePlaybackURI(activeURI)
	if activation.AttemptID == 0 {
		return 0, false
	}
	switch {
	case activeEntryID != 0 && activation.EntryID != 0 && activation.EntryID == activeEntryID:
		return activation.AttemptID, true
	case activePos >= 0 && activation.Index >= 0 && activation.Index == activePos:
		return activation.AttemptID, true
	case activeEntryID == 0 && activePos < 0 && s.hasUniqueURI(activeURI) && activeURI != "" && activation.URI == activeURI:
		return activation.AttemptID, true
	default:
		return 0, false
	}
}

func mapEndReason(reason mpv.Reason) string {
	switch reason {
	case mpv.EndFileEOF:
		return TrackEndReasonEOF
	case mpv.EndFileStop:
		return TrackEndReasonStop
	case mpv.EndFileQuit:
		return TrackEndReasonQuit
	case mpv.EndFileError:
		return TrackEndReasonError
	case mpv.EndFileRedirect:
		return TrackEndReasonRedirect
	default:
		return ""
	}
}

type mpvErrorBackend struct {
	err error
}

func (b *mpvErrorBackend) Load(context.Context, string) error { return b.err }
func (b *mpvErrorBackend) ActivatePreloaded(context.Context, string) (BackendActivationRef, error) {
	return BackendActivationRef{}, b.err
}
func (b *mpvErrorBackend) Play(context.Context) error                { return b.err }
func (b *mpvErrorBackend) Pause(context.Context) error               { return b.err }
func (b *mpvErrorBackend) Stop(context.Context) error                { return b.err }
func (b *mpvErrorBackend) SeekTo(context.Context, int64) error       { return b.err }
func (b *mpvErrorBackend) SetVolume(context.Context, int) error      { return b.err }
func (b *mpvErrorBackend) PositionMS() (int64, error)                { return 0, b.err }
func (b *mpvErrorBackend) DurationMS() (*int64, error)               { return nil, b.err }
func (b *mpvErrorBackend) Events() <-chan BackendEvent               { return nil }
func (b *mpvErrorBackend) SupportsPreload() bool                     { return false }
func (b *mpvErrorBackend) PreloadNext(context.Context, string) error { return b.err }
func (b *mpvErrorBackend) ClearPreloaded(context.Context) error      { return b.err }
func (b *mpvErrorBackend) Close() error                              { return nil }
