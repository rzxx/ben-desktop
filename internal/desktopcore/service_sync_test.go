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

	apitypes "ben/desktop/api/types"
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

func TestConnectPeerEmitsAvailabilityInvalidationAfterApplyingOps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "sync-availability-events")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	_, _ = seedSharedLibraryForSync(t, owner, joiner, library)

	seedPlaylistRecording(t, owner, library.LibraryID, "rec-sync-avail", "Sync")
	playlist, err := owner.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := owner.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-sync-avail",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	var (
		eventsMu sync.Mutex
		events   []apitypes.CatalogChangeEvent
	)
	stopListening := joiner.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})
	defer stopListening()

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, event := range events {
		if event.Kind != apitypes.CatalogChangeInvalidateAvailability {
			continue
		}
		if !event.InvalidateAll {
			t.Fatalf("expected sync availability event to invalidate all, got %+v", event)
		}
		return
	}
	t.Fatalf("expected sync to emit availability invalidation event, got %+v", events)
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

func TestBuildSyncResponseStaysIncrementalBelowCheckpointCutover(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "checkpoint-cutover-incremental")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	playlistID, manifest := seedCheckpointBacklog(t, owner, library.LibraryID, ownerLocal.DeviceID, 1)
	record, ok, err := owner.loadCheckpointTransferRecord(ctx, library.LibraryID, manifest.CheckpointID, false)
	if err != nil {
		t.Fatalf("load checkpoint transfer: %v", err)
	}
	if !ok {
		t.Fatal("expected published checkpoint transfer")
	}
	if _, err := joiner.installCheckpointRecord(ctx, joinerLocal.DeviceID, record); err != nil {
		t.Fatalf("install initial checkpoint: %v", err)
	}

	appendCheckpointTailOps(t, owner, library.LibraryID, ownerLocal.DeviceID, playlistID, 4999)

	req, err := joiner.buildSyncRequest(ctx, library.LibraryID, joinerLocal.DeviceID, joinerLocal.PeerID, defaultSyncBatchSize)
	if err != nil {
		t.Fatalf("build sync request: %v", err)
	}
	resp, err := owner.buildSyncResponse(ctx, req)
	if err != nil {
		t.Fatalf("build sync response: %v", err)
	}
	if resp.NeedCheckpoint {
		t.Fatalf("expected incremental response below cutover, got %+v", resp)
	}
	if resp.Checkpoint != nil {
		t.Fatalf("unexpected checkpoint summary on incremental response: %+v", resp.Checkpoint)
	}
	if len(resp.Ops) == 0 || !resp.HasMore {
		t.Fatalf("expected paged incremental ops below cutover, got %+v", resp)
	}
}

func TestBuildSyncResponseUsesCheckpointAtBacklogCutover(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "checkpoint-cutover-bootstrap")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	playlistID, manifest := seedCheckpointBacklog(t, owner, library.LibraryID, ownerLocal.DeviceID, 1)
	record, ok, err := owner.loadCheckpointTransferRecord(ctx, library.LibraryID, manifest.CheckpointID, false)
	if err != nil {
		t.Fatalf("load checkpoint transfer: %v", err)
	}
	if !ok {
		t.Fatal("expected published checkpoint transfer")
	}
	if _, err := joiner.installCheckpointRecord(ctx, joinerLocal.DeviceID, record); err != nil {
		t.Fatalf("install initial checkpoint: %v", err)
	}

	appendCheckpointTailOps(t, owner, library.LibraryID, ownerLocal.DeviceID, playlistID, incrementalSyncBacklogCutover)

	req, err := joiner.buildSyncRequest(ctx, library.LibraryID, joinerLocal.DeviceID, joinerLocal.PeerID, defaultSyncBatchSize)
	if err != nil {
		t.Fatalf("build sync request: %v", err)
	}
	resp, err := owner.buildSyncResponse(ctx, req)
	if err != nil {
		t.Fatalf("build sync response: %v", err)
	}
	if !resp.NeedCheckpoint {
		t.Fatalf("expected checkpoint bootstrap at cutover, got %+v", resp)
	}
	if resp.Checkpoint == nil || resp.Checkpoint.CheckpointID != manifest.CheckpointID {
		t.Fatalf("unexpected checkpoint summary at cutover: %+v", resp.Checkpoint)
	}
	if len(resp.Ops) != 0 {
		t.Fatalf("expected checkpoint bootstrap to omit incremental ops, got %d", len(resp.Ops))
	}
}

