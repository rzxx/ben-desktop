package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/playback"
	"ben/desktop/internal/settings"
)

type hostBridge interface {
	playback.CorePlaybackBridge
	ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error)
	ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error)
	CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error)
	SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error)
	RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error)
	LeaveLibrary(ctx context.Context, libraryID string) error
	DeleteLibrary(ctx context.Context, libraryID string) error
	ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error)
	UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error
	RemoveLibraryMember(ctx context.Context, deviceID string) error
	SetScanRoots(ctx context.Context, roots []string) error
	AddScanRoots(ctx context.Context, roots []string) ([]string, error)
	RemoveScanRoots(ctx context.Context, roots []string) ([]string, error)
	ScanRoots(ctx context.Context) ([]string, error)
	ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error)
	GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error)
	ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error)
	GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error)
	ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error)
	GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error)
	ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error)
	ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error)
	SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error
	SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error
	ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error)
	ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error)
	GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error)
	ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error)
	ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error)
	CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error)
	RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error)
	DeletePlaylist(ctx context.Context, playlistID string) error
	AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error)
	MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error)
	RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error
	LikeRecording(ctx context.Context, recordingID string) error
	UnlikeRecording(ctx context.Context, recordingID string) error
	IsRecordingLiked(ctx context.Context, recordingID string) (bool, error)
	CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error)
	ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error)
	RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error
	StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error)
	GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error)
	FinalizeJoinSession(ctx context.Context, sessionID string) (apitypes.JoinLibraryResult, error)
	CancelJoinSession(ctx context.Context, sessionID string) error
	ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error)
	ApproveJoinRequest(ctx context.Context, requestID, role string) error
	RejectJoinRequest(ctx context.Context, requestID, reason string) error
	GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error)
	ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error)
	CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error)
	PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error)
	UnpinRecordingOffline(ctx context.Context, recordingID string) error
	PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	UnpinAlbumOffline(ctx context.Context, albumID string) error
	PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	UnpinPlaylistOffline(ctx context.Context, playlistID string) error
	PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error)
	UnpinLikedOffline(ctx context.Context) error
	ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error)
	GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error)
	GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error)
}

type coreHost struct {
	mu sync.RWMutex

	started          bool
	bridge           hostBridge
	blobRoot         string
	preferredProfile string
}

func newCoreHost() *coreHost {
	return &coreHost{}
}

func (h *coreHost) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return nil
	}

	coreSettings := loadCoreRuntimeSettings()
	h.bridge = openCoreBridge(ctx, coreSettings)
	h.blobRoot = resolvedBlobRoot(coreSettings)
	h.preferredProfile = preferredProfile(coreSettings)
	h.started = true
	return nil
}

func (h *coreHost) Close() error {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	bridge := h.bridge
	h.bridge = nil
	h.blobRoot = ""
	h.preferredProfile = ""
	h.started = false
	h.mu.Unlock()

	if bridge == nil {
		return nil
	}
	return bridge.Close()
}

func (h *coreHost) Bridge() hostBridge {
	if h == nil {
		return desktopcore.NewUnavailableCore(fmt.Errorf("core bridge is not available"))
	}

	h.mu.RLock()
	bridge := h.bridge
	h.mu.RUnlock()
	if bridge == nil {
		return desktopcore.NewUnavailableCore(fmt.Errorf("core bridge is not available"))
	}
	return bridge
}

func (h *coreHost) BlobRoot() string {
	if h == nil {
		return ""
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.blobRoot
}

func (h *coreHost) PreferredProfile() string {
	if h == nil {
		return settings.DefaultTranscodeProfile
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.preferredProfile == "" {
		return settings.DefaultTranscodeProfile
	}
	return h.preferredProfile
}

func loadCoreRuntimeSettings() settings.CoreRuntimeSettings {
	coreSettings := settings.CoreRuntimeSettings{}
	settingsPath, err := settings.DefaultPath("ben-desktop")
	if err != nil {
		log.Printf("playback: resolve settings path: %v", err)
		return coreSettings
	}

	settingsStore, err := settings.NewStore(settingsPath)
	if err != nil {
		log.Printf("playback: open settings store: %v", err)
		return coreSettings
	}
	defer func() {
		if closeErr := settingsStore.Close(); closeErr != nil {
			log.Printf("playback: close settings store: %v", closeErr)
		}
	}()

	state, err := settingsStore.Load()
	if err != nil {
		log.Printf("playback: load settings: %v", err)
		return coreSettings
	}
	return state.Core
}

func openCoreBridge(ctx context.Context, coreSettings settings.CoreRuntimeSettings) hostBridge {
	bridge, err := desktopcore.OpenFromSettings(ctx, coreSettings)
	if err != nil {
		log.Printf("playback: core bridge unavailable: %v", err)
		return desktopcore.NewUnavailableCore(err)
	}
	return bridge
}
