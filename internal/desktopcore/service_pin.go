package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PinService struct {
	app *App

	refreshMu         sync.Mutex
	refreshTimers     map[string]*time.Timer
	refreshRetryDelay time.Duration
}

type pinCoverageCache struct {
	service         *PinService
	local           apitypes.LocalContext
	likedPlaylistID string
	albumMembers    map[string][]string
	playlistMembers map[string][]string
	variantClusters map[string]string
	variantExists   map[string]bool
	clusterExists   map[string]bool
}

func newPinService(app *App) *PinService {
	service := &PinService{
		app:               app,
		refreshTimers:     make(map[string]*time.Timer),
		refreshRetryDelay: 15 * time.Second,
	}
	if app != nil {
		app.SubscribeCatalogChanges(service.handlePinnedScopeCatalogChange)
	}
	return service
}

func (s *PinService) handlePinnedScopeCatalogChange(event apitypes.CatalogChangeEvent) {
	if s == nil || s.app == nil {
		return
	}
	switch event.Kind {
	case apitypes.CatalogChangeInvalidateAvailability:
		if !event.InvalidateAll {
			return
		}
	case apitypes.CatalogChangeInvalidateBase:
		if !pinCatalogChangeRelevant(event) {
			return
		}
	default:
		return
	}

	local, err := s.app.EnsureLocalContext(context.Background())
	if err != nil || strings.TrimSpace(local.LibraryID) == "" {
		return
	}

	ctx := context.Background()
	if event.Kind == apitypes.CatalogChangeInvalidateAvailability {
		s.schedulePendingPinScopeRefresh(ctx, local, pinnedScopeDebounceWait)
		return
	}
	if recordingPins, albumPins, playlistPins, targeted, err := s.affectedPinRootsForEvent(ctx, local, event); err == nil {
		if targeted {
			for _, pin := range recordingPins {
				s.schedulePinScopeRefresh(pin.LibraryID, pin.Scope, pin.ScopeID, pin.Profile)
			}
			for _, pin := range albumPins {
				s.schedulePinScopeRefresh(pin.LibraryID, pin.Scope, pin.ScopeID, pin.Profile)
			}
			for _, pin := range playlistPins {
				s.schedulePinScopeRefresh(pin.LibraryID, pin.Scope, pin.ScopeID, pin.Profile)
			}
			return
		}
	}

	if !event.InvalidateAll {
		return
	}
	s.scheduleAllPinScopeRefresh(ctx, local, pinnedScopeDebounceWait)
}

func pinCatalogChangeRelevant(event apitypes.CatalogChangeEvent) bool {
	if len(compactNonEmptyStrings(event.AlbumIDs)) > 0 || len(compactNonEmptyStrings(event.RecordingIDs)) > 0 {
		return true
	}
	switch event.Entity {
	case apitypes.CatalogChangeEntityAlbum,
		apitypes.CatalogChangeEntityAlbums,
		apitypes.CatalogChangeEntityTracks,
		apitypes.CatalogChangeEntityAlbumTracks,
		apitypes.CatalogChangeEntityPlaylistTracks,
		apitypes.CatalogChangeEntityLiked:
		return true
	}
	return event.InvalidateAll && event.Entity == ""
}

func newPinCoverageCache(service *PinService, local apitypes.LocalContext) *pinCoverageCache {
	return &pinCoverageCache{
		service:         service,
		local:           local,
		likedPlaylistID: likedPlaylistIDForLibrary(local.LibraryID),
		albumMembers:    make(map[string][]string),
		playlistMembers: make(map[string][]string),
		variantClusters: make(map[string]string),
		variantExists:   make(map[string]bool),
		clusterExists:   make(map[string]bool),
	}
}

func (s *PinService) SubscribePinChanges(listener func(apitypes.PinChangeEvent)) func() {
	if s == nil || s.app == nil {
		return func() {}
	}
	return s.app.SubscribePinChanges(listener)
}

func (s *PinService) StartPin(ctx context.Context, req apitypes.PinIntentRequest) (JobSnapshot, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}

	subject, profile, err := s.normalizeSubject(local, req.Subject, req.Profile)
	if err != nil {
		return JobSnapshot{}, err
	}

	switch subject.Kind {
	case apitypes.PinSubjectRecordingCluster, apitypes.PinSubjectRecordingVariant:
		return s.startRecordingPin(ctx, local, subject.ID, profile)
	case apitypes.PinSubjectAlbumVariant:
		scopeID, recordingIDs, resolveProfile, err := s.app.playback.resolvePinScope(ctx, local, "album", subject.ID, profile)
		if err != nil {
			return JobSnapshot{}, err
		}
		if err := s.app.playback.validateAlbumPinStart(ctx, local, recordingIDs, resolveProfile); err != nil {
			if strings.Contains(err.Error(), "local albums do not need offline pinning") {
				return s.startLocalOnlyScopePinJob(ctx, local, "album", scopeID, resolveProfile, recordingIDs, jobKindPinAlbum, "queued album pin", "album pin recorded")
			}
			return JobSnapshot{}, err
		}
		return s.startCollectionPin(ctx, local, "album", scopeID, recordingIDs, resolveProfile, jobKindPinAlbum, "queued album pin", "album pin canceled because the library is no longer active", "album pin")
	case apitypes.PinSubjectPlaylist, apitypes.PinSubjectLikedPlaylist:
		scopeID, recordingIDs, resolveProfile, err := s.app.playback.resolvePinScope(ctx, local, "playlist", subject.ID, profile)
		if err != nil {
			return JobSnapshot{}, err
		}
		return s.startCollectionPin(ctx, local, "playlist", scopeID, recordingIDs, resolveProfile, jobKindPinPlaylist, "queued playlist pin", "playlist pin canceled because the library is no longer active", "playlist pin")
	default:
		return JobSnapshot{}, fmt.Errorf("unsupported pin subject kind %q", subject.Kind)
	}
}

func (s *PinService) Unpin(ctx context.Context, req apitypes.PinIntentRequest) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}

	subject, _, err := s.normalizeSubject(local, req.Subject, req.Profile)
	if err != nil {
		return err
	}

	switch subject.Kind {
	case apitypes.PinSubjectRecordingCluster, apitypes.PinSubjectRecordingVariant:
		return s.unpinRecording(ctx, local, subject.ID)
	case apitypes.PinSubjectAlbumVariant:
		return s.unpinAlbum(ctx, local, subject.ID)
	case apitypes.PinSubjectPlaylist:
		return s.unpinPlaylist(ctx, local, subject.ID)
	case apitypes.PinSubjectLikedPlaylist:
		return s.unpinLiked(ctx, local)
	default:
		return fmt.Errorf("unsupported pin subject kind %q", subject.Kind)
	}
}