func TestTransportStartupCatchupInstallsCheckpointAfterRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registry := newManagedMemorySyncRegistry()
	owner := openPlaylistTestAppAtPath(t, filepath.Join(t.TempDir(), "owner"))
	t.Cleanup(func() {
		_ = owner.Close()
	})
	owner.transportService.backgroundInterval = 0
	owner.transportService.factory = registry.factory("memory://owner", owner)
	joinerRoot := t.TempDir()
	joiner := openPlaylistTestAppAtPath(t, joinerRoot)
	joiner.transportService.backgroundInterval = 0
	joiner.transportService.factory = registry.factory("memory://joiner", joiner)

	library, err := owner.CreateLibrary(ctx, "checkpoint-restart-bootstrap")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	playlistID, manifest := seedCheckpointBacklog(t, owner, library.LibraryID, ownerLocal.DeviceID, 1)
	if err := joiner.Close(); err != nil {
		t.Fatalf("close joiner before restart: %v", err)
	}

	joiner = openPlaylistTestAppAtPath(t, joinerRoot)
	t.Cleanup(func() {
		_ = joiner.Close()
	})
	joiner.transportService.backgroundInterval = 0
	joiner.transportService.factory = registry.factory("memory://joiner", joiner)
	joiner.transportService.Stop()
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("restart joiner runtime services: %v", err)
	}

	waitForPlaylistName(t, ctx, joiner, library.LibraryID, playlistID, "Queue")

	ownerAck := waitForCheckpointAck(t, ctx, owner, library.LibraryID, joinerLocal.DeviceID)
	if ownerAck.CheckpointID != manifest.CheckpointID || ownerAck.Source != checkpointAckSourceInstalled {
		t.Fatalf("unexpected restart checkpoint ack: %+v", ownerAck)
	}

	var peerState PeerSyncState
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Take(&peerState).Error; err != nil {
		t.Fatalf("load peer sync state after restart: %v", err)
	}
	if peerState.LastApplied != 1 || peerState.LastError != "" {
		t.Fatalf("unexpected restart peer sync state: %+v", peerState)
	}
}

func TestManagedTransportAutoAppliesRemoteUpdateWithoutReconnect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	registry := newManagedMemorySyncRegistry()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)
	owner.transportService.factory = registry.factory("memory://owner", owner)
	joiner.transportService.factory = registry.factory("memory://joiner", joiner)

	library, err := owner.CreateLibrary(ctx, "managed-remote-update")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	playlist, err := owner.CreatePlaylist(ctx, "After Connect", "")
	if err != nil {
		t.Fatalf("create post-connect playlist: %v", err)
	}

	waitForPlaylistName(t, ctx, joiner, library.LibraryID, playlist.PlaylistID, "After Connect")

	var peerState PeerSyncState
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Take(&peerState).Error; err != nil {
		t.Fatalf("load peer sync state after remote update: %v", err)
	}
	if peerState.LastApplied == 0 || peerState.LastError != "" {
		t.Fatalf("unexpected peer sync error after remote update: %+v", peerState)
	}
}

