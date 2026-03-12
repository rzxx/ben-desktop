package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/desktopcore"
	"ben/desktop/internal/settings"
)

type artworkResolveBridgeStub struct {
	*desktopcore.UnavailableCore
	result apitypes.ArtworkResolveResult
	err    error
}

func (b *artworkResolveBridgeStub) ResolveArtworkRef(context.Context, apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
	if b.err != nil {
		return apitypes.ArtworkResolveResult{}, b.err
	}
	return b.result, nil
}

type passthroughBridgeStub struct {
	*desktopcore.UnavailableCore

	listLibrariesFn             func(context.Context) ([]apitypes.LibrarySummary, error)
	activeLibraryFn             func(context.Context) (apitypes.LibrarySummary, bool, error)
	createLibraryFn             func(context.Context, string) (apitypes.LibrarySummary, error)
	selectLibraryFn             func(context.Context, string) (apitypes.LibrarySummary, error)
	renameLibraryFn             func(context.Context, string, string) (apitypes.LibrarySummary, error)
	leaveLibraryFn              func(context.Context, string) error
	deleteLibraryFn             func(context.Context, string) error
	listLibraryMembersFn        func(context.Context) ([]apitypes.LibraryMemberStatus, error)
	updateLibraryMemberRoleFn   func(context.Context, string, string) error
	removeLibraryMemberFn       func(context.Context, string) error
	setScanRootsFn              func(context.Context, []string) error
	addScanRootsFn              func(context.Context, []string) ([]string, error)
	removeScanRootsFn           func(context.Context, []string) ([]string, error)
	scanRootsFn                 func(context.Context) ([]string, error)
	listRecordingVariantsFn     func(context.Context, apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error)
	setPreferredRecordingFn     func(context.Context, string, string) error
	setPreferredAlbumFn         func(context.Context, string, string) error
	createPlaylistFn            func(context.Context, string, string) (apitypes.PlaylistRecord, error)
	renamePlaylistFn            func(context.Context, string, string) (apitypes.PlaylistRecord, error)
	deletePlaylistFn            func(context.Context, string) error
	addPlaylistItemFn           func(context.Context, apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error)
	movePlaylistItemFn          func(context.Context, apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error)
	removePlaylistItemFn        func(context.Context, string, string) error
	likeRecordingFn             func(context.Context, string) error
	unlikeRecordingFn           func(context.Context, string) error
	isRecordingLikedFn          func(context.Context, string) (bool, error)
	getCacheOverviewFn          func(context.Context) (apitypes.CacheOverview, error)
	listCacheEntriesFn          func(context.Context, apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error)
	cleanupCacheFn              func(context.Context, apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error)
	pinRecordingOfflineFn       func(context.Context, string, string) (apitypes.PlaybackRecordingResult, error)
	unpinRecordingOfflineFn     func(context.Context, string) error
	pinAlbumOfflineFn           func(context.Context, string, string) (apitypes.PlaybackBatchResult, error)
	unpinAlbumOfflineFn         func(context.Context, string) error
	pinPlaylistOfflineFn        func(context.Context, string, string) (apitypes.PlaybackBatchResult, error)
	unpinPlaylistOfflineFn      func(context.Context, string) error
	pinLikedOfflineFn           func(context.Context, string) (apitypes.PlaybackBatchResult, error)
	unpinLikedOfflineFn         func(context.Context) error
	inspectPlaybackRecordingFn  func(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error)
	preparePlaybackRecordingFn  func(context.Context, string, string, apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error)
	getPlaybackPreparationFn    func(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error)
	resolvePlaybackRecordingFn  func(context.Context, string, string) (apitypes.PlaybackResolveResult, error)
	listRecordingAvailabilityFn func(context.Context, string, string) ([]apitypes.RecordingAvailabilityItem, error)
	recordingAvailabilityOVFn   func(context.Context, string, string) (apitypes.RecordingAvailabilityOverview, error)
	getRecordingAvailabilityFn  func(context.Context, string, string) (apitypes.RecordingPlaybackAvailability, error)
	albumAvailabilityOVFn       func(context.Context, string, string) (apitypes.AlbumAvailabilityOverview, error)
}

func (b *passthroughBridgeStub) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	return b.listLibrariesFn(ctx)
}

func (b *passthroughBridgeStub) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	return b.activeLibraryFn(ctx)
}

func (b *passthroughBridgeStub) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	return b.createLibraryFn(ctx, name)
}

func (b *passthroughBridgeStub) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	return b.selectLibraryFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	return b.renameLibraryFn(ctx, libraryID, name)
}

func (b *passthroughBridgeStub) LeaveLibrary(ctx context.Context, libraryID string) error {
	return b.leaveLibraryFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) DeleteLibrary(ctx context.Context, libraryID string) error {
	return b.deleteLibraryFn(ctx, libraryID)
}