func (s *PinService) startRecordingPin(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (JobSnapshot, error) {
	target, err := s.app.playback.resolveRecordingPinTarget(ctx, local, recordingID, profile)
	if err != nil {
		return JobSnapshot{}, err
	}
	if err := s.upsertPinRoot(ctx, local, "recording", target.scopeID, target.profile); err != nil {
		return JobSnapshot{}, err
	}
	s.app.playback.emitPinAvailabilityInvalidation(local, "recording", target.scopeID, compactNonEmptyStrings([]string{target.scopeRecordingID, target.clusterID}))

	jobID := pinJobID(local.LibraryID, "recording", target.scopeID, target.profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindPinRecording,
		local.LibraryID,
		"queued track pin",
		"track pin canceled because the library is no longer active",
		func(runCtx context.Context) {
			s.app.playback.runRecordingPinJob(runCtx, local, target)
		},
	)
}

func (s *PinService) startCollectionPin(
	ctx context.Context,
	local apitypes.LocalContext,
	scope string,
	scopeID string,
	recordingIDs []string,
	profile string,
	jobKind string,
	queuedMessage string,
	canceledMessage string,
	label string,
) (JobSnapshot, error) {
	if err := s.upsertPinRoot(ctx, local, scope, scopeID, profile); err != nil {
		return JobSnapshot{}, err
	}
	s.app.playback.emitPinAvailabilityInvalidation(local, scope, scopeID, recordingIDs)

	jobID := pinJobID(local.LibraryID, scope, scopeID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKind,
		local.LibraryID,
		queuedMessage,
		canceledMessage,
		func(runCtx context.Context) {
			s.app.playback.runPinScopeJob(runCtx, local, scope, scopeID, profile, recordingIDs, jobKind, label)
		},
	)
}

func (s *PinService) unpinRecording(ctx context.Context, local apitypes.LocalContext, recordingID string) error {
	recordingID = strings.TrimSpace(recordingID)
	availabilityIDs := []string{recordingID}
	if exactRecordingID, ok, resolveErr := s.app.playback.trackVariantExists(ctx, local.LibraryID, recordingID); resolveErr == nil && ok && strings.TrimSpace(exactRecordingID) != "" {
		recordingID = strings.TrimSpace(exactRecordingID)
		if clusterID, clusterOK, clusterErr := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID); clusterErr == nil && clusterOK && strings.TrimSpace(clusterID) != "" && strings.TrimSpace(clusterID) != recordingID {
			availabilityIDs = append(availabilityIDs, strings.TrimSpace(clusterID))
		}
	} else if resolvedRecordingID, ok, resolveErr := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID); resolveErr == nil && ok && strings.TrimSpace(resolvedRecordingID) != "" {
		recordingID = strings.TrimSpace(resolvedRecordingID)
	}
	if err := s.deletePinRoot(ctx, local, "recording", recordingID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:         apitypes.CatalogChangeInvalidateAvailability,
		Entity:       apitypes.CatalogChangeEntityTracks,
		RecordingIDs: compactNonEmptyStrings(availabilityIDs),
	})
	s.app.emitPinChange(apitypes.PinChangeEvent{InvalidateAll: true})
	return nil
}

func (s *PinService) unpinAlbum(ctx context.Context, local apitypes.LocalContext, albumID string) error {
	if albumScopes, err := s.app.playback.resolveAlbumScopes(ctx, local, []string{albumID}); err == nil {
		if resolvedAlbumID := strings.TrimSpace(albumScopes[albumID].ResolvedAlbumID); resolvedAlbumID != "" {
			albumID = resolvedAlbumID
		}
	}
	if err := s.deletePinRoot(ctx, local, "album", albumID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateAvailability,
		Entity:   apitypes.CatalogChangeEntityAlbum,
		EntityID: albumID,
		AlbumIDs: []string{albumID},
	})
	s.app.emitPinChange(apitypes.PinChangeEvent{InvalidateAll: true})
	return nil
}

func (s *PinService) unpinPlaylist(ctx context.Context, local apitypes.LocalContext, playlistID string) error {
	if err := s.deletePinRoot(ctx, local, "playlist", playlistID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateAvailability,
		Entity:   apitypes.CatalogChangeEntityPlaylistTracks,
		EntityID: playlistID,
		QueryKey: "playlistTracks:" + playlistID,
	})
	s.app.emitPinChange(apitypes.PinChangeEvent{InvalidateAll: true})
	return nil
}

func (s *PinService) unpinLiked(ctx context.Context, local apitypes.LocalContext) error {
	playlistID := likedPlaylistIDForLibrary(local.LibraryID)
	if err := s.deletePinRoot(ctx, local, "playlist", playlistID); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateAvailability,
		Entity:   apitypes.CatalogChangeEntityLiked,
		EntityID: playlistID,
		QueryKey: "liked",
	})
	s.app.emitPinChange(apitypes.PinChangeEvent{InvalidateAll: true})
	return nil
}

func (s *PinService) upsertPinRoot(ctx context.Context, local apitypes.LocalContext, scope, scopeID, profile string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scope == "" || scopeID == "" {
		return fmt.Errorf("pin root scope and scope id are required")
	}
	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var existing PinRoot
		err := tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
			Take(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		profile = strings.TrimSpace(profile)
		if err == nil && strings.TrimSpace(existing.Profile) == profile {
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "device_id"},
				{Name: "scope"},
				{Name: "scope_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"profile", "updated_at"}),
		}).Create(&PinRoot{
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
			Scope:     scope,
			ScopeID:   scopeID,
			Profile:   profile,
			CreatedAt: now,
			UpdatedAt: now,
		}).Error
	}); err != nil {
		return err
	}
	return s.reconcileScope(ctx, local, scope, scopeID, profile)
}

func (s *PinService) deletePinRoot(ctx context.Context, local apitypes.LocalContext, scope, scopeID string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return fmt.Errorf("%s id is required", scope)
	}
	return s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
			Delete(&PinBlobRef{}).Error; err != nil {
			return err
		}
		if err := tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
			Delete(&PinMember{}).Error; err != nil {
			return err
		}
		result := tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
			Delete(&PinRoot{})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		return nil
	})
}

func (s *PinService) GetPinState(ctx context.Context, req apitypes.PinStateRequest) (apitypes.PinState, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PinState{}, err
	}

	subject, profile, err := s.normalizeSubject(local, req.Subject, req.Profile)
	if err != nil {
		return apitypes.PinState{}, err
	}
	pins, err := s.loadLocalPins(ctx, local)
	if err != nil {
		return apitypes.PinState{}, err
	}

	cache := newPinCoverageCache(s, local)
	return s.pinStateForSubject(ctx, cache, pins, subject, profile), nil
}

func (s *PinService) ListPinStates(ctx context.Context, req apitypes.PinStateListRequest) ([]apitypes.PinState, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	pins, err := s.loadLocalPins(ctx, local)
	if err != nil {
		return nil, err
	}

	cache := newPinCoverageCache(s, local)
	profile := s.app.playback.resolvePlaybackProfile(req.Profile)
	out := make([]apitypes.PinState, 0, len(req.Subjects))
	for _, subject := range req.Subjects {
		normalized, _, normalizeErr := s.normalizeSubject(local, subject, profile)
		if normalizeErr != nil {
			continue
		}
		out = append(out, s.pinStateForSubject(ctx, cache, pins, normalized, profile))
	}
	return out, nil
}

