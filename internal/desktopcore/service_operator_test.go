package desktopcore

import (
	"context"
	"testing"
	"time"

	apitypes "ben/core/api/types"
)

func TestOperatorReadsWithoutActiveLibrary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)

	local, err := app.EnsureLocalContext(ctx)
	if err != nil {
		t.Fatalf("ensure local context: %v", err)
	}
	if local.DeviceID == "" || local.Device == "" {
		t.Fatalf("local context missing device identity: %+v", local)
	}
	if local.LibraryID != "" {
		t.Fatalf("expected no active library, got %+v", local)
	}

	activity, err := app.ActivityStatus(ctx)
	if err != nil {
		t.Fatalf("activity status: %v", err)
	}
	if activity.Scan.Phase != "idle" || activity.Artwork.Phase != "idle" {
		t.Fatalf("unexpected idle activity: %+v", activity)
	}

	status := app.NetworkStatus()
	if status.Running {
		t.Fatalf("network should be stopped without active library: %+v", status)
	}
	if status.DeviceID != local.DeviceID {
		t.Fatalf("network device id = %q, want %q", status.DeviceID, local.DeviceID)
	}
}

func TestInspectAndOplogDiagnostics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "diagnostics")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	now := time.Now().UTC()
	seedCacheRecording(t, app, library.LibraryID, local.DeviceID, cacheSeedInput{
		RecordingID:    "rec-1",
		AlbumID:        "album-1",
		SourceFileID:   "src-1",
		EncodingID:     "enc-1",
		BlobID:         testBlobID("a"),
		Profile:        "desktop",
		LastVerifiedAt: now.Add(-time.Hour),
	})
	seedArtworkCache(t, app, library.LibraryID, "album", "album-1", testBlobID("b"), now)
	if err := app.db.WithContext(ctx).Create([]OplogEntry{
		{
			LibraryID:   library.LibraryID,
			OpID:        "op-1",
			DeviceID:    local.DeviceID,
			Seq:         1,
			TSNS:        now.Add(-30 * time.Minute).UnixNano(),
			EntityType:  entityTypeOptimizedAsset,
			EntityID:    "enc-1",
			OpKind:      "upsert",
			PayloadJSON: `{"encoding_id":"enc-1"}`,
		},
		{
			LibraryID:   library.LibraryID,
			OpID:        "op-2",
			DeviceID:    "peer-device",
			Seq:         1,
			TSNS:        now.Add(-48 * time.Hour).UnixNano(),
			EntityType:  entityTypeDeviceAssetCache,
			EntityID:    "peer-device:enc-1",
			OpKind:      "upsert",
			PayloadJSON: `{"encoding_id":"enc-1","device_id":"peer-device","is_cached":true}`,
		},
	}).Error; err != nil {
		t.Fatalf("seed oplog entries: %v", err)
	}
	successAt := now.Add(-2 * time.Minute)
	attemptAt := now.Add(-3 * time.Minute)
	if err := app.db.WithContext(ctx).Create(&PeerSyncState{
		LibraryID:     library.LibraryID,
		DeviceID:      "peer-device",
		PeerID:        "peer-1",
		LastAttemptAt: &attemptAt,
		LastSuccessAt: &successAt,
		LastError:     "",
		LastApplied:   12,
		UpdatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("seed peer sync state: %v", err)
	}

	summary, err := app.Inspect(ctx)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if summary.Libraries != 1 || summary.Devices != 1 || summary.Memberships != 1 {
		t.Fatalf("unexpected inspect summary: %+v", summary)
	}
	if summary.Content != 1 || summary.Encodings != 1 || summary.ArtworkVariants != 1 {
		t.Fatalf("unexpected inspect media summary: %+v", summary)
	}

	report, err := app.InspectLibraryOplog(ctx, "")
	if err != nil {
		t.Fatalf("inspect library oplog: %v", err)
	}
	if report.LibraryID != library.LibraryID {
		t.Fatalf("report library id = %q, want %q", report.LibraryID, library.LibraryID)
	}
	if len(report.OplogByEntityType) != 2 {
		t.Fatalf("unexpected entity groups: %+v", report.OplogByEntityType)
	}
	if report.Transcode.OplogEncodings != 1 || report.Transcode.OplogDeviceEncodings != 1 {
		t.Fatalf("unexpected transcode diagnostics: %+v", report.Transcode)
	}
	if report.Materialized.Encodings != 1 || report.Materialized.ArtworkVariants != 1 {
		t.Fatalf("unexpected materialized counts: %+v", report.Materialized)
	}
	recency := map[string]int64{}
	for _, bucket := range report.OplogByRecency {
		recency[bucket.Bucket] = bucket.Count
	}
	if recency["last_hour"] != 1 || recency["last_7d"] != 1 {
		t.Fatalf("unexpected recency buckets: %+v", report.OplogByRecency)
	}

	network := app.NetworkStatus()
	if network.LibraryID != library.LibraryID {
		t.Fatalf("network library id = %q, want %q", network.LibraryID, library.LibraryID)
	}
	if network.ServiceTag == "" || network.ActivePeerID != "peer-1" {
		t.Fatalf("unexpected network status: %+v", network)
	}
	if network.LastBatchApplied != 12 {
		t.Fatalf("last batch applied = %d, want 12", network.LastBatchApplied)
	}
}

func TestCheckpointStatusReportsCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "checkpoints")
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
	if err := app.db.WithContext(ctx).Create(&LibraryCheckpoint{
		LibraryID:         library.LibraryID,
		CheckpointID:      "ckpt-1",
		CreatedByDeviceID: local.DeviceID,
		BaseClocksJSON:    `{"` + local.DeviceID + `":5}`,
		ChunkCount:        2,
		EntryCount:        7,
		ContentHash:       "hash-1",
		Status:            "published",
		CreatedAt:         now,
		UpdatedAt:         now,
		PublishedAt:       &now,
	}).Error; err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&DeviceCheckpointAck{
		LibraryID:    library.LibraryID,
		DeviceID:     local.DeviceID,
		CheckpointID: "ckpt-1",
		Source:       checkpointAckSourceCovered,
		AckedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed local checkpoint ack: %v", err)
	}

	status, err := app.CheckpointStatus(ctx)
	if err != nil {
		t.Fatalf("checkpoint status: %v", err)
	}
	if status.CheckpointID != "ckpt-1" || status.AckedDevices != 1 || status.TotalDevices != 2 {
		t.Fatalf("unexpected checkpoint status: %+v", status)
	}
	if status.Compactable {
		t.Fatalf("checkpoint should not be compactable yet: %+v", status)
	}

	if err := app.db.WithContext(ctx).Create(&DeviceCheckpointAck{
		LibraryID:    library.LibraryID,
		DeviceID:     "peer-device",
		CheckpointID: "ckpt-1",
		Source:       checkpointAckSourceCovered,
		AckedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed peer checkpoint ack: %v", err)
	}

	status, err = app.CheckpointStatus(ctx)
	if err != nil {
		t.Fatalf("checkpoint status after ack: %v", err)
	}
	if !status.Compactable || status.AckedDevices != 2 {
		t.Fatalf("expected compactable checkpoint after all acks: %+v", status)
	}
}

func TestInspectLibraryOplogRequiresActiveLibraryWhenLibraryIDMissing(t *testing.T) {
	t.Parallel()

	app := openCacheTestApp(t, 1024)
	_, err := app.InspectLibraryOplog(context.Background(), "")
	if err != apitypes.ErrNoActiveLibrary {
		t.Fatalf("inspect library oplog err = %v, want ErrNoActiveLibrary", err)
	}
}
