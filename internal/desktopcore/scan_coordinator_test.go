package desktopcore

import "testing"

func TestScanCoordinatorDoesNotBatchManualRequests(t *testing.T) {
	coord := &scanCoordinator{
		pending: []scanCoordinatorRequest{
			{class: scanRequestRepair, fullRoots: []string{"G:\\music-a"}},
			{class: scanRequestRepair, fullRoots: []string{"G:\\music-b"}},
			{class: scanRequestDelta, deltaPaths: []string{"G:\\music-c\\track.flac"}},
		},
	}

	batch := coord.takeNextBatchLocked()
	if len(batch) != 1 {
		t.Fatalf("manual batch size = %d, want 1", len(batch))
	}
	if got := batch[0].fullRoots; len(got) != 1 || got[0] != "G:\\music-a" {
		t.Fatalf("manual batch roots = %+v, want [G:\\music-a]", got)
	}
	if len(coord.pending) != 2 {
		t.Fatalf("pending count = %d, want 2", len(coord.pending))
	}
	if coord.pending[0].class != scanRequestRepair {
		t.Fatalf("remaining first request class = %q, want %q", coord.pending[0].class, scanRequestRepair)
	}
	if coord.pending[1].class != scanRequestDelta {
		t.Fatalf("remaining second request class = %q, want %q", coord.pending[1].class, scanRequestDelta)
	}
}

func TestScanCoordinatorBatchesInternalRequestsAheadOfManual(t *testing.T) {
	coord := &scanCoordinator{
		pending: []scanCoordinatorRequest{
			{class: scanRequestDelta, deltaPaths: []string{"G:\\music-a\\track.flac"}},
			{class: scanRequestStartup, fullRoots: []string{"G:\\music-a"}},
			{class: scanRequestRepair, fullRoots: []string{"G:\\music-b"}},
		},
	}

	batch := coord.takeNextBatchLocked()
	if len(batch) != 2 {
		t.Fatalf("internal batch size = %d, want 2", len(batch))
	}
	if batch[0].class != scanRequestDelta || batch[1].class != scanRequestStartup {
		t.Fatalf("internal batch classes = [%q %q], want [%q %q]", batch[0].class, batch[1].class, scanRequestDelta, scanRequestStartup)
	}
	if len(coord.pending) != 1 || coord.pending[0].class != scanRequestRepair {
		t.Fatalf("remaining pending = %+v, want single manual request", coord.pending)
	}
}
