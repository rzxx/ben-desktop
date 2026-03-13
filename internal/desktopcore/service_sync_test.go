package desktopcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apitypes "ben/core/api/types"
	"gorm.io/gorm"
)

func TestConnectPeerAppliesIncrementalPlaylistSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "shared-sync")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	seedPlaylistRecording(t, owner, library.LibraryID, "rec-1", "One")
	playlist, err := owner.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := owner.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-1",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	if _, err := owner.RenamePlaylist(ctx, playlist.PlaylistID, "Queue 2"); err != nil {
		t.Fatalf("rename playlist: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	var syncedPlaylist Playlist
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", library.LibraryID, playlist.PlaylistID).
		Take(&syncedPlaylist).Error; err != nil {
		t.Fatalf("load synced playlist: %v", err)
	}
	if syncedPlaylist.Name != "Queue 2" {
		t.Fatalf("synced playlist name = %q, want %q", syncedPlaylist.Name, "Queue 2")
	}

	var syncedItems []PlaylistItem
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", library.LibraryID, playlist.PlaylistID).
		Order("position_key ASC, item_id ASC").
		Find(&syncedItems).Error; err != nil {
		t.Fatalf("load synced playlist items: %v", err)
	}
	if len(syncedItems) != 1 || syncedItems[0].TrackVariantID != "rec-1" {
		t.Fatalf("unexpected synced playlist items: %+v", syncedItems)
	}

	var peerState PeerSyncState
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Take(&peerState).Error; err != nil {
		t.Fatalf("load peer sync state: %v", err)
	}
	if peerState.LastApplied == 0 || peerState.LastError != "" {
		t.Fatalf("unexpected peer sync state: %+v", peerState)
	}
	if peerState.PeerID != ownerLocal.PeerID {
		t.Fatalf("peer sync state peer id = %q, want %q", peerState.PeerID, ownerLocal.PeerID)
	}
	if joinerLocal.LibraryID != library.LibraryID {
		t.Fatalf("joiner active library = %q, want %q", joinerLocal.LibraryID, library.LibraryID)
	}
}

func TestVerifyTransportPeerAuthRejectsTamperedMembershipCert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "auth-verify")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	_, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	certRow, ok, err := joiner.loadMembershipCert(ctx, library.LibraryID, joinerLocal.DeviceID)
	if err != nil {
		t.Fatalf("load joiner membership cert: %v", err)
	}
	if !ok {
		t.Fatal("expected joiner membership cert")
	}
	authorityRows, err := joiner.loadAdmissionAuthorityChain(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("load joiner authority chain: %v", err)
	}
	auth := transportPeerAuth{
		Cert:           membershipCertEnvelopeFromRow(certRow),
		AuthorityChain: admissionAuthorityChainFromRows(authorityRows),
	}
	auth.Cert.Sig = []byte("tampered")

	_, err = owner.verifyTransportPeerAuth(ctx, library.LibraryID, joinerLocal.DeviceID, joinerLocal.PeerID, joinerLocal.PeerID, auth)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "membership certificate") {
		t.Fatalf("expected membership certificate verification error, got %v", err)
	}
}

func TestSyncNowInstallsCheckpointWhenBacklogReachesCutover(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "checkpoint-sync")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	playlistID, manifest := seedCheckpointBacklog(t, owner, library.LibraryID, ownerLocal.DeviceID, 5001)
	if manifest.EntryCount != 5001 {
		t.Fatalf("checkpoint entry count = %d, want 5001", manifest.EntryCount)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.SyncNow(ctx); err != nil {
		t.Fatalf("sync now: %v", err)
	}

	var syncedPlaylist Playlist
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", library.LibraryID, playlistID).
		Take(&syncedPlaylist).Error; err != nil {
		t.Fatalf("load checkpoint-installed playlist: %v", err)
	}
	if syncedPlaylist.Name != "Queue 5001" {
		t.Fatalf("checkpoint-installed playlist name = %q, want %q", syncedPlaylist.Name, "Queue 5001")
	}

	var ack DeviceCheckpointAck
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, joinerLocal.DeviceID).
		Take(&ack).Error; err != nil {
		t.Fatalf("load owner checkpoint ack: %v", err)
	}
	if ack.CheckpointID != manifest.CheckpointID || ack.Source != checkpointAckSourceInstalled {
		t.Fatalf("unexpected checkpoint ack: %+v", ack)
	}

	var peerState PeerSyncState
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Take(&peerState).Error; err != nil {
		t.Fatalf("load checkpoint peer sync state: %v", err)
	}
	if peerState.LastApplied != 5001 || peerState.LastError != "" {
		t.Fatalf("unexpected checkpoint peer sync state: %+v", peerState)
	}
}

func TestStartSyncNowQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "sync-now-async")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	seedPlaylistRecording(t, owner, library.LibraryID, "rec-1", "One")
	playlist, err := owner.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := owner.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-1",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	job, err := joiner.StartSyncNow(ctx)
	if err != nil {
		t.Fatalf("start sync now: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindSyncNow {
		t.Fatalf("unexpected queued sync job: %+v", job)
	}

	final := waitForJobPhase(t, ctx, joiner, "sync:"+library.LibraryID, JobPhaseCompleted)
	if final.Kind != jobKindSyncNow || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final sync job: %+v", final)
	}

	var syncedPlaylist Playlist
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", library.LibraryID, playlist.PlaylistID).
		Take(&syncedPlaylist).Error; err != nil {
		t.Fatalf("load synced playlist: %v", err)
	}
	if syncedPlaylist.Name != "Queue" {
		t.Fatalf("synced playlist name = %q, want %q", syncedPlaylist.Name, "Queue")
	}

	var peerState PeerSyncState
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Take(&peerState).Error; err != nil {
		t.Fatalf("load peer sync state: %v", err)
	}
	if peerState.LastApplied == 0 || peerState.LastError != "" {
		t.Fatalf("unexpected peer sync state: %+v", peerState)
	}
	if joinerLocal.LibraryID != library.LibraryID {
		t.Fatalf("joiner active library = %q, want %q", joinerLocal.LibraryID, library.LibraryID)
	}
}

func TestStartSyncNowCancelsWhenActiveLibraryChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)

	first, err := app.CreateLibrary(ctx, "sync-cancel-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	second, err := app.CreateLibrary(ctx, "sync-cancel-b")
	if err != nil {
		t.Fatalf("create second library: %v", err)
	}
	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("select first library: %v", err)
	}

	transport := &blockingSyncTransport{started: make(chan struct{}, 1)}
	app.SetSyncTransport(transport)

	job, err := app.StartSyncNow(ctx)
	if err != nil {
		t.Fatalf("start sync now: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindSyncNow {
		t.Fatalf("unexpected queued sync job: %+v", job)
	}

	select {
	case <-transport.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync transport to start")
	}

	if _, err := app.SelectLibrary(ctx, second.LibraryID); err != nil {
		t.Fatalf("switch active library: %v", err)
	}

	final := waitForJobPhase(t, ctx, app, "sync:"+first.LibraryID, JobPhaseFailed)
	if !strings.Contains(final.Message, "no longer active") {
		t.Fatalf("expected canceled sync job message, got %+v", final)
	}
}

