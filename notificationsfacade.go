package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/playback"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const EventNotificationChanged = "notifications:snapshot"

const (
	scanNotificationEmitInterval         = 250 * time.Millisecond
	scanNotificationEmitProgressDelta    = 0.02
	artworkNotificationEmitInterval      = 250 * time.Millisecond
	artworkNotificationEmitProgressDelta = 0.02
	joinSessionRefreshInterval           = 2 * time.Second
	networkSyncPollInterval              = 500 * time.Millisecond
)

type notificationEmitState struct {
	LastEmittedAt       time.Time
	LastEmittedProgress float64
	LastEmittedPhase    apitypes.NotificationPhase
	HasEmitted          bool
}

type NotificationsFacade struct {
	host     *coreHost
	playback *PlaybackService

	mu sync.Mutex

	now              func() time.Time
	emitNotification func(apitypes.NotificationSnapshot)

	app           *application.App
	notifications map[string]apitypes.NotificationSnapshot
	stopListening []func()

	scanEmitStates           map[string]notificationEmitState
	artworkNotificationID    string
	artworkNotificationPhase string
	artworkEmitStates        map[string]notificationEmitState

	activeTranscodes map[string]apitypes.NotificationSnapshot

	playbackNotificationID string
	playbackRecordingID    string
	playbackPreloadID      string
	playbackPreloadEntryID string
	playbackSkipEventID    string
	playbackSkipPrimed     bool

	activeJoinSessions map[string]struct{}
	activeManualSyncID string

	lastRuntimeSyncNotification apitypes.NotificationSnapshot
	lastRuntimeSyncActive       bool
}

func NewNotificationsFacade(host *coreHost, playbackService *PlaybackService) *NotificationsFacade {
	return &NotificationsFacade{
		host:               host,
		now:                func() time.Time { return time.Now().UTC() },
		playback:           playbackService,
		notifications:      make(map[string]apitypes.NotificationSnapshot),
		scanEmitStates:     make(map[string]notificationEmitState),
		artworkEmitStates:  make(map[string]notificationEmitState),
		activeTranscodes:   make(map[string]apitypes.NotificationSnapshot),
		activeJoinSessions: make(map[string]struct{}),
	}
}

func (s *NotificationsFacade) ServiceName() string { return "NotificationsFacade" }

func (s *NotificationsFacade) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if s.host == nil {
		return nil
	}
	if err := s.host.Start(ctx); err != nil {
		return err
	}

	app := application.Get()

	s.mu.Lock()
	s.app = app
	s.mu.Unlock()

	stops := []func(){
		s.host.SubscribeJobSnapshots(s.handleJobSnapshot),
		s.host.SubscribeActivitySnapshots(s.handleActivitySnapshot),
	}
	if s.playback != nil {
		stops = append(stops, s.playback.subscribeSnapshots(s.handlePlaybackSnapshot))
	}
	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	stops = append(stops, backgroundCancel)
	go s.runJoinSessionRefreshLoop(backgroundCtx)
	go s.runNetworkSyncPollLoop(backgroundCtx)

	s.mu.Lock()
	s.stopListening = stops
	s.mu.Unlock()

	if s.host.App != nil {
		jobs, err := s.host.ListJobs(ctx, "")
		if err == nil {
			for _, job := range jobs {
				s.handleJobSnapshot(job)
			}
		}
		s.handleActivitySnapshot(s.host.ActivityStatusSnapshot())
	}
	if s.playback != nil {
		if snapshot, err := s.playback.GetPlaybackSnapshot(); err == nil {
			s.handlePlaybackSnapshot(snapshot)
		}
	}

	return nil
}

func (s *NotificationsFacade) ServiceShutdown() error {
	s.mu.Lock()
	stops := s.stopListening
	s.stopListening = nil
	s.app = nil
	s.mu.Unlock()

	for _, stop := range stops {
		stop()
	}
	return nil
}

func (s *NotificationsFacade) ListNotifications() []apitypes.NotificationSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]apitypes.NotificationSnapshot, 0, len(s.notifications))
	for _, notification := range s.notifications {
		out = append(out, notification)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *NotificationsFacade) SubscribeNotificationEvents() string {
	return EventNotificationChanged
}

func (s *NotificationsFacade) GetNotificationPreferences() (apitypes.NotificationPreferences, error) {
	state, err := loadSettingsState()
	if err != nil {
		return apitypes.NotificationPreferences{}, err
	}
	return apitypes.NotificationPreferences{
		Verbosity: apitypes.NormalizeNotificationVerbosity(apitypes.NotificationVerbosity(state.Notifications.Verbosity)),
	}, nil
}

func (s *NotificationsFacade) SetNotificationVerbosity(level apitypes.NotificationVerbosity) (apitypes.NotificationPreferences, error) {
	state, err := loadSettingsState()
	if err != nil {
		return apitypes.NotificationPreferences{}, err
	}
	next := apitypes.NormalizeNotificationVerbosity(level)
	state.Notifications.Verbosity = string(next)
	if err := saveSettingsState(state); err != nil {
		return apitypes.NotificationPreferences{}, err
	}
	return apitypes.NotificationPreferences{Verbosity: next}, nil
}