func (s *PinService) pinStateForSubject(
	ctx context.Context,
	cache *pinCoverageCache,
	pins []PinRoot,
	subject apitypes.PinSubjectRef,
	profile string,
) apitypes.PinState {
	subject.ID = strings.TrimSpace(subject.ID)
	profile = s.app.playback.resolvePlaybackProfile(profile)
	state := apitypes.PinState{
		Subject: subject,
	}
	if subject.Kind == "" || subject.ID == "" {
		return state
	}

	directSources := s.directSourcesForSubject(cache, pins, subject)
	coveredSources := s.coveredSourcesForSubject(ctx, cache, pins, subject)
	state.Direct = len(directSources) > 0
	state.Covered = len(coveredSources) > 0
	state.Pinned = state.Direct || state.Covered
	state.Sources = append(state.Sources, directSources...)
	state.Sources = append(state.Sources, coveredSources...)
	state.Sources = uniquePinSources(state.Sources)
	if state.Pinned {
		state.Pending = s.pendingForSubject(ctx, cache, subject, profile)
	}
	return state
}

func (s *PinService) normalizeSubject(local apitypes.LocalContext, subject apitypes.PinSubjectRef, profile string) (apitypes.PinSubjectRef, string, error) {
	subject.Kind = apitypes.PinSubjectKind(strings.TrimSpace(string(subject.Kind)))
	subject.ID = strings.TrimSpace(subject.ID)
	profile = s.app.playback.resolvePlaybackProfile(profile)

	switch subject.Kind {
	case apitypes.PinSubjectRecordingCluster,
		apitypes.PinSubjectRecordingVariant,
		apitypes.PinSubjectAlbumVariant,
		apitypes.PinSubjectPlaylist:
		if subject.ID == "" {
			return apitypes.PinSubjectRef{}, "", fmt.Errorf("%s id is required", subject.Kind)
		}
	case apitypes.PinSubjectLikedPlaylist:
		if subject.ID == "" {
			subject.ID = likedPlaylistIDForLibrary(local.LibraryID)
		}
	default:
		return apitypes.PinSubjectRef{}, "", fmt.Errorf("unsupported pin subject kind %q", subject.Kind)
	}

	return subject, profile, nil
}

func (s *PinService) loadLocalPins(ctx context.Context, local apitypes.LocalContext) ([]PinRoot, error) {
	var pins []PinRoot
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", local.LibraryID, local.DeviceID).
		Order("scope ASC, scope_id ASC").
		Find(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

func (s *PinService) directSourcesForSubject(cache *pinCoverageCache, pins []PinRoot, subject apitypes.PinSubjectRef) []apitypes.PinSourceRef {
	out := make([]apitypes.PinSourceRef, 0, 1)
	for _, pin := range pins {
		if !s.pinMatchesSubject(pin, subject) {
			continue
		}
		source := pinSourceRef(cache, pin)
		source.Subject = subject
		source.Direct = true
		out = append(out, source)
	}
	return uniquePinSources(out)
}

func (s *PinService) coveredSourcesForSubject(
	ctx context.Context,
	cache *pinCoverageCache,
	pins []PinRoot,
	subject apitypes.PinSubjectRef,
) []apitypes.PinSourceRef {
	switch subject.Kind {
	case apitypes.PinSubjectRecordingCluster:
		return s.coveredSourcesForRecording(ctx, cache, pins, subject.ID, "", subject)
	case apitypes.PinSubjectRecordingVariant:
		return s.coveredSourcesForRecording(ctx, cache, pins, subject.ID, subject.ID, subject)
	case apitypes.PinSubjectAlbumVariant:
		recordingIDs, err := cache.albumMemberIDs(ctx, subject.ID)
		if err != nil {
			return nil
		}
		return s.coveredSourcesForCollection(ctx, cache, pins, recordingIDs, subject)
	case apitypes.PinSubjectPlaylist, apitypes.PinSubjectLikedPlaylist:
		recordingIDs, err := cache.playlistMemberIDs(ctx, subject.ID)
		if err != nil {
			return nil
		}
		return s.coveredSourcesForCollection(ctx, cache, pins, recordingIDs, subject)
	default:
		return nil
	}
}

func (s *PinService) coveredSourcesForCollection(
	ctx context.Context,
	cache *pinCoverageCache,
	pins []PinRoot,
	recordingIDs []string,
	subject apitypes.PinSubjectRef,
) []apitypes.PinSourceRef {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil
	}

	sources := make([]apitypes.PinSourceRef, 0)
	for _, recordingID := range recordingIDs {
		recordingSources := s.coveredSourcesForRecording(ctx, cache, pins, recordingID, "", subject)
		if len(recordingSources) == 0 {
			return nil
		}
		sources = append(sources, recordingSources...)
	}
	return uniquePinSources(sources)
}

func (s *PinService) coveredSourcesForRecording(
	ctx context.Context,
	cache *pinCoverageCache,
	pins []PinRoot,
	recordingID string,
	variantID string,
	subject apitypes.PinSubjectRef,
) []apitypes.PinSourceRef {
	recordingID = strings.TrimSpace(recordingID)
	variantID = strings.TrimSpace(variantID)
	if recordingID == "" {
		return nil
	}

	clusterID := recordingID
	if variantID != "" {
		if resolvedClusterID, ok, err := cache.variantClusterID(ctx, variantID); err == nil && ok {
			clusterID = resolvedClusterID
		}
	} else if resolvedClusterID, ok, err := cache.variantClusterID(ctx, recordingID); err == nil && ok {
		variantID = recordingID
		clusterID = resolvedClusterID
	}

	out := make([]apitypes.PinSourceRef, 0)
	for _, pin := range pins {
		source := pinSourceRef(cache, pin)
		if source.Subject.Kind == subject.Kind && source.Subject.ID == subject.ID {
			continue
		}
		switch strings.TrimSpace(pin.Scope) {
		case "recording":
			scopeID := strings.TrimSpace(pin.ScopeID)
			if scopeID == "" {
				continue
			}
			if variantID != "" && scopeID == variantID {
				out = append(out, source)
				continue
			}
			if scopeID == clusterID {
				out = append(out, source)
				continue
			}
			if resolvedClusterID, ok, err := cache.variantClusterID(ctx, scopeID); err == nil && ok && resolvedClusterID == clusterID {
				out = append(out, source)
			}
		case "album":
			memberIDs, err := cache.albumMemberIDs(ctx, pin.ScopeID)
			if err != nil || len(memberIDs) == 0 {
				continue
			}
			if recordingIDsContain(ctx, cache, memberIDs, clusterID, variantID) {
				out = append(out, source)
			}
		case "playlist":
			memberIDs, err := cache.playlistMemberIDs(ctx, pin.ScopeID)
			if err != nil || len(memberIDs) == 0 {
				continue
			}
			if recordingIDsContain(ctx, cache, memberIDs, clusterID, variantID) {
				out = append(out, source)
			}
		}
	}
	return uniquePinSources(out)
}

func (s *PinService) pendingForSubject(
	ctx context.Context,
	cache *pinCoverageCache,
	subject apitypes.PinSubjectRef,
	profile string,
) bool {
	switch subject.Kind {
	case apitypes.PinSubjectRecordingCluster, apitypes.PinSubjectRecordingVariant:
		return s.recordingPending(ctx, subject.ID, profile)
	case apitypes.PinSubjectAlbumVariant:
		recordingIDs, err := cache.albumMemberIDs(ctx, subject.ID)
		if err != nil {
			return false
		}
		return s.collectionPending(ctx, recordingIDs, profile)
	case apitypes.PinSubjectPlaylist, apitypes.PinSubjectLikedPlaylist:
		recordingIDs, err := cache.playlistMemberIDs(ctx, subject.ID)
		if err != nil {
			return false
		}
		return s.collectionPending(ctx, recordingIDs, profile)
	default:
		return false
	}
}

func (s *PinService) collectionPending(ctx context.Context, recordingIDs []string, profile string) bool {
	local, err := s.app.EnsureLocalContext(ctx)
	if err != nil {
		return false
	}
	for _, recordingID := range compactNonEmptyStrings(recordingIDs) {
		if s.recordingPendingWithLocal(ctx, local, recordingID, profile) {
			return true
		}
	}
	return false
}

func (s *PinService) recordingPending(ctx context.Context, recordingID, profile string) bool {
	local, err := s.app.EnsureLocalContext(ctx)
	if err != nil {
		return false
	}
	return s.recordingPendingWithLocal(ctx, local, recordingID, profile)
}

func (s *PinService) recordingPendingWithLocal(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) bool {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return false
	}
	if _, ok, err := s.app.playback.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, recordingID); err == nil && ok {
		return false
	}
	if ok, err := s.hasPinnedCachedEncoding(ctx, local, recordingID, profile); err == nil && ok {
		return false
	}
	return true
}

