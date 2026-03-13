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

func TestNetworkStatusReflectsRuntimeSyncState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)

	library, err := app.CreateLibrary(ctx, "network-state")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	app.transportService.Stop()
	runtime := &activeTransportRuntime{
		libraryID: library.LibraryID,
		deviceID:  local.DeviceID,
		transport: &fakeManagedTransport{
			libraryID: library.LibraryID,
			deviceID:  local.DeviceID,
			peerID:    "peer-" + library.LibraryID,
		},
	}
	app.transportService.mu.Lock()
	app.transportService.current = runtime
	app.transportService.mu.Unlock()

	app.transportService.beginRuntimeSync(runtime, apitypes.NetworkSyncReasonStartup)
	app.transportService.noteRuntimeSyncProgress(library.LibraryID, "peer-remote", apitypes.NetworkSyncActivityCheckpointInstall, 42, 7)

	status := app.NetworkStatus()
	if status.Mode != apitypes.NetworkSyncModeCatchup || status.Reason != apitypes.NetworkSyncReasonStartup {
		t.Fatalf("unexpected sync mode/reason: %+v", status.NetworkSyncState)
	}
	if status.Activity != apitypes.NetworkSyncActivityCheckpointInstall || status.ActivePeerID != "peer-remote" {
		t.Fatalf("unexpected runtime sync activity: %+v", status.NetworkSyncState)
	}
	if status.BacklogEstimate != 42 || status.LastBatchApplied != 7 || status.StartedAt == nil {
		t.Fatalf("unexpected runtime sync progress: %+v", status.NetworkSyncState)
	}

	app.transportService.finishRuntimeSync(runtime, nil)
	status = app.NetworkStatus()
	if status.Mode != apitypes.NetworkSyncModeIdle || status.CompletedAt == nil || status.LastSyncError != "" {
		t.Fatalf("unexpected completed runtime status: %+v", status.NetworkSyncState)
	}
}

func TestLibp2pTransportConnectPeerAppliesIncrementalSync(t *testing.T) {
	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)
	owner.transportService.backgroundInterval = 0
	joiner.transportService.backgroundInterval = 0

	var (
		ownerMu     sync.Mutex
		joinerMu    sync.Mutex
		ownerWraps  []*countingManagedTransport
		joinerWraps []*countingManagedTransport
	)
	owner.transportService.factory = func(ctx context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
		base, err := owner.newLibp2pSyncTransport(ctx, local)
		if err != nil {
			return nil, err
		}
		wrapped := &countingManagedTransport{managedSyncTransport: base}
		ownerMu.Lock()
		ownerWraps = append(ownerWraps, wrapped)
		ownerMu.Unlock()
		return wrapped, nil
	}
	joiner.transportService.factory = func(ctx context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
		base, err := joiner.newLibp2pSyncTransport(ctx, local)
		if err != nil {
			return nil, err
		}
		wrapped := &countingManagedTransport{managedSyncTransport: base}
		joinerMu.Lock()
		joinerWraps = append(joinerWraps, wrapped)
		joinerMu.Unlock()
		return wrapped, nil
	}

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
	second, err := owner.CreateLibrary(ctx, "libp2p-switch")
	if err != nil {
		t.Fatalf("create second owner library: %v", err)
	}
	seedSharedLibraryForSync(t, owner, joiner, second)
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("switch joiner runtime services: %v", err)
	}
	seedPlaylistRecording(t, owner, second.LibraryID, "rec-switch", "Switch")
	switchPlaylist, err := owner.CreatePlaylist(ctx, "Queue Switch", "")
	if err != nil {
		t.Fatalf("create switch playlist: %v", err)
	}
	if _, err := owner.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  switchPlaylist.PlaylistID,
		RecordingID: "rec-switch",
	}); err != nil {
		t.Fatalf("add switch playlist item: %v", err)
	}

	ownerMu.Lock()
	if len(ownerWraps) < 2 {
		t.Fatalf("owner transport instances = %d, want at least 2", len(ownerWraps))
	}
	ownerFirst := ownerWraps[0]
	ownerSecond := ownerWraps[1]
	ownerMu.Unlock()
	joinerMu.Lock()
	if len(joinerWraps) < 2 {
		t.Fatalf("joiner transport instances = %d, want at least 2", len(joinerWraps))
	}
	joinerFirst := joinerWraps[0]
	joinerSecond := joinerWraps[1]
	joinerMu.Unlock()
	if ownerFirst.closed != 1 {
		t.Fatalf("owner first transport close count = %d, want 1 after switching libraries", ownerFirst.closed)
	}
	if joinerFirst.closed != 1 {
		t.Fatalf("joiner first transport close count = %d, want 1 after switching libraries", joinerFirst.closed)
	}

	addresses = owner.transportService.ListenAddrs()
	if len(addresses) == 0 {
		t.Fatal("owner switched transport did not expose any listen addresses")
	}
	if err := joiner.ConnectPeer(ctx, addresses[0]); err != nil {
		t.Fatalf("connect peer after switch: %v", err)
	}
	var switched Playlist
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", second.LibraryID, switchPlaylist.PlaylistID).
		Take(&switched).Error; err != nil {
		t.Fatalf("load switched playlist: %v", err)
	}
	if switched.Name != switchPlaylist.Name {
		t.Fatalf("switched playlist name = %q, want %q", switched.Name, switchPlaylist.Name)
	}
	if ownerSecond.closed != 0 {
		t.Fatalf("owner second transport close count = %d, want 0 while second library is active", ownerSecond.closed)
	}
	if joinerSecond.closed != 0 {
		t.Fatalf("joiner second transport close count = %d, want 0 while second library is active", joinerSecond.closed)
	}
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

type countingManagedTransport struct {
	managedSyncTransport
	closed int
}

func (c *countingManagedTransport) Close() error {
	c.closed++
	return c.managedSyncTransport.Close()
}
