package desktopcore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"gorm.io/gorm"
)

func TestLocalOplogMutationBroadcastsOnlyAfterCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-oplog-commit"}, nil
	}

	var peerUpdates atomic.Int32
	app.transportService.peerUpdateBroadcastHook = func(_ *activeTransportRuntime) {
		peerUpdates.Add(1)
	}

	if _, err := app.CreateLibrary(ctx, "oplog-commit-broadcast"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	if err := app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		_, err := app.appendLocalOplogTx(tx, local, "playlist", "playlist-commit", "upsert", map[string]any{"name": "Commit"})
		return err
	}); err != nil {
		t.Fatalf("append committed oplog entry: %v", err)
	}
	waitForAtomicInt32(t, &peerUpdates, 1)

	err = app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if _, err := app.appendLocalOplogTx(tx, local, "playlist", "playlist-rollback", "upsert", map[string]any{"name": "Rollback"}); err != nil {
			return err
		}
		return errors.New("rollback mutation")
	})
	if err == nil || err.Error() != "rollback mutation" {
		t.Fatalf("rollback transaction err = %v, want rollback mutation", err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := peerUpdates.Load(); got != 1 {
		t.Fatalf("peer update broadcasts after rollback = %d, want 1", got)
	}
}

func TestLocalOplogMutationCoalescesBroadcastsWithinATransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-oplog-retry"}, nil
	}

	var peerUpdates atomic.Int32
	app.transportService.peerUpdateBroadcastHook = func(_ *activeTransportRuntime) {
		peerUpdates.Add(1)
	}

	if _, err := app.CreateLibrary(ctx, "oplog-commit-retry"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if _, err := app.appendLocalOplogTx(tx, local, "playlist", "playlist-retry-a", "upsert", map[string]any{"name": "One"}); err != nil {
			return err
		}
		if _, err := app.appendLocalOplogTx(tx, local, "playlist", "playlist-retry-b", "upsert", map[string]any{"name": "Two"}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("append coalesced oplog entries: %v", err)
	}
	waitForAtomicInt32(t, &peerUpdates, 1)
	if got := peerUpdates.Load(); got != 1 {
		t.Fatalf("peer update broadcasts for coalesced transaction = %d, want 1", got)
	}
}

func TestLocalOplogMutationCommitHookWaitsForTransactionReturn(t *testing.T) {
	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-oplog-long-commit"}, nil
	}

	var peerUpdates atomic.Int32
	app.transportService.peerUpdateBroadcastHook = func(_ *activeTransportRuntime) {
		peerUpdates.Add(1)
	}

	if _, err := app.CreateLibrary(ctx, "oplog-long-commit"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- app.storage.Transaction(ctx, func(tx *gorm.DB) error {
			if _, err := app.appendLocalOplogTx(tx, local, "playlist", "playlist-long-commit", "upsert", map[string]any{"name": "Long Commit"}); err != nil {
				return err
			}
			time.Sleep(150 * time.Millisecond)
			return nil
		})
	}()

	time.Sleep(80 * time.Millisecond)
	if got := peerUpdates.Load(); got != 0 {
		t.Fatalf("peer update broadcasts before transaction return = %d, want 0", got)
	}
	if err := <-done; err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	waitForAtomicInt32(t, &peerUpdates, 1)
}

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
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runtime.ctx = runtimeCtx
	runtime.cancel = cancel
	app.runtimeMu.Lock()
	app.activeRuntime = &activeLibraryRuntime{
		libraryID:        library.LibraryID,
		deviceID:         local.DeviceID,
		ctx:              runtimeCtx,
		cancel:           cancel,
		transportRuntime: runtime,
	}
	app.runtimeMu.Unlock()

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

func TestUpsertDevicePresenceEmitsAvailabilityInvalidationWhenPeerComesOnline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)

	library, err := app.CreateLibrary(ctx, "presence-availability-events")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	lastSeen := time.Now().UTC().Add(-3 * availabilityOnlineWindow)
	remoteDeviceID := seedRemoteLibraryMember(
		t,
		app,
		library.LibraryID,
		"dev-presence-remote",
		lastSeen,
	)

	var (
		eventsMu sync.Mutex
		events   []apitypes.CatalogChangeEvent
	)
	stopListening := app.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})
	defer stopListening()

	if err := app.transportService.upsertDevicePresence(
		ctx,
		remoteDeviceID,
		"peer-presence-remote",
		"Remote device",
	); err != nil {
		t.Fatalf("upsert device presence: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, event := range events {
		if event.Kind != apitypes.CatalogChangeInvalidateAvailability {
			continue
		}
		if !event.InvalidateAll {
			t.Fatalf("expected presence availability event to invalidate all, got %+v", event)
		}
		return
	}
	t.Fatalf("expected presence update to emit availability invalidation event, got %+v", events)
}