func (s *PinService) hasPinnedCachedEncoding(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (bool, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return false, nil
	}
	if exactID, ok, err := s.app.playback.trackVariantExists(ctx, local.LibraryID, recordingID); err != nil {
		return false, err
	} else if ok && strings.TrimSpace(exactID) != "" {
		blobIDs, err := s.app.cache.cachedBlobIDsForResolvedRecordings(ctx, local.LibraryID, local.DeviceID, []string{strings.TrimSpace(exactID)}, profile)
		if err != nil {
			return false, err
		}
		return len(blobIDs) > 0, nil
	}
	if _, _, ok, err := s.app.playback.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, recordingID, profile); err != nil {
		return false, err
	} else {
		return ok, nil
	}
}

func pinSourceRef(cache *pinCoverageCache, pin PinRoot) apitypes.PinSourceRef {
	scope := strings.TrimSpace(pin.Scope)
	scopeID := strings.TrimSpace(pin.ScopeID)
	profile := strings.TrimSpace(pin.Profile)

	switch scope {
	case "recording":
		kind := apitypes.PinSubjectRecordingCluster
		if cache != nil && scopeID != "" {
			if exact, ok := cache.variantExistsValue(scopeID); ok && exact {
				kind = apitypes.PinSubjectRecordingVariant
			}
		}
		return apitypes.PinSourceRef{
			Subject: apitypes.PinSubjectRef{Kind: kind, ID: scopeID},
			Profile: profile,
		}
	case "album":
		return apitypes.PinSourceRef{
			Subject: apitypes.PinSubjectRef{Kind: apitypes.PinSubjectAlbumVariant, ID: scopeID},
			Profile: profile,
		}
	case "playlist":
		kind := apitypes.PinSubjectPlaylist
		if cache != nil && scopeID == cache.likedPlaylistID {
			kind = apitypes.PinSubjectLikedPlaylist
		}
		return apitypes.PinSourceRef{
			Subject: apitypes.PinSubjectRef{Kind: kind, ID: scopeID},
			Profile: profile,
		}
	default:
		return apitypes.PinSourceRef{}
	}
}

