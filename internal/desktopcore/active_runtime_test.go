package desktopcore

import (
	"context"
	"strings"
	"testing"
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