func TestConnectPeerAppliesIncrementalLibraryAndCatalogSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "sync-track.flac")
	if err := os.WriteFile(audioPath, []byte("sync-audio"), 0o644); err != nil {
		t.Fatalf("write sync audio: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Sync Track",
				Album:       "Sync Album",
				AlbumArtist: "Sync Artist",
				Artists:     []string{"Sync Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
				DurationMS:  205000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
		},
	}
	owner := openCacheTestAppWithTagReader(t, 1024, reader)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "shared-catalog")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := owner.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set owner scan roots: %v", err)
	}
	if _, err := owner.RescanNow(ctx); err != nil {
		t.Fatalf("owner rescan: %v", err)
	}
	if _, err := owner.RenameLibrary(ctx, library.LibraryID, "shared-catalog-renamed"); err != nil {
		t.Fatalf("rename shared library: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	libraries, err := joiner.ListLibraries(ctx)
	if err != nil {
		t.Fatalf("list joiner libraries: %v", err)
	}
	if len(libraries) != 1 || libraries[0].Name != "shared-catalog-renamed" {
		t.Fatalf("unexpected synced libraries: %+v", libraries)
	}

	recordings, err := joiner.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list synced recordings: %v", err)
	}
	if len(recordings.Items) != 1 || recordings.Items[0].Title != "Sync Track" {
		t.Fatalf("unexpected synced recordings: %+v", recordings.Items)
	}

	var syncedRoots []ScanRoot
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Order("root_path ASC").
		Find(&syncedRoots).Error; err != nil {
		t.Fatalf("load synced scan roots: %v", err)
	}
	if len(syncedRoots) != 1 || syncedRoots[0].RootPath != filepath.Clean(root) {
		t.Fatalf("unexpected synced scan roots: %+v", syncedRoots)
	}

	var syncedSource SourceFileModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", library.LibraryID, ownerLocal.DeviceID, true).
		Take(&syncedSource).Error; err != nil {
		t.Fatalf("load synced source file: %v", err)
	}
	if syncedSource.LocalPath != filepath.Clean(audioPath) {
		t.Fatalf("synced source path = %q, want %q", syncedSource.LocalPath, filepath.Clean(audioPath))
	}
}

func TestInstallCheckpointRecordReplaysLibraryAndCatalogState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "checkpoint-track.flac")
	if err := os.WriteFile(audioPath, []byte("checkpoint-audio"), 0o644); err != nil {
		t.Fatalf("write checkpoint audio: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Checkpoint Track",
				Album:       "Checkpoint Album",
				AlbumArtist: "Checkpoint Artist",
				Artists:     []string{"Checkpoint Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
				DurationMS:  198000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
		},
	}
	owner := openCacheTestAppWithTagReader(t, 1024, reader)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "checkpoint-catalog")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := owner.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set owner scan roots: %v", err)
	}
	if _, err := owner.RescanNow(ctx); err != nil {
		t.Fatalf("owner rescan: %v", err)
	}
	if _, err := owner.RenameLibrary(ctx, library.LibraryID, "checkpoint-catalog-renamed"); err != nil {
		t.Fatalf("rename shared library: %v", err)
	}

	manifest, err := owner.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish checkpoint: %v", err)
	}
	record, ok, err := owner.loadCheckpointTransferRecord(ctx, library.LibraryID, manifest.CheckpointID, false)
	if err != nil {
		t.Fatalf("load checkpoint transfer: %v", err)
	}
	if !ok {
		t.Fatalf("expected checkpoint transfer record")
	}

	applied, err := joiner.installCheckpointRecord(ctx, joinerLocal.DeviceID, record)
	if err != nil {
		t.Fatalf("install checkpoint record: %v", err)
	}
	if applied == 0 {
		t.Fatalf("expected checkpoint install to replay entries")
	}

	libraries, err := joiner.ListLibraries(ctx)
	if err != nil {
		t.Fatalf("list joiner libraries: %v", err)
	}
	if len(libraries) != 1 || libraries[0].Name != "checkpoint-catalog-renamed" {
		t.Fatalf("unexpected checkpoint-installed libraries: %+v", libraries)
	}

	recordings, err := joiner.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list checkpoint-installed recordings: %v", err)
	}
	if len(recordings.Items) != 1 || recordings.Items[0].Title != "Checkpoint Track" {
		t.Fatalf("unexpected checkpoint-installed recordings: %+v", recordings.Items)
	}

	var syncedRoots []ScanRoot
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Order("root_path ASC").
		Find(&syncedRoots).Error; err != nil {
		t.Fatalf("load checkpoint-installed scan roots: %v", err)
	}
	if len(syncedRoots) != 1 || syncedRoots[0].RootPath != filepath.Clean(root) {
		t.Fatalf("unexpected checkpoint-installed scan roots: %+v", syncedRoots)
	}

	var syncedSource SourceFileModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", library.LibraryID, ownerLocal.DeviceID, true).
		Take(&syncedSource).Error; err != nil {
		t.Fatalf("load checkpoint-installed source file: %v", err)
	}
	if syncedSource.LocalPath != filepath.Clean(audioPath) {
		t.Fatalf("checkpoint-installed source path = %q, want %q", syncedSource.LocalPath, filepath.Clean(audioPath))
	}
}