func (s *NotificationsFacade) handleJobSnapshot(job desktopcore.JobSnapshot) {
	s.trackNotificationDrivenJobs(job)
	if strings.TrimSpace(job.Kind) == "prepare-playback" {
		return
	}
	if !shouldPersistJobNotification(job.Kind) {
		return
	}
	snapshot := notificationFromJob(job)
	if snapshot.ID == "" {
		return
	}
	s.upsertNotification(snapshot)
}

func (s *NotificationsFacade) handleActivitySnapshot(status apitypes.ActivityStatus) {
	s.handleScanMaintenance(status.Maintenance)
	s.handleArtworkActivity(status.Artwork)
	s.handleTranscodeActivity(status.Transcodes)
}

func (s *NotificationsFacade) handleScanMaintenance(status apitypes.ScanMaintenanceStatus) {
	const maintenanceNotificationID = "scan-maintenance:active"

	if status.RepairRequired {
		local := s.localContext()
		errorText := strings.TrimSpace(status.Detail)
		if errorText == "" {
			errorText = strings.TrimSpace(status.Reason)
		}
		s.upsertNotification(apitypes.NotificationSnapshot{
			ID:         maintenanceNotificationID,
			Kind:       "scan-maintenance",
			LibraryID:  strings.TrimSpace(local.LibraryID),
			Audience:   apitypes.NotificationAudienceUser,
			Importance: apitypes.NotificationImportanceImportant,
			Phase:      apitypes.NotificationPhaseError,
			Message:    "Automatic scan needs a repair run.",
			Error:      errorText,
			Sticky:     true,
		})
		return
	}

	notification, ok := s.notificationByID(maintenanceNotificationID)
	if !ok {
		return
	}
	local := s.localContext()
	currentLibraryID := strings.TrimSpace(local.LibraryID)
	previousLibraryID := strings.TrimSpace(notification.LibraryID)
	if previousLibraryID != "" && currentLibraryID != previousLibraryID {
		return
	}
	notification.Kind = "scan-maintenance"
	notification.LibraryID = currentLibraryID
	notification.Audience = apitypes.NotificationAudienceUser
	notification.Importance = apitypes.NotificationImportanceImportant
	notification.Phase = apitypes.NotificationPhaseSuccess
	notification.Message = "Library repair no longer required."
	notification.Error = ""
	notification.Progress = 1
	notification.Sticky = false
	s.upsertNotification(notification)
}

func (s *NotificationsFacade) handleArtworkActivity(status apitypes.ArtworkActivityStatus) {
	phase := activityPhase(status.Phase)
	if phase == "" {
		return
	}
	id := s.activityNotificationID("artwork", phase)
	if id == "" {
		return
	}
	s.upsertNotification(apitypes.NotificationSnapshot{
		ID:         id,
		Kind:       "artwork-activity",
		Audience:   apitypes.NotificationAudienceSystem,
		Importance: apitypes.NotificationImportanceDebug,
		Phase:      phase,
		Message:    artworkActivityMessage(status),
		Error:      activityError(status.Phase, status.Errors),
		Progress:   artworkActivityProgress(status),
		Sticky:     phase == apitypes.NotificationPhaseError,
	})
}

func (s *NotificationsFacade) handleTranscodeActivity(items []apitypes.TranscodeActivityStatus) {
	local := s.localContext()
	nextActive := make(map[string]apitypes.NotificationSnapshot, len(items))
	for _, item := range items {
		notification := notificationFromTranscode(item, local.DeviceID)
		if notification.ID == "" {
			continue
		}
		nextActive[notification.ID] = notification
		s.upsertNotification(notification)
	}

	s.mu.Lock()
	previous := make(map[string]apitypes.NotificationSnapshot, len(s.activeTranscodes))
	for id, notification := range s.activeTranscodes {
		previous[id] = notification
	}
	s.activeTranscodes = nextActive
	s.mu.Unlock()

	for id, notification := range previous {
		if _, ok := nextActive[id]; ok {
			continue
		}
		notification.Phase = apitypes.NotificationPhaseSuccess
		notification.Message = transcodeCompletionMessage(notification)
		notification.Error = ""
		notification.Sticky = false
		s.upsertNotification(notification)
	}
}

func (s *NotificationsFacade) handlePlaybackSnapshot(snapshot playback.SessionSnapshot) {
	s.handlePlaybackSkipEvent(snapshot.LastSkipEvent)
	s.handlePlaybackLoadingSnapshot(snapshot)
	s.handlePlaybackPreloadSnapshot(snapshot)
}