func TestInstallCheckpointReplacementPrunesSupersededLocalState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "checkpoint-replacement-cleanup")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	playlistID, first := seedCheckpointBacklog(t, owner, library.LibraryID, ownerLocal.DeviceID, 1)
	firstRecord, ok, err := owner.loadCheckpointTransferRecord(ctx, library.LibraryID, first.CheckpointID, false)
	if err != nil {
		t.Fatalf("load first checkpoint transfer: %v", err)
	}
	if !ok {
		t.Fatal("expected first checkpoint transfer")
	}
	if _, err := joiner.installCheckpointRecord(ctx, joinerLocal.DeviceID, firstRecord); err != nil {
		t.Fatalf("install first checkpoint: %v", err)
	}

	appendCheckpointTailOps(t, owner, library.LibraryID, ownerLocal.DeviceID, playlistID, 3)
	second, err := owner.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish replacement checkpoint: %v", err)
	}
	secondRecord, ok, err := owner.loadCheckpointTransferRecord(ctx, library.LibraryID, second.CheckpointID, false)
	if err != nil {
		t.Fatalf("load second checkpoint transfer: %v", err)
	}
	if !ok {
		t.Fatal("expected second checkpoint transfer")
	}
	if _, err := joiner.installCheckpointRecord(ctx, joinerLocal.DeviceID, secondRecord); err != nil {
		t.Fatalf("install second checkpoint: %v", err)
	}

	var checkpointCount int64
	if err := joiner.db.WithContext(ctx).Model(&LibraryCheckpoint{}).
		Where("library_id = ?", library.LibraryID).
		Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count local checkpoints: %v", err)
	}
	if checkpointCount != 1 {
		t.Fatalf("local checkpoint count = %d, want 1", checkpointCount)
	}

	var staleAckCount int64
	if err := joiner.db.WithContext(ctx).Model(&DeviceCheckpointAck{}).
		Where("library_id = ? AND checkpoint_id = ?", library.LibraryID, first.CheckpointID).
		Count(&staleAckCount).Error; err != nil {
		t.Fatalf("count stale replacement acks: %v", err)
	}
	if staleAckCount != 0 {
		t.Fatalf("stale replacement ack count = %d, want 0", staleAckCount)
	}

	var chunkCount int64
	if err := joiner.db.WithContext(ctx).Model(&LibraryCheckpointChunk{}).
		Where("library_id = ?", library.LibraryID).
		Count(&chunkCount).Error; err != nil {
		t.Fatalf("count replacement checkpoint chunks: %v", err)
	}
	if chunkCount != int64(second.ChunkCount) {
		t.Fatalf("replacement chunk count = %d, want %d", chunkCount, second.ChunkCount)
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

func TestStartSyncNowEmitsCheckpointInstallJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "sync-now-checkpoint-job")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)
	_, manifest := seedCheckpointBacklog(t, owner, library.LibraryID, ownerLocal.DeviceID, 1)

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

	waitForJobPhaseWithin(t, ctx, joiner, "sync:"+library.LibraryID, JobPhaseCompleted, 15*time.Second)
	installJob := waitForJobPhaseWithin(t, ctx, joiner, checkpointInstallJobID(library.LibraryID, manifest.CheckpointID), JobPhaseCompleted, 15*time.Second)
	if installJob.Kind != jobKindInstallCheckpoint || installJob.LibraryID != library.LibraryID {
		t.Fatalf("unexpected checkpoint install job: %+v", installJob)
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

func TestClassifyTransientCatchupError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantKind  string
		wantDelay time.Duration
	}{
		{
			name:      "protocol unavailable from peerstore readiness gate",
			err:       fmt.Errorf("open stream: peer does not advertise sync protocol /ben/desktop/sync/2.0.0 yet"),
			wantKind:  "sync.catchup.protocol_unavailable",
			wantDelay: syncCatchupProtocolRetryDelay,
		},
		{
			name:      "protocol negotiation failed remotely",
			err:       fmt.Errorf("open stream: failed to negotiate protocol: protocols not supported: [/ben/desktop/sync/2.0.0]"),
			wantKind:  "sync.catchup.protocol_unavailable",
			wantDelay: syncCatchupProtocolRetryDelay,
		},
		{
			name:      "remote stream reset",
			err:       fmt.Errorf("open stream: failed to negotiate protocol: stream reset (remote): code: 0x0"),
			wantKind:  "sync.catchup.transport_reset",
			wantDelay: syncCatchupStreamRetryDelay,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyTransientCatchupError(tc.err)
			if got == nil {
				t.Fatal("expected transient catch-up classification")
			}
			if got.kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", got.kind, tc.wantKind)
			}
			if got.retryAfter != tc.wantDelay {
				t.Fatalf("retryAfter = %v, want %v", got.retryAfter, tc.wantDelay)
			}
			if got.Error() != tc.err.Error() {
				t.Fatalf("error string = %q, want %q", got.Error(), tc.err.Error())
			}
		})
	}

	if got := classifyTransientCatchupError(fmt.Errorf("permanent failure")); got != nil {
		t.Fatalf("unexpected transient classification for permanent failure: %+v", got)
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
	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("owner repair: %v", err)
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
	if len(syncedRoots) != 0 {
		t.Fatalf("expected synced scan roots to remain local-only, got %+v", syncedRoots)
	}

	var syncedSource SourceFileModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", library.LibraryID, ownerLocal.DeviceID, true).
		Take(&syncedSource).Error; err != nil {
		t.Fatalf("load synced source file: %v", err)
	}
	if syncedSource.LocalPath != "" {
		t.Fatalf("expected synced source path to remain local-only, got %q", syncedSource.LocalPath)
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
	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("owner repair: %v", err)
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
	if len(syncedRoots) != 0 {
		t.Fatalf("expected checkpoint-installed scan roots to remain local-only, got %+v", syncedRoots)
	}

	var syncedSource SourceFileModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", library.LibraryID, ownerLocal.DeviceID, true).
		Take(&syncedSource).Error; err != nil {
		t.Fatalf("load checkpoint-installed source file: %v", err)
	}
	if syncedSource.LocalPath != "" {
		t.Fatalf("expected checkpoint-installed source path to remain local-only, got %q", syncedSource.LocalPath)
	}
}

func TestConnectPeerCanonicalizesOwnerAlbumReplacement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	oldA := filepath.Join(root, "01-old.flac")
	oldB := filepath.Join(root, "02-old.flac")
	for _, path := range []string{oldA, oldB} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write initial audio %s: %v", path, err)
		}
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(oldA): {
				Title:       "Track One",
				Album:       "Mutable Album",
				AlbumArtist: "Mutable Artist",
				Artists:     []string{"Mutable Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2024,
				DurationMS:  180000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
			filepath.Clean(oldB): {
				Title:       "Track Two",
				Album:       "Mutable Album",
				AlbumArtist: "Mutable Artist",
				Artists:     []string{"Mutable Artist"},
				TrackNo:     2,
				DiscNo:      1,
				Year:        2024,
				DurationMS:  181000,
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

	library, err := owner.CreateLibrary(ctx, "sync-mutable-album")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := owner.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set owner scan roots: %v", err)
	}
	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("initial owner repair: %v", err)
	}

	initialAlbums, err := owner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list initial owner albums: %v", err)
	}
	if len(initialAlbums.Items) != 1 {
		t.Fatalf("initial owner album count = %d, want 1", len(initialAlbums.Items))
	}
	oldAlbumID := initialAlbums.Items[0].LibraryAlbumID
	oldVariantAlbumID := initialAlbums.Items[0].PreferredVariantAlbumID
	if err := owner.pin.upsertPinRoot(ctx, ownerLocal, "album", oldVariantAlbumID, "desktop"); err != nil {
		t.Fatalf("upsert owner album pin root: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))
	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("initial connect peer: %v", err)
	}

	for _, path := range []string{oldA, oldB} {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove old audio file %s: %v", path, err)
		}
		delete(reader.tagsByPath, filepath.Clean(path))
	}

	newA := filepath.Join(root, "01-new.flac")
	newB := filepath.Join(root, "02-new.flac")
	newC := filepath.Join(root, "03-new.flac")
	for _, path := range []string{newA, newB, newC} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write new audio %s: %v", path, err)
		}
	}
	reader.tagsByPath[filepath.Clean(newA)] = Tags{
		Title:       "Track One",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	reader.tagsByPath[filepath.Clean(newB)] = Tags{
		Title:       "Track Two",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     2,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  181000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	reader.tagsByPath[filepath.Clean(newC)] = Tags{
		Title:       "Track Three",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     3,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  182000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}

	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("updated owner repair: %v", err)
	}
	updatedOwnerAlbums, err := owner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list updated owner albums: %v", err)
	}
	if len(updatedOwnerAlbums.Items) != 1 {
		t.Fatalf("updated owner album count = %d, want 1", len(updatedOwnerAlbums.Items))
	}
	newAlbumID := updatedOwnerAlbums.Items[0].LibraryAlbumID
	if newAlbumID != oldAlbumID {
		t.Fatalf("expected owner library album id to stay stable, got %q want %q", newAlbumID, oldAlbumID)
	}
	newVariantAlbumID := updatedOwnerAlbums.Items[0].PreferredVariantAlbumID
	if newVariantAlbumID == oldVariantAlbumID {
		t.Fatalf("expected owner preferred variant album id to change after update")
	}

	if err := joiner.SyncNow(ctx); err != nil {
		t.Fatalf("sync joiner after owner update: %v", err)
	}

	joinerAlbums, err := joiner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list joiner albums: %v", err)
	}
	if len(joinerAlbums.Items) != 1 {
		t.Fatalf("joiner album count = %d, want 1", len(joinerAlbums.Items))
	}
	if joinerAlbums.Items[0].LibraryAlbumID != newAlbumID {
		t.Fatalf("joiner library album id = %q, want %q", joinerAlbums.Items[0].LibraryAlbumID, newAlbumID)
	}
	if joinerAlbums.Items[0].PreferredVariantAlbumID != newVariantAlbumID {
		t.Fatalf("joiner preferred variant album id = %q, want %q", joinerAlbums.Items[0].PreferredVariantAlbumID, newVariantAlbumID)
	}
	if joinerAlbums.Items[0].VariantCount != 1 || joinerAlbums.Items[0].HasVariants {
		t.Fatalf("unexpected joiner album variants: %+v", joinerAlbums.Items[0])
	}

	joinerVariants, err := joiner.ListAlbumVariants(ctx, apitypes.AlbumVariantListRequest{
		AlbumID:     joinerAlbums.Items[0].AlbumID,
		PageRequest: apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		t.Fatalf("list joiner album variants: %v", err)
	}
	if len(joinerVariants.Items) != 1 {
		t.Fatalf("joiner variant count = %d, want 1", len(joinerVariants.Items))
	}

	var staleAlbumCount int64
	if err := joiner.db.WithContext(ctx).
		Model(&AlbumVariantModel{}).
		Where("library_id = ? AND album_variant_id = ?", library.LibraryID, oldVariantAlbumID).
		Count(&staleAlbumCount).Error; err != nil {
		t.Fatalf("count stale joiner album rows: %v", err)
	}
	if staleAlbumCount != 0 {
		t.Fatalf("stale joiner album row count = %d, want 0", staleAlbumCount)
	}
	assertAlbumPinCount(t, joiner, library.LibraryID, ownerLocal.DeviceID, newVariantAlbumID, 0)
}