func uniquePinSources(items []apitypes.PinSourceRef) []apitypes.PinSourceRef {
	if len(items) == 0 {
		return nil
	}
	out := make([]apitypes.PinSourceRef, 0, len(items))
	seen := make(map[string]int, len(items))
	for _, item := range items {
		item.Subject.Kind = apitypes.PinSubjectKind(strings.TrimSpace(string(item.Subject.Kind)))
		item.Subject.ID = strings.TrimSpace(item.Subject.ID)
		item.Profile = strings.TrimSpace(item.Profile)
		if item.Subject.Kind == "" || item.Subject.ID == "" {
			continue
		}
		key := string(item.Subject.Kind) + "|" + item.Subject.ID + "|" + item.Profile
		if index, ok := seen[key]; ok {
			if item.Direct {
				out[index].Direct = true
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	return out
}

func (s *PinService) startLocalOnlyScopePinJob(
	ctx context.Context,
	local apitypes.LocalContext,
	scope string,
	scopeID string,
	profile string,
	recordingIDs []string,
	jobKind string,
	queuedMessage string,
	completedMessage string,
) (JobSnapshot, error) {
	if err := s.upsertPinRoot(ctx, local, scope, scopeID, profile); err != nil {
		return JobSnapshot{}, err
	}
	s.app.playback.emitPinAvailabilityInvalidation(local, scope, scopeID, recordingIDs)
	jobID := pinJobID(local.LibraryID, scope, scopeID, profile)
	return s.app.startActiveLibraryJob(
		ctx,
		jobID,
		jobKind,
		local.LibraryID,
		queuedMessage,
		completedMessage,
		func(runCtx context.Context) {
			job := s.app.jobs.Track(jobID, jobKind, local.LibraryID)
			if job == nil {
				return
			}
			job.Queued(0, queuedMessage)
			job.Complete(1, completedMessage)
			_ = runCtx
		},
	)
}

func (s *PinService) affectedPinRootsForEvent(ctx context.Context, local apitypes.LocalContext, event apitypes.CatalogChangeEvent) ([]PinRoot, []PinRoot, []PinRoot, bool, error) {
	albumIDs := compactNonEmptyStrings(event.AlbumIDs)
	recordingIDs := compactNonEmptyStrings(event.RecordingIDs)
	playlistIDs := []string{}

	switch event.Entity {
	case apitypes.CatalogChangeEntityAlbum:
		if entityID := strings.TrimSpace(event.EntityID); entityID != "" {
			albumIDs = append(albumIDs, entityID)
		}
	case apitypes.CatalogChangeEntityPlaylistTracks, apitypes.CatalogChangeEntityLiked:
		if entityID := strings.TrimSpace(event.EntityID); entityID != "" {
			playlistIDs = append(playlistIDs, entityID)
		}
	}

	if len(recordingIDs) > 0 {
		recordingAlbumIDs, err := s.albumIDsForRecordings(ctx, local.LibraryID, recordingIDs)
		if err != nil {
			return nil, nil, nil, false, err
		}
		recordingPlaylistIDs, err := s.playlistIDsForRecordings(ctx, local.LibraryID, recordingIDs)
		if err != nil {
			return nil, nil, nil, false, err
		}
		albumIDs = append(albumIDs, recordingAlbumIDs...)
		playlistIDs = append(playlistIDs, recordingPlaylistIDs...)
	}

	albumClusterIDs, err := s.albumClusterIDs(ctx, local.LibraryID, albumIDs)
	if err != nil {
		return nil, nil, nil, false, err
	}
	playlistIDs = compactNonEmptyStrings(playlistIDs)
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(albumClusterIDs) == 0 && len(playlistIDs) == 0 && len(recordingIDs) == 0 {
		return nil, nil, nil, false, nil
	}

	recordingPins, err := s.listPinRoots(ctx, local.LibraryID, local.DeviceID, map[string][]string{"recording": recordingIDs})
	if err != nil {
		return nil, nil, nil, false, err
	}
	albumPins, err := s.listAlbumPinRootsForClusters(ctx, local.LibraryID, local.DeviceID, albumClusterIDs)
	if err != nil {
		return nil, nil, nil, false, err
	}
	playlistPins, err := s.listPinRoots(ctx, local.LibraryID, local.DeviceID, map[string][]string{"playlist": playlistIDs})
	if err != nil {
		return nil, nil, nil, false, err
	}
	return recordingPins, albumPins, playlistPins, true, nil
}

func (s *PinService) albumClusterIDs(ctx context.Context, libraryID string, albumIDs []string) ([]string, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(albumIDs))
	seen := make(map[string]struct{}, len(albumIDs))
	for _, albumID := range albumIDs {
		clusterID := strings.TrimSpace(albumID)
		if resolvedClusterID, ok, err := s.app.catalog.albumClusterIDForVariant(ctx, libraryID, albumID); err != nil {
			return nil, err
		} else if ok && strings.TrimSpace(resolvedClusterID) != "" {
			clusterID = strings.TrimSpace(resolvedClusterID)
		}
		if clusterID == "" {
			continue
		}
		if _, ok := seen[clusterID]; ok {
			continue
		}
		seen[clusterID] = struct{}{}
		out = append(out, clusterID)
	}
	return out, nil
}

func (s *PinService) albumIDsForRecordings(ctx context.Context, libraryID string, recordingIDs []string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}

	type row struct{ AlbumID string }
	var rows []row
	query := `
SELECT DISTINCT av.album_cluster_id AS album_id
FROM album_tracks at
JOIN album_variants av ON av.library_id = at.library_id AND av.album_variant_id = at.album_variant_id
JOIN track_variants tv ON tv.library_id = at.library_id AND tv.track_variant_id = at.track_variant_id
WHERE at.library_id = ? AND (tv.track_variant_id IN ? OR tv.track_cluster_id IN ?)`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, recordingIDs, recordingIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.AlbumID))
	}
	return compactNonEmptyStrings(out), nil
}

func (s *PinService) playlistIDsForRecordings(ctx context.Context, libraryID string, recordingIDs []string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}

	type variantRow struct {
		TrackVariantID string
		TrackClusterID string
	}
	var variants []variantRow
	if err := s.app.storage.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Select("track_variant_id, track_cluster_id").
		Where("library_id = ? AND (track_variant_id IN ? OR track_cluster_id IN ?)", libraryID, recordingIDs, recordingIDs).
		Scan(&variants).Error; err != nil {
		return nil, err
	}
	clusterIDs := make([]string, 0, len(variants))
	seen := make(map[string]struct{}, len(variants))
	for _, row := range variants {
		clusterID := strings.TrimSpace(row.TrackClusterID)
		if clusterID == "" {
			continue
		}
		if _, ok := seen[clusterID]; ok {
			continue
		}
		seen[clusterID] = struct{}{}
		clusterIDs = append(clusterIDs, clusterID)
	}
	if len(clusterIDs) == 0 {
		return nil, nil
	}

	type row struct{ PlaylistID string }
	var rows []row
	query := `
SELECT DISTINCT playlist_id
FROM playlist_items
WHERE library_id = ? AND deleted_at IS NULL AND track_variant_id IN ?`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimSpace(row.PlaylistID))
	}
	return compactNonEmptyStrings(out), nil
}

