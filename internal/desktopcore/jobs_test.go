package desktopcore

import (
	"testing"
	"time"
)

func TestJobsServiceListFiltersAndSortsByRecentUpdate(t *testing.T) {
	t.Parallel()

	svc := NewJobsService()

	svc.Put(JobSnapshot{
		JobID:     "job-old",
		Kind:      "scan",
		LibraryID: "lib-1",
		Phase:     JobPhaseRunning,
	})
	time.Sleep(10 * time.Millisecond)
	svc.Put(JobSnapshot{
		JobID:     "job-new",
		Kind:      "sync",
		LibraryID: "lib-1",
		Phase:     JobPhaseCompleted,
	})
	time.Sleep(10 * time.Millisecond)
	svc.Put(JobSnapshot{
		JobID:     "job-other",
		Kind:      "scan",
		LibraryID: "lib-2",
		Phase:     JobPhaseQueued,
	})

	all := svc.List("")
	if len(all) != 3 {
		t.Fatalf("list all len = %d, want 3", len(all))
	}
	if all[0].JobID != "job-other" || all[1].JobID != "job-new" || all[2].JobID != "job-old" {
		t.Fatalf("list all order = %v", []string{all[0].JobID, all[1].JobID, all[2].JobID})
	}

	filtered := svc.List("lib-1")
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].JobID != "job-new" || filtered[1].JobID != "job-old" {
		t.Fatalf("filtered order = %v", []string{filtered[0].JobID, filtered[1].JobID})
	}
}

func TestJobsServiceGetReportsMissingJobs(t *testing.T) {
	t.Parallel()

	svc := NewJobsService()
	if _, ok := svc.Get("missing"); ok {
		t.Fatalf("expected missing job lookup to report ok=false")
	}
}

func TestJobsServiceBeginReusesActiveJob(t *testing.T) {
	t.Parallel()

	svc := NewJobsService()
	first, started := svc.Begin("job-1", "scan", "lib-1", "queued")
	if !started {
		t.Fatalf("expected first begin to start a job")
	}
	if first.Phase != JobPhaseQueued {
		t.Fatalf("first phase = %q, want queued", first.Phase)
	}

	second, started := svc.Begin("job-1", "scan", "lib-1", "queued again")
	if started {
		t.Fatalf("expected active job begin to reuse existing snapshot")
	}
	if second.JobID != first.JobID || second.CreatedAt != first.CreatedAt {
		t.Fatalf("reused snapshot = %+v, want %+v", second, first)
	}

	svc.Put(JobSnapshot{JobID: "job-1", Kind: "scan", LibraryID: "lib-1", Phase: JobPhaseCompleted})
	third, started := svc.Begin("job-1", "scan", "lib-1", "queued after completion")
	if !started {
		t.Fatalf("expected completed job to be restartable")
	}
	if third.Phase != JobPhaseQueued {
		t.Fatalf("third phase = %q, want queued", third.Phase)
	}
}

func TestJobsServiceSubscribeReceivesJobSnapshots(t *testing.T) {
	t.Parallel()

	svc := NewJobsService()
	events := make([]JobSnapshot, 0, 2)
	stop := svc.Subscribe(func(snapshot JobSnapshot) {
		events = append(events, snapshot)
	})

	svc.Begin("job-1", "scan", "lib-1", "queued")
	svc.Put(JobSnapshot{JobID: "job-1", Kind: "scan", LibraryID: "lib-1", Phase: JobPhaseRunning, Progress: 0.5})
	stop()
	svc.Put(JobSnapshot{JobID: "job-1", Kind: "scan", LibraryID: "lib-1", Phase: JobPhaseCompleted, Progress: 1})

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Phase != JobPhaseQueued {
		t.Fatalf("first event phase = %q, want queued", events[0].Phase)
	}
	if events[1].Phase != JobPhaseRunning {
		t.Fatalf("second event phase = %q, want running", events[1].Phase)
	}
}