func TestInstallCheckpointCanonicalizesOwnerAlbumReplacement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	oldA := filepath.Join(root, "01-old.flac")
	oldB := filepath.Join(root, "02-old.flac")
	for _, path := range []string{oldA, oldB} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write initial audio %s: %v", path, err)
		}
	}

	reader := staticTagReader{
		tagsByPath: map[string]Tags{
			filepath.Clean(oldA): {
				Title:       "Track One",
				Album:       "Mutable Album",
				AlbumArtist: "Mutable Artist",
				Artists:     []string{"Mutable Artist"},
				TrackNo:     1,
				DiscNo:      1,
				Year:        2024,
				DurationMS:  180000,
				Container:   "flac",
				Codec:       "flac",
				Bitrate:     1411200,
				SampleRate:  44100,
				Channels:    2,
				IsLossless:  true,
				QualityRank: 1443200,
			},
			filepath.Clean(oldB): {
				Title:       "Track Two",
				Album:       "Mutable Album",
				AlbumArtist: "Mutable Artist",
				Artists:     []string{"Mutable Artist"},
				TrackNo:     2,
				DiscNo:      1,
				Year:        2024,
				DurationMS:  181000,
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

	library, err := owner.CreateLibrary(ctx, "checkpoint-mutable-album")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := owner.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set owner scan roots: %v", err)
	}
	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("initial owner repair: %v", err)
	}

	initialAlbums, err := owner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list initial owner albums: %v", err)
	}
	if len(initialAlbums.Items) != 1 {
		t.Fatalf("initial owner album count = %d, want 1", len(initialAlbums.Items))
	}
	oldAlbumID := initialAlbums.Items[0].LibraryAlbumID
	oldVariantAlbumID := initialAlbums.Items[0].PreferredVariantAlbumID
	if err := owner.pin.upsertPinRoot(ctx, ownerLocal, "album", oldVariantAlbumID, "desktop"); err != nil {
		t.Fatalf("upsert owner album pin root: %v", err)
	}

	for _, path := range []string{oldA, oldB} {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove old audio file %s: %v", path, err)
		}
		delete(reader.tagsByPath, filepath.Clean(path))
	}
	newA := filepath.Join(root, "01-new.flac")
	newB := filepath.Join(root, "02-new.flac")
	newC := filepath.Join(root, "03-new.flac")
	for _, path := range []string{newA, newB, newC} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatalf("write new audio %s: %v", path, err)
		}
	}
	reader.tagsByPath[filepath.Clean(newA)] = Tags{
		Title:       "Track One",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     1,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  180000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	reader.tagsByPath[filepath.Clean(newB)] = Tags{
		Title:       "Track Two",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     2,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  181000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}
	reader.tagsByPath[filepath.Clean(newC)] = Tags{
		Title:       "Track Three",
		Album:       "Mutable Album",
		AlbumArtist: "Mutable Artist",
		Artists:     []string{"Mutable Artist"},
		TrackNo:     3,
		DiscNo:      1,
		Year:        2025,
		DurationMS:  182000,
		Container:   "flac",
		Codec:       "flac",
		Bitrate:     1411200,
		SampleRate:  44100,
		Channels:    2,
		IsLossless:  true,
		QualityRank: 1443200,
	}

	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("updated owner repair: %v", err)
	}
	updatedOwnerAlbums, err := owner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list updated owner albums: %v", err)
	}
	if len(updatedOwnerAlbums.Items) != 1 {
		t.Fatalf("updated owner album count = %d, want 1", len(updatedOwnerAlbums.Items))
	}
	newAlbumID := updatedOwnerAlbums.Items[0].LibraryAlbumID
	if newAlbumID != oldAlbumID {
		t.Fatalf("expected owner library album id to stay stable, got %q want %q", newAlbumID, oldAlbumID)
	}
	newVariantAlbumID := updatedOwnerAlbums.Items[0].PreferredVariantAlbumID
	if newVariantAlbumID == oldVariantAlbumID {
		t.Fatalf("expected owner preferred variant album id to change after update")
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
	if _, err := joiner.installCheckpointRecord(ctx, joinerLocal.DeviceID, record); err != nil {
		t.Fatalf("install checkpoint record: %v", err)
	}

	joinerAlbums, err := joiner.ListAlbums(ctx, apitypes.AlbumListRequest{})
	if err != nil {
		t.Fatalf("list joiner albums: %v", err)
	}
	if len(joinerAlbums.Items) != 1 {
		t.Fatalf("joiner album count = %d, want 1", len(joinerAlbums.Items))
	}
	if joinerAlbums.Items[0].LibraryAlbumID != newAlbumID {
		t.Fatalf("joiner library album id = %q, want %q", joinerAlbums.Items[0].LibraryAlbumID, newAlbumID)
	}
	if joinerAlbums.Items[0].PreferredVariantAlbumID != newVariantAlbumID {
		t.Fatalf("joiner preferred variant album id = %q, want %q", joinerAlbums.Items[0].PreferredVariantAlbumID, newVariantAlbumID)
	}
	if joinerAlbums.Items[0].VariantCount != 1 || joinerAlbums.Items[0].HasVariants {
		t.Fatalf("unexpected joiner album variants after checkpoint: %+v", joinerAlbums.Items[0])
	}

	var staleAlbumCount int64
	if err := joiner.db.WithContext(ctx).
		Model(&AlbumVariantModel{}).
		Where("library_id = ? AND album_variant_id = ?", library.LibraryID, oldVariantAlbumID).
		Count(&staleAlbumCount).Error; err != nil {
		t.Fatalf("count stale checkpoint joiner album rows: %v", err)
	}
	if staleAlbumCount != 0 {
		t.Fatalf("stale checkpoint joiner album row count = %d, want 0", staleAlbumCount)
	}
	assertAlbumPinCount(t, joiner, library.LibraryID, ownerLocal.DeviceID, newVariantAlbumID, 0)
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
	if _, err := owner.RepairLibrary(ctx); err != nil {
		t.Fatalf("owner repair: %v", err)
	}

	recordings, err := owner.ListRecordings(ctx, apitypes.RecordingListRequest{})
	if err != nil {
		t.Fatalf("list owner recordings: %v", err)
	}
	if len(recordings.Items) != 1 {
		t.Fatalf("recording count = %d, want 1", len(recordings.Items))
	}
	recordingID := recordings.Items[0].LibraryRecordingID
	recordingVariantID := recordings.Items[0].PreferredVariantRecordingID
	var recording TrackVariantModel
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND track_variant_id = ?", library.LibraryID, recordingVariantID).
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
	albumID := albums.Items[0].LibraryAlbumID
	albumVariantID := albums.Items[0].PreferredVariantAlbumID
	var album AlbumVariantModel
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND album_variant_id = ?", library.LibraryID, albumVariantID).
		Take(&album).Error; err != nil {
		t.Fatalf("load owner album row: %v", err)
	}

	if err := owner.SetPreferredRecordingVariant(ctx, recordingID, recordingVariantID); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if err := owner.SetPreferredAlbumVariant(ctx, albumID, albumVariantID); err != nil {
		t.Fatalf("set preferred album variant: %v", err)
	}

	var source SourceFileModel
	if err := owner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND is_present = ?", library.LibraryID, ownerLocal.DeviceID, true).
		Take(&source).Error; err != nil {
		t.Fatalf("load owner source file: %v", err)
	}

	encodingBlobID := testBlobID("7")
	artworkBlobID, err := owner.blobs.StoreArtworkBytes([]byte(strings.Repeat("x", 64)), ".webp")
	if err != nil {
		t.Fatalf("store owner artwork blob: %v", err)
	}
	now := time.Now().UTC()
	if err := owner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := owner.upsertOptimizedAssetTx(tx, ownerLocal, OptimizedAssetModel{
			OptimizedAssetID:  "enc-sync",
			SourceFileID:      source.SourceFileID,
			TrackVariantID:    recordingVariantID,
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
			ScopeID:         albumVariantID,
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

	if _, err := owner.StartPin(ctx, apitypes.PinIntentRequest{
		Profile: "desktop",
		Subject: apitypes.PinSubjectRef{Kind: apitypes.PinSubjectRecordingCluster, ID: recordingID},
	}); err != nil {
		t.Fatalf("start recording pin: %v", err)
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
	if trackPref.ChosenVariantID != recordingVariantID {
		t.Fatalf("track preference = %q, want %q", trackPref.ChosenVariantID, recordingVariantID)
	}

	var albumPref DeviceVariantPreference
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", library.LibraryID, ownerLocal.DeviceID, "album", album.AlbumClusterID).
		Take(&albumPref).Error; err != nil {
		t.Fatalf("load synced album preference: %v", err)
	}
	if albumPref.ChosenVariantID != albumVariantID {
		t.Fatalf("album preference = %q, want %q", albumPref.ChosenVariantID, albumVariantID)
	}

	var pinCount int64
	if err := joiner.db.WithContext(ctx).
		Model(&PinRoot{}).
		Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", library.LibraryID, ownerLocal.DeviceID, "recording", recordingID).
		Count(&pinCount).Error; err != nil {
		t.Fatalf("count remote device pins: %v", err)
	}
	if pinCount != 0 {
		t.Fatalf("expected remote device pin intent to remain local-only, count=%d", pinCount)
	}

	var encoding OptimizedAssetModel
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND optimized_asset_id = ?", library.LibraryID, "enc-sync").
		Take(&encoding).Error; err != nil {
		t.Fatalf("load synced optimized asset: %v", err)
	}
	if encoding.BlobID != encodingBlobID || encoding.TrackVariantID != recordingVariantID {
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
		Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", library.LibraryID, "album", albumVariantID, defaultArtworkVariant320).
		Take(&artwork).Error; err != nil {
		t.Fatalf("load synced artwork variant: %v", err)
	}
	if artwork.BlobID != artworkBlobID {
		t.Fatalf("artwork blob = %q, want %q", artwork.BlobID, artworkBlobID)
	}

	resolvedArtwork, err := joiner.ResolveAlbumArtwork(ctx, albumID, defaultArtworkVariant320)
	if err != nil {
		t.Fatalf("resolve synced album artwork: %v", err)
	}
	if !resolvedArtwork.Available {
		t.Fatalf("expected synced album artwork blob to be available locally")
	}

	cacheEntries, err := joiner.ListCacheEntries(ctx, apitypes.CacheEntryListRequest{})
	if err != nil {
		t.Fatalf("list joiner cache entries: %v", err)
	}
	var artworkCached bool
	for _, entry := range cacheEntries.Items {
		if entry.BlobID != artworkBlobID {
			continue
		}
		if entry.Kind != apitypes.CacheKindThumbnail {
			t.Fatalf("expected synced artwork cache entry to remain thumbnail, got %+v", entry)
		}
		if entry.Pinned {
			t.Fatalf("expected synced artwork cache entry to remain unpinned without local pin intent, got %+v", entry)
		}
		artworkCached = true
	}
	if !artworkCached {
		t.Fatalf("expected synced artwork cache entry to exist locally")
	}
}

func TestConnectPeerBackfillsMissingArtworkBlobWithoutNewOps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "artwork-blob-backfill")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)

	artworkBlobID, err := owner.blobs.StoreArtworkBytes([]byte(strings.Repeat("y", 64)), ".webp")
	if err != nil {
		t.Fatalf("store owner artwork blob: %v", err)
	}
	now := time.Now().UTC()
	if err := owner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return owner.upsertArtworkVariantTx(tx, ownerLocal, ArtworkVariant{
			ScopeType:       "album",
			ScopeID:         "album-backfill",
			Variant:         defaultArtworkVariant320,
			BlobID:          artworkBlobID,
			MIME:            "image/webp",
			FileExt:         ".webp",
			W:               320,
			H:               320,
			Bytes:           64,
			ChosenSource:    "embedded_front",
			ChosenSourceRef: "album-backfill.flac",
			UpdatedAt:       now,
		})
	}); err != nil {
		t.Fatalf("seed artwork variant: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("initial connect peer: %v", err)
	}

	ref := apitypes.ArtworkRef{
		BlobID:  artworkBlobID,
		MIME:    "image/webp",
		FileExt: ".webp",
		Variant: defaultArtworkVariant320,
	}
	resolved, err := joiner.ResolveArtworkRef(ctx, ref)
	if err != nil {
		t.Fatalf("resolve initially fetched artwork: %v", err)
	}
	if !resolved.Available {
		t.Fatalf("expected initially fetched artwork to be available")
	}
	if err := os.Remove(resolved.LocalPath); err != nil {
		t.Fatalf("remove joiner artwork blob: %v", err)
	}

	missing, err := joiner.ResolveArtworkRef(ctx, ref)
	if err != nil {
		t.Fatalf("resolve missing artwork: %v", err)
	}
	if missing.Available {
		t.Fatalf("expected removed artwork blob to stay unavailable until next sync")
	}

	if err := joiner.ConnectPeer(ctx, "memory://owner"); err != nil {
		t.Fatalf("second connect peer: %v", err)
	}

	refetched, err := joiner.ResolveArtworkRef(ctx, ref)
	if err != nil {
		t.Fatalf("resolve refetched artwork: %v", err)
	}
	if !refetched.Available {
		t.Fatalf("expected sync without new ops to backfill missing artwork blob")
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

func (t *memorySyncTransport) ResolvePeerByIdentity(ctx context.Context, local apitypes.LocalContext, peerID, deviceID string) (SyncPeer, error) {
	t.registry.mu.RLock()
	defer t.registry.mu.RUnlock()

	peerID = strings.TrimSpace(peerID)
	deviceID = strings.TrimSpace(deviceID)
	libraryID := strings.TrimSpace(local.LibraryID)
	for addr, app := range t.registry.apps {
		if addr == t.selfAddr {
			continue
		}
		remote, ok, err := app.ActiveLibrary(ctx)
		if err != nil || !ok {
			continue
		}
		if libraryID != "" && strings.TrimSpace(remote.LibraryID) != libraryID {
			continue
		}
		local, err := app.EnsureLocalContext(ctx)
		if err != nil {
			continue
		}
		local, err = app.ensureLocalPeerContext(ctx, local)
		if err != nil {
			continue
		}
		if peerID != "" && strings.TrimSpace(local.PeerID) == peerID {
			return &memorySyncPeer{addr: addr, app: app}, nil
		}
		if deviceID != "" && strings.TrimSpace(local.DeviceID) == deviceID {
			return &memorySyncPeer{addr: addr, app: app}, nil
		}
	}
	if peerID != "" {
		return nil, fmt.Errorf("peer %q not found", peerID)
	}
	if deviceID != "" {
		return nil, fmt.Errorf("device %q not found", deviceID)
	}
	return nil, fmt.Errorf("peer identity is required")
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

func (p *memorySyncPeer) NotifyLibraryChanged(ctx context.Context, req LibraryChangedRequest) (LibraryChangedResponse, error) {
	if p.app.transportService != nil {
		runtime := p.app.transportService.activeRuntimeForLibrary(req.LibraryID)
		if runtime != nil && runtime.ctx.Err() == nil && runtime.transport != nil {
			return p.app.transportService.handleLibraryChangedSignal(ctx, runtime, req.PeerID, req)
		}
	}
	local, err := p.app.EnsureLocalContext(ctx)
	if err != nil {
		return LibraryChangedResponse{}, err
	}
	local, err = p.app.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return LibraryChangedResponse{}, err
	}
	return p.app.buildLibraryChangedResponse(ctx, req.LibraryID, local.DeviceID, local.PeerID)
}

func (p *memorySyncPeer) FetchCheckpoint(ctx context.Context, req CheckpointFetchRequest) (CheckpointFetchResponse, error) {
	if _, err := p.app.verifyTransportPeerAuth(ctx, req.LibraryID, req.Auth.Cert.DeviceID, req.Auth.Cert.PeerID, req.Auth.Cert.PeerID, req.Auth); err != nil {
		return CheckpointFetchResponse{}, err
	}
	return p.app.buildCheckpointFetchResponse(ctx, req)
}

func (p *memorySyncPeer) FetchPlaybackAsset(ctx context.Context, req PlaybackAssetRequest) (PlaybackAssetResponse, error) {
	if _, err := p.app.verifyTransportPeerAuth(ctx, req.LibraryID, req.DeviceID, req.PeerID, req.PeerID, req.Auth); err != nil {
		return PlaybackAssetResponse{}, err
	}
	return p.app.buildPlaybackAssetResponse(ctx, req)
}

func (p *memorySyncPeer) FetchArtworkBlob(ctx context.Context, req ArtworkBlobRequest) (ArtworkBlobResponse, error) {
	if _, err := p.app.verifyTransportPeerAuth(ctx, req.LibraryID, req.DeviceID, req.PeerID, req.PeerID, req.Auth); err != nil {
		return ArtworkBlobResponse{}, err
	}
	return p.app.buildArtworkBlobResponse(ctx, req)
}

func (p *memorySyncPeer) RefreshMembership(ctx context.Context, req MembershipRefreshRequest) (MembershipRefreshResponse, error) {
	return p.app.buildMembershipRefreshResponse(ctx, req)
}

type managedMemorySyncRegistry struct {
	base *memorySyncRegistry
}

func newManagedMemorySyncRegistry() *managedMemorySyncRegistry {
	return &managedMemorySyncRegistry{base: newMemorySyncRegistry()}
}

func (r *managedMemorySyncRegistry) register(addr string, app *App) {
	if r == nil || r.base == nil {
		return
	}
	r.base.mu.Lock()
	r.base.apps[addr] = app
	r.base.mu.Unlock()
}

func (r *managedMemorySyncRegistry) factory(addr string, app *App) transportFactory {
	return func(ctx context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
		local, err := app.ensureLocalPeerContext(ctx, local)
		if err != nil {
			return nil, err
		}
		r.register(addr, app)
		return &memoryManagedSyncTransport{
			SyncTransport: r.base.transport(addr, app),
			addr:          addr,
			peerID:        local.PeerID,
		}, nil
	}
}

type memoryManagedSyncTransport struct {
	SyncTransport
	addr   string
	peerID string
	closed bool
}

func (t *memoryManagedSyncTransport) LocalPeerID() string {
	return strings.TrimSpace(t.peerID)
}

func (t *memoryManagedSyncTransport) ListenAddrs() []string {
	if t == nil || t.closed || strings.TrimSpace(t.addr) == "" {
		return nil
	}
	return []string{t.addr}
}

func (t *memoryManagedSyncTransport) Close() error {
	t.closed = true
	return nil
}

func openPlaylistTestAppAtPath(t *testing.T, root string) *App {
	t.Helper()

	app, err := Open(context.Background(), Config{
		DBPath:          filepath.Join(root, "library.db"),
		BlobRoot:        filepath.Join(root, "blobs"),
		IdentityKeyPath: filepath.Join(root, "identity.key"),
	})
	if err != nil {
		t.Fatalf("open app at path: %v", err)
	}
	return app
}

func appendCheckpointTailOps(t *testing.T, app *App, libraryID, deviceID, playlistID string, tailOps int) {
	t.Helper()

	if tailOps <= 0 {
		return
	}
	ctx := context.Background()

	var clock DeviceClock
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Take(&clock).Error; err != nil {
		t.Fatalf("load checkpoint tail clock: %v", err)
	}

	base := time.Now().UTC()
	entries := make([]OplogEntry, 0, tailOps)
	lastSeq := clock.LastSeqSeen
	for offset := 1; offset <= tailOps; offset++ {
		seq := lastSeq + int64(offset)
		name := fmt.Sprintf("Queue %d", seq)
		ts := base.Add(time.Duration(offset) * time.Millisecond)
		entries = append(entries, OplogEntry{
			LibraryID:   libraryID,
			OpID:        fmt.Sprintf("%s:%d", deviceID, seq),
			DeviceID:    deviceID,
			Seq:         seq,
			TSNS:        ts.UnixNano(),
			EntityType:  "playlist",
			EntityID:    playlistID,
			OpKind:      "upsert",
			PayloadJSON: fmt.Sprintf(`{"playlistId":%q,"name":%q,"kind":"normal","createdBy":%q}`, playlistID, name, deviceID),
		})
	}
	for start := 0; start < len(entries); start += 250 {
		end := start + 250
		if end > len(entries) {
			end = len(entries)
		}
		if err := app.db.WithContext(ctx).Create(entries[start:end]).Error; err != nil {
			t.Fatalf("seed checkpoint tail ops: %v", err)
		}
	}

	finalSeq := lastSeq + int64(tailOps)
	if err := app.db.WithContext(ctx).Model(&Playlist{}).
		Where("library_id = ? AND playlist_id = ?", libraryID, playlistID).
		Updates(map[string]any{
			"name":       fmt.Sprintf("Queue %d", finalSeq),
			"updated_at": base.Add(time.Duration(tailOps) * time.Millisecond),
		}).Error; err != nil {
		t.Fatalf("update checkpoint tail playlist state: %v", err)
	}
	if err := app.db.WithContext(ctx).Model(&DeviceClock{}).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Update("last_seq_seen", finalSeq).Error; err != nil {
		t.Fatalf("update checkpoint tail device clock: %v", err)
	}
}

func waitForPlaylistName(t *testing.T, ctx context.Context, app *App, libraryID, playlistID, wantName string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var playlist Playlist
		err := app.db.WithContext(ctx).
			Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", libraryID, playlistID).
			Take(&playlist).Error
		if err == nil && playlist.Name == wantName {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	var playlist Playlist
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", libraryID, playlistID).
		Take(&playlist).Error; err != nil {
		t.Fatalf("load playlist after wait: %v", err)
	}
	t.Fatalf("playlist %q name = %q, want %q", playlistID, playlist.Name, wantName)
}

func waitForCheckpointAck(t *testing.T, ctx context.Context, app *App, libraryID, deviceID string) DeviceCheckpointAck {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var ack DeviceCheckpointAck
		err := app.db.WithContext(ctx).
			Where("library_id = ? AND device_id = ?", libraryID, deviceID).
			Take(&ack).Error
		if err == nil {
			return ack
		}
		time.Sleep(25 * time.Millisecond)
	}

	var ack DeviceCheckpointAck
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Take(&ack).Error; err != nil {
		t.Fatalf("load checkpoint ack after wait: %v", err)
	}
	return ack
}

func waitForJobPhaseWithin(t *testing.T, ctx context.Context, app *App, jobID string, want JobPhase, timeout time.Duration) JobSnapshot {
	t.Helper()

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, ok, err := app.GetJob(ctx, jobID)
		if err == nil && ok && job.Phase == want {
			return job
		}
		time.Sleep(25 * time.Millisecond)
	}

	job, ok, err := app.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job after wait: %v", err)
	}
	if !ok {
		t.Fatalf("job %q not found after wait", jobID)
	}
	t.Fatalf("job %q phase = %q, want %q", jobID, job.Phase, want)
	return JobSnapshot{}
}