func (s *NotificationsFacade) handlePlaybackSkipEvent(event *playback.PlaybackSkipEvent) {
	eventID := ""
	if event != nil {
		eventID = strings.TrimSpace(event.EventID)
	}

	s.mu.Lock()
	if !s.playbackSkipPrimed {
		s.playbackSkipPrimed = true
		s.playbackSkipEventID = eventID
		s.mu.Unlock()
		return
	}
	if eventID == "" || eventID == s.playbackSkipEventID {
		s.mu.Unlock()
		return
	}
	s.playbackSkipEventID = eventID
	s.mu.Unlock()

	phase := apitypes.NotificationPhaseSuccess
	if event.Stopped {
		phase = apitypes.NotificationPhaseError
	}

	var subject *apitypes.NotificationSubject
	if event.FirstEntry != nil {
		subject = &apitypes.NotificationSubject{
			RecordingID: strings.TrimSpace(event.FirstEntry.Item.RecordingID),
			Title:       strings.TrimSpace(event.FirstEntry.Item.Title),
			Subtitle:    strings.TrimSpace(event.FirstEntry.Item.Subtitle),
			ArtworkRef:  strings.TrimSpace(event.FirstEntry.Item.ArtworkRef),
		}
	}

	s.upsertNotification(apitypes.NotificationSnapshot{
		ID:         "playback-skip:" + eventID,
		Kind:       "playback-skip",
		Audience:   apitypes.NotificationAudienceUser,
		Importance: apitypes.NotificationImportanceImportant,
		Phase:      phase,
		Message:    strings.TrimSpace(event.Message),
		Sticky:     event.Stopped,
		Progress:   1,
		Subject:    subject,
	})
}

func (s *NotificationsFacade) handlePlaybackLoadingSnapshot(snapshot playback.SessionSnapshot) {
	item := snapshot.LoadingItem
	status := snapshot.LoadingPreparation
	if item == nil {
		s.finishPlaybackNotification(snapshot)
		return
	}

	phase := apitypes.NotificationPhaseRunning
	message := "Preparing playback..."
	progress := 0.35
	sticky := false
	errorText := ""
	if status != nil {
		switch status.Status.Phase {
		case apitypes.PlaybackPreparationPreparingFetch:
			message = "Fetching audio from another device..."
			progress = 0.45
		case apitypes.PlaybackPreparationPreparingTranscode:
			message = "Preparing a playable file..."
			progress = 0.7
		case apitypes.PlaybackPreparationReady:
			message = "Starting playback..."
			progress = 0.95
		case apitypes.PlaybackPreparationUnavailable, apitypes.PlaybackPreparationFailed:
			message = "Playback preparation failed."
			progress = 1
			phase = apitypes.NotificationPhaseError
			sticky = true
			errorText = playbackPreparationError(status.Status, snapshot.LastError)
		default:
			message = "Preparing playback..."
		}
	}

	id := s.ensurePlaybackNotificationID(strings.TrimSpace(item.RecordingID))
	s.upsertNotification(apitypes.NotificationSnapshot{
		ID:         id,
		Kind:       "playback-loading",
		Audience:   apitypes.NotificationAudienceUser,
		Importance: apitypes.NotificationImportanceImportant,
		Phase:      phase,
		Message:    message,
		Error:      errorText,
		Progress:   progress,
		Sticky:     sticky,
		Subject: &apitypes.NotificationSubject{
			RecordingID: strings.TrimSpace(item.RecordingID),
			Title:       strings.TrimSpace(item.Title),
			Subtitle:    strings.TrimSpace(item.Subtitle),
			ArtworkRef:  strings.TrimSpace(item.ArtworkRef),
		},
	})
	if phase == apitypes.NotificationPhaseError {
		s.clearPlaybackNotificationID()
	}
}

func (s *NotificationsFacade) finishPlaybackNotification(snapshot playback.SessionSnapshot) {
	s.mu.Lock()
	id := s.playbackNotificationID
	recordingID := s.playbackRecordingID
	s.playbackNotificationID = ""
	s.playbackRecordingID = ""
	s.mu.Unlock()
	if id == "" {
		return
	}

	notification, ok := s.notificationByID(id)
	if !ok {
		return
	}
	if notification.Phase == apitypes.NotificationPhaseError {
		return
	}
	if snapshot.CurrentItem != nil && strings.TrimSpace(snapshot.CurrentItem.RecordingID) == recordingID {
		notification.Phase = apitypes.NotificationPhaseSuccess
		notification.Message = "Playback started."
		notification.Sticky = false
		notification.Error = ""
		notification.Progress = 1
		s.upsertNotification(notification)
		return
	}
	if errorText := strings.TrimSpace(snapshot.LastError); errorText != "" {
		notification.Phase = apitypes.NotificationPhaseError
		notification.Message = "Playback preparation failed."
		notification.Error = errorText
		notification.Sticky = true
		notification.Progress = 1
		s.upsertNotification(notification)
		return
	}
	notification.Phase = apitypes.NotificationPhaseSuccess
	if notification.Message == "" {
		notification.Message = "Playback preparation ended."
	}
	notification.Sticky = false
	notification.Error = ""
	notification.Progress = 1
	s.upsertNotification(notification)
}

