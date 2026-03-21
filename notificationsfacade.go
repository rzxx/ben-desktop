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
	"ben/desktop/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const EventNotificationChanged = "notifications:snapshot"

type NotificationsFacade struct {
	host     *coreHost
	playback *PlaybackService

	mu sync.Mutex

	app           *application.App
	notifications map[string]apitypes.NotificationSnapshot
	stopListening []func()

	scanNotificationID    string
	scanNotificationPhase string
	scanNotificationKind  string
	scanAudience          apitypes.NotificationAudience
	scanImportance        apitypes.NotificationImportance

	artworkNotificationID    string
	artworkNotificationPhase string

	activeTranscodes map[string]apitypes.NotificationSnapshot

	playbackNotificationID string
	playbackRecordingID    string
}

func NewNotificationsFacade(host *coreHost, playbackService *PlaybackService) *NotificationsFacade {
	return &NotificationsFacade{
		host:             host,
		playback:         playbackService,
		notifications:    make(map[string]apitypes.NotificationSnapshot),
		activeTranscodes: make(map[string]apitypes.NotificationSnapshot),
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
		s.host.App.SubscribeJobSnapshots(s.handleJobSnapshot),
		s.host.App.SubscribeActivitySnapshots(s.handleActivitySnapshot),
	}
	if s.playback != nil {
		stops = append(stops, s.playback.subscribeSnapshots(s.handlePlaybackSnapshot))
	}

	s.mu.Lock()
	s.stopListening = stops
	s.mu.Unlock()

	if s.host.App != nil {
		jobs, err := s.host.App.ListJobs(ctx, "")
		if err == nil {
			for _, job := range jobs {
				s.handleJobSnapshot(job)
			}
		}
		s.handleActivitySnapshot(s.host.App.ActivityStatusSnapshot())
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
	if strings.TrimSpace(job.Kind) == "prepare-playback" {
		return
	}
	if isScanJobKind(job.Kind) {
		s.handleScanJobSnapshot(job)
		return
	}
	snapshot := notificationFromJob(job)
	if snapshot.ID == "" {
		return
	}
	s.upsertNotification(snapshot)
}

func (s *NotificationsFacade) handleActivitySnapshot(status apitypes.ActivityStatus) {
	s.handleScanActivity(status.Scan)
	s.handleArtworkActivity(status.Artwork)
	s.handleTranscodeActivity(status.Transcodes)
}

func (s *NotificationsFacade) handleScanActivity(status apitypes.ScanActivityStatus) {
	phase := activityPhase(status.Phase)
	if phase == "" {
		return
	}
	id := s.activityNotificationID("scan", phase)
	if id == "" {
		return
	}
	kind, audience, importance := s.scanNotificationMetadata()
	s.upsertNotification(apitypes.NotificationSnapshot{
		ID:         id,
		Kind:       kind,
		Audience:   audience,
		Importance: importance,
		Phase:      phase,
		Message:    scanActivityMessage(status),
		Error:      activityError(status.Phase, status.Errors),
		Progress:   scanActivityProgress(status),
		Sticky:     phase == apitypes.NotificationPhaseError,
	})
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

func (s *NotificationsFacade) handleScanJobSnapshot(job desktopcore.JobSnapshot) {
	phase := notificationPhaseFromJob(job.Phase)
	if phase == "" {
		return
	}
	id := s.activityNotificationID("scan", phase)
	if id == "" {
		return
	}
	kind := strings.TrimSpace(job.Kind)
	audience, importance := classifyJob(kind)
	s.setScanNotificationMetadata(kind, audience, importance)
	s.upsertNotification(apitypes.NotificationSnapshot{
		ID:         id,
		Kind:       kind,
		LibraryID:  strings.TrimSpace(job.LibraryID),
		Audience:   audience,
		Importance: importance,
		Phase:      phase,
		Message:    strings.TrimSpace(job.Message),
		Error:      strings.TrimSpace(job.Error),
		Progress:   job.Progress,
		Sticky:     job.Phase == desktopcore.JobPhaseFailed,
		CreatedAt:  job.CreatedAt,
		UpdatedAt:  job.UpdatedAt,
		FinishedAt: job.FinishedAt,
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
			message = "Ready to play."
			progress = 1
			phase = apitypes.NotificationPhaseSuccess
		case apitypes.PlaybackPreparationFailed:
			message = "Playback preparation failed."
			progress = 1
			phase = apitypes.NotificationPhaseError
			sticky = true
			errorText = "Playback preparation failed."
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
	if phase == apitypes.NotificationPhaseSuccess || phase == apitypes.NotificationPhaseError {
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
	notification.Phase = apitypes.NotificationPhaseSuccess
	if snapshot.CurrentItem != nil && strings.TrimSpace(snapshot.CurrentItem.RecordingID) == recordingID {
		notification.Message = "Playback ready."
	} else if notification.Message == "" {
		notification.Message = "Playback ready."
	}
	notification.Sticky = false
	notification.Error = ""
	notification.Progress = 1
	s.upsertNotification(notification)
}

func (s *NotificationsFacade) activityNotificationID(kind string, phase apitypes.NotificationPhase) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowID := kind + ":" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	switch kind {
	case "scan":
		switch phase {
		case apitypes.NotificationPhaseQueued, apitypes.NotificationPhaseRunning:
			if s.scanNotificationID == "" || !isNotificationActivePhase(s.scanNotificationPhase) {
				s.scanNotificationID = nowID
			}
			s.scanNotificationPhase = string(phase)
			return s.scanNotificationID
		case apitypes.NotificationPhaseSuccess, apitypes.NotificationPhaseError:
			if s.scanNotificationID == "" {
				s.scanNotificationID = nowID
			}
			s.scanNotificationPhase = string(phase)
			return s.scanNotificationID
		default:
			return ""
		}
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
		s.playbackNotificationID = "playback:" + recordingID + ":" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	s.playbackRecordingID = recordingID
	return s.playbackNotificationID
}

func (s *NotificationsFacade) setScanNotificationMetadata(
	kind string,
	audience apitypes.NotificationAudience,
	importance apitypes.NotificationImportance,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scanNotificationKind = strings.TrimSpace(kind)
	s.scanAudience = audience
	s.scanImportance = importance
}

func (s *NotificationsFacade) scanNotificationMetadata() (string, apitypes.NotificationAudience, apitypes.NotificationImportance) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kind := strings.TrimSpace(s.scanNotificationKind)
	if kind == "" {
		kind = "scan-activity"
	}
	audience := s.scanAudience
	if audience == "" {
		audience = apitypes.NotificationAudienceSystem
	}
	importance := s.scanImportance
	if importance == "" {
		importance = apitypes.NotificationImportanceDebug
	}
	return kind, audience, importance
}

func (s *NotificationsFacade) clearPlaybackNotificationID() {
	s.mu.Lock()
	s.playbackNotificationID = ""
	s.playbackRecordingID = ""
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
	notification = normalizeNotificationSnapshot(notification)
	if notification.ID == "" {
		return
	}

	s.mu.Lock()
	existing, ok := s.notifications[notification.ID]
	if ok {
		notification.CreatedAt = existing.CreatedAt
		if notification.LibraryID == "" {
			notification.LibraryID = existing.LibraryID
		}
		if notification.Subject == nil {
			notification.Subject = existing.Subject
		}
	}
	s.notifications[notification.ID] = notification
	app := s.app
	s.mu.Unlock()

	if app != nil && app.Event != nil {
		app.Event.Emit(EventNotificationChanged, notification)
	}
}

func normalizeNotificationSnapshot(notification apitypes.NotificationSnapshot) apitypes.NotificationSnapshot {
	now := time.Now().UTC()
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
	requestKind := strings.TrimSpace(item.RequestKind)
	requesterDeviceID := strings.TrimSpace(item.RequesterDeviceID)
	audience := apitypes.NotificationAudienceSystem
	importance := apitypes.NotificationImportanceDebug
	if requesterDeviceID != "" && requesterDeviceID == strings.TrimSpace(localDeviceID) {
		audience = apitypes.NotificationAudienceUser
		importance = apitypes.NotificationImportanceImportant
	} else if requestKind == "local" {
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
	case "scan-library",
		"scan-root",
		"sync-now",
		"connect-peer",
		"join-session",
		"finalize-join-session",
		"ensure-recording-encoding",
		"ensure-album-encodings",
		"ensure-playlist-encodings",
		"publish-checkpoint",
		"compact-checkpoint":
		return apitypes.NotificationAudienceUser, apitypes.NotificationImportanceNormal
	case "install-checkpoint",
		"watch-scan":
		return apitypes.NotificationAudienceSystem, apitypes.NotificationImportanceDebug
	default:
		return apitypes.NotificationAudienceSystem, apitypes.NotificationImportanceDebug
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

func scanActivityProgress(status apitypes.ScanActivityStatus) float64 {
	if status.TracksTotal > 0 {
		return clampNotificationProgress(float64(status.TracksDone) / float64(status.TracksTotal))
	}
	if status.RootsTotal > 0 {
		return clampNotificationProgress(float64(status.RootsDone) / float64(status.RootsTotal))
	}
	return 0.1
}

func artworkActivityProgress(status apitypes.ArtworkActivityStatus) float64 {
	if status.AlbumsTotal > 0 {
		return clampNotificationProgress(float64(status.AlbumsDone) / float64(status.AlbumsTotal))
	}
	return 0.1
}

func scanActivityMessage(status apitypes.ScanActivityStatus) string {
	switch strings.TrimSpace(status.Phase) {
	case "enumerating":
		if root := strings.TrimSpace(status.CurrentRoot); root != "" {
			return "Scanning " + root
		}
		return "Enumerating scan roots..."
	case "ingesting":
		if path := strings.TrimSpace(status.CurrentPath); path != "" {
			return "Ingesting " + path
		}
		return "Ingesting scanned tracks..."
	case "completed":
		return "Scan completed."
	case "failed":
		return "Scan failed."
	default:
		return "Scanning library..."
	}
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
	case "scan-library", "scan-root", "watch-scan":
		return true
	default:
		return false
	}
}

func isNotificationActivePhase(phase string) bool {
	switch phase {
	case string(apitypes.NotificationPhaseQueued), string(apitypes.NotificationPhaseRunning):
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

func loadSettingsState() (settings.State, error) {
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		return settings.State{}, err
	}
	store, err := settings.NewStore(settingsPath)
	if err != nil {
		return settings.State{}, err
	}
	defer store.Close()
	return store.Load()
}

func saveSettingsState(state settings.State) error {
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		return err
	}
	store, err := settings.NewStore(settingsPath)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Save(state)
}