func TestMarkDevicePresenceOfflineEmitsAvailabilityInvalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)

	library, err := app.CreateLibrary(ctx, "presence-offline-events")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	remoteDeviceID := seedRemoteLibraryMember(
		t,
		app,
		library.LibraryID,
		"dev-presence-offline-remote",
		time.Now().UTC(),
	)
	if err := app.transportService.upsertDevicePresence(
		ctx,
		remoteDeviceID,
		"peer-presence-offline-remote",
		"Remote device",
	); err != nil {
		t.Fatalf("upsert remote presence: %v", err)
	}

	var (
		eventsMu sync.Mutex
		events   []apitypes.CatalogChangeEvent
	)
	stopListening := app.SubscribeCatalogChanges(func(event apitypes.CatalogChangeEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})
	defer stopListening()

	if err := app.transportService.markDevicePresenceOffline(ctx, library.LibraryID, "peer-presence-offline-remote"); err != nil {
		t.Fatalf("mark device presence offline: %v", err)
	}

	var device Device
	if err := app.db.WithContext(ctx).Where("device_id = ?", remoteDeviceID).Take(&device).Error; err != nil {
		t.Fatalf("load remote device: %v", err)
	}
	if device.LastSeenAt == nil || device.LastSeenAt.UTC().After(time.Now().UTC().Add(-availabilityOnlineWindow)) {
		t.Fatalf("expected remote device to be marked offline, got %+v", device.LastSeenAt)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, event := range events {
		if event.Kind != apitypes.CatalogChangeInvalidateAvailability {
			continue
		}
		if !event.InvalidateAll {
			t.Fatalf("expected offline presence event to invalidate all, got %+v", event)
		}
		return
	}
	t.Fatalf("expected offline presence update to emit availability invalidation event, got %+v", events)
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

func TestLibp2pTransportAutoAppliesRemoteUpdateWithoutReconnect(t *testing.T) {
	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)
	owner.transportService.backgroundInterval = 0
	joiner.transportService.backgroundInterval = 0

	library, err := owner.CreateLibrary(ctx, "libp2p-remote-update")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}

	addresses := owner.transportService.ListenAddrs()
	if len(addresses) == 0 {
		t.Fatal("owner transport did not expose any listen addresses")
	}
	if err := joiner.ConnectPeer(ctx, addresses[0]); err != nil {
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
		t.Fatalf("unexpected peer sync state after remote update: %+v", peerState)
	}
	if peerState.PeerID != ownerLocal.PeerID {
		t.Fatalf("peer sync state peer id after remote update = %q, want %q", peerState.PeerID, ownerLocal.PeerID)
	}
}

func TestTransportServiceDoesNotPollWhenPeriodicFallbackDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.backgroundInterval = 0
	app.transportService.eventSyncDebounce = 10 * time.Millisecond

	transport := &testPeerListTransport{peerID: "peer-no-poll"}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "transport-no-poll"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	waitForPeerListCalls(t, transport, 1)
	time.Sleep(80 * time.Millisecond)
	if got := transport.ListPeerCalls(); got != 1 {
		t.Fatalf("list peers calls after idle wait = %d, want 1", got)
	}
}

func TestTransportServicePeriodicFallbackPollsWithoutEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.backgroundInterval = 20 * time.Millisecond

	transport := &testPeerListTransport{peerID: "peer-periodic-poll"}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "transport-periodic-poll"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	waitForPeerListCalls(t, transport, 2)
}

func TestLocalOplogMutationBroadcastsPeerUpdateWithoutLocalCatchup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond

	signalPeer := &testSignalPeer{peerID: "peer-remote-update"}
	transport := &testPeerListTransport{
		peerID: "peer-local-update",
		peers:  []SyncPeer{signalPeer},
	}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "transport-local-update"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForPeerListCalls(t, transport, 1)

	if _, err := app.CreatePlaylist(ctx, "Queued playlist", string(apitypes.PlaylistKindNormal)); err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	waitForPeerListCalls(t, transport, 2)
	waitForSignalPeerCalls(t, signalPeer, 1)
	if got := signalPeer.NotifyCalls(); got != 1 {
		t.Fatalf("notify library changed calls = %d, want 1", got)
	}
}

func TestLocalOplogMutationSchedulesCheckpointMaintenanceWithoutCatchup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond

	transport := &testPeerListTransport{peerID: "peer-checkpoint-update"}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	var catchupRuns atomic.Int32
	var maintenanceRuns atomic.Int32
	app.transportService.catchupRunHook = func(_ *activeTransportRuntime, _ apitypes.NetworkSyncReason) {
		catchupRuns.Add(1)
	}
	app.transportService.checkpointMaintenanceRunHook = func(_ *activeTransportRuntime) {
		maintenanceRuns.Add(1)
	}

	if _, err := app.CreateLibrary(ctx, "transport-checkpoint-update"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForAtomicInt32(t, &catchupRuns, 1)
	waitForAtomicInt32(t, &maintenanceRuns, 1)
	catchupRuns.Store(0)
	maintenanceRuns.Store(0)

	if _, err := app.CreatePlaylist(ctx, "Checkpoint playlist", string(apitypes.PlaylistKindNormal)); err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	waitForAtomicInt32(t, &maintenanceRuns, 1)
	if got := catchupRuns.Load(); got != 0 {
		t.Fatalf("catch-up runs after local mutation = %d, want 0", got)
	}
}

func TestCatchupFailureStillSchedulesCheckpointMaintenance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.backgroundInterval = 0
	app.transportService.eventSyncDebounce = 10 * time.Millisecond

	transport := &testPeerListTransport{
		peerID:          "peer-maintenance-after-failure",
		failOnListCalls: map[int]error{1: errors.New("startup catch-up failed")},
	}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	var maintenanceRuns atomic.Int32
	app.transportService.checkpointMaintenanceRunHook = func(_ *activeTransportRuntime) {
		maintenanceRuns.Add(1)
	}

	if _, err := app.CreateLibrary(ctx, "transport-maintenance-after-failure"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	waitForAtomicInt32(t, &maintenanceRuns, 1)
}