func TestConnectPeerAppliesReplicatedPreferencesPinsAndMaterializedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	audioPath := filepath.Join(root, "replicated-state.flac")
	if err := os.WriteFile(audioPath, []byte("replicated-state-audio"), 0o644); err != nil {
		t.Fatalf("write replicated audio: %v", err)
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(audioPath): {
				Title:       "Replicated State Track",
				Album:       "Replicated State Album",
				AlbumArtist: "Replicated State Artist",
				Artists:     []string{"Replicated State Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2026,
				DurationMS:  201000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
		},
	}
	owner := openCacheTestAppWithTagReader(t, 1024, reader)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "replicated-state-sync")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := owner.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set owner scan roots: %v", err)
	}
	if _, err := owner.RescanNow(ctx); err != nil {
		t.Fatalf("owner rescan: %v", err)
	}

	recordings, err := owner.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list owner recordings: %v", err)
	}
	if len(recordings.Items) != 1 {
		t.Fatalf("recording count = %d, want 1", len(recordings.Items))
	}
	recordingID := recordings.Items[0].RecordingID
	var recording TrackVariantModel
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND track_variant_id = ?", library.LibraryID, recordingID).
		Take(&recording).Error; err != nil {
		t.Fatalf("load owner recording row: %v", err)
	}

	albums, err := owner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list owner albums: %v", err)
	}
	if len(albums.Items) != 1 {
		t.Fatalf("album count = %d, want 1", len(albums.Items))
	}
	albumID := albums.Items[0].AlbumID
	var album AlbumVariantModel
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND album_variant_id = ?", library.LibraryID, albumID).
		Take(&album).Error; err != nil {
		t.Fatalf("load owner album row: %v", err)
	}

	if err := owner.SetPreferredRecordingVariant(ctx, recordingID, recordingID); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if err := owner.SetPreferredAlbumVariant(ctx, albumID, albumID); err != nil {
		t.Fatalf("set preferred album variant: %v", err)
	}

	var source SourceFileModel
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", library.LibraryID, ownerLocal.DeviceID, true).
		Take(&source).Error; err != nil {
		t.Fatalf("load owner source file: %v", err)
	}

	encodingBlobID := testBlobID("7")
	artworkBlobID := testBlobID("8")
	now := time.Now().UTC()
	if err := owner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := owner.upsertOptimizedAssetTx(tx, ownerLocal, OptimizedAssetModel{
			OptimizedAssetID:  "enc-sync",
			SourceFileID:      source.SourceFileID,
			TrackVariantID:    recordingID,
			Profile:           "desktop",
			BlobID:            encodingBlobID,
			MIME:              "audio/mp4",
			DurationMS:        201000,
			Bitrate:           128000,
			Codec:             "aac",
			Container:         "m4a",
			CreatedByDeviceID: ownerLocal.DeviceID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}); err != nil {
			return err
		}
		if err := owner.upsertDeviceAssetCacheTx(tx, ownerLocal, DeviceAssetCacheModel{
			DeviceID:         ownerLocal.DeviceID,
			OptimizedAssetID: "enc-sync",
			IsCached:         true,
			LastVerifiedAt:   &now,
			UpdatedAt:        now,
		}); err != nil {
			return err
		}
		return owner.upsertArtworkVariantTx(tx, ownerLocal, ArtworkVariant{
			ScopeType:       "album",
			ScopeID:         albumID,
			Variant:         defaultArtworkVariant320,
			BlobID:          artworkBlobID,
			MIME:            "image/webp",
			FileExt:         ".webp",
			W:               320,
			H:               320,
			Bytes:           64,
			ChosenSource:    "embedded_front",
			ChosenSourceRef: filepath.Clean(audioPath),
			UpdatedAt:       now,
		})
	}); err != nil {
		t.Fatalf("seed oplog-backed replicated state: %v", err)
	}
	writeCacheBlob(t, owner, encodingBlobID, 128)

	if _, err := owner.PinRecordingOffline(ctx, recordingID, "desktop"); err != nil {
		t.Fatalf("pin recording offline: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	var trackPref DeviceVariantPreference
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", library.LibraryID, ownerLocal.DeviceID, "track", recording.TrackClusterID).
		Take(&trackPref).Error; err != nil {
		t.Fatalf("load synced track preference: %v", err)
	}
	if trackPref.ChosenVariantID != recordingID {
		t.Fatalf("track preference = %q, want %q", trackPref.ChosenVariantID, recordingID)
	}

	var albumPref DeviceVariantPreference
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", library.LibraryID, ownerLocal.DeviceID, "album", album.AlbumClusterID).
		Take(&albumPref).Error; err != nil {
		t.Fatalf("load synced album preference: %v", err)
	}
	if albumPref.ChosenVariantID != albumID {
		t.Fatalf("album preference = %q, want %q", albumPref.ChosenVariantID, albumID)
	}

	var pin OfflinePin
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", library.LibraryID, ownerLocal.DeviceID, "recording", recordingID).
		Take(&pin).Error; err != nil {
		t.Fatalf("load synced offline pin: %v", err)
	}
	if pin.Profile != "desktop" {
		t.Fatalf("offline pin profile = %q, want desktop", pin.Profile)
	}

	var encoding OptimizedAssetModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND optimized_asset_id = ?", library.LibraryID, "enc-sync").
		Take(&encoding).Error; err != nil {
		t.Fatalf("load synced optimized asset: %v", err)
	}
	if encoding.BlobID != encodingBlobID || encoding.TrackVariantID != recordingID {
		t.Fatalf("unexpected synced optimized asset: %+v", encoding)
	}

	var deviceCache DeviceAssetCacheModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND optimized_asset_id = ?", library.LibraryID, ownerLocal.DeviceID, "enc-sync").
		Take(&deviceCache).Error; err != nil {
		t.Fatalf("load synced device asset cache: %v", err)
	}
	if !deviceCache.IsCached {
		t.Fatalf("expected synced device asset cache to remain cached")
	}

	var artwork ArtworkVariant
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", library.LibraryID, "album", albumID, defaultArtworkVariant320).
		Take(&artwork).Error; err != nil {
		t.Fatalf("load synced artwork variant: %v", err)
	}
	if artwork.BlobID != artworkBlobID {
		t.Fatalf("artwork blob = %q, want %q", artwork.BlobID, artworkBlobID)
	}
}

func TestConnectPeerAppliesPlaylistArtworkDeletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "playlist-artwork-delete")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	seedPlaylistRecording(t, owner, library.LibraryID, "rec-playlist", "Playlist")

	playlist, err := owner.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	now := time.Now().UTC()
	if err := owner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return owner.upsertArtworkVariantTx(tx, ownerLocal, ArtworkVariant{
			ScopeType:       "playlist",
			ScopeID:         playlist.PlaylistID,
			Variant:         defaultArtworkVariant320,
			BlobID:          testBlobID("9"),
			MIME:            "image/webp",
			FileExt:         ".webp",
			W:               320,
			H:               320,
			Bytes:           32,
			ChosenSource:    "manual",
			ChosenSourceRef: "test",
			UpdatedAt:       now,
		})
	}); err != nil {
		t.Fatalf("seed playlist artwork: %v", err)
	}
	if err := owner.DeletePlaylist(ctx, playlist.PlaylistID); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	var activePlaylists int64
	if err := joiner.db.WithContext(ctx).
		Model(&Playlist{}).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", library.LibraryID, playlist.PlaylistID).
		Count(&activePlaylists).Error; err != nil {
		t.Fatalf("count active synced playlist: %v", err)
	}
	if activePlaylists != 0 {
		t.Fatalf("active synced playlist count = %d, want 0", activePlaylists)
	}

	var artworkCount int64
	if err := joiner.db.WithContext(ctx).
		Model(&ArtworkVariant{}).
		Where("library_id = ? AND scope_type = ? AND scope_id = ?", library.LibraryID, "playlist", playlist.PlaylistID).
		Count(&artworkCount).Error; err != nil {
		t.Fatalf("count synced playlist artwork: %v", err)
	}
	if artworkCount != 0 {
		t.Fatalf("playlist artwork count = %d, want 0", artworkCount)
	}
}