func (b *passthroughBridgeStub) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	return b.listLibraryMembersFn(ctx)
}

func (b *passthroughBridgeStub) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	return b.updateLibraryMemberRoleFn(ctx, deviceID, role)
}

func (b *passthroughBridgeStub) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	return b.removeLibraryMemberFn(ctx, deviceID)
}

func (b *passthroughBridgeStub) SetScanRoots(ctx context.Context, roots []string) error {
	return b.setScanRootsFn(ctx, roots)
}

func (b *passthroughBridgeStub) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return b.addScanRootsFn(ctx, roots)
}

func (b *passthroughBridgeStub) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	return b.removeScanRootsFn(ctx, roots)
}

func (b *passthroughBridgeStub) ScanRoots(ctx context.Context) ([]string, error) {
	return b.scanRootsFn(ctx)
}

func (b *passthroughBridgeStub) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	return b.listRecordingVariantsFn(ctx, req)
}

func (b *passthroughBridgeStub) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	return b.setPreferredRecordingFn(ctx, recordingID, variantRecordingID)
}

func (b *passthroughBridgeStub) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	return b.setPreferredAlbumFn(ctx, albumID, variantAlbumID)
}

func (b *passthroughBridgeStub) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	return b.createPlaylistFn(ctx, name, kind)
}

func (b *passthroughBridgeStub) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	return b.renamePlaylistFn(ctx, playlistID, name)
}

func (b *passthroughBridgeStub) DeletePlaylist(ctx context.Context, playlistID string) error {
	return b.deletePlaylistFn(ctx, playlistID)
}

func (b *passthroughBridgeStub) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	return b.addPlaylistItemFn(ctx, req)
}

func (b *passthroughBridgeStub) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	return b.movePlaylistItemFn(ctx, req)
}

func (b *passthroughBridgeStub) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	return b.removePlaylistItemFn(ctx, playlistID, itemID)
}

func (b *passthroughBridgeStub) LikeRecording(ctx context.Context, recordingID string) error {
	return b.likeRecordingFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) UnlikeRecording(ctx context.Context, recordingID string) error {
	return b.unlikeRecordingFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	return b.isRecordingLikedFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) GetCacheOverview(ctx context.Context) (apitypes.CacheOverview, error) {
	return b.getCacheOverviewFn(ctx)
}

func (b *passthroughBridgeStub) ListCacheEntries(ctx context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
	return b.listCacheEntriesFn(ctx, req)
}

func (b *passthroughBridgeStub) CleanupCache(ctx context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
	return b.cleanupCacheFn(ctx, req)
}

