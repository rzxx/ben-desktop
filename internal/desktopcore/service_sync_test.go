package desktopcore

import (
	"context"
	"fmt"
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
		if err := tx.Create(&Device{
			DeviceID:   joinerDevice.DeviceID,
			Name:       joinerDevice.Name,
			PeerID:     joinerPeerID,
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		return tx.Create(&Membership{
			LibraryID:        library.LibraryID,
			DeviceID:         joinerDevice.DeviceID,
			Role:             roleMember,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error
	}); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}

	if err := joiner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&Library{
			LibraryID: library.LibraryID,
			Name:      library.Name,
			CreatedAt: now,
		}).Error; err != nil {
			return err
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
	return p.app.buildSyncResponse(ctx, req)
}

func (p *memorySyncPeer) FetchCheckpoint(ctx context.Context, libraryID, checkpointID string) (checkpointTransferRecord, error) {
	record, ok, err := p.app.loadCheckpointTransferRecord(ctx, libraryID, checkpointID, false)
	if err != nil {
		return checkpointTransferRecord{}, err
	}
	if !ok {
		return checkpointTransferRecord{}, fmt.Errorf("checkpoint %q not found", checkpointID)
	}
	return record, nil
}