func seedSharedLibraryForSync(t *testing.T, owner, joiner *App, library apitypes.LibrarySummary) (apitypes.LocalContext, apitypes.LocalContext) {
	t.Helper()

	ctx := context.Background()
	ownerLocal, err := owner.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("owner active context: %v", err)
	}
	ownerPeerID, err := owner.ensureDevicePeerID(ctx, ownerLocal.DeviceID, ownerLocal.Device)
	if err != nil {
		t.Fatalf("owner peer id: %v", err)
	}
	ownerLocal.PeerID = ownerPeerID

	joinerDevice, err := joiner.ensureCurrentDevice(ctx)
	if err != nil {
		t.Fatalf("joiner current device: %v", err)
	}
	joinerPeerID, err := joiner.ensureDevicePeerID(ctx, joinerDevice.DeviceID, joinerDevice.Name)
	if err != nil {
		t.Fatalf("joiner peer id: %v", err)
	}
	now := time.Now().UTC()

	if err := owner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		libraryRow, _, _, err := ensureLibraryJoinMaterialTx(tx, library.LibraryID, now)
		if err != nil {
			return err
		}
		library.Name = libraryRow.Name
		if err := tx.Create(&Device{
			DeviceID:   joinerDevice.DeviceID,
			Name:       joinerDevice.Name,
			PeerID:     joinerPeerID,
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Membership{
			LibraryID:        library.LibraryID,
			DeviceID:         joinerDevice.DeviceID,
			Role:             roleMember,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		_, err = issueMembershipCertTx(tx, library.LibraryID, joinerDevice.DeviceID, joinerPeerID, roleMember, defaultMembershipCertTTL)
		return err
	}); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}

	if err := joiner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ownerLibrary Library
		if err := owner.db.WithContext(ctx).Where("library_id = ?", library.LibraryID).Take(&ownerLibrary).Error; err != nil {
			return err
		}
		var authorityRows []AdmissionAuthority
		if err := owner.db.WithContext(ctx).
			Where("library_id = ?", library.LibraryID).
			Order("version ASC").
			Find(&authorityRows).Error; err != nil {
			return err
		}
		joinerCert, ok, err := joinerPeerMembershipCert(owner, ctx, library.LibraryID, joinerDevice.DeviceID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("joiner membership certificate not found")
		}
		if err := tx.Create(&Library{
			LibraryID:     library.LibraryID,
			Name:          ownerLibrary.Name,
			RootPublicKey: ownerLibrary.RootPublicKey,
			LibraryKey:    ownerLibrary.LibraryKey,
			CreatedAt:     now,
		}).Error; err != nil {
			return err
		}
		if len(authorityRows) > 0 {
			if err := tx.Create(&authorityRows).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&Device{
			DeviceID:   ownerLocal.DeviceID,
			Name:       ownerLocal.Device,
			PeerID:     ownerPeerID,
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create([]Membership{
			{
				LibraryID:        library.LibraryID,
				DeviceID:         ownerLocal.DeviceID,
				Role:             roleAdmin,
				CapabilitiesJSON: "{}",
				JoinedAt:         now,
			},
			{
				LibraryID:        library.LibraryID,
				DeviceID:         joinerDevice.DeviceID,
				Role:             roleMember,
				CapabilitiesJSON: "{}",
				JoinedAt:         now,
			},
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Device{}).
			Where("device_id = ?", joinerDevice.DeviceID).
			Updates(map[string]any{
				"peer_id":           joinerPeerID,
				"active_library_id": library.LibraryID,
			}).Error; err != nil {
			return err
		}
		if err := saveMembershipCertTx(tx, joinerCert); err != nil {
			return err
		}
		return ensureLikedPlaylistTx(tx, library.LibraryID, joinerDevice.DeviceID, now)
	}); err != nil {
		t.Fatalf("seed joiner library: %v", err)
	}

	joinerLocal, err := joiner.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("joiner active context: %v", err)
	}
	joinerLocal.PeerID = joinerPeerID
	return ownerLocal, joinerLocal
}