func (s *PinService) listPinRoots(ctx context.Context, libraryID, deviceID string, byScope map[string][]string) ([]PinRoot, error) {
	var pins []PinRoot
	query := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID)
	if byScope != nil {
		recordingIDs := compactNonEmptyStrings(byScope["recording"])
		albumIDs := compactNonEmptyStrings(byScope["album"])
		playlistIDs := compactNonEmptyStrings(byScope["playlist"])
		if len(recordingIDs) == 0 && len(albumIDs) == 0 && len(playlistIDs) == 0 {
			return []PinRoot{}, nil
		}

		clauses := make([]string, 0, 3)
		args := make([]any, 0, 6)
		if len(recordingIDs) > 0 {
			clauses = append(clauses, "(scope = ? AND scope_id IN ?)")
			args = append(args, "recording", recordingIDs)
		}
		if len(albumIDs) > 0 {
			clauses = append(clauses, "(scope = ? AND scope_id IN ?)")
			args = append(args, "album", albumIDs)
		}
		if len(playlistIDs) > 0 {
			clauses = append(clauses, "(scope = ? AND scope_id IN ?)")
			args = append(args, "playlist", playlistIDs)
		}
		query = query.Where(strings.Join(clauses, " OR "), args...)
	}
	if err := query.Find(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

func (s *PinService) listAlbumPinRootsForClusters(ctx context.Context, libraryID, deviceID string, clusterIDs []string) ([]PinRoot, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return []PinRoot{}, nil
	}

	var pins []PinRoot
	query := `
SELECT pr.library_id, pr.device_id, pr.scope, pr.scope_id, pr.profile, pr.created_at, pr.updated_at
FROM pin_roots pr
JOIN album_variants av ON av.library_id = pr.library_id AND av.album_variant_id = pr.scope_id
WHERE pr.library_id = ? AND pr.device_id = ? AND pr.scope = 'album' AND av.album_cluster_id IN ?`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, deviceID, clusterIDs).Scan(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

func (s *PinService) schedulePinScopeRefresh(libraryID, scope, scopeID, profile string) {
	s.schedulePinScopeRefreshAfter(libraryID, scope, scopeID, profile, pinnedScopeDebounceWait)
}

func (s *PinService) schedulePinScopeRefreshRetry(libraryID, scope, scopeID, profile string) {
	if s == nil {
		return
	}
	delay := s.refreshRetryDelay
	if delay <= 0 {
		delay = pinnedScopeDebounceWait
	}
	s.schedulePinScopeRefreshAfter(libraryID, scope, scopeID, profile, delay)
}

type pinScopeRefreshLoader func(context.Context, string, string) ([]PinRoot, error)

func (s *PinService) scheduleAllPinScopeRefresh(ctx context.Context, local apitypes.LocalContext, delay time.Duration) {
	s.schedulePinScopeRefreshes(ctx, local, delay, s.loadAllPinRootsForRefresh)
}

func (s *PinService) schedulePendingPinScopeRefresh(ctx context.Context, local apitypes.LocalContext, delay time.Duration) {
	s.schedulePinScopeRefreshes(ctx, local, delay, s.loadPendingPinRootsForRefresh)
}

func (s *PinService) schedulePinScopeRefreshes(ctx context.Context, local apitypes.LocalContext, delay time.Duration, load pinScopeRefreshLoader) {
	if s == nil || s.app == nil || load == nil {
		return
	}
	libraryID := strings.TrimSpace(local.LibraryID)
	deviceID := strings.TrimSpace(local.DeviceID)
	if libraryID == "" || deviceID == "" {
		return
	}
	pins, err := load(ctx, libraryID, deviceID)
	if err != nil {
		return
	}
	for _, pin := range pins {
		s.schedulePinScopeRefreshAfter(pin.LibraryID, pin.Scope, pin.ScopeID, pin.Profile, delay)
	}
}

func (s *PinService) loadAllPinRootsForRefresh(ctx context.Context, libraryID, deviceID string) ([]PinRoot, error) {
	return s.listPinRoots(ctx, libraryID, deviceID, nil)
}

func (s *PinService) loadPendingPinRootsForRefresh(ctx context.Context, libraryID, deviceID string) ([]PinRoot, error) {
	var pins []PinRoot
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND pending_count > 0", libraryID, deviceID).
		Find(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

func (s *PinService) schedulePinScopeRefreshAfter(libraryID, scope, scopeID, profile string, delay time.Duration) {
	libraryID = strings.TrimSpace(libraryID)
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	profile = strings.TrimSpace(profile)
	if libraryID == "" || scope == "" || scopeID == "" || profile == "" {
		return
	}
	if delay <= 0 {
		delay = pinnedScopeDebounceWait
	}

	jobID := refreshPinScopeJobID(libraryID, scope, scopeID, profile)
	s.refreshMu.Lock()
	if timer, ok := s.refreshTimers[jobID]; ok {
		timer.Reset(delay)
		s.refreshMu.Unlock()
		return
	}
	s.refreshTimers[jobID] = time.AfterFunc(delay, func() {
		s.refreshMu.Lock()
		delete(s.refreshTimers, jobID)
		s.refreshMu.Unlock()

		_, _ = s.app.startActiveLibraryJob(
			context.Background(),
			jobID,
			refreshPinScopeJobKind(scope),
			libraryID,
			"queued pinned scope refresh",
			"pinned scope refresh canceled because the library is no longer active",
			func(runCtx context.Context) {
				s.runPinScopeRefreshJob(runCtx, libraryID, scope, scopeID, profile)
			},
		)
	})
	s.refreshMu.Unlock()
}

func (s *PinService) runPinScopeRefreshJob(ctx context.Context, libraryID, scope, scopeID, profile string) {
	kind := refreshPinScopeJobKind(scope)
	job := s.app.jobs.Track(refreshPinScopeJobID(libraryID, scope, scopeID, profile), kind, libraryID)
	if job == nil {
		return
	}
	job.Queued(0, "queued pinned scope refresh")

	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}
	if strings.TrimSpace(local.LibraryID) != strings.TrimSpace(libraryID) {
		job.Fail(0, "pinned scope refresh canceled because the library is no longer active", nil)
		return
	}

	var pin PinRoot
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
		Take(&pin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			job.Complete(1, "Pinned scope no longer active")
			return
		}
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}

	var resolvedScopeID string
	var recordingIDs []string
	switch strings.TrimSpace(scope) {
	case "recording":
		target, resolveErr := s.app.playback.resolveRecordingPinTarget(ctx, local, scopeID, profile)
		if resolveErr != nil {
			job.Fail(0, "pinned scope refresh failed", resolveErr)
			return
		}
		resolvedScopeID = target.scopeID
		recordingIDs = compactNonEmptyStrings([]string{target.scopeRecordingID, target.clusterID})
	case "album", "playlist":
		var resolveErr error
		resolvedScopeID, recordingIDs, _, resolveErr = s.app.playback.resolvePinScope(ctx, local, scope, scopeID, profile)
		if resolveErr != nil {
			job.Fail(0, "pinned scope refresh failed", resolveErr)
			return
		}
	default:
		job.Fail(0, "pinned scope refresh failed", fmt.Errorf("unsupported pin scope %q", scope))
		return
	}
	pendingIDs, err := s.filterRecordingsNeedingPinFetch(ctx, local, recordingIDs, profile)
	if err != nil {
		job.Fail(0, "pinned scope refresh failed", err)
		return
	}
	if len(pendingIDs) == 0 {
		if err := s.reconcileScope(ctx, local, scope, resolvedScopeID, profile); err != nil {
			job.Fail(0, "pinned scope refresh failed", err)
			return
		}
		s.app.playback.emitPinAvailabilityInvalidation(local, scope, resolvedScopeID, recordingIDs)
		job.Complete(1, "Pinned scope already up to date")
		return
	}

	outcome, err := s.app.playback.materializePinScopeWithJob(ctx, local, scope, resolvedScopeID, pendingIDs, profile, job)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			job.Fail(pinJobProgress(outcome.result.Tracks, maxInt(outcome.total, 1)), "pinned scope refresh canceled", nil)
			return
		}
		job.Fail(pinJobProgress(outcome.result.Tracks, maxInt(outcome.total, 1)), "pinned scope refresh failed", err)
		return
	}
	job.Complete(1, pinScopeCompletionMessage(outcome.result, outcome.pendingCount, outcome.total))
	if outcome.pendingCount > 0 {
		s.schedulePinScopeRefreshRetry(local.LibraryID, scope, resolvedScopeID, profile)
	}
	s.app.playback.emitPinAvailabilityInvalidation(local, scope, resolvedScopeID, recordingIDs)
}

func (s *PinService) filterRecordingsNeedingPinFetch(ctx context.Context, local apitypes.LocalContext, recordingIDs []string, preferredProfile string) ([]string, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}
	profile := s.app.playback.resolvePlaybackProfile(preferredProfile)

	out := make([]string, 0, len(recordingIDs))
	for _, recordingID := range recordingIDs {
		if _, ok, err := s.app.playback.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, recordingID); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		if ok, err := s.hasPinnedCachedEncoding(ctx, local, recordingID, profile); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		out = append(out, recordingID)
	}
	return out, nil
}

func pinJobID(libraryID, scope, scopeID, profile string) string {
	return "pin:" + strings.TrimSpace(libraryID) + ":" + strings.TrimSpace(scope) + ":" + strings.TrimSpace(scopeID) + ":" + strings.TrimSpace(profile)
}

func refreshPinScopeJobID(libraryID, scope, scopeID, profile string) string {
	return "pin:refresh:" + strings.TrimSpace(libraryID) + ":" + strings.TrimSpace(scope) + ":" + strings.TrimSpace(scopeID) + ":" + strings.TrimSpace(profile)
}