func (s *NotificationsFacade) handlePlaybackPreloadSnapshot(snapshot playback.SessionSnapshot) {
	preparation := snapshot.NextPreparation
	if preparation == nil || preparation.Status.Purpose != apitypes.PlaybackPreparationPreloadNext {
		s.finishPlaybackPreloadNotification("")
		return
	}

	entryID := strings.TrimSpace(preparation.EntryID)
	recordingID := strings.TrimSpace(preparation.Status.RecordingID)
	if entryID == "" || recordingID == "" {
		s.finishPlaybackPreloadNotification("")
		return
	}

	subject := playbackSubjectFromSnapshot(snapshot, entryID)
	switch preparation.Status.Phase {
	case apitypes.PlaybackPreparationPreparingFetch:
		id := s.ensurePlaybackPreloadNotificationID(entryID)
		s.upsertNotification(apitypes.NotificationSnapshot{
			ID:         id,
			Kind:       "playback-preload",
			Audience:   apitypes.NotificationAudienceSystem,
			Importance: apitypes.NotificationImportanceNormal,
			Phase:      apitypes.NotificationPhaseRunning,
			Message:    "Fetching the next track from another device...",
			Progress:   0.45,
			Subject:    subject,
		})
	case apitypes.PlaybackPreparationUnavailable, apitypes.PlaybackPreparationFailed:
		id := s.ensurePlaybackPreloadNotificationID(entryID)
		s.upsertNotification(apitypes.NotificationSnapshot{
			ID:         id,
			Kind:       "playback-preload",
			Audience:   apitypes.NotificationAudienceSystem,
			Importance: apitypes.NotificationImportanceNormal,
			Phase:      apitypes.NotificationPhaseError,
			Message:    "Preloading the next track failed.",
			Error:      playbackPreparationError(preparation.Status, snapshot.LastError),
			Progress:   1,
			Sticky:     true,
			Subject:    subject,
		})
		s.clearPlaybackPreloadNotificationID()
	default:
		s.finishPlaybackPreloadNotification("Next track is ready.")
	}
}

func (s *NotificationsFacade) finishPlaybackPreloadNotification(message string) {
	s.mu.Lock()
	id := s.playbackPreloadID
	s.playbackPreloadID = ""
	s.playbackPreloadEntryID = ""
	s.mu.Unlock()
	if id == "" {
		return
	}
	notification, ok := s.notificationByID(id)
	if !ok || notification.Phase == apitypes.NotificationPhaseError {
		return
	}
	notification.Phase = apitypes.NotificationPhaseSuccess
	if strings.TrimSpace(message) != "" {
		notification.Message = strings.TrimSpace(message)
	} else if notification.Message == "" {
		notification.Message = "Next track is ready."
	}
	notification.Error = ""
	notification.Sticky = false
	notification.Progress = 1
	s.upsertNotification(notification)
}

func (s *NotificationsFacade) activityNotificationID(kind string, phase apitypes.NotificationPhase) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch kind {
	case "artwork":
		switch phase {
		case apitypes.NotificationPhaseRunning:
			if s.artworkNotificationID == "" {
				s.artworkNotificationID = "artwork:activity"
			}
			s.artworkNotificationPhase = string(phase)
			return s.artworkNotificationID
		case apitypes.NotificationPhaseSuccess, apitypes.NotificationPhaseError:
			if s.artworkNotificationID == "" {
				s.artworkNotificationID = "artwork:activity"
			}
			s.artworkNotificationPhase = string(phase)
			return s.artworkNotificationID
		default:
			return ""
		}
	default:
		return ""
	}
}

func (s *NotificationsFacade) ensurePlaybackNotificationID(recordingID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.playbackNotificationID == "" || s.playbackRecordingID != recordingID {
		s.playbackNotificationID = "playback:" + recordingID + ":" + fmt.Sprintf("%d", s.currentTime().UnixNano())
	}
	s.playbackRecordingID = recordingID
	return s.playbackNotificationID
}

func (s *NotificationsFacade) ensurePlaybackPreloadNotificationID(entryID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.playbackPreloadID == "" || s.playbackPreloadEntryID != entryID {
		s.playbackPreloadID = "playback-preload:" + entryID
	}
	s.playbackPreloadEntryID = entryID
	return s.playbackPreloadID
}

func (s *NotificationsFacade) clearPlaybackNotificationID() {
	s.mu.Lock()
	s.playbackNotificationID = ""
	s.playbackRecordingID = ""
	s.mu.Unlock()
}

func (s *NotificationsFacade) clearPlaybackPreloadNotificationID() {
	s.mu.Lock()
	s.playbackPreloadID = ""
	s.playbackPreloadEntryID = ""
	s.mu.Unlock()
}

func (s *NotificationsFacade) localContext() apitypes.LocalContext {
	if s.host == nil {
		return apitypes.LocalContext{}
	}
	runtime := s.host.NetworkRuntime()
	if runtime == nil {
		return apitypes.LocalContext{}
	}
	local, err := runtime.EnsureLocalContext(context.Background())
	if err != nil {
		return apitypes.LocalContext{}
	}
	return local
}

func (s *NotificationsFacade) notificationByID(id string) (apitypes.NotificationSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	notification, ok := s.notifications[id]
	return notification, ok
}