func joinerPeerMembershipCert(owner *App, ctx context.Context, libraryID, deviceID string) (membershipCertEnvelope, bool, error) {
	row, ok, err := owner.loadMembershipCert(ctx, libraryID, deviceID)
	if err != nil || !ok {
		return membershipCertEnvelope{}, ok, err
	}
	return membershipCertEnvelopeFromRow(row), true, nil
}

func seedCheckpointBacklog(t *testing.T, app *App, libraryID, deviceID string, totalOps int) (string, apitypes.LibraryCheckpointManifest) {
	t.Helper()

	ctx := context.Background()
	playlist, err := app.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create checkpoint playlist: %v", err)
	}
	if totalOps <= 1 {
		manifest, err := app.PublishCheckpoint(ctx)
		if err != nil {
			t.Fatalf("publish checkpoint: %v", err)
		}
		return playlist.PlaylistID, manifest
	}

	base := time.Now().UTC()
	entries := make([]OplogEntry, 0, totalOps-1)
	for seq := 2; seq <= totalOps; seq++ {
		name := fmt.Sprintf("Queue %d", seq)
		ts := base.Add(time.Duration(seq) * time.Millisecond)
		entries = append(entries, OplogEntry{
			LibraryID:   libraryID,
			OpID:        fmt.Sprintf("%s:%d", deviceID, seq),
			DeviceID:    deviceID,
			Seq:         int64(seq),
			TSNS:        ts.UnixNano(),
			EntityType:  "playlist",
			EntityID:    playlist.PlaylistID,
			OpKind:      "upsert",
			PayloadJSON: fmt.Sprintf(`{"playlistId":%q,"name":%q,"kind":"normal","createdBy":%q}`, playlist.PlaylistID, name, deviceID),
		})
	}
	for start := 0; start < len(entries); start += 250 {
		end := start + 250
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[start:end]
		if err := app.db.WithContext(ctx).Create(batch).Error; err != nil {
			t.Fatalf("seed checkpoint backlog entries: %v", err)
		}
	}
	if err := app.db.WithContext(ctx).Model(&Playlist{}).
		Where("library_id = ? AND playlist_id = ?", libraryID, playlist.PlaylistID).
		Updates(map[string]any{
			"name":       fmt.Sprintf("Queue %d", totalOps),
			"updated_at": base.Add(time.Duration(totalOps) * time.Millisecond),
		}).Error; err != nil {
		t.Fatalf("update checkpoint playlist state: %v", err)
	}
	if err := app.db.WithContext(ctx).Model(&DeviceClock{}).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Update("last_seq_seen", totalOps).Error; err != nil {
		t.Fatalf("update device clock: %v", err)
	}

	manifest, err := app.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish checkpoint: %v", err)
	}
	return playlist.PlaylistID, manifest
}

