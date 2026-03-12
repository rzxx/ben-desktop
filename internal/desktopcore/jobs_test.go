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