func (s *NotificationsFacade) upsertNotification(notification apitypes.NotificationSnapshot) {
	notification = normalizeNotificationSnapshotAt(notification, s.currentTime())
	if notification.ID == "" {
		return
	}

	s.mu.Lock()
	if s.scanEmitStates == nil {
		s.scanEmitStates = make(map[string]notificationEmitState)
	}
	if s.artworkEmitStates == nil {
		s.artworkEmitStates = make(map[string]notificationEmitState)
	}
	existing, ok := s.notifications[notification.ID]
	if ok {
		notification.CreatedAt = existing.CreatedAt
		if notification.LibraryID == "" {
			notification.LibraryID = existing.LibraryID
		}
		if notification.Subject == nil {
			notification.Subject = existing.Subject
		}
		if notificationSemanticallyEqual(existing, notification) {
			s.mu.Unlock()
			return
		}
	}
	s.notifications[notification.ID] = notification
	app := s.app
	shouldEmit := true
	if isScanNotification(notification) {
		shouldEmit = shouldEmitActivityNotificationLocked(
			notification,
			s.scanEmitStates,
			scanNotificationEmitInterval,
			scanNotificationEmitProgressDelta,
			true,
		)
		if shouldEmit {
			recordActivityNotificationEmitLocked(notification, s.scanEmitStates)
		}
	} else if isArtworkNotification(notification) {
		shouldEmit = shouldEmitActivityNotificationLocked(
			notification,
			s.artworkEmitStates,
			artworkNotificationEmitInterval,
			artworkNotificationEmitProgressDelta,
			false,
		)
		if shouldEmit {
			recordActivityNotificationEmitLocked(notification, s.artworkEmitStates)
		}
	}
	s.mu.Unlock()

	if shouldEmit {
		s.emitNotificationSnapshot(notification, app)
	}
}

func normalizeNotificationSnapshot(notification apitypes.NotificationSnapshot) apitypes.NotificationSnapshot {
	return normalizeNotificationSnapshotAt(notification, time.Now().UTC())
}

func normalizeNotificationSnapshotAt(notification apitypes.NotificationSnapshot, now time.Time) apitypes.NotificationSnapshot {
	notification.ID = strings.TrimSpace(notification.ID)
	notification.Kind = strings.TrimSpace(notification.Kind)
	notification.LibraryID = strings.TrimSpace(notification.LibraryID)
	notification.Message = strings.TrimSpace(notification.Message)
	notification.Error = strings.TrimSpace(notification.Error)
	notification.Progress = clampNotificationProgress(notification.Progress)
	if notification.Audience == "" {
		notification.Audience = apitypes.NotificationAudienceSystem
	}
	if notification.Importance == "" {
		notification.Importance = apitypes.NotificationImportanceNormal
	}
	if notification.Phase == "" {
		notification.Phase = apitypes.NotificationPhaseRunning
	}
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = now
	}
	notification.UpdatedAt = now
	if notification.Phase == apitypes.NotificationPhaseSuccess || notification.Phase == apitypes.NotificationPhaseError {
		if notification.FinishedAt.IsZero() {
			notification.FinishedAt = now
		}
	}
	if notification.Phase == apitypes.NotificationPhaseError {
		notification.Sticky = true
	}
	if notification.Subject != nil {
		notification.Subject.RecordingID = strings.TrimSpace(notification.Subject.RecordingID)
		notification.Subject.Title = strings.TrimSpace(notification.Subject.Title)
		notification.Subject.Subtitle = strings.TrimSpace(notification.Subject.Subtitle)
		notification.Subject.ArtworkRef = strings.TrimSpace(notification.Subject.ArtworkRef)
		if *notification.Subject == (apitypes.NotificationSubject{}) {
			notification.Subject = nil
		}
	}
	return notification
}

func notificationFromJob(job desktopcore.JobSnapshot) apitypes.NotificationSnapshot {
	audience, importance := classifyJob(job.Kind)
	return apitypes.NotificationSnapshot{
		ID:         "job:" + strings.TrimSpace(job.JobID),
		Kind:       strings.TrimSpace(job.Kind),
		LibraryID:  strings.TrimSpace(job.LibraryID),
		Audience:   audience,
		Importance: importance,
		Phase:      notificationPhaseFromJob(job.Phase),
		Message:    strings.TrimSpace(job.Message),
		Error:      strings.TrimSpace(job.Error),
		Progress:   job.Progress,
		Sticky:     job.Phase == desktopcore.JobPhaseFailed,
		CreatedAt:  job.CreatedAt,
		UpdatedAt:  job.UpdatedAt,
		FinishedAt: job.FinishedAt,
	}
}

func notificationFromTranscode(item apitypes.TranscodeActivityStatus, localDeviceID string) apitypes.NotificationSnapshot {
	requesterDeviceID := strings.TrimSpace(item.RequesterDeviceID)
	audience := apitypes.NotificationAudienceSystem
	importance := apitypes.NotificationImportanceDebug
	if requesterDeviceID != "" && requesterDeviceID == strings.TrimSpace(localDeviceID) {
		audience = apitypes.NotificationAudienceUser
		importance = apitypes.NotificationImportanceImportant
	}
	message := "Transcoding track..."
	switch strings.TrimSpace(item.Phase) {
	case "waiting_existing":
		message = "Waiting for an existing transcode..."
	case "running":
		message = "Transcoding track..."
	}
	idParts := []string{
		"transcode",
		strings.TrimSpace(item.RecordingID),
		strings.TrimSpace(item.SourceFileID),
		strings.TrimSpace(item.Profile),
		strings.TrimSpace(item.RequestKind),
		fmt.Sprintf("%d", item.StartedAt.UTC().UnixNano()),
	}
	return apitypes.NotificationSnapshot{
		ID:         strings.Join(idParts, ":"),
		Kind:       "transcode-activity",
		Audience:   audience,
		Importance: importance,
		Phase:      apitypes.NotificationPhaseRunning,
		Message:    message,
		Progress:   0.7,
		CreatedAt:  item.StartedAt,
		UpdatedAt:  item.StartedAt,
		Subject: &apitypes.NotificationSubject{
			RecordingID: strings.TrimSpace(item.RecordingID),
			ArtworkRef:  strings.TrimSpace(item.RecordingID),
		},
	}
}