func refreshPinScopeJobKind(scope string) string {
	switch strings.TrimSpace(scope) {
	case "recording":
		return jobKindRefreshPinnedRecording
	case "album":
		return jobKindRefreshPinnedAlbum
	case "playlist":
		return jobKindRefreshPinnedPlaylist
	default:
		return jobKindRefreshPinnedPlaylist
	}
}

func (s *PinService) pinMatchesSubject(pin PinRoot, subject apitypes.PinSubjectRef) bool {
	scope := strings.TrimSpace(pin.Scope)
	scopeID := strings.TrimSpace(pin.ScopeID)
	switch subject.Kind {
	case apitypes.PinSubjectRecordingCluster:
		return scope == "recording" && scopeID == subject.ID
	case apitypes.PinSubjectRecordingVariant:
		return scope == "recording" && scopeID == subject.ID
	case apitypes.PinSubjectAlbumVariant:
		return scope == "album" && scopeID == subject.ID
	case apitypes.PinSubjectPlaylist:
		return scope == "playlist" && scopeID == subject.ID
	case apitypes.PinSubjectLikedPlaylist:
		return scope == "playlist" && scopeID == subject.ID
	default:
		return false
	}
}

func recordingIDsContain(ctx context.Context, cache *pinCoverageCache, memberIDs []string, clusterID, variantID string) bool {
	for _, memberID := range compactNonEmptyStrings(memberIDs) {
		if variantID != "" && memberID == variantID {
			return true
		}
		if memberID == clusterID {
			return true
		}
		if resolvedClusterID, ok, err := cache.variantClusterID(ctx, memberID); err == nil && ok && resolvedClusterID == clusterID {
			return true
		}
	}
	return false
}

func (c *pinCoverageCache) albumMemberIDs(ctx context.Context, albumID string) ([]string, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return nil, nil
	}
	if cached, ok := c.albumMembers[albumID]; ok {
		return cached, nil
	}
	albumScopes, err := c.service.app.playback.resolveAlbumScopes(ctx, c.local, []string{albumID})
	if err != nil {
		return nil, err
	}
	recordingIDs := albumScopes[albumID].playbackRecordingIDs()
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	c.albumMembers[albumID] = recordingIDs
	return recordingIDs, nil
}

func (c *pinCoverageCache) playlistMemberIDs(ctx context.Context, playlistID string) ([]string, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, nil
	}
	if cached, ok := c.playlistMembers[playlistID]; ok {
		return cached, nil
	}
	recordingIDs, err := c.service.app.playback.libraryRecordingIDsForPlaylist(ctx, c.local.LibraryID, playlistID)
	if err != nil {
		return nil, err
	}
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	c.playlistMembers[playlistID] = recordingIDs
	return recordingIDs, nil
}

func (c *pinCoverageCache) variantClusterID(ctx context.Context, recordingID string) (string, bool, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "", false, nil
	}
	if clusterID, ok := c.variantClusters[recordingID]; ok {
		return clusterID, c.variantExists[recordingID], nil
	}
	clusterID, ok, err := c.service.app.catalog.trackClusterIDForVariant(ctx, c.local.LibraryID, recordingID)
	if err != nil {
		return "", false, err
	}
	clusterID = strings.TrimSpace(clusterID)
	c.variantClusters[recordingID] = clusterID
	c.variantExists[recordingID] = ok && clusterID != ""
	return clusterID, ok && clusterID != "", nil
}

func (c *pinCoverageCache) variantExistsValue(recordingID string) (bool, bool) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return false, false
	}
	exists, ok := c.variantExists[recordingID]
	return exists, ok
}

type resolvedPinMember struct {
	LibraryRecordingID string
	VariantRecordingID string
	ResolutionPolicy   string
}

type artworkScopeRef struct {
	ScopeType string
	ScopeID   string
}

func (s *PinService) reconcileScope(ctx context.Context, local apitypes.LocalContext, scope, scopeID, profile string) error {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	if scope == "" || scopeID == "" {
		return nil
	}

	var root PinRoot
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, scope, scopeID).
		Take(&root).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}
	if strings.TrimSpace(root.Profile) != "" {
		profile = strings.TrimSpace(root.Profile)
	}

	members, err := s.resolveMembers(ctx, local, root.Scope, root.ScopeID, profile)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	rows := make([]PinMember, 0, len(members))
	pendingCount := 0
	for _, member := range members {
		pending, lastError := s.memberPending(ctx, local, member.VariantRecordingID, profile)
		if pending {
			pendingCount++
		}
		rows = append(rows, PinMember{
			LibraryID:          local.LibraryID,
			DeviceID:           local.DeviceID,
			Scope:              root.Scope,
			ScopeID:            root.ScopeID,
			Profile:            profile,
			VariantRecordingID: member.VariantRecordingID,
			LibraryRecordingID: member.LibraryRecordingID,
			ResolutionPolicy:   member.ResolutionPolicy,
			Pending:            pending,
			LastError:          lastError,
			UpdatedAt:          now,
		})
	}

	blobRefs, err := s.resolveBlobRefs(ctx, local, root.Scope, root.ScopeID, profile, rows, now)
	if err != nil {
		return err
	}

	return s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Where(
			"library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?",
			local.LibraryID,
			local.DeviceID,
			root.Scope,
			root.ScopeID,
		).Delete(&PinMember{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.CreateInBatches(rows, 200).Error; err != nil {
				return err
			}
		}

		if err := tx.Where(
			"library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?",
			local.LibraryID,
			local.DeviceID,
			root.Scope,
			root.ScopeID,
		).Delete(&PinBlobRef{}).Error; err != nil {
			return err
		}
		if len(blobRefs) > 0 {
			if err := tx.CreateInBatches(blobRefs, 200).Error; err != nil {
				return err
			}
		}

		return tx.Model(&PinRoot{}).
			Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", local.LibraryID, local.DeviceID, root.Scope, root.ScopeID).
			Updates(map[string]any{
				"profile":            profile,
				"pending_count":      pendingCount,
				"last_reconciled_at": now,
				"updated_at":         now,
			}).Error
	})
}