func TestLocalOplogMutationRetriesPeerBroadcastAfterListPeersFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond
	app.transportService.peerRetryDelay = 20 * time.Millisecond

	signalPeer := &testSignalPeer{peerID: "peer-list-retry"}
	transport := &testPeerListTransport{
		peerID:          "peer-local-list-retry",
		peers:           []SyncPeer{signalPeer},
		failOnListCalls: map[int]error{2: errors.New("transient peer list failure")},
	}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "transport-list-retry"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForPeerListCalls(t, transport, 1)

	if _, err := app.CreatePlaylist(ctx, "Retry after list failure", string(apitypes.PlaylistKindNormal)); err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	waitForPeerListCalls(t, transport, 3)
	waitForSignalPeerCalls(t, signalPeer, 1)
	if got := signalPeer.NotifyCalls(); got != 1 {
		t.Fatalf("notify calls after list retry = %d, want 1", got)
	}
}

func TestLocalOplogMutationRetriesPeerBroadcastAfterNotifyFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond
	app.transportService.peerRetryDelay = 20 * time.Millisecond

	signalPeer := &testSignalPeer{
		peerID:            "peer-notify-retry",
		failOnNotifyCalls: map[int]error{1: errors.New("transient notify failure")},
	}
	transport := &testPeerListTransport{
		peerID: "peer-local-notify-retry",
		peers:  []SyncPeer{signalPeer},
	}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "transport-notify-retry"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForPeerListCalls(t, transport, 1)

	if _, err := app.CreatePlaylist(ctx, "Retry after notify failure", string(apitypes.PlaylistKindNormal)); err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	waitForSignalPeerCalls(t, signalPeer, 2)
	if got := signalPeer.NotifyCalls(); got != 2 {
		t.Fatalf("notify calls after retry = %d, want 2", got)
	}
}

func TestLocalOplogMutationRetriesOnlyFailingPeerWithRetryCap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond
	app.transportService.peerRetryDelay = 20 * time.Millisecond
	app.transportService.peerRetryMaxDelay = 20 * time.Millisecond
	app.transportService.peerRetryMaxCount = 3

	healthyPeer := &testSignalPeer{peerID: "peer-notify-healthy"}
	brokenPeer := &testSignalPeer{
		peerID: "peer-notify-broken",
		failOnNotifyCalls: map[int]error{
			1: errors.New("permanent notify failure"),
			2: errors.New("permanent notify failure"),
			3: errors.New("permanent notify failure"),
			4: errors.New("permanent notify failure"),
			5: errors.New("permanent notify failure"),
		},
	}
	transport := &testPeerListTransport{
		peerID: "peer-local-notify-cap",
		peers:  []SyncPeer{healthyPeer, brokenPeer},
	}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "transport-notify-cap"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForPeerListCalls(t, transport, 1)
	transport.listPeerCalls.Store(0)

	if _, err := app.CreatePlaylist(ctx, "Retry cap after notify failure", string(apitypes.PlaylistKindNormal)); err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	waitForSignalPeerCalls(t, brokenPeer, 3)
	time.Sleep(120 * time.Millisecond)
	if got := brokenPeer.NotifyCalls(); got != 3 {
		t.Fatalf("broken peer notify calls after retry cap = %d, want 3", got)
	}
	if got := healthyPeer.NotifyCalls(); got != 1 {
		t.Fatalf("healthy peer notify calls after targeted retries = %d, want 1", got)
	}
	if got := transport.ListPeerCalls(); got != 1 {
		t.Fatalf("list peers calls across initial notify plus targeted retries = %d, want 1", got)
	}
}

func TestPeerUpdateRetriesDoNotWaitForSlowestPeerBackoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.peerRetryDelay = 20 * time.Millisecond
	app.transportService.peerRetryMaxDelay = 200 * time.Millisecond
	app.transportService.peerRetryMaxCount = 6

	fastPeer := &testSignalPeer{peerID: "peer-retry-fast"}
	slowPeer := &testSignalPeer{peerID: "peer-retry-slow"}
	transport := &testPeerListTransport{
		peerID: "peer-local-retry-split",
		peers:  []SyncPeer{fastPeer, slowPeer},
	}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	library, err := app.CreateLibrary(ctx, "transport-retry-split")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	runtime := app.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil {
		t.Fatal("expected active transport runtime")
	}

	app.transportService.scheduleRuntimePeerUpdateRetries(runtime, map[string]transportPeerUpdateRetryState{
		transportPeerUpdateKey(fastPeer): {peerID: fastPeer.PeerID(), attempt: 1},
		transportPeerUpdateKey(slowPeer): {peerID: slowPeer.PeerID(), attempt: 4},
	})

	waitForSignalPeerCallsWithin(t, fastPeer, 1, 100*time.Millisecond)
	if got := slowPeer.NotifyCalls(); got != 0 {
		t.Fatalf("slow peer notify calls before its backoff elapsed = %d, want 0", got)
	}
	waitForSignalPeerCallsWithin(t, slowPeer, 1, 350*time.Millisecond)
}

func TestRuntimeCatchupSignalSchedulesSingleRerun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)

	transport := &testPeerListTransport{peerID: "peer-rerun"}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var runs atomic.Int32
	app.transportService.catchupRunHook = func(_ *activeTransportRuntime, _ apitypes.NetworkSyncReason) {
		runs.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}

	library, err := app.CreateLibrary(ctx, "transport-rerun")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	runtime := app.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil {
		t.Fatal("expected active transport runtime")
	}

	waitForChannelSignal(t, started)
	app.transportService.scheduleRuntimeCatchup(runtime, apitypes.NetworkSyncReasonUpdate, 0)
	app.transportService.scheduleRuntimeCatchup(runtime, apitypes.NetworkSyncReasonUpdate, 0)
	close(release)

	waitForAtomicInt32(t, &runs, 2)
	if got := runs.Load(); got != 2 {
		t.Fatalf("catch-up runs = %d, want 2 (initial + one rerun)", got)
	}
}