func notificationPhaseFromJob(phase desktopcore.JobPhase) apitypes.NotificationPhase {
	switch phase {
	case desktopcore.JobPhaseQueued:
		return apitypes.NotificationPhaseQueued
	case desktopcore.JobPhaseCompleted:
		return apitypes.NotificationPhaseSuccess
	case desktopcore.JobPhaseFailed:
		return apitypes.NotificationPhaseError
	default:
		return apitypes.NotificationPhaseRunning
	}
}

func classifyJob(kind string) (apitypes.NotificationAudience, apitypes.NotificationImportance) {
	switch strings.TrimSpace(kind) {
	case "repair-library",
		"sync-now",
		"connect-peer",
		"join-session",
		"finalize-join-session",
		"pin-recording",
		"pin-album",
		"pin-playlist",
		"ensure-recording-encoding",
		"ensure-album-encodings",
		"ensure-playlist-encodings",
		"publish-checkpoint",
		"compact-checkpoint":
		return apitypes.NotificationAudienceUser, apitypes.NotificationImportanceNormal
	case "install-checkpoint",
		"refresh-pinned-recording",
		"refresh-pinned-album",
		"refresh-pinned-playlist",
		"watch-scan-delta",
		"startup-scan":
		return apitypes.NotificationAudienceSystem, apitypes.NotificationImportanceDebug
	default:
		return apitypes.NotificationAudienceSystem, apitypes.NotificationImportanceDebug
	}
}

func shouldPersistJobNotification(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "watch-scan-delta", "startup-scan":
		return false
	default:
		return true
	}
}

func activityPhase(phase string) apitypes.NotificationPhase {
	switch strings.TrimSpace(phase) {
	case "", "idle":
		return ""
	case "completed":
		return apitypes.NotificationPhaseSuccess
	case "failed":
		return apitypes.NotificationPhaseError
	default:
		return apitypes.NotificationPhaseRunning
	}
}

func activityError(phase string, errors int) string {
	if strings.TrimSpace(phase) != "failed" || errors <= 0 {
		return ""
	}
	return fmt.Sprintf("%d error(s)", errors)
}

func artworkActivityProgress(status apitypes.ArtworkActivityStatus) float64 {
	if status.AlbumsTotal > 0 {
		return clampNotificationProgress(float64(status.AlbumsDone) / float64(status.AlbumsTotal))
	}
	return 0.1
}

func artworkActivityMessage(status apitypes.ArtworkActivityStatus) string {
	switch strings.TrimSpace(status.Phase) {
	case "running":
		if albumID := strings.TrimSpace(status.CurrentAlbumID); albumID != "" {
			return "Generating artwork for " + albumID
		}
		return "Generating artwork..."
	case "completed":
		return "Artwork generation completed."
	case "failed":
		return "Artwork generation failed."
	default:
		return "Generating artwork..."
	}
}

func transcodeCompletionMessage(notification apitypes.NotificationSnapshot) string {
	if notification.Audience == apitypes.NotificationAudienceUser {
		return "Transcode ready."
	}
	return "Background transcode completed."
}

func isScanJobKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "repair-library", "watch-scan-delta", "startup-scan":
		return true
	default:
		return false
	}
}

func clampNotificationProgress(progress float64) float64 {
	switch {
	case progress < 0:
		return 0
	case progress > 1:
		return 1
	default:
		return progress
	}
}

func (s *NotificationsFacade) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now().UTC()
}

func (s *NotificationsFacade) emitNotificationSnapshot(
	notification apitypes.NotificationSnapshot,
	app *application.App,
) {
	if s != nil && s.emitNotification != nil {
		s.emitNotification(notification)
		return
	}
	if app != nil && app.Event != nil {
		app.Event.Emit(EventNotificationChanged, notification)
	}
}

func shouldEmitActivityNotificationLocked(
	notification apitypes.NotificationSnapshot,
	states map[string]notificationEmitState,
	interval time.Duration,
	progressDelta float64,
	emitQueuedImmediately bool,
) bool {
	switch notification.Phase {
	case apitypes.NotificationPhaseSuccess,
		apitypes.NotificationPhaseError:
		return true
	}
	if emitQueuedImmediately && notification.Phase == apitypes.NotificationPhaseQueued {
		return true
	}

	state, ok := states[notification.ID]
	if !ok || !state.HasEmitted {
		return true
	}
	if state.LastEmittedPhase != notification.Phase {
		return true
	}
	if notification.Phase != apitypes.NotificationPhaseRunning {
		return true
	}
	if notification.Progress-state.LastEmittedProgress >= progressDelta {
		return true
	}
	return notification.UpdatedAt.Sub(state.LastEmittedAt) >= interval
}