type memorySyncRegistry struct {
	mu   sync.RWMutex
	apps map[string]*App
}

func newMemorySyncRegistry() *memorySyncRegistry {
	return &memorySyncRegistry{apps: make(map[string]*App)}
}

func (r *memorySyncRegistry) transport(addr string, app *App) *memorySyncTransport {
	r.mu.Lock()
	r.apps[addr] = app
	r.mu.Unlock()
	return &memorySyncTransport{selfAddr: addr, registry: r}
}

type memorySyncTransport struct {
	selfAddr string
	registry *memorySyncRegistry
}

func (t *memorySyncTransport) ListPeers(ctx context.Context, local apitypes.LocalContext) ([]SyncPeer, error) {
	t.registry.mu.RLock()
	defer t.registry.mu.RUnlock()

	peers := make([]SyncPeer, 0, len(t.registry.apps))
	for addr, app := range t.registry.apps {
		if addr == t.selfAddr {
			continue
		}
		remote, ok, err := app.ActiveLibrary(ctx)
		if err != nil || !ok {
			continue
		}
		if remote.LibraryID != local.LibraryID {
			continue
		}
		peers = append(peers, &memorySyncPeer{addr: addr, app: app})
	}
	return peers, nil
}

func (t *memorySyncTransport) ResolvePeer(_ context.Context, _ apitypes.LocalContext, peerAddr string) (SyncPeer, error) {
	t.registry.mu.RLock()
	defer t.registry.mu.RUnlock()

	app, ok := t.registry.apps[peerAddr]
	if !ok {
		return nil, fmt.Errorf("peer %q not found", peerAddr)
	}
	return &memorySyncPeer{addr: peerAddr, app: app}, nil
}