func TestRuntimeCatchupPreservesQueuedPeerRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-merge-targeted"}, nil
	}

	library, err := app.CreateLibrary(ctx, "transport-merge-targeted")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	app.transportService.Stop()
	runtime := installTransportRuntimeForTest(t, app, library.LibraryID, local.DeviceID, &fakeManagedTransport{
		libraryID: library.LibraryID,
		deviceID:  local.DeviceID,
		peerID:    "peer-merge-targeted",
	})

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var (
		peersMu sync.Mutex
		peers   []string
		runs    atomic.Int32
	)
	app.transportService.catchupPeerRunHook = func(_ *activeTransportRuntime, peer SyncPeer, reason apitypes.NetworkSyncReason) {
		if reason != apitypes.NetworkSyncReasonConnect {
			t.Fatalf("queued peer catch-up reason = %q, want %q", reason, apitypes.NetworkSyncReasonConnect)
		}
		runs.Add(1)
		if peer != nil {
			peersMu.Lock()
			peers = append(peers, peer.PeerID())
			peersMu.Unlock()
		}
		if runs.Load() == 1 {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
	}

	app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, &testSignalPeer{peerID: "peer-one"}, 0)
	waitForChannelSignal(t, started)
	app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, &testSignalPeer{peerID: "peer-two"}, 0)
	app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, &testSignalPeer{peerID: "peer-three"}, 0)
	close(release)

	waitForAtomicInt32(t, &runs, 3)
	peersMu.Lock()
	gotPeers := append([]string(nil), peers...)
	peersMu.Unlock()
	if len(gotPeers) != 3 {
		t.Fatalf("queued peer catch-up peers = %v, want three runs", gotPeers)
	}
	assertPeerRuns(t, gotPeers, "peer-one", "peer-two", "peer-three")
}

func TestRuntimeStopClearsPendingScheduledTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 20 * time.Millisecond

	transport := &testPeerListTransport{peerID: "peer-stop"}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	var catchupRuns atomic.Int32
	var peerUpdateRuns atomic.Int32
	var maintenanceRuns atomic.Int32
	app.transportService.catchupRunHook = func(_ *activeTransportRuntime, _ apitypes.NetworkSyncReason) {
		catchupRuns.Add(1)
	}
	app.transportService.peerUpdateBroadcastHook = func(_ *activeTransportRuntime) {
		peerUpdateRuns.Add(1)
	}
	app.transportService.checkpointMaintenanceRunHook = func(_ *activeTransportRuntime) {
		maintenanceRuns.Add(1)
	}

	library, err := app.CreateLibrary(ctx, "transport-stop-clear")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForAtomicInt32(t, &catchupRuns, 1)
	waitForAtomicInt32(t, &maintenanceRuns, 1)
	catchupRuns.Store(0)
	peerUpdateRuns.Store(0)
	maintenanceRuns.Store(0)

	runtime := app.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil {
		t.Fatal("expected active transport runtime")
	}
	app.transportService.scheduleRuntimeCatchup(runtime, apitypes.NetworkSyncReasonUpdate, 40*time.Millisecond)
	app.transportService.scheduleRuntimePeerUpdateBroadcast(runtime, 40*time.Millisecond)
	app.transportService.scheduleRuntimeCheckpointMaintenance(runtime, 40*time.Millisecond)
	app.transportService.stopRuntime(runtime)

	time.Sleep(100 * time.Millisecond)
	if got := catchupRuns.Load(); got != 0 {
		t.Fatalf("catch-up runs after stop = %d, want 0", got)
	}
	if got := peerUpdateRuns.Load(); got != 0 {
		t.Fatalf("peer update broadcasts after stop = %d, want 0", got)
	}
	if got := maintenanceRuns.Load(); got != 0 {
		t.Fatalf("checkpoint maintenance runs after stop = %d, want 0", got)
	}
}

func TestStartupCatchupCompletesBeforeCheckpointMaintenance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.eventSyncDebounce = 10 * time.Millisecond

	transport := &testPeerListTransport{peerID: "peer-maintenance"}
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var catchupRuns atomic.Int32
	var maintenanceRuns atomic.Int32
	app.transportService.catchupRunHook = func(_ *activeTransportRuntime, _ apitypes.NetworkSyncReason) {
		catchupRuns.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}
	app.transportService.checkpointMaintenanceRunHook = func(_ *activeTransportRuntime) {
		maintenanceRuns.Add(1)
	}

	if _, err := app.CreateLibrary(ctx, "transport-maintenance"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	waitForChannelSignal(t, started)
	time.Sleep(50 * time.Millisecond)
	if got := maintenanceRuns.Load(); got != 0 {
		t.Fatalf("checkpoint maintenance runs before startup catch-up completes = %d, want 0", got)
	}

	close(release)
	waitForAtomicInt32(t, &catchupRuns, 1)
	waitForAtomicInt32(t, &maintenanceRuns, 1)
}