func recordActivityNotificationEmitLocked(
	notification apitypes.NotificationSnapshot,
	states map[string]notificationEmitState,
) {
	states[notification.ID] = notificationEmitState{
		LastEmittedAt:       notification.UpdatedAt,
		LastEmittedProgress: notification.Progress,
		LastEmittedPhase:    notification.Phase,
		HasEmitted:          true,
	}
}

func isArtworkNotification(notification apitypes.NotificationSnapshot) bool {
	return strings.TrimSpace(notification.ID) == "artwork:activity" ||
		strings.TrimSpace(notification.Kind) == "artwork-activity"
}

func isScanNotification(notification apitypes.NotificationSnapshot) bool {
	if !strings.HasPrefix(strings.TrimSpace(notification.ID), "job:") {
		return false
	}
	return isScanJobKind(notification.Kind)
}

func (s *NotificationsFacade) trackNotificationDrivenJobs(job desktopcore.JobSnapshot) {
	jobID := strings.TrimSpace(job.JobID)
	if jobID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeJoinSessions == nil {
		s.activeJoinSessions = make(map[string]struct{})
	}

	switch strings.TrimSpace(job.Kind) {
	case "join-session":
		if notificationTracksActiveJob(job.Phase) {
			s.activeJoinSessions[jobID] = struct{}{}
		} else {
			delete(s.activeJoinSessions, jobID)
		}
	case "sync-now":
		if notificationTracksActiveJob(job.Phase) {
			s.activeManualSyncID = jobID
		} else if s.activeManualSyncID == jobID {
			s.activeManualSyncID = ""
		}
	}
}

func notificationTracksActiveJob(phase desktopcore.JobPhase) bool {
	return phase == desktopcore.JobPhaseQueued || phase == desktopcore.JobPhaseRunning
}

func (s *NotificationsFacade) runJoinSessionRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(joinSessionRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sessionIDs := s.activeJoinSessionIDs()
			if len(sessionIDs) == 0 || s.host == nil {
				continue
			}
			runtime := s.host.InviteRuntime()
			if runtime == nil {
				continue
			}
			for _, sessionID := range sessionIDs {
				if _, err := runtime.GetJoinSession(ctx, sessionID); err != nil {
					continue
				}
			}
		}
	}
}

func (s *NotificationsFacade) activeJoinSessionIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.activeJoinSessions))
	for sessionID := range s.activeJoinSessions {
		out = append(out, sessionID)
	}
	sort.Strings(out)
	return out
}

func (s *NotificationsFacade) runNetworkSyncPollLoop(ctx context.Context) {
	ticker := time.NewTicker(networkSyncPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollNetworkSyncStatus()
		}
	}
}

func (s *NotificationsFacade) pollNetworkSyncStatus() {
	if s.host == nil {
		return
	}
	runtime := s.host.NetworkRuntime()
	if runtime == nil {
		return
	}

	status := runtime.NetworkStatus()
	manualJobID := s.currentManualSyncJobID()
	if status.Mode != apitypes.NetworkSyncModeIdle && status.Reason == apitypes.NetworkSyncReasonManual && manualJobID != "" {
		s.upsertNotification(apitypes.NotificationSnapshot{
			ID:         "job:" + manualJobID,
			Kind:       "sync-now",
			LibraryID:  strings.TrimSpace(status.LibraryID),
			Audience:   apitypes.NotificationAudienceUser,
			Importance: apitypes.NotificationImportanceNormal,
			Phase:      apitypes.NotificationPhaseRunning,
			Message:    networkSyncRunningMessage(status, false),
			Progress:   networkSyncProgress(status),
		})
		return
	}

	if status.Mode != apitypes.NetworkSyncModeIdle && status.Reason != apitypes.NetworkSyncReasonManual {
		notification := apitypes.NotificationSnapshot{
			ID:         "sync:runtime",
			Kind:       "sync-activity",
			LibraryID:  strings.TrimSpace(status.LibraryID),
			Audience:   apitypes.NotificationAudienceSystem,
			Importance: apitypes.NotificationImportanceNormal,
			Phase:      apitypes.NotificationPhaseRunning,
			Message:    networkSyncRunningMessage(status, true),
			Progress:   networkSyncProgress(status),
		}
		s.mu.Lock()
		s.lastRuntimeSyncNotification = notification
		s.lastRuntimeSyncActive = true
		s.mu.Unlock()
		s.upsertNotification(notification)
		return
	}

	s.mu.Lock()
	previous := s.lastRuntimeSyncNotification
	wasActive := s.lastRuntimeSyncActive
	s.lastRuntimeSyncActive = false
	s.mu.Unlock()
	if !wasActive || previous.ID == "" {
		return
	}
	if errorText := strings.TrimSpace(status.LastSyncError); errorText != "" {
		previous.Phase = apitypes.NotificationPhaseError
		previous.Message = "Background sync failed."
		previous.Error = errorText
		previous.Sticky = true
		previous.Progress = 1
	} else {
		previous.Phase = apitypes.NotificationPhaseSuccess
		previous.Message = networkSyncCompletionMessage(previous, status)
		previous.Error = ""
		previous.Sticky = false
		previous.Progress = 1
	}
	s.upsertNotification(previous)
}