type blockingSyncTransport struct {
	started chan struct{}
}

func (t *blockingSyncTransport) ListPeers(ctx context.Context, local apitypes.LocalContext) ([]SyncPeer, error) {
	if t.started != nil {
		select {
		case t.started <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (t *blockingSyncTransport) ResolvePeer(ctx context.Context, local apitypes.LocalContext, peerAddr string) (SyncPeer, error) {
	return nil, fmt.Errorf("resolve peer not implemented for blocking sync transport")
}

type memorySyncPeer struct {
	addr string
	app  *App
}

func (p *memorySyncPeer) Address() string { return p.addr }

func (p *memorySyncPeer) DeviceID() string {
	local, err := p.app.EnsureLocalContext(context.Background())
	if err != nil {
		return ""
	}
	return local.DeviceID
}

func (p *memorySyncPeer) PeerID() string {
	local, err := p.app.EnsureLocalContext(context.Background())
	if err != nil {
		return ""
	}
	local, err = p.app.ensureLocalPeerContext(context.Background(), local)
	if err != nil {
		return ""
	}
	return local.PeerID
}

func (p *memorySyncPeer) Sync(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	if _, err := p.app.verifyTransportPeerAuth(ctx, req.LibraryID, req.DeviceID, req.PeerID, req.PeerID, req.Auth); err != nil {
		return SyncResponse{}, err
	}
	return p.app.buildSyncResponse(ctx, req)
}

func (p *memorySyncPeer) FetchCheckpoint(ctx context.Context, req CheckpointFetchRequest) (CheckpointFetchResponse, error) {
	if _, err := p.app.verifyTransportPeerAuth(ctx, req.LibraryID, req.Auth.Cert.DeviceID, req.Auth.Cert.PeerID, req.Auth.Cert.PeerID, req.Auth); err != nil {
		return CheckpointFetchResponse{}, err
	}
	return p.app.buildCheckpointFetchResponse(ctx, req)
}

func (p *memorySyncPeer) RefreshMembership(ctx context.Context, req MembershipRefreshRequest) (MembershipRefreshResponse, error) {
	return p.app.buildMembershipRefreshResponse(ctx, req)
}