func TestRuntimeCatchupPreservesDebouncedGlobalUpdateDuringImmediatePeerCatchup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-merge-global"}, nil
	}

	library, err := app.CreateLibrary(ctx, "transport-merge-global")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	app.transportService.Stop()
	runtime := installTransportRuntimeForTest(t, app, library.LibraryID, local.DeviceID, &fakeManagedTransport{
		libraryID: library.LibraryID,
		deviceID:  local.DeviceID,
		peerID:    "peer-merge-global",
	})

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var (
		runsMu sync.Mutex
		runs   []recordedCatchupRun
	)
	app.transportService.catchupPeerRunHook = func(_ *activeTransportRuntime, peer SyncPeer, reason apitypes.NetworkSyncReason) {
		run := recordedCatchupRun{reason: reason}
		if peer != nil {
			run.peerID = peer.PeerID()
		}
		runsMu.Lock()
		runs = append(runs, run)
		runsMu.Unlock()
		if run.peerID != "" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
	}

	app.transportService.scheduleRuntimeCatchup(runtime, apitypes.NetworkSyncReasonUpdate, 60*time.Millisecond)
	app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, &testSignalPeer{peerID: "peer-connect"}, 0)
	waitForChannelSignal(t, started)
	time.Sleep(80 * time.Millisecond)
	close(release)

	waitForCatchupRuns(t, &runsMu, &runs, 2)
	runsMu.Lock()
	gotRuns := append([]recordedCatchupRun(nil), runs...)
	runsMu.Unlock()
	if len(gotRuns) != 2 {
		t.Fatalf("catch-up runs = %+v, want targeted connect followed by global update", gotRuns)
	}
	if gotRuns[0].reason != apitypes.NetworkSyncReasonConnect || gotRuns[0].peerID != "peer-connect" {
		t.Fatalf("first catch-up run = %+v, want targeted connect", gotRuns[0])
	}
	if gotRuns[1].reason != apitypes.NetworkSyncReasonUpdate || gotRuns[1].peerID != "" {
		t.Fatalf("second catch-up run = %+v, want global update", gotRuns[1])
	}
}

func TestRuntimeCatchupPreservesQueuedPeerWorkWhileGlobalUpdateIsDebounced(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{peerID: "peer-merge-queued-global"}, nil
	}

	library, err := app.CreateLibrary(ctx, "transport-merge-queued-global")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	app.transportService.Stop()
	runtime := installTransportRuntimeForTest(t, app, library.LibraryID, local.DeviceID, &fakeManagedTransport{
		libraryID: library.LibraryID,
		deviceID:  local.DeviceID,
		peerID:    "peer-merge-queued-global",
	})

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var (
		runsMu sync.Mutex
		runs   []recordedCatchupRun
	)
	app.transportService.catchupPeerRunHook = func(_ *activeTransportRuntime, peer SyncPeer, reason apitypes.NetworkSyncReason) {
		run := recordedCatchupRun{reason: reason}
		if peer != nil {
			run.peerID = peer.PeerID()
		}
		runsMu.Lock()
		runs = append(runs, run)
		runsMu.Unlock()
		if run.peerID == "peer-one" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
	}

	app.transportService.scheduleRuntimeCatchup(runtime, apitypes.NetworkSyncReasonUpdate, 60*time.Millisecond)
	app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, &testSignalPeer{peerID: "peer-one"}, 0)
	waitForChannelSignal(t, started)
	app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, &testSignalPeer{peerID: "peer-two"}, 0)
	time.Sleep(80 * time.Millisecond)
	close(release)

	waitForCatchupRuns(t, &runsMu, &runs, 3)
	runsMu.Lock()
	gotRuns := append([]recordedCatchupRun(nil), runs...)
	runsMu.Unlock()
	if len(gotRuns) != 3 {
		t.Fatalf("catch-up runs = %+v, want peer-one, peer-two, then global update", gotRuns)
	}
	if gotRuns[0].peerID != "peer-one" || gotRuns[0].reason != apitypes.NetworkSyncReasonConnect {
		t.Fatalf("first catch-up run = %+v, want peer-one connect", gotRuns[0])
	}
	if gotRuns[1].peerID != "peer-two" || gotRuns[1].reason != apitypes.NetworkSyncReasonConnect {
		t.Fatalf("second catch-up run = %+v, want peer-two connect", gotRuns[1])
	}
	if gotRuns[2].peerID != "" || gotRuns[2].reason != apitypes.NetworkSyncReasonUpdate {
		t.Fatalf("third catch-up run = %+v, want global update", gotRuns[2])
	}
}

func TestHandleLibraryChangedSignalRejectsAuthOrLibraryMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "library-changed-auth")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	joiner.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return &fakeManagedTransport{
			libraryID: library.LibraryID,
			deviceID:  "joiner-device",
			peerID:    "peer-joiner-signal",
		}, nil
	}
	var catchupRuns atomic.Int32
	joiner.transportService.catchupRunHook = func(_ *activeTransportRuntime, _ apitypes.NetworkSyncReason) {
		catchupRuns.Add(1)
	}
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}
	waitForAtomicInt32(t, &catchupRuns, 1)
	catchupRuns.Store(0)

	runtime := joiner.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil {
		t.Fatal("expected active joiner runtime")
	}

	localAuth, err := owner.ensureLocalTransportMembershipAuth(ctx, ownerLocal, ownerLocal.PeerID)
	if err != nil {
		t.Fatalf("build owner auth: %v", err)
	}

	if _, err := joiner.transportService.handleLibraryChangedSignal(ctx, runtime, ownerLocal.PeerID, LibraryChangedRequest{
		LibraryID: "wrong-library",
		DeviceID:  ownerLocal.DeviceID,
		PeerID:    ownerLocal.PeerID,
		Auth:      localAuth,
	}); err == nil {
		t.Fatal("expected library mismatch error")
	}
	if _, err := joiner.transportService.handleLibraryChangedSignal(ctx, runtime, ownerLocal.PeerID, LibraryChangedRequest{
		LibraryID: library.LibraryID,
		DeviceID:  ownerLocal.DeviceID,
		PeerID:    ownerLocal.PeerID,
		Auth: func() transportPeerAuth {
			bad := localAuth
			bad.Cert.Sig = []byte("tampered")
			return bad
		}(),
	}); err == nil {
		t.Fatal("expected auth verification error")
	}
	time.Sleep(30 * time.Millisecond)
	if got := catchupRuns.Load(); got != 0 {
		t.Fatalf("catch-up runs after rejected signal = %d, want 0", got)
	}
}