func (s *PinService) resolveMembers(ctx context.Context, local apitypes.LocalContext, scope, scopeID, profile string) ([]resolvedPinMember, error) {
	scope = strings.TrimSpace(scope)
	scopeID = strings.TrimSpace(scopeID)
	profile = s.app.playback.resolvePlaybackProfile(profile)

	switch scope {
	case "recording":
		target, err := s.app.playback.resolveRecordingPinTarget(ctx, local, scopeID, profile)
		if err != nil {
			return nil, err
		}
		policy := "logical_preferred"
		libraryRecordingID := strings.TrimSpace(target.scopeRecordingID)
		if exactID, ok, err := s.app.playback.trackVariantExists(ctx, local.LibraryID, scopeID); err != nil {
			return nil, err
		} else if ok && strings.TrimSpace(exactID) != "" {
			policy = "exact_variant"
			libraryRecordingID = strings.TrimSpace(target.clusterID)
			if libraryRecordingID == "" {
				libraryRecordingID = strings.TrimSpace(exactID)
			}
			return []resolvedPinMember{{
				LibraryRecordingID: libraryRecordingID,
				VariantRecordingID: strings.TrimSpace(exactID),
				ResolutionPolicy:   policy,
			}}, nil
		}
		return []resolvedPinMember{{
			LibraryRecordingID: libraryRecordingID,
			VariantRecordingID: strings.TrimSpace(target.resolvedRecordingID),
			ResolutionPolicy:   policy,
		}}, nil
	case "album":
		albumScopes, err := s.app.playback.resolveAlbumScopes(ctx, local, []string{scopeID})
		if err != nil {
			return nil, err
		}
		recordingIDs := albumScopes[scopeID].pinRecordingIDs()
		out := make([]resolvedPinMember, 0, len(recordingIDs))
		seen := make(map[string]struct{}, len(recordingIDs))
		for _, recordingID := range compactNonEmptyStrings(recordingIDs) {
			clusterID, ok, err := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingID)
			if err != nil {
				return nil, err
			}
			libraryRecordingID := strings.TrimSpace(clusterID)
			if !ok || libraryRecordingID == "" {
				libraryRecordingID = recordingID
			}
			key := libraryRecordingID + "|" + recordingID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, resolvedPinMember{
				LibraryRecordingID: libraryRecordingID,
				VariantRecordingID: recordingID,
				ResolutionPolicy:   "exact_variant",
			})
		}
		return out, nil
	case "playlist":
		recordingIDs, err := s.app.playback.libraryRecordingIDsForPlaylist(ctx, local.LibraryID, scopeID)
		if err != nil {
			return nil, err
		}
		recordingIDs = compactNonEmptyStrings(recordingIDs)
		resolution, err := s.app.playback.resolvePlaybackVariantsBatch(ctx, local, recordingIDs, profile)
		if err != nil {
			return nil, err
		}
		out := make([]resolvedPinMember, 0, len(recordingIDs))
		seen := make(map[string]struct{}, len(recordingIDs))
		for _, recordingID := range recordingIDs {
			resolvedRecordingID := strings.TrimSpace(resolution.resolvedByRecording[recordingID])
			if resolvedRecordingID == "" {
				continue
			}
			key := recordingID + "|" + resolvedRecordingID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, resolvedPinMember{
				LibraryRecordingID: recordingID,
				VariantRecordingID: resolvedRecordingID,
				ResolutionPolicy:   "logical_preferred",
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported pin scope %q", scope)
	}
}

func (s *PinService) memberPending(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (bool, string) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return true, "recording resolution unavailable"
	}
	if _, ok, err := s.app.playback.bestLocalRecordingPath(ctx, local.LibraryID, local.DeviceID, recordingID); err == nil && ok {
		return false, ""
	}
	if ok, err := s.hasPinnedCachedEncoding(ctx, local, recordingID, profile); err == nil && ok {
		return false, ""
	}
	return true, "waiting for local or cached media"
}

func (s *PinService) resolveBlobRefs(
	ctx context.Context,
	local apitypes.LocalContext,
	scope string,
	scopeID string,
	profile string,
	members []PinMember,
	now time.Time,
) ([]PinBlobRef, error) {
	out := make([]PinBlobRef, 0, len(members))
	seen := make(map[string]struct{})

	for _, member := range members {
		blobIDs, err := s.app.cache.cachedBlobIDsForResolvedRecordings(ctx, local.LibraryID, local.DeviceID, []string{member.VariantRecordingID}, profile)
		if err != nil {
			return nil, err
		}
		for _, blobID := range blobIDs {
			key := strings.Join([]string{blobID, "audio", member.VariantRecordingID}, "|")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, PinBlobRef{
				LibraryID:   local.LibraryID,
				DeviceID:    local.DeviceID,
				Scope:       scope,
				ScopeID:     scopeID,
				Profile:     profile,
				BlobID:      blobID,
				RefKind:     "audio",
				SubjectID:   member.VariantRecordingID,
				RecordingID: member.VariantRecordingID,
				UpdatedAt:   now,
			})
		}
	}

	artworkScopes, err := s.resolveArtworkScopes(ctx, local, scope, scopeID, members)
	if err != nil {
		return nil, err
	}
	for _, artworkScope := range artworkScopes {
		var rows []ArtworkVariant
		if err := s.app.storage.WithContext(ctx).
			Where("library_id = ? AND scope_type = ? AND scope_id = ?", local.LibraryID, artworkScope.ScopeType, artworkScope.ScopeID).
			Order("variant ASC").
			Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			key := strings.Join([]string{strings.TrimSpace(row.BlobID), "artwork", artworkScope.ScopeType, artworkScope.ScopeID, strings.TrimSpace(row.Variant)}, "|")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, PinBlobRef{
				LibraryID:        local.LibraryID,
				DeviceID:         local.DeviceID,
				Scope:            scope,
				ScopeID:          scopeID,
				Profile:          profile,
				BlobID:           strings.TrimSpace(row.BlobID),
				RefKind:          "artwork",
				SubjectID:        artworkScope.ScopeType + ":" + artworkScope.ScopeID + ":" + strings.TrimSpace(row.Variant),
				ArtworkScopeType: artworkScope.ScopeType,
				ArtworkScopeID:   artworkScope.ScopeID,
				ArtworkVariant:   strings.TrimSpace(row.Variant),
				UpdatedAt:        now,
			})
		}
	}

	return out, nil
}

func (s *PinService) resolveArtworkScopes(
	ctx context.Context,
	local apitypes.LocalContext,
	scope string,
	scopeID string,
	members []PinMember,
) ([]artworkScopeRef, error) {
	seen := make(map[string]struct{})
	out := make([]artworkScopeRef, 0, 4)
	addScope := func(scopeType, id string) {
		scopeType = strings.TrimSpace(scopeType)
		id = strings.TrimSpace(id)
		if scopeType == "" || id == "" {
			return
		}
		key := scopeType + "|" + id
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, artworkScopeRef{ScopeType: scopeType, ScopeID: id})
	}

	switch strings.TrimSpace(scope) {
	case "album":
		addScope("album", scopeID)
	case "playlist":
		addScope("playlist", scopeID)
	}

	variantIDs := make([]string, 0, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.VariantRecordingID) != "" {
			variantIDs = append(variantIDs, strings.TrimSpace(member.VariantRecordingID))
		}
	}
	variantIDs = compactNonEmptyStrings(variantIDs)
	if len(variantIDs) == 0 {
		return out, nil
	}

	type row struct {
		AlbumVariantID string
	}
	var rows []row
	query := `
SELECT DISTINCT at.album_variant_id AS album_variant_id
FROM album_tracks at
WHERE at.library_id = ? AND at.track_variant_id IN ?`
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID, variantIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		addScope("album", row.AlbumVariantID)
	}
	return out, nil
}
