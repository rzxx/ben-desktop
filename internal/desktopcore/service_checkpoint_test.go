package desktopcore

import (
	"context"
	"testing"
	"time"
)

func TestPublishCheckpointRetainsOnlyLatestManifestAndAck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "checkpoint-publish")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        local.DeviceID + ":1",
		DeviceID:    local.DeviceID,
		Seq:         1,
		TSNS:        now.UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-1",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-1"}`,
	})

	first, err := app.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish first checkpoint: %v", err)
	}
	if first.LibraryID != library.LibraryID || first.CheckpointID == "" {
		t.Fatalf("unexpected first manifest: %+v", first)
	}
	if first.EntryCount != 1 || first.BaseClocks[local.DeviceID] != 1 {
		t.Fatalf("unexpected first checkpoint summary: %+v", first)
	}

	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        local.DeviceID + ":2",
		DeviceID:    local.DeviceID,
		Seq:         2,
		TSNS:        now.Add(time.Second).UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-2",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-2"}`,
	})

	second, err := app.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish second checkpoint: %v", err)
	}
	if second.CheckpointID == first.CheckpointID {
		t.Fatalf("expected a new checkpoint id after appending oplog state")
	}
	if second.EntryCount != 2 || second.BaseClocks[local.DeviceID] != 2 {
		t.Fatalf("unexpected second checkpoint summary: %+v", second)
	}

	var checkpointCount int64
	if err := app.db.WithContext(ctx).Model(&LibraryCheckpoint{}).
		Where("library_id = ?", library.LibraryID).
		Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count checkpoints: %v", err)
	}
	if checkpointCount != 1 {
		t.Fatalf("checkpoint count = %d, want 1", checkpointCount)
	}

	var currentAckCount int64
	if err := app.db.WithContext(ctx).Model(&DeviceCheckpointAck{}).
		Where("library_id = ? AND checkpoint_id = ? AND device_id = ?", library.LibraryID, second.CheckpointID, local.DeviceID).
		Count(&currentAckCount).Error; err != nil {
		t.Fatalf("count current checkpoint ack: %v", err)
	}
	if currentAckCount != 1 {
		t.Fatalf("current checkpoint ack count = %d, want 1", currentAckCount)
	}

	var staleAckCount int64
	if err := app.db.WithContext(ctx).Model(&DeviceCheckpointAck{}).
		Where("library_id = ? AND checkpoint_id = ?", library.LibraryID, first.CheckpointID).
		Count(&staleAckCount).Error; err != nil {
		t.Fatalf("count stale checkpoint acks: %v", err)
	}
	if staleAckCount != 0 {
		t.Fatalf("stale checkpoint ack count = %d, want 0", staleAckCount)
	}
}

func TestCompactCheckpointRequiresCoverageThenDeletesCoveredOps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "checkpoint-compact")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         "peer-device",
		Role:             roleMember,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed peer membership: %v", err)
	}
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        local.DeviceID + ":1",
		DeviceID:    local.DeviceID,
		Seq:         1,
		TSNS:        now.UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-local",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-local"}`,
	})
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        "peer-device:1",
		DeviceID:    "peer-device",
		Seq:         1,
		TSNS:        now.Add(time.Second).UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-peer",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-peer"}`,
	})

	manifest, err := app.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish checkpoint: %v", err)
	}

	blocked, err := app.CompactCheckpoint(ctx, false)
	if err != nil {
		t.Fatalf("compact checkpoint before peer ack: %v", err)
	}
	if blocked.Compactable {
		t.Fatalf("expected compaction to be blocked: %+v", blocked)
	}
	if blocked.DeletedOps != 0 || len(blocked.PendingDevices) != 1 || blocked.PendingDevices[0].DeviceID != "peer-device" {
		t.Fatalf("unexpected blocked compaction result: %+v", blocked)
	}

	if err := app.db.WithContext(ctx).Create(&DeviceCheckpointAck{
		LibraryID:    library.LibraryID,
		DeviceID:     "peer-device",
		CheckpointID: manifest.CheckpointID,
		Source:       checkpointAckSourceCovered,
		AckedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed peer checkpoint ack: %v", err)
	}

	compacted, err := app.CompactCheckpoint(ctx, false)
	if err != nil {
		t.Fatalf("compact checkpoint after peer ack: %v", err)
	}
	if !compacted.Compactable || compacted.DeletedOps != 2 {
		t.Fatalf("unexpected compaction result: %+v", compacted)
	}

	var remainingOps int64
	if err := app.db.WithContext(ctx).Model(&OplogEntry{}).
		Where("library_id = ?", library.LibraryID).
		Count(&remainingOps).Error; err != nil {
		t.Fatalf("count remaining ops: %v", err)
	}
	if remainingOps != 0 {
		t.Fatalf("remaining oplog entries = %d, want 0", remainingOps)
	}
}

func TestStartPublishCheckpointQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "checkpoint-publish-async")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        local.DeviceID + ":1",
		DeviceID:    local.DeviceID,
		Seq:         1,
		TSNS:        now.UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-1",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-1"}`,
	})

	job, err := app.StartPublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("start publish checkpoint: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindPublishCheckpoint {
		t.Fatalf("unexpected queued publish job: %+v", job)
	}

	final := waitForJobPhase(t, ctx, app, "checkpoint:publish:"+library.LibraryID, JobPhaseCompleted)
	if final.Kind != jobKindPublishCheckpoint || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final publish job: %+v", final)
	}

	status, err := app.CheckpointStatus(ctx)
	if err != nil {
		t.Fatalf("checkpoint status: %v", err)
	}
	if status.CheckpointID == "" || status.EntryCount != 1 {
		t.Fatalf("unexpected checkpoint status: %+v", status)
	}
}

func TestStartCompactCheckpointQueuesAsyncJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "checkpoint-compact-async")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         "peer-device",
		Role:             roleMember,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed peer membership: %v", err)
	}
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        local.DeviceID + ":1",
		DeviceID:    local.DeviceID,
		Seq:         1,
		TSNS:        now.UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-local",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-local"}`,
	})
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        "peer-device:1",
		DeviceID:    "peer-device",
		Seq:         1,
		TSNS:        now.Add(time.Second).UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-peer",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-peer"}`,
	})

	manifest, err := app.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish checkpoint: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&DeviceCheckpointAck{
		LibraryID:    library.LibraryID,
		DeviceID:     "peer-device",
		CheckpointID: manifest.CheckpointID,
		Source:       checkpointAckSourceCovered,
		AckedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed peer checkpoint ack: %v", err)
	}

	job, err := app.StartCompactCheckpoint(ctx, false)
	if err != nil {
		t.Fatalf("start compact checkpoint: %v", err)
	}
	if job.Phase != JobPhaseQueued || job.Kind != jobKindCompactCheckpoint {
		t.Fatalf("unexpected queued compact job: %+v", job)
	}

	final := waitForJobPhase(t, ctx, app, "checkpoint:compact:"+library.LibraryID, JobPhaseCompleted)
	if final.Kind != jobKindCompactCheckpoint || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final compact job: %+v", final)
	}

	var remainingOps int64
	if err := app.db.WithContext(ctx).Model(&OplogEntry{}).
		Where("library_id = ?", library.LibraryID).
		Count(&remainingOps).Error; err != nil {
		t.Fatalf("count remaining ops: %v", err)
	}
	if remainingOps != 0 {
		t.Fatalf("remaining oplog entries = %d, want 0", remainingOps)
	}
}

func TestRemoveLibraryMemberPrunesCheckpointAck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "checkpoint-ack-prune")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).Create(&Membership{
		LibraryID:        library.LibraryID,
		DeviceID:         "peer-device",
		Role:             roleMember,
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error; err != nil {
		t.Fatalf("seed peer membership: %v", err)
	}
	seedCheckpointOp(t, app, OplogEntry{
		LibraryID:   library.LibraryID,
		OpID:        local.DeviceID + ":1",
		DeviceID:    local.DeviceID,
		Seq:         1,
		TSNS:        now.UnixNano(),
		EntityType:  "playlist",
		EntityID:    "pl-1",
		OpKind:      "upsert",
		PayloadJSON: `{"playlist_id":"pl-1"}`,
	})

	manifest, err := app.PublishCheckpoint(ctx)
	if err != nil {
		t.Fatalf("publish checkpoint: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&DeviceCheckpointAck{
		LibraryID:    library.LibraryID,
		DeviceID:     "peer-device",
		CheckpointID: manifest.CheckpointID,
		Source:       checkpointAckSourceCovered,
		AckedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed peer checkpoint ack: %v", err)
	}

	if err := app.RemoveLibraryMember(ctx, "peer-device"); err != nil {
		t.Fatalf("remove library member: %v", err)
	}

	var ackCount int64
	if err := app.db.WithContext(ctx).Model(&DeviceCheckpointAck{}).
		Where("library_id = ? AND device_id = ?", library.LibraryID, "peer-device").
		Count(&ackCount).Error; err != nil {
		t.Fatalf("count pruned checkpoint acks: %v", err)
	}
	if ackCount != 0 {
		t.Fatalf("checkpoint ack count after member removal = %d, want 0", ackCount)
	}
}

func seedCheckpointOp(t *testing.T, app *App, op OplogEntry) {
	t.Helper()
	if err := app.db.Create(&op).Error; err != nil {
		t.Fatalf("seed checkpoint op %s: %v", op.OpID, err)
	}
}