func TestHandleLibraryChangedSignalSchedulesTargetedCatchupWithoutListingPeers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "library-changed-targeted")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)
	transport := &testPeerListTransport{
		peerID:       joinerLocal.PeerID,
		resolvedPeer: &testSignalPeer{peerID: ownerLocal.PeerID},
	}
	joiner.transportService.factory = func(context.Context, apitypes.LocalContext) (managedSyncTransport, error) {
		return transport, nil
	}

	var (
		runsMu sync.Mutex
		runs   []recordedCatchupRun
	)
	joiner.transportService.catchupPeerRunHook = func(_ *activeTransportRuntime, peer SyncPeer, reason apitypes.NetworkSyncReason) {
		run := recordedCatchupRun{reason: reason}
		if peer != nil {
			run.peerID = peer.PeerID()
		}
		runsMu.Lock()
		runs = append(runs, run)
		runsMu.Unlock()
	}
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}
	waitForCatchupRuns(t, &runsMu, &runs, 1)
	runsMu.Lock()
	runs = nil
	runsMu.Unlock()
	transport.listPeerCalls.Store(0)

	runtime := joiner.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil {
		t.Fatal("expected active joiner runtime")
	}

	localAuth, err := owner.ensureLocalTransportMembershipAuth(ctx, ownerLocal, ownerLocal.PeerID)
	if err != nil {
		t.Fatalf("build owner auth: %v", err)
	}
	if _, err := joiner.transportService.handleLibraryChangedSignal(ctx, runtime, ownerLocal.PeerID, LibraryChangedRequest{
		LibraryID: library.LibraryID,
		DeviceID:  ownerLocal.DeviceID,
		PeerID:    ownerLocal.PeerID,
		Auth:      localAuth,
	}); err != nil {
		t.Fatalf("handle library changed signal: %v", err)
	}

	waitForCatchupRuns(t, &runsMu, &runs, 1)
	runsMu.Lock()
	gotRuns := append([]recordedCatchupRun(nil), runs...)
	runsMu.Unlock()
	if len(gotRuns) != 1 {
		t.Fatalf("catch-up runs = %+v, want one targeted update catch-up", gotRuns)
	}
	if gotRuns[0].reason != apitypes.NetworkSyncReasonUpdate || gotRuns[0].peerID != ownerLocal.PeerID {
		t.Fatalf("targeted signal catch-up run = %+v, want update for %q", gotRuns[0], ownerLocal.PeerID)
	}
	if got := transport.ListPeerCalls(); got != 0 {
		t.Fatalf("list peers calls after targeted signal catch-up = %d, want 0", got)
	}
}

func TestHandleLibraryChangedSignalHandsOffToReplacementRuntime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)
	joiner.transportService.eventSyncDebounce = 10 * time.Millisecond

	library, err := owner.CreateLibrary(ctx, "library-changed-runtime-handoff")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	ownerLocal, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)
	if _, _, ok, err := joiner.syncActiveLibraryRuntimeState(ctx); err != nil {
		t.Fatalf("sync joiner active runtime state: %v", err)
	} else if !ok {
		t.Fatal("expected active joiner library runtime")
	}

	oldRuntime := installTransportRuntimeForTest(t, joiner, library.LibraryID, joinerLocal.DeviceID, &fakeManagedTransport{
		libraryID: library.LibraryID,
		deviceID:  joinerLocal.DeviceID,
		peerID:    joinerLocal.PeerID,
	})
	newTransport := &testPeerListTransport{
		peerID:       joinerLocal.PeerID,
		resolvedPeer: &testSignalPeer{peerID: ownerLocal.PeerID},
	}
	newRuntime := installTransportRuntimeForTest(t, joiner, library.LibraryID, joinerLocal.DeviceID, newTransport)

	var (
		runsMu sync.Mutex
		runs   []struct {
			runtime *activeTransportRuntime
			reason  apitypes.NetworkSyncReason
			peerID  string
		}
	)
	joiner.transportService.catchupPeerRunHook = func(runtime *activeTransportRuntime, peer SyncPeer, reason apitypes.NetworkSyncReason) {
		run := struct {
			runtime *activeTransportRuntime
			reason  apitypes.NetworkSyncReason
			peerID  string
		}{
			runtime: runtime,
			reason:  reason,
		}
		if peer != nil {
			run.peerID = peer.PeerID()
		}
		runsMu.Lock()
		runs = append(runs, run)
		runsMu.Unlock()
	}

	localAuth, err := owner.ensureLocalTransportMembershipAuth(ctx, ownerLocal, ownerLocal.PeerID)
	if err != nil {
		t.Fatalf("build owner auth: %v", err)
	}
	if _, err := joiner.transportService.handleLibraryChangedSignal(ctx, oldRuntime, ownerLocal.PeerID, LibraryChangedRequest{
		LibraryID: library.LibraryID,
		DeviceID:  ownerLocal.DeviceID,
		PeerID:    ownerLocal.PeerID,
		Auth:      localAuth,
	}); err != nil {
		t.Fatalf("handle library changed signal through replaced runtime: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runsMu.Lock()
		count := len(runs)
		var got struct {
			runtime *activeTransportRuntime
			reason  apitypes.NetworkSyncReason
			peerID  string
		}
		if count > 0 {
			got = runs[0]
		}
		runsMu.Unlock()
		if count == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if got.runtime != newRuntime {
			t.Fatalf("catch-up runtime = %p, want replacement runtime %p", got.runtime, newRuntime)
		}
		if got.reason != apitypes.NetworkSyncReasonUpdate || got.peerID != ownerLocal.PeerID {
			t.Fatalf("replacement runtime catch-up = %+v, want update for %q", got, ownerLocal.PeerID)
		}
		if gotCalls := newTransport.ListPeerCalls(); gotCalls != 0 {
			t.Fatalf("replacement runtime list peers calls = %d, want 0", gotCalls)
		}
		return
	}
	runsMu.Lock()
	defer runsMu.Unlock()
	t.Fatalf("timed out waiting for replacement runtime catch-up, got %+v", runs)
}