func (s *NotificationsFacade) currentManualSyncJobID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeManualSyncID
}

func networkSyncRunningMessage(status apitypes.NetworkStatus, background bool) string {
	target := strings.TrimSpace(status.ActivePeerID)
	if target == "" {
		target = "peer"
	}

	switch status.Activity {
	case apitypes.NetworkSyncActivityCheckpointInstall:
		if background {
			return fmt.Sprintf("Background sync is installing a checkpoint from %s.", target)
		}
		return fmt.Sprintf("Installing a checkpoint from %s.", target)
	case apitypes.NetworkSyncActivityCheckpointMirror:
		if background {
			return fmt.Sprintf("Background sync is mirroring a checkpoint with %s.", target)
		}
		return fmt.Sprintf("Mirroring a checkpoint with %s.", target)
	default:
		applied := status.LastBatchApplied
		remaining := status.BacklogEstimate
		prefix := "Syncing"
		if background {
			prefix = "Background sync is updating"
		}
		if remaining > 0 {
			return fmt.Sprintf("%s %s, applied %d ops, %d remaining.", prefix, target, applied, remaining)
		}
		return fmt.Sprintf("%s %s, applied %d ops.", prefix, target, applied)
	}
}

func networkSyncCompletionMessage(previous apitypes.NotificationSnapshot, status apitypes.NetworkStatus) string {
	if status.LastBatchApplied > 0 {
		return fmt.Sprintf("Background sync applied %d ops.", status.LastBatchApplied)
	}
	if previous.Progress > 0 {
		return "Background sync completed."
	}
	return "Background sync finished."
}

func networkSyncProgress(status apitypes.NetworkStatus) float64 {
	if status.BacklogEstimate <= 0 || status.LastBatchApplied <= 0 {
		return 0.35
	}
	applied := float64(status.LastBatchApplied)
	remaining := float64(status.BacklogEstimate)
	return clampNotificationProgress(applied / (applied + remaining))
}

func playbackPreparationError(status apitypes.PlaybackPreparationStatus, fallback string) string {
	if text := strings.TrimSpace(fallback); text != "" {
		return text
	}
	switch status.Reason {
	case apitypes.PlaybackUnavailableProviderOffline:
		return "The source device is offline."
	case apitypes.PlaybackUnavailableNetworkOff:
		return "Network playback is unavailable."
	case apitypes.PlaybackUnavailableNoPath:
		return "No playable source is available."
	default:
		if status.Phase == apitypes.PlaybackPreparationFailed || status.Phase == apitypes.PlaybackPreparationUnavailable {
			return "Playback preparation failed."
		}
		return ""
	}
}

func playbackSubjectFromSnapshot(snapshot playback.SessionSnapshot, entryID string) *apitypes.NotificationSubject {
	if entryID == "" {
		return nil
	}
	for _, entry := range snapshot.UpcomingEntries {
		if strings.TrimSpace(entry.EntryID) == entryID {
			return notificationSubjectFromSessionItem(entry.Item)
		}
	}
	if snapshot.CurrentEntry != nil && strings.TrimSpace(snapshot.CurrentEntry.EntryID) == entryID {
		return notificationSubjectFromSessionItem(snapshot.CurrentEntry.Item)
	}
	if snapshot.LoadingEntry != nil && strings.TrimSpace(snapshot.LoadingEntry.EntryID) == entryID {
		return notificationSubjectFromSessionItem(snapshot.LoadingEntry.Item)
	}
	if snapshot.ContextQueue != nil {
		for _, entry := range snapshot.ContextQueue.Entries {
			if strings.TrimSpace(entry.EntryID) == entryID {
				return notificationSubjectFromSessionItem(entry.Item)
			}
		}
	}
	for _, entry := range snapshot.UserQueue {
		if strings.TrimSpace(entry.EntryID) == entryID {
			return notificationSubjectFromSessionItem(entry.Item)
		}
	}
	return nil
}

func notificationSubjectFromSessionItem(item playback.SessionItem) *apitypes.NotificationSubject {
	return &apitypes.NotificationSubject{
		RecordingID: strings.TrimSpace(item.RecordingID),
		Title:       strings.TrimSpace(item.Title),
		Subtitle:    strings.TrimSpace(item.Subtitle),
		ArtworkRef:  strings.TrimSpace(item.ArtworkRef),
	}
}

func notificationSemanticallyEqual(left, right apitypes.NotificationSnapshot) bool {
	return left.ID == right.ID &&
		left.Kind == right.Kind &&
		left.LibraryID == right.LibraryID &&
		left.Audience == right.Audience &&
		left.Importance == right.Importance &&
		left.Phase == right.Phase &&
		left.Message == right.Message &&
		left.Error == right.Error &&
		left.Progress == right.Progress &&
		left.Sticky == right.Sticky &&
		notificationSubjectsEqual(left.Subject, right.Subject)
}

func notificationSubjectsEqual(left, right *apitypes.NotificationSubject) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.RecordingID == right.RecordingID &&
			left.Title == right.Title &&
			left.Subtitle == right.Subtitle &&
			left.ArtworkRef == right.ArtworkRef
	}
}
