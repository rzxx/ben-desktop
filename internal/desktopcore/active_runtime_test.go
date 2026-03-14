package desktopcore

import (
	"context"
	"sync"
	"strings"
	"testing"

	apitypes "ben/desktop/api/types"
)

func TestStartActiveLibraryJobFailsQueuedJobWhenLibraryIsNotActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)

	first, err := app.CreateLibrary(ctx, "inactive-job-library")
	if err != nil {
		t.Fatalf("create first library: %v", err)
	}
	if _, err := app.CreateLibrary(ctx, "active-job-library"); err != nil {
		t.Fatalf("create second library: %v", err)
	}

	jobID := "job-startup-failure"
	snapshot, err := app.startActiveLibraryJob(
		ctx,
		jobID,
		"test-startup",
		first.LibraryID,
		"queued startup test",
		"job canceled because the library is no longer active",
		func(context.Context) {
			t.Fatal("job callback should not run for an inactive library")
		},
	)
	if err == nil {
		t.Fatal("expected inactive library job startup to fail")
	}
	if snapshot.Phase != JobPhaseFailed {
		t.Fatalf("startup failure snapshot = %+v, want failed job", snapshot)
	}
	if !strings.Contains(snapshot.Message, "no longer active") {
		t.Fatalf("startup failure message = %q, want active-library cancellation message", snapshot.Message)
	}
	if snapshot.Error != "" {
		t.Fatalf("startup failure error = %q, want empty error text for active-library cancellation", snapshot.Error)
	}

	job, ok, err := app.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get failed startup job: %v", err)
	}
	if !ok {
		t.Fatalf("expected failed startup job %q to be recorded", jobID)
	}
	if job.Phase != JobPhaseFailed {
		t.Fatalf("persisted startup failure job = %+v, want failed phase", job)
	}
}

func TestActiveLibraryRuntimeOwnsTransportAndWatcherLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	app.transportService.backgroundInterval = 0

	var (
		mu         sync.Mutex
		transports []*fakeManagedTransport
	)
	app.transportService.factory = func(_ context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
		transport := &fakeManagedTransport{
			libraryID: local.LibraryID,
			deviceID:  local.DeviceID,
			peerID:    "peer-" + local.LibraryID,
		}
		mu.Lock()
		transports = append(transports, transport)
		mu.Unlock()
		return transport, nil
	}

	if _, err := app.CreateLibrary(ctx, "runtime-a"); err != nil {
		t.Fatalf("create first library: %v", err)
	}
	root := t.TempDir()
	if err := app.SetScanRoots(ctx, []string{root}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}
	if err := app.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("sync runtime services: %v", err)
	}

	app.runtimeMu.Lock()
	firstRuntime := app.activeRuntime
	var firstWatcher *activeScanWatcher
	if firstRuntime != nil {
		firstWatcher = firstRuntime.scanWatcher
	}
	app.runtimeMu.Unlock()
	if firstRuntime == nil {
		t.Fatal("expected active runtime for first library")
	}
	if firstRuntime.transportRuntime == nil {
		t.Fatal("expected transport runtime on active library runtime")
	}
	if firstWatcher == nil {
		t.Fatal("expected scan watcher on active library runtime")
	}

	mu.Lock()
	if len(transports) != 1 {
		t.Fatalf("transport instances = %d, want 1", len(transports))
	}
	firstTransport := transports[0]
	mu.Unlock()

	if _, err := app.CreateLibrary(ctx, "runtime-b"); err != nil {
		t.Fatalf("create second library: %v", err)
	}

	app.runtimeMu.Lock()
	secondRuntime := app.activeRuntime
	app.runtimeMu.Unlock()
	if secondRuntime == nil {
		t.Fatal("expected active runtime after switching libraries")
	}
	if secondRuntime == firstRuntime {
		t.Fatal("expected active runtime replacement on library switch")
	}
	if secondRuntime.transportRuntime == nil {
		t.Fatal("expected transport runtime on replacement active runtime")
	}
	if secondRuntime.scanWatcher != nil {
		t.Fatal("expected replacement runtime to start without watcher until roots are configured")
	}

	if firstTransport.closed != 1 {
		t.Fatalf("first transport close count = %d, want 1", firstTransport.closed)
	}
	select {
	case <-firstWatcher.done:
	default:
		t.Fatal("expected first watcher to be stopped with replaced active runtime")
	}
}