func (b *passthroughBridgeStub) PinRecordingOffline(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
	return b.pinRecordingOfflineFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinRecordingOffline(ctx context.Context, recordingID string) error {
	return b.unpinRecordingOfflineFn(ctx, recordingID)
}

func (b *passthroughBridgeStub) PinAlbumOffline(ctx context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return b.pinAlbumOfflineFn(ctx, albumID, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinAlbumOffline(ctx context.Context, albumID string) error {
	return b.unpinAlbumOfflineFn(ctx, albumID)
}

func (b *passthroughBridgeStub) PinPlaylistOffline(ctx context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return b.pinPlaylistOfflineFn(ctx, playlistID, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinPlaylistOffline(ctx context.Context, playlistID string) error {
	return b.unpinPlaylistOfflineFn(ctx, playlistID)
}

func (b *passthroughBridgeStub) PinLikedOffline(ctx context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
	return b.pinLikedOfflineFn(ctx, preferredProfile)
}

func (b *passthroughBridgeStub) UnpinLikedOffline(ctx context.Context) error {
	return b.unpinLikedOfflineFn(ctx)
}

func (b *passthroughBridgeStub) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.inspectPlaybackRecordingFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return b.preparePlaybackRecordingFn(ctx, recordingID, preferredProfile, purpose)
}

func (b *passthroughBridgeStub) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.getPlaybackPreparationFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return b.resolvePlaybackRecordingFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) ListRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
	return b.listRecordingAvailabilityFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) GetRecordingAvailabilityOverview(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
	return b.recordingAvailabilityOVFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return b.getRecordingAvailabilityFn(ctx, recordingID, preferredProfile)
}

func (b *passthroughBridgeStub) GetAlbumAvailabilityOverview(ctx context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
	return b.albumAvailabilityOVFn(ctx, albumID, preferredProfile)
}

func TestResolveBlobURLReturnsFileURLWhenBlobExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hashHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	blobPath := filepath.Join(root, "b3", hashHex[:2], hashHex[2:4], hashHex)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatalf("mkdir blob dir: %v", err)
	}
	if err := os.WriteFile(blobPath, []byte("art"), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	service := &PlaybackService{blobRoot: root}
	got, err := service.ResolveBlobURL("b3:" + hashHex)
	if err != nil {
		t.Fatalf("resolve blob url: %v", err)
	}
	want, err := fileURLFromPath(blobPath)
	if err != nil {
		t.Fatalf("file url from path: %v", err)
	}
	if got != want {
		t.Fatalf("blob url = %q, want %q", got, want)
	}
}

func TestResolveBlobURLReturnsEmptyForMissingBlob(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{blobRoot: t.TempDir()}
	got, err := service.ResolveBlobURL("b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("resolve blob url: %v", err)
	}
	if got != "" {
		t.Fatalf("blob url = %q, want empty", got)
	}
}

func TestResolveThumbnailURLReturnsTypedFileURLWhenBlobExists(t *testing.T) {
	t.Parallel()

	blobPath := filepath.Join(t.TempDir(), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	payload := []byte("art")
	if err := os.WriteFile(blobPath, payload, 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	service := &PlaybackService{
		bridge: &artworkResolveBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			result: apitypes.ArtworkResolveResult{
				Artwork: apitypes.ArtworkRef{
					BlobID:  "b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					MIME:    "image/webp",
					FileExt: ".webp",
					Variant: "320_webp",
				},
				LocalPath: blobPath,
				Available: true,
			},
		},
	}
	got, err := service.ResolveThumbnailURL(apitypes.ArtworkRef{
		BlobID:  "b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MIME:    "image/webp",
		FileExt: ".webp",
		Variant: "320_webp",
	})
	if err != nil {
		t.Fatalf("resolve thumbnail url: %v", err)
	}
	want, err := fileURLFromPath(blobPath + ".webp")
	if err != nil {
		t.Fatalf("alias file url: %v", err)
	}
	if got != want {
		t.Fatalf("thumbnail url = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(blobPath + ".webp"); err != nil || string(data) != string(payload) {
		t.Fatalf("expected typed alias payload, got data=%q err=%v", string(data), err)
	}
}

func TestResolveThumbnailURLReturnsEmptyForMissingBlob(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{
		bridge: &artworkResolveBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			result: apitypes.ArtworkResolveResult{
				Artwork: apitypes.ArtworkRef{
					BlobID:  "b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					MIME:    "image/jpeg",
					FileExt: ".jpg",
				},
				Available: false,
			},
		},
	}
	got, err := service.ResolveThumbnailURL(apitypes.ArtworkRef{
		BlobID:  "b3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MIME:    "image/jpeg",
		FileExt: ".jpg",
	})
	if err != nil {
		t.Fatalf("resolve thumbnail url: %v", err)
	}
	if got != "" {
		t.Fatalf("thumbnail url = %q, want empty", got)
	}
}

func TestResolveThumbnailURLFallsBackToMIMEForLegacyArtworkRef(t *testing.T) {
	t.Parallel()

	blobPath := filepath.Join(t.TempDir(), "fedcba98765432100123456789abcdef0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(blobPath, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	service := &PlaybackService{
		bridge: &artworkResolveBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			result: apitypes.ArtworkResolveResult{
				Artwork: apitypes.ArtworkRef{
					BlobID: "b3:fedcba98765432100123456789abcdef0123456789abcdef0123456789abcdef",
					MIME:   "image/jpeg",
				},
				LocalPath: blobPath,
				Available: true,
			},
		},
	}
	got, err := service.ResolveThumbnailURL(apitypes.ArtworkRef{
		BlobID: "b3:fedcba98765432100123456789abcdef0123456789abcdef0123456789abcdef",
		MIME:   "image/jpeg",
	})
	if err != nil {
		t.Fatalf("resolve legacy thumbnail: %v", err)
	}
	want, err := fileURLFromPath(blobPath + ".jpg")
	if err != nil {
		t.Fatalf("legacy file url: %v", err)
	}
	if got != want {
		t.Fatalf("legacy thumbnail url = %q, want %q", got, want)
	}
}

func TestListAlbumsReturnsBridgeErrorWhenUnavailable(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{
		bridge: desktopcore.NewUnavailableCore(errors.New("core unavailable")),
	}
	_, err := service.ListAlbums(context.Background(), apitypes.AlbumListRequest{})
	if err == nil || err.Error() != "core unavailable" {
		t.Fatalf("list albums error = %v, want core unavailable", err)
	}
}

func TestPlaybackServiceLibraryMethodsForwardToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	joinedAt := time.Now().UTC()
	summary := apitypes.LibrarySummary{LibraryID: "lib-1", Name: "Library", Role: "admin", JoinedAt: joinedAt, IsActive: true}
	member := apitypes.LibraryMemberStatus{LibraryID: "lib-1", DeviceID: "dev-1", Role: "admin"}
	calls := make([]string, 0, 10)

	service := &PlaybackService{
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			listLibrariesFn: func(context.Context) ([]apitypes.LibrarySummary, error) {
				calls = append(calls, "list")
				return []apitypes.LibrarySummary{summary}, nil
			},
			activeLibraryFn: func(context.Context) (apitypes.LibrarySummary, bool, error) {
				calls = append(calls, "active")
				return summary, true, nil
			},
			createLibraryFn: func(_ context.Context, name string) (apitypes.LibrarySummary, error) {
				calls = append(calls, "create:"+name)
				return summary, nil
			},
			selectLibraryFn: func(_ context.Context, libraryID string) (apitypes.LibrarySummary, error) {
				calls = append(calls, "select:"+libraryID)
				return summary, nil
			},
			renameLibraryFn: func(_ context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
				calls = append(calls, "rename:"+libraryID+":"+name)
				return summary, nil
			},
			leaveLibraryFn: func(_ context.Context, libraryID string) error {
				calls = append(calls, "leave:"+libraryID)
				return nil
			},
			deleteLibraryFn: func(_ context.Context, libraryID string) error {
				calls = append(calls, "delete:"+libraryID)
				return nil
			},
			listLibraryMembersFn: func(context.Context) ([]apitypes.LibraryMemberStatus, error) {
				calls = append(calls, "members")
				return []apitypes.LibraryMemberStatus{member}, nil
			},
			updateLibraryMemberRoleFn: func(_ context.Context, deviceID, role string) error {
				calls = append(calls, "role:"+deviceID+":"+role)
				return nil
			},
			removeLibraryMemberFn: func(_ context.Context, deviceID string) error {
				calls = append(calls, "remove:"+deviceID)
				return nil
			},
		},
	}

	libraries, err := service.ListLibraries(ctx)
	if err != nil || len(libraries) != 1 || libraries[0].LibraryID != summary.LibraryID {
		t.Fatalf("list libraries = %+v, err=%v", libraries, err)
	}
	active, ok, err := service.ActiveLibrary(ctx)
	if err != nil || !ok || active.LibraryID != summary.LibraryID {
		t.Fatalf("active library = %+v, ok=%v, err=%v", active, ok, err)
	}
	if created, err := service.CreateLibrary(ctx, "Library"); err != nil || created.LibraryID != summary.LibraryID {
		t.Fatalf("create library = %+v, err=%v", created, err)
	}
	if selected, err := service.SelectLibrary(ctx, "lib-1"); err != nil || selected.LibraryID != summary.LibraryID {
		t.Fatalf("select library = %+v, err=%v", selected, err)
	}
	if renamed, err := service.RenameLibrary(ctx, "lib-1", "Renamed"); err != nil || renamed.LibraryID != summary.LibraryID {
		t.Fatalf("rename library = %+v, err=%v", renamed, err)
	}
	if err := service.LeaveLibrary(ctx, "lib-1"); err != nil {
		t.Fatalf("leave library: %v", err)
	}
	if err := service.DeleteLibrary(ctx, "lib-1"); err != nil {
		t.Fatalf("delete library: %v", err)
	}
	members, err := service.ListLibraryMembers(ctx)
	if err != nil || len(members) != 1 || members[0].DeviceID != member.DeviceID {
		t.Fatalf("list members = %+v, err=%v", members, err)
	}
	if err := service.UpdateLibraryMemberRole(ctx, "dev-1", "guest"); err != nil {
		t.Fatalf("update member role: %v", err)
	}
	if err := service.RemoveLibraryMember(ctx, "dev-1"); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	want := []string{
		"list",
		"active",
		"create:Library",
		"select:lib-1",
		"rename:lib-1:Renamed",
		"leave:lib-1",
		"delete:lib-1",
		"members",
		"role:dev-1:guest",
		"remove:dev-1",
	}
	if len(calls) != len(want) {
		t.Fatalf("library call count = %d, want %d (%v)", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("library call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestPlaybackServiceScanRootMethodsForwardToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	calls := make([]string, 0, 4)
	wantRoots := []string{`C:\music\a`, `C:\music\b`}

	service := &PlaybackService{
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			setScanRootsFn: func(_ context.Context, roots []string) error {
				calls = append(calls, "set:"+strings.Join(roots, ","))
				return nil
			},
			addScanRootsFn: func(_ context.Context, roots []string) ([]string, error) {
				calls = append(calls, "add:"+strings.Join(roots, ","))
				return append([]string(nil), wantRoots...), nil
			},
			removeScanRootsFn: func(_ context.Context, roots []string) ([]string, error) {
				calls = append(calls, "remove:"+strings.Join(roots, ","))
				return []string{wantRoots[0]}, nil
			},
			scanRootsFn: func(context.Context) ([]string, error) {
				calls = append(calls, "list")
				return append([]string(nil), wantRoots...), nil
			},
		},
	}

	if err := service.SetScanRoots(ctx, []string{wantRoots[0]}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if roots, err := service.AddScanRoots(ctx, []string{wantRoots[1]}); err != nil || len(roots) != 2 {
		t.Fatalf("add scan roots = %v, err=%v", roots, err)
	}
	if roots, err := service.RemoveScanRoots(ctx, []string{wantRoots[1]}); err != nil || len(roots) != 1 || roots[0] != wantRoots[0] {
		t.Fatalf("remove scan roots = %v, err=%v", roots, err)
	}
	if roots, err := service.ScanRoots(ctx); err != nil || len(roots) != 2 {
		t.Fatalf("scan roots = %v, err=%v", roots, err)
	}

	want := []string{
		"set:" + wantRoots[0],
		"add:" + wantRoots[1],
		"remove:" + wantRoots[1],
		"list",
	}
	if len(calls) != len(want) {
		t.Fatalf("scan root call count = %d, want %d (%v)", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("scan root call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestPlaybackServicePlaylistCacheAndPreferenceMethodsForwardToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	variantPage := apitypes.Page[apitypes.RecordingVariantItem]{
		Items: []apitypes.RecordingVariantItem{{RecordingID: "rec-1"}},
		Page:  apitypes.PageInfo{Returned: 1, Total: 1},
	}
	playlist := apitypes.PlaylistRecord{LibraryID: "lib-1", PlaylistID: "pl-1", Name: "Queue", Kind: apitypes.PlaylistKindNormal}
	item := apitypes.PlaylistItemRecord{LibraryID: "lib-1", PlaylistID: "pl-1", ItemID: "item-1", RecordingID: "rec-1"}
	overview := apitypes.CacheOverview{UsedBytes: 128, EntryCount: 1}
	cachePage := apitypes.Page[apitypes.CacheEntryItem]{
		Items: []apitypes.CacheEntryItem{{BlobID: "b3:" + strings.Repeat("a", 64)}},
		Page:  apitypes.PageInfo{Returned: 1, Total: 1},
	}
	cleanup := apitypes.CacheCleanupResult{DeletedBlobs: []string{"blob-1"}, DeletedBytes: 32, RemainingBytes: 96}
	calls := make([]string, 0, 13)

	service := &PlaybackService{
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			listRecordingVariantsFn: func(_ context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
				calls = append(calls, "variants:"+req.RecordingID)
				return variantPage, nil
			},
			setPreferredRecordingFn: func(_ context.Context, recordingID, variantRecordingID string) error {
				calls = append(calls, "pref-recording:"+recordingID+":"+variantRecordingID)
				return nil
			},
			setPreferredAlbumFn: func(_ context.Context, albumID, variantAlbumID string) error {
				calls = append(calls, "pref-album:"+albumID+":"+variantAlbumID)
				return nil
			},
			createPlaylistFn: func(_ context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
				calls = append(calls, "create-playlist:"+name+":"+kind)
				return playlist, nil
			},
			renamePlaylistFn: func(_ context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
				calls = append(calls, "rename-playlist:"+playlistID+":"+name)
				return playlist, nil
			},
			deletePlaylistFn: func(_ context.Context, playlistID string) error {
				calls = append(calls, "delete-playlist:"+playlistID)
				return nil
			},
			addPlaylistItemFn: func(_ context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
				calls = append(calls, "add-item:"+req.PlaylistID+":"+req.RecordingID)
				return item, nil
			},
			movePlaylistItemFn: func(_ context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
				calls = append(calls, "move-item:"+req.PlaylistID+":"+req.ItemID)
				return item, nil
			},
			removePlaylistItemFn: func(_ context.Context, playlistID, itemID string) error {
				calls = append(calls, "remove-item:"+playlistID+":"+itemID)
				return nil
			},
			likeRecordingFn: func(_ context.Context, recordingID string) error {
				calls = append(calls, "like:"+recordingID)
				return nil
			},
			unlikeRecordingFn: func(_ context.Context, recordingID string) error {
				calls = append(calls, "unlike:"+recordingID)
				return nil
			},
			isRecordingLikedFn: func(_ context.Context, recordingID string) (bool, error) {
				calls = append(calls, "is-liked:"+recordingID)
				return true, nil
			},
			getCacheOverviewFn: func(context.Context) (apitypes.CacheOverview, error) {
				calls = append(calls, "cache-overview")
				return overview, nil
			},
			listCacheEntriesFn: func(_ context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
				calls = append(calls, "cache-list")
				return cachePage, nil
			},
			cleanupCacheFn: func(_ context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
				calls = append(calls, "cache-cleanup:"+string(req.Mode))
				return cleanup, nil
			},
		},
	}

	page, err := service.ListRecordingVariants(ctx, apitypes.RecordingVariantListRequest{RecordingID: "rec-1"})
	if err != nil || len(page.Items) != 1 || page.Items[0].RecordingID != "rec-1" {
		t.Fatalf("list recording variants = %+v, err=%v", page, err)
	}
	if err := service.SetPreferredRecordingVariant(ctx, "rec-1", "rec-variant"); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if err := service.SetPreferredAlbumVariant(ctx, "album-1", "album-variant"); err != nil {
		t.Fatalf("set preferred album variant: %v", err)
	}
	if created, err := service.CreatePlaylist(ctx, "Queue", string(apitypes.PlaylistKindNormal)); err != nil || created.PlaylistID != playlist.PlaylistID {
		t.Fatalf("create playlist = %+v, err=%v", created, err)
	}
	if renamed, err := service.RenamePlaylist(ctx, "pl-1", "Roadtrip"); err != nil || renamed.PlaylistID != playlist.PlaylistID {
		t.Fatalf("rename playlist = %+v, err=%v", renamed, err)
	}
	if err := service.DeletePlaylist(ctx, "pl-1"); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}
	if added, err := service.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{PlaylistID: "pl-1", RecordingID: "rec-1"}); err != nil || added.ItemID != item.ItemID {
		t.Fatalf("add playlist item = %+v, err=%v", added, err)
	}
	if moved, err := service.MovePlaylistItem(ctx, apitypes.PlaylistMoveItemRequest{PlaylistID: "pl-1", ItemID: "item-1"}); err != nil || moved.ItemID != item.ItemID {
		t.Fatalf("move playlist item = %+v, err=%v", moved, err)
	}
	if err := service.RemovePlaylistItem(ctx, "pl-1", "item-1"); err != nil {
		t.Fatalf("remove playlist item: %v", err)
	}
	if err := service.LikeRecording(ctx, "rec-1"); err != nil {
		t.Fatalf("like recording: %v", err)
	}
	if err := service.UnlikeRecording(ctx, "rec-1"); err != nil {
		t.Fatalf("unlike recording: %v", err)
	}
	if liked, err := service.IsRecordingLiked(ctx, "rec-1"); err != nil || !liked {
		t.Fatalf("is recording liked = %v, err=%v", liked, err)
	}
	gotOverview, err := service.GetCacheOverview(ctx)
	if err != nil || gotOverview.UsedBytes != overview.UsedBytes {
		t.Fatalf("get cache overview = %+v, err=%v", gotOverview, err)
	}
	gotCachePage, err := service.ListCacheEntries(ctx, apitypes.CacheEntryListRequest{})
	if err != nil || len(gotCachePage.Items) != 1 || gotCachePage.Items[0].BlobID != cachePage.Items[0].BlobID {
		t.Fatalf("list cache entries = %+v, err=%v", gotCachePage, err)
	}
	gotCleanup, err := service.CleanupCache(ctx, apitypes.CacheCleanupRequest{Mode: apitypes.CacheCleanupAllUnpinned})
	if err != nil || gotCleanup.DeletedBytes != cleanup.DeletedBytes {
		t.Fatalf("cleanup cache = %+v, err=%v", gotCleanup, err)
	}

	want := []string{
		"variants:rec-1",
		"pref-recording:rec-1:rec-variant",
		"pref-album:album-1:album-variant",
		"create-playlist:Queue:normal",
		"rename-playlist:pl-1:Roadtrip",
		"delete-playlist:pl-1",
		"add-item:pl-1:rec-1",
		"move-item:pl-1:item-1",
		"remove-item:pl-1:item-1",
		"like:rec-1",
		"unlike:rec-1",
		"is-liked:rec-1",
		"cache-overview",
		"cache-list",
		"cache-cleanup:all_unpinned",
	}
	if len(calls) != len(want) {
		t.Fatalf("passthrough call count = %d, want %d (%v)", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("passthrough call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestPlaybackServicePlaybackHelperMethodsForwardToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC()
	inspection := apitypes.PlaybackPreparationStatus{RecordingID: "rec-1", PreferredProfile: "desktop", Phase: apitypes.PlaybackPreparationReady, UpdatedAt: now}
	prepared := apitypes.PlaybackPreparationStatus{RecordingID: "rec-1", PreferredProfile: "desktop", Purpose: apitypes.PlaybackPreparationPlayNow, Phase: apitypes.PlaybackPreparationPreparingFetch, UpdatedAt: now}
	resolved := apitypes.PlaybackResolveResult{RecordingID: "rec-1", PlayableURI: "file:///track.m4a"}
	pinned := apitypes.PlaybackRecordingResult{BlobID: "blob-1", Profile: "desktop", FromLocal: true, SourceKind: apitypes.PlaybackSourceCachedOpt}
	batch := apitypes.PlaybackBatchResult{Tracks: 2, TotalBytes: 2048, LocalHits: 2}
	availabilityItems := []apitypes.RecordingAvailabilityItem{{DeviceID: "dev-1", CachedOptimized: true}}
	availability := apitypes.RecordingPlaybackAvailability{RecordingID: "rec-1", PreferredProfile: "desktop", State: apitypes.AvailabilityPlayableCachedOpt}
	recordingOverview := apitypes.RecordingAvailabilityOverview{RecordingID: "rec-1", PreferredProfile: "desktop"}
	albumOverview := apitypes.AlbumAvailabilityOverview{AlbumID: "album-1", PreferredProfile: "desktop"}
	calls := make([]string, 0, 14)

	service := &PlaybackService{
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			inspectPlaybackRecordingFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
				calls = append(calls, "inspect:"+recordingID+":"+preferredProfile)
				return inspection, nil
			},
			preparePlaybackRecordingFn: func(_ context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
				calls = append(calls, "prepare:"+recordingID+":"+preferredProfile+":"+string(purpose))
				return prepared, nil
			},
			getPlaybackPreparationFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
				calls = append(calls, "get-prep:"+recordingID+":"+preferredProfile)
				return prepared, nil
			},
			resolvePlaybackRecordingFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
				calls = append(calls, "resolve:"+recordingID+":"+preferredProfile)
				return resolved, nil
			},
			pinRecordingOfflineFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackRecordingResult, error) {
				calls = append(calls, "pin-recording:"+recordingID+":"+preferredProfile)
				return pinned, nil
			},
			unpinRecordingOfflineFn: func(_ context.Context, recordingID string) error {
				calls = append(calls, "unpin-recording:"+recordingID)
				return nil
			},
			pinAlbumOfflineFn: func(_ context.Context, albumID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
				calls = append(calls, "pin-album:"+albumID+":"+preferredProfile)
				return batch, nil
			},
			unpinAlbumOfflineFn: func(_ context.Context, albumID string) error {
				calls = append(calls, "unpin-album:"+albumID)
				return nil
			},
			pinPlaylistOfflineFn: func(_ context.Context, playlistID, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
				calls = append(calls, "pin-playlist:"+playlistID+":"+preferredProfile)
				return batch, nil
			},
			unpinPlaylistOfflineFn: func(_ context.Context, playlistID string) error {
				calls = append(calls, "unpin-playlist:"+playlistID)
				return nil
			},
			pinLikedOfflineFn: func(_ context.Context, preferredProfile string) (apitypes.PlaybackBatchResult, error) {
				calls = append(calls, "pin-liked:"+preferredProfile)
				return batch, nil
			},
			unpinLikedOfflineFn: func(context.Context) error {
				calls = append(calls, "unpin-liked")
				return nil
			},
			listRecordingAvailabilityFn: func(_ context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
				calls = append(calls, "list-availability:"+recordingID+":"+preferredProfile)
				return availabilityItems, nil
			},
			recordingAvailabilityOVFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
				calls = append(calls, "recording-overview:"+recordingID+":"+preferredProfile)
				return recordingOverview, nil
			},
			getRecordingAvailabilityFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
				calls = append(calls, "get-availability:"+recordingID+":"+preferredProfile)
				return availability, nil
			},
			albumAvailabilityOVFn: func(_ context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
				calls = append(calls, "album-overview:"+albumID+":"+preferredProfile)
				return albumOverview, nil
			},
		},
	}

	if got, err := service.InspectPlaybackRecording(ctx, "rec-1", "desktop"); err != nil || got.Phase != inspection.Phase {
		t.Fatalf("inspect playback recording = %+v, err=%v", got, err)
	}
	if got, err := service.PreparePlaybackRecording(ctx, "rec-1", "desktop", apitypes.PlaybackPreparationPlayNow); err != nil || got.Purpose != prepared.Purpose {
		t.Fatalf("prepare playback recording = %+v, err=%v", got, err)
	}
	if got, err := service.GetPlaybackPreparation(ctx, "rec-1", "desktop"); err != nil || got.Phase != prepared.Phase {
		t.Fatalf("get playback preparation = %+v, err=%v", got, err)
	}
	if got, err := service.ResolvePlaybackRecording(ctx, "rec-1", "desktop"); err != nil || got.PlayableURI != resolved.PlayableURI {
		t.Fatalf("resolve playback recording = %+v, err=%v", got, err)
	}
	if got, err := service.PinRecordingOffline(ctx, "rec-1", "desktop"); err != nil || got.BlobID != pinned.BlobID {
		t.Fatalf("pin recording offline = %+v, err=%v", got, err)
	}
	if err := service.UnpinRecordingOffline(ctx, "rec-1"); err != nil {
		t.Fatalf("unpin recording offline: %v", err)
	}
	if got, err := service.PinAlbumOffline(ctx, "album-1", "desktop"); err != nil || got.Tracks != batch.Tracks {
		t.Fatalf("pin album offline = %+v, err=%v", got, err)
	}
	if err := service.UnpinAlbumOffline(ctx, "album-1"); err != nil {
		t.Fatalf("unpin album offline: %v", err)
	}
	if got, err := service.PinPlaylistOffline(ctx, "playlist-1", "desktop"); err != nil || got.Tracks != batch.Tracks {
		t.Fatalf("pin playlist offline = %+v, err=%v", got, err)
	}
	if err := service.UnpinPlaylistOffline(ctx, "playlist-1"); err != nil {
		t.Fatalf("unpin playlist offline: %v", err)
	}
	if got, err := service.PinLikedOffline(ctx, "desktop"); err != nil || got.Tracks != batch.Tracks {
		t.Fatalf("pin liked offline = %+v, err=%v", got, err)
	}
	if err := service.UnpinLikedOffline(ctx); err != nil {
		t.Fatalf("unpin liked offline: %v", err)
	}
	if got, err := service.ListRecordingAvailability(ctx, "rec-1", "desktop"); err != nil || len(got) != 1 || got[0].DeviceID != availabilityItems[0].DeviceID {
		t.Fatalf("list recording availability = %+v, err=%v", got, err)
	}
	if got, err := service.GetRecordingAvailabilityOverview(ctx, "rec-1", "desktop"); err != nil || got.RecordingID != recordingOverview.RecordingID {
		t.Fatalf("get recording availability overview = %+v, err=%v", got, err)
	}
	if got, err := service.GetRecordingAvailability(ctx, "rec-1", "desktop"); err != nil || got.State != availability.State {
		t.Fatalf("get recording availability = %+v, err=%v", got, err)
	}
	if got, err := service.GetAlbumAvailabilityOverview(ctx, "album-1", "desktop"); err != nil || got.AlbumID != albumOverview.AlbumID {
		t.Fatalf("get album availability overview = %+v, err=%v", got, err)
	}

	want := []string{
		"inspect:rec-1:desktop",
		"prepare:rec-1:desktop:play_now",
		"get-prep:rec-1:desktop",
		"resolve:rec-1:desktop",
		"pin-recording:rec-1:desktop",
		"unpin-recording:rec-1",
		"pin-album:album-1:desktop",
		"unpin-album:album-1",
		"pin-playlist:playlist-1:desktop",
		"unpin-playlist:playlist-1",
		"pin-liked:desktop",
		"unpin-liked",
		"list-availability:rec-1:desktop",
		"recording-overview:rec-1:desktop",
		"get-availability:rec-1:desktop",
		"album-overview:album-1:desktop",
	}
	if len(calls) != len(want) {
		t.Fatalf("playback helper call count = %d, want %d (%v)", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("playback helper call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestPlaybackServiceListLibrariesReturnsUnavailableErrorWhenBridgeMissing(t *testing.T) {
	t.Parallel()

	service := &PlaybackService{}
	_, err := service.ListLibraries(context.Background())
	if err == nil || err.Error() != "core bridge is not available" {
		t.Fatalf("list libraries error = %v, want core bridge is not available", err)
	}
}

func TestPreferredProfileDefaultsToSupportedAACProfile(t *testing.T) {
	t.Parallel()

	if got := preferredProfile(settings.CoreRuntimeSettings{}); got != settings.DefaultTranscodeProfile {
		t.Fatalf("preferred profile = %q, want %q", got, settings.DefaultTranscodeProfile)
	}
}

func TestPreferredProfileNormalizesLegacyDesktopProfile(t *testing.T) {
	t.Parallel()

	got := preferredProfile(settings.CoreRuntimeSettings{TranscodeProfile: " desktop "})
	if got != settings.DefaultTranscodeProfile {
		t.Fatalf("preferred profile = %q, want %q", got, settings.DefaultTranscodeProfile)
	}
}

func TestResolvedBlobRootUsesCoreDefaultsWhenSettingsEmpty(t *testing.T) {
	t.Parallel()

	configRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}

	got := resolvedBlobRoot(settings.CoreRuntimeSettings{})
	want := filepath.Join(configRoot, "ben", "v2", "blobs")
	if got != want {
		t.Fatalf("resolved blob root = %q, want %q", got, want)
	}
}