func TestLibp2pPeerConnectedSchedulesRuntimeCatchup(t *testing.T) {
	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "libp2p-connect-scheduler")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}

	var reasonsMu sync.Mutex
	var reasons []apitypes.NetworkSyncReason
	joiner.transportService.catchupRunHook = func(_ *activeTransportRuntime, reason apitypes.NetworkSyncReason) {
		reasonsMu.Lock()
		reasons = append(reasons, reason)
		reasonsMu.Unlock()
	}

	runtime := joiner.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil || runtime.transport == nil {
		t.Fatal("expected active joiner transport runtime")
	}
	transport, ok := runtime.transport.(*libp2pSyncTransport)
	if !ok {
		t.Fatal("expected libp2p transport")
	}
	peerID, err := peer.Decode(ownerLocal.PeerID)
	if err != nil {
		t.Fatalf("decode owner peer id: %v", err)
	}

	transport.handlePeerConnected(peerID)
	waitForSyncReason(t, &reasonsMu, &reasons, apitypes.NetworkSyncReasonConnect)
}

func TestLibp2pPeerConnectedCatchupDoesNotListAllPeers(t *testing.T) {
	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "libp2p-connect-targeted")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	ownerLocal, _ := seedSharedLibraryForSync(t, owner, joiner, library)
	if err := joiner.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start joiner runtime services: %v", err)
	}

	var (
		targetMu     sync.Mutex
		targetPeer   string
		targetReason apitypes.NetworkSyncReason
	)
	joiner.transportService.catchupPeerRunHook = func(_ *activeTransportRuntime, peer SyncPeer, reason apitypes.NetworkSyncReason) {
		targetMu.Lock()
		defer targetMu.Unlock()
		targetReason = reason
		if peer != nil {
			targetPeer = peer.PeerID()
		}
	}

	runtime := joiner.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil || runtime.transport == nil {
		t.Fatal("expected active joiner transport runtime")
	}
	base, ok := runtime.transport.(*libp2pSyncTransport)
	if !ok {
		t.Fatal("expected libp2p transport")
	}
	peerID, err := peer.Decode(ownerLocal.PeerID)
	if err != nil {
		t.Fatalf("decode owner peer id: %v", err)
	}

	base.handlePeerConnected(peerID)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		targetMu.Lock()
		gotPeer := targetPeer
		gotReason := targetReason
		targetMu.Unlock()
		if gotPeer != "" || gotReason != "" {
			if gotReason != apitypes.NetworkSyncReasonConnect {
				t.Fatalf("connect catch-up reason = %q, want %q", gotReason, apitypes.NetworkSyncReasonConnect)
			}
			if gotPeer != ownerLocal.PeerID {
				t.Fatalf("connect catch-up peer = %q, want %q", gotPeer, ownerLocal.PeerID)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for targeted connect catch-up hook")
}

type fakeManagedTransport struct {
	libraryID string
	deviceID  string
	peerID    string
	closed    int
}

type recordedCatchupRun struct {
	reason apitypes.NetworkSyncReason
	peerID string
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

type countingManagedPeerListTransport struct {
	managedSyncTransport
	listPeerCalls atomic.Int32
}

func (c *countingManagedPeerListTransport) ListPeers(ctx context.Context, local apitypes.LocalContext) ([]SyncPeer, error) {
	c.listPeerCalls.Add(1)
	return c.managedSyncTransport.ListPeers(ctx, local)
}

func (c *countingManagedPeerListTransport) ListPeerCalls() int {
	return int(c.listPeerCalls.Load())
}

type peerListCallCounter interface {
	ListPeerCalls() int
}

type testPeerListTransport struct {
	peerID          string
	peers           []SyncPeer
	resolvedPeer    SyncPeer
	failOnListCalls map[int]error
	listPeerCalls   atomic.Int32
}

func (t *testPeerListTransport) ListPeers(context.Context, apitypes.LocalContext) ([]SyncPeer, error) {
	call := int(t.listPeerCalls.Add(1))
	if err := t.failOnListCalls[call]; err != nil {
		return nil, err
	}
	return append([]SyncPeer(nil), t.peers...), nil
}

func (t *testPeerListTransport) ResolvePeer(context.Context, apitypes.LocalContext, string) (SyncPeer, error) {
	return nil, nil
}

func (t *testPeerListTransport) ResolvePeerByIdentity(_ context.Context, _ apitypes.LocalContext, peerID, deviceID string) (SyncPeer, error) {
	if t.resolvedPeer != nil {
		return t.resolvedPeer, nil
	}
	for _, candidate := range t.peers {
		if candidate == nil {
			continue
		}
		if peerID != "" && candidate.PeerID() == peerID {
			return candidate, nil
		}
		if deviceID != "" && candidate.DeviceID() == deviceID {
			return candidate, nil
		}
	}
	return nil, errors.New("peer not found")
}

func (t *testPeerListTransport) LocalPeerID() string {
	return t.peerID
}

func (t *testPeerListTransport) ListenAddrs() []string {
	return []string{"memory://" + t.peerID}
}

func (t *testPeerListTransport) Close() error {
	return nil
}

func (t *testPeerListTransport) ListPeerCalls() int {
	return int(t.listPeerCalls.Load())
}

func waitForPeerListCalls(t *testing.T, transport peerListCallCounter, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if transport.ListPeerCalls() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("list peers calls = %d, want at least %d", transport.ListPeerCalls(), want)
}

func waitForManagedPeerListCalls(t *testing.T, transport *countingManagedPeerListTransport, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if transport.ListPeerCalls() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("managed list peers calls = %d, want at least %d", transport.ListPeerCalls(), want)
}

type signalNotifyCounter interface {
	NotifyCalls() int
}

func waitForSignalPeerCalls(t *testing.T, peer signalNotifyCounter, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if peer.NotifyCalls() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("notify library changed calls = %d, want at least %d", peer.NotifyCalls(), want)
}

func waitForSignalPeerCallsWithin(t *testing.T, peer signalNotifyCounter, want int, within time.Duration) {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if peer.NotifyCalls() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("notify library changed calls = %d, want at least %d within %v", peer.NotifyCalls(), want, within)
}

type testSignalPeer struct {
	peerID            string
	failOnNotifyCalls map[int]error
	notifyCalls       atomic.Int32
}

func (p *testSignalPeer) Address() string { return "memory://" + p.peerID }

func (p *testSignalPeer) DeviceID() string { return "device-" + p.peerID }

func (p *testSignalPeer) PeerID() string { return p.peerID }

func (p *testSignalPeer) Sync(context.Context, SyncRequest) (SyncResponse, error) {
	return SyncResponse{}, nil
}

func (p *testSignalPeer) NotifyLibraryChanged(context.Context, LibraryChangedRequest) (LibraryChangedResponse, error) {
	call := int(p.notifyCalls.Add(1))
	if err := p.failOnNotifyCalls[call]; err != nil {
		return LibraryChangedResponse{}, err
	}
	return LibraryChangedResponse{
		LibraryID: "test-library",
		DeviceID:  p.DeviceID(),
		PeerID:    p.PeerID(),
	}, nil
}

func (p *testSignalPeer) FetchCheckpoint(context.Context, CheckpointFetchRequest) (CheckpointFetchResponse, error) {
	return CheckpointFetchResponse{}, nil
}

func (p *testSignalPeer) FetchPlaybackAsset(context.Context, PlaybackAssetRequest) (PlaybackAssetResponse, error) {
	return PlaybackAssetResponse{}, nil
}

func (p *testSignalPeer) FetchArtworkBlob(context.Context, ArtworkBlobRequest) (ArtworkBlobResponse, error) {
	return ArtworkBlobResponse{}, nil
}

func (p *testSignalPeer) RefreshMembership(context.Context, MembershipRefreshRequest) (MembershipRefreshResponse, error) {
	return MembershipRefreshResponse{}, nil
}

func (p *testSignalPeer) NotifyCalls() int {
	return int(p.notifyCalls.Load())
}

func waitForAtomicInt32(t *testing.T, value *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if value.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("atomic count = %d, want at least %d", value.Load(), want)
}

func waitForChannelSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func waitForCatchupRuns(t *testing.T, mu *sync.Mutex, runs *[]recordedCatchupRun, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(*runs)
		mu.Unlock()
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	current := append([]recordedCatchupRun(nil), (*runs)...)
	mu.Unlock()
	t.Fatalf("catch-up runs = %+v, want at least %d", current, want)
}

func assertPeerRuns(t *testing.T, peers []string, want ...string) {
	t.Helper()

	got := make(map[string]int, len(peers))
	for _, peerID := range peers {
		got[peerID]++
	}
	for _, peerID := range want {
		if got[peerID] == 0 {
			t.Fatalf("peer catch-up runs = %v, want %q to be preserved", peers, peerID)
		}
		got[peerID]--
	}
	for peerID, remaining := range got {
		if remaining > 0 {
			t.Fatalf("peer catch-up runs = %v, unexpected extra peer %q", peers, peerID)
		}
	}
}

func installTransportRuntimeForTest(t *testing.T, app *App, libraryID, deviceID string, transport managedSyncTransport) *activeTransportRuntime {
	t.Helper()

	app.runtimeMu.Lock()
	defer app.runtimeMu.Unlock()
	if app.activeRuntime == nil {
		t.Fatal("expected active library runtime")
	}
	runtimeCtx, cancel := context.WithCancel(app.activeRuntime.ctx)
	runtime := &activeTransportRuntime{
		libraryID: libraryID,
		deviceID:  deviceID,
		transport: transport,
		ctx:       runtimeCtx,
		cancel:    cancel,
	}
	app.activeRuntime.transportRuntime = runtime
	t.Cleanup(func() {
		cancel()
	})
	return runtime
}

func waitForSyncReason(t *testing.T, mu *sync.Mutex, reasons *[]apitypes.NetworkSyncReason, want apitypes.NetworkSyncReason) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		current := append([]apitypes.NetworkSyncReason(nil), (*reasons)...)
		mu.Unlock()
		for _, reason := range current {
			if reason == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	current := append([]apitypes.NetworkSyncReason(nil), (*reasons)...)
	mu.Unlock()
	t.Fatalf("sync reasons = %v, want %q", current, want)
}
