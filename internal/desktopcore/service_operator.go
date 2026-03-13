package desktopcore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/core/api/types"
)

const (
	checkpointAckSourceCovered   = "covered"
	checkpointAckSourceInstalled = "installed"
)

func (a *App) EnsureLocalContext(ctx context.Context) (apitypes.LocalContext, error) {
	device, err := a.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.LocalContext{}, fmt.Errorf("ensure current device: %w", err)
	}

	active, ok, err := a.activeLibraryForDevice(ctx, device.DeviceID)
	if err != nil {
		return apitypes.LocalContext{}, fmt.Errorf("resolve active library: %w", err)
	}
	if !ok {
		return apitypes.LocalContext{
			DeviceID: device.DeviceID,
			Device:   device.Name,
			PeerID:   strings.TrimSpace(device.PeerID),
		}, nil
	}

	return apitypes.LocalContext{
		LibraryID: strings.TrimSpace(active.LibraryID),
		DeviceID:  device.DeviceID,
		Device:    device.Name,
		Role:      strings.TrimSpace(active.Role),
		PeerID:    strings.TrimSpace(device.PeerID),
	}, nil
}

func (a *App) Inspect(ctx context.Context) (apitypes.InspectSummary, error) {
	count := func(model any) (int64, error) {
		var total int64
		if err := a.db.WithContext(ctx).Model(model).Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}
	countWhere := func(model any, query string, args ...any) (int64, error) {
		var total int64
		if err := a.db.WithContext(ctx).Model(model).Where(query, args...).Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	var out apitypes.InspectSummary
	var err error
	if out.Libraries, err = count(&Library{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count libraries: %w", err)
	}
	if out.Devices, err = count(&Device{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count devices: %w", err)
	}
	if out.Memberships, err = count(&Membership{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count memberships: %w", err)
	}
	if out.Artists, err = count(&Artist{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count artists: %w", err)
	}
	if out.Credits, err = count(&Credit{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count credits: %w", err)
	}
	if out.Albums, err = count(&AlbumVariantModel{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count albums: %w", err)
	}
	if out.Recordings, err = count(&TrackVariantModel{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count recordings: %w", err)
	}
	if out.DeviceVariantPrefs, err = count(&DeviceVariantPreference{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count device variant preferences: %w", err)
	}
	if out.Content, err = count(&SourceFileModel{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count source files: %w", err)
	}
	if out.DeviceContentPresent, err = countWhere(&SourceFileModel{}, "is_present = ?", true); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count present source files: %w", err)
	}
	if out.Encodings, err = count(&OptimizedAssetModel{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count optimized assets: %w", err)
	}
	if out.DeviceEncodings, err = count(&DeviceAssetCacheModel{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count device asset caches: %w", err)
	}
	if out.AlbumTracks, err = count(&AlbumTrack{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count album tracks: %w", err)
	}
	if out.ArtworkVariants, err = count(&ArtworkVariant{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count artwork variants: %w", err)
	}
	if out.Playlists, err = count(&Playlist{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count playlists: %w", err)
	}
	if out.PlaylistItems, err = count(&PlaylistItem{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count playlist items: %w", err)
	}
	if out.OplogEntries, err = count(&OplogEntry{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count oplog entries: %w", err)
	}
	if out.DeviceClocks, err = count(&DeviceClock{}); err != nil {
		return apitypes.InspectSummary{}, fmt.Errorf("count device clocks: %w", err)
	}
	return out, nil
}

func (a *App) InspectLibraryOplog(ctx context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		local, err := a.EnsureLocalContext(ctx)
		if err != nil {
			return apitypes.LibraryOplogDiagnostics{}, err
		}
		if strings.TrimSpace(local.LibraryID) == "" {
			return apitypes.LibraryOplogDiagnostics{}, apitypes.ErrNoActiveLibrary
		}
		libraryID = local.LibraryID
	}

	report := apitypes.LibraryOplogDiagnostics{
		LibraryID:   libraryID,
		GeneratedAt: time.Now().UTC(),
	}

	var err error
	if report.OplogByEntityType, err = a.inspectOplogGroups(ctx, libraryID, "entity_type"); err != nil {
		return apitypes.LibraryOplogDiagnostics{}, fmt.Errorf("group oplog by entity type: %w", err)
	}
	if report.OplogByDeviceID, err = a.inspectOplogGroups(ctx, libraryID, "device_id"); err != nil {
		return apitypes.LibraryOplogDiagnostics{}, fmt.Errorf("group oplog by device id: %w", err)
	}
	if report.OplogByRecency, err = a.inspectOplogRecency(ctx, libraryID, report.GeneratedAt); err != nil {
		return apitypes.LibraryOplogDiagnostics{}, fmt.Errorf("group oplog by recency: %w", err)
	}
	if report.Materialized, err = a.inspectLibraryMaterializedCounts(ctx, libraryID); err != nil {
		return apitypes.LibraryOplogDiagnostics{}, err
	}
	report.Transcode = apitypes.TranscodeOplogDiagnostics{
		OplogEncodings:       groupCount(report.OplogByEntityType, entityTypeOptimizedAsset),
		OplogDeviceEncodings: groupCount(report.OplogByEntityType, entityTypeDeviceAssetCache),
		Encodings:            report.Materialized.Encodings,
		DeviceEncodings:      report.Materialized.DeviceEncodings,
		ArtworkVariants:      report.Materialized.ArtworkVariants,
	}

	return report, nil
}

func (a *App) ActivityStatus(context.Context) (apitypes.ActivityStatus, error) {
	return a.ActivityStatusSnapshot(), nil
}

func (a *App) NetworkStatus() apitypes.NetworkStatus {
	local, err := a.EnsureLocalContext(context.Background())
	if err != nil {
		return apitypes.NetworkStatus{}
	}

	out := apitypes.NetworkStatus{
		Running:   a.transportRunning(),
		LibraryID: strings.TrimSpace(local.LibraryID),
		DeviceID:  strings.TrimSpace(local.DeviceID),
		PeerID:    strings.TrimSpace(local.PeerID),
	}
	if out.LibraryID == "" {
		return out
	}
	out.ServiceTag = serviceTagForLibrary(out.LibraryID)
	out.Mode = apitypes.NetworkSyncModeIdle

	type row struct {
		PeerID        string
		LastAttemptAt *time.Time
		LastSuccessAt *time.Time
		LastError     string
		LastApplied   int64
	}
	var latest row
	err = a.db.WithContext(context.Background()).
		Table("peer_sync_states").
		Select("peer_id, last_attempt_at, last_success_at, last_error, last_applied").
		Where("library_id = ?", out.LibraryID).
		Order("updated_at DESC, last_applied DESC, peer_id ASC").
		Limit(1).
		Scan(&latest).Error
	if err != nil {
		return out
	}
	out.ActivePeerID = strings.TrimSpace(latest.PeerID)
	out.LastBatchApplied = int(latest.LastApplied)
	out.LastSyncError = strings.TrimSpace(latest.LastError)
	out.CompletedAt = cloneTimePtr(latest.LastSuccessAt)
	if latest.LastAttemptAt != nil && (latest.LastSuccessAt == nil || latest.LastAttemptAt.After(*latest.LastSuccessAt)) {
		out.StartedAt = cloneTimePtr(latest.LastAttemptAt)
		out.Activity = apitypes.NetworkSyncActivityOps
		out.Reason = apitypes.NetworkSyncReasonManual
	}

	return out
}

func (a *App) CheckpointStatus(ctx context.Context) (apitypes.LibraryCheckpointStatus, error) {
	local, err := a.EnsureLocalContext(ctx)
	if err != nil {
		return apitypes.LibraryCheckpointStatus{}, err
	}
	libraryID := strings.TrimSpace(local.LibraryID)
	if libraryID == "" {
		return apitypes.LibraryCheckpointStatus{}, apitypes.ErrNoActiveLibrary
	}

	var checkpoint LibraryCheckpoint
	err = a.db.WithContext(ctx).
		Where("library_id = ? AND published_at IS NOT NULL", libraryID).
		Order("published_at DESC").
		Limit(1).
		Take(&checkpoint).Error
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "record not found") {
			return apitypes.LibraryCheckpointStatus{LibraryID: libraryID}, nil
		}
		return apitypes.LibraryCheckpointStatus{}, err
	}

	type memberRow struct {
		DeviceID string
		Role     string
	}
	var members []memberRow
	if err := a.db.WithContext(ctx).
		Table("memberships").
		Select("device_id, role").
		Where("library_id = ?", libraryID).
		Order("device_id ASC").
		Scan(&members).Error; err != nil {
		return apitypes.LibraryCheckpointStatus{}, err
	}

	var acks []DeviceCheckpointAck
	if err := a.db.WithContext(ctx).
		Where("library_id = ? AND checkpoint_id = ?", libraryID, checkpoint.CheckpointID).
		Find(&acks).Error; err != nil {
		return apitypes.LibraryCheckpointStatus{}, err
	}
	ackByDevice := make(map[string]DeviceCheckpointAck, len(acks))
	for _, ack := range acks {
		ackByDevice[strings.TrimSpace(ack.DeviceID)] = ack
	}

	devices := make([]apitypes.CheckpointDeviceCoverage, 0, len(members))
	acked := 0
	compactable := len(members) > 0
	for _, member := range members {
		state := "pending"
		if ack, ok := ackByDevice[strings.TrimSpace(member.DeviceID)]; ok {
			state = strings.TrimSpace(ack.Source)
			if state == "" {
				state = checkpointAckSourceCovered
			}
		}
		if state != "pending" {
			acked++
		} else {
			compactable = false
		}
		devices = append(devices, apitypes.CheckpointDeviceCoverage{
			DeviceID: strings.TrimSpace(member.DeviceID),
			Role:     strings.TrimSpace(member.Role),
			State:    state,
		})
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].DeviceID < devices[j].DeviceID
	})

	return apitypes.LibraryCheckpointStatus{
		LibraryID:        libraryID,
		CheckpointID:     strings.TrimSpace(checkpoint.CheckpointID),
		ChunkCount:       checkpoint.ChunkCount,
		EntryCount:       checkpoint.EntryCount,
		AckedDevices:     acked,
		TotalDevices:     len(devices),
		Compactable:      compactable,
		LastCheckpointAt: cloneTimePtr(&checkpoint.CreatedAt),
		PublishedAt:      cloneTimePtr(checkpoint.PublishedAt),
		Devices:          devices,
	}, nil
}

func (a *App) inspectOplogGroups(ctx context.Context, libraryID, column string) ([]apitypes.OplogDiagnosticsGroup, error) {
	type row struct {
		Key   string
		Count int64
	}

	var rows []row
	if err := a.db.WithContext(ctx).
		Model(&OplogEntry{}).
		Select(column+" AS key, COUNT(*) AS count").
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Group(column).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]apitypes.OplogDiagnosticsGroup, 0, len(rows))
	for _, row := range rows {
		out = append(out, apitypes.OplogDiagnosticsGroup{
			Key:   strings.TrimSpace(row.Key),
			Count: row.Count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out, nil
}

func (a *App) inspectOplogRecency(ctx context.Context, libraryID string, now time.Time) ([]apitypes.OplogRecencyBucket, error) {
	type bucket struct {
		name string
		min  int64
		max  int64
	}

	nowNS := now.UnixNano()
	buckets := []bucket{
		{name: "last_hour", min: now.Add(-time.Hour).UnixNano(), max: nowNS},
		{name: "last_24h", min: now.Add(-24 * time.Hour).UnixNano(), max: now.Add(-time.Hour).UnixNano()},
		{name: "last_7d", min: now.Add(-7 * 24 * time.Hour).UnixNano(), max: now.Add(-24 * time.Hour).UnixNano()},
		{name: "older", min: 0, max: now.Add(-7 * 24 * time.Hour).UnixNano()},
	}

	out := make([]apitypes.OplogRecencyBucket, 0, len(buckets))
	for _, bucket := range buckets {
		var total int64
		query := a.db.WithContext(ctx).Model(&OplogEntry{}).Where("library_id = ?", strings.TrimSpace(libraryID))
		if bucket.name == "older" {
			query = query.Where("tsns < ?", bucket.max)
		} else {
			query = query.Where("tsns >= ? AND tsns < ?", bucket.min, bucket.max)
		}
		if err := query.Count(&total).Error; err != nil {
			return nil, err
		}
		out = append(out, apitypes.OplogRecencyBucket{Bucket: bucket.name, Count: total})
	}
	return out, nil
}

func (a *App) inspectLibraryMaterializedCounts(ctx context.Context, libraryID string) (apitypes.LibraryMaterializedCounts, error) {
	count := func(model any) (int64, error) {
		var total int64
		if err := a.db.WithContext(ctx).Model(model).Where("library_id = ?", strings.TrimSpace(libraryID)).Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}
	countWhere := func(model any, query string, args ...any) (int64, error) {
		var total int64
		if err := a.db.WithContext(ctx).Model(model).Where("library_id = ?", strings.TrimSpace(libraryID)).Where(query, args...).Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	var out apitypes.LibraryMaterializedCounts
	var err error
	if out.Artists, err = count(&Artist{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count artists: %w", err)
	}
	if out.Credits, err = count(&Credit{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count credits: %w", err)
	}
	if out.Albums, err = count(&AlbumVariantModel{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count albums: %w", err)
	}
	if out.Recordings, err = count(&TrackVariantModel{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count recordings: %w", err)
	}
	if out.Contents, err = count(&SourceFileModel{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count contents: %w", err)
	}
	if out.DeviceContentCount, err = countWhere(&SourceFileModel{}, "is_present = ?", true); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count present source files: %w", err)
	}
	if out.AlbumTracks, err = count(&AlbumTrack{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count album tracks: %w", err)
	}
	if out.Encodings, err = count(&OptimizedAssetModel{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count encodings: %w", err)
	}
	if out.DeviceEncodings, err = count(&DeviceAssetCacheModel{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count device encodings: %w", err)
	}
	if out.ArtworkVariants, err = count(&ArtworkVariant{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count artwork variants: %w", err)
	}
	if out.Playlists, err = count(&Playlist{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count playlists: %w", err)
	}
	if out.PlaylistItems, err = count(&PlaylistItem{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count playlist items: %w", err)
	}
	if out.OplogEntries, err = count(&OplogEntry{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count oplog entries: %w", err)
	}
	if out.DeviceClocks, err = count(&DeviceClock{}); err != nil {
		return apitypes.LibraryMaterializedCounts{}, fmt.Errorf("count device clocks: %w", err)
	}
	return out, nil
}

func groupCount(groups []apitypes.OplogDiagnosticsGroup, key string) int64 {
	for _, group := range groups {
		if strings.TrimSpace(group.Key) == strings.TrimSpace(key) {
			return group.Count
		}
	}
	return 0
}
