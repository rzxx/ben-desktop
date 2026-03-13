package desktopcore

import (
	"context"
	"sync"
	"testing"
	"time"

	apitypes "ben/core/api/types"
)

func TestTransportServiceStartsAndStopsWithActiveLibrarySelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)

	var (
		mu        sync.Mutex
		instances []*fakeManagedTransport
	)
	app.transportService.factory = func(_ context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
		transport := &fakeManagedTransport{
			libraryID: local.LibraryID,
			deviceID:  local.DeviceID,
			peerID:    "peer-" + local.LibraryID,
		}
		mu.Lock()
		instances = append(instances, transport)
		mu.Unlock()
		return transport, nil
	}

	first, err := app.CreateLibrary(ctx, "transport-a")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	second, err := app.CreateLibrary(ctx, "transport-b")
	if err != nil {
		t.Fatalf("create second library: %v", err)
	}

	mu.Lock()
	if len(instances) != 2 {
		t.Fatalf("transport instances = %d, want 2", len(instances))
	}
	firstTransport := instances[0]
	secondTransport := instances[1]
	mu.Unlock()

	if firstTransport.closed != 1 {
		t.Fatalf("first transport close count = %d, want 1 after library switch", firstTransport.closed)
	}
	if secondTransport.closed != 0 {
		t.Fatalf("second transport close count = %d, want 0 while active", secondTransport.closed)
	}

	if _, err := app.SelectLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("reselect first library: %v", err)
	}

	mu.Lock()
	if len(instances) != 3 {
		t.Fatalf("transport instances after reselect = %d, want 3", len(instances))
	}
	thirdTransport := instances[2]
	mu.Unlock()

	if secondTransport.closed != 1 {
		t.Fatalf("second transport close count = %d, want 1 after reselect", secondTransport.closed)
	}
	if thirdTransport.closed != 0 {
		t.Fatalf("third transport close count = %d, want 0 while active", thirdTransport.closed)
	}

	if err := app.LeaveLibrary(ctx, first.LibraryID); err != nil {
		t.Fatalf("leave active library: %v", err)
	}
	if thirdTransport.closed != 1 {
		t.Fatalf("third transport close count = %d, want 1 after leaving active library", thirdTransport.closed)
	}

	if got := app.transportService.ListenAddrs(); len(got) != 0 {
		t.Fatalf("listen addrs = %v, want none after stop", got)
	}
	if second.LibraryID == "" {
		t.Fatal("expected second library id to be populated")
	}
}

func TestLibp2pTransportConnectPeerAppliesIncrementalSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "libp2p-sync")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}

	seedPlaylistRecording(t, owner, library.LibraryID, "rec-transport", "Transport")
	playlist, err := owner.CreatePlaylist(ctx, "Queue", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := owner.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-transport",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	addresses := owner.transportService.ListenAddrs()
	if len(addresses) == 0 {
		t.Fatal("owner transport did not expose any listen addresses")
	}
	if err := joiner.ConnectPeer(ctx, addresses[0]); err != nil {
		t.Fatalf("connect peer: %v", err)
	}

	var synced Playlist
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", library.LibraryID, playlist.PlaylistID).
		Take(&synced).Error; err != nil {
		t.Fatalf("load synced playlist: %v", err)
	}
	if synced.Name != playlist.Name {
		t.Fatalf("synced playlist name = %q, want %q", synced.Name, playlist.Name)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var peerState PeerSyncState
		err := joiner.db.WithContext(ctx).
			Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
			Take(&peerState).Error
		if err == nil && peerState.LastApplied > 0 && peerState.LastError == "" {
			if peerState.PeerID == "" {
				t.Fatalf("peer sync state missing remote peer id: %+v", peerState)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for peer sync state to reflect libp2p sync")
}

type fakeManagedTransport struct {
	libraryID string
	deviceID  string
	peerID    string
	closed    int
}

func (f *fakeManagedTransport) ListPeers(context.Context, apitypes.LocalContext) ([]SyncPeer, error) {
	return nil, nil
}

func (f *fakeManagedTransport) ResolvePeer(context.Context, apitypes.LocalContext, string) (SyncPeer, error) {
	return nil, nil
}

func (f *fakeManagedTransport) LocalPeerID() string {
	return f.peerID
}

func (f *fakeManagedTransport) ListenAddrs() []string {
	if f.closed > 0 {
		return nil
	}
	return []string{"memory://" + f.libraryID}
}

func (f *fakeManagedTransport) Close() error {
	f.closed++
	return nil
}
