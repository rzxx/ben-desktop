package desktopcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/core/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultSyncBatchSize          = 500
	incrementalSyncBacklogCutover = 5000
	maxSyncCatchupRounds          = 64

	jobKindSyncNow = "sync-now"
)

type SyncTransport interface {
	ListPeers(ctx context.Context, local apitypes.LocalContext) ([]SyncPeer, error)
	ResolvePeer(ctx context.Context, local apitypes.LocalContext, peerAddr string) (SyncPeer, error)
}

type SyncPeer interface {
	Address() string
	DeviceID() string
	PeerID() string
	Sync(ctx context.Context, req SyncRequest) (SyncResponse, error)
	FetchCheckpoint(ctx context.Context, libraryID, checkpointID string) (checkpointTransferRecord, error)
}

type SyncRequest struct {
	LibraryID             string
	DeviceID              string
	PeerID                string
	Clocks                map[string]int64
	InstalledCheckpointID string
	MaxOps                int
}

type SyncResponse struct {
	LibraryID      string
	DeviceID       string
	PeerID         string
	Ops            []checkpointOplogEntry
	HasMore        bool
	RemainingOps   int64
	NeedCheckpoint bool
	Checkpoint     *apitypes.LibraryCheckpointManifest
}

type syncBatch struct {
	Ops          []checkpointOplogEntry
	HasMore      bool
	RemainingOps int64
	TotalMissing int64
}

type checkpointTransferRecord struct {
	Manifest apitypes.LibraryCheckpointManifest
	Chunks   []checkpointChunk
}

func (a *App) SetSyncTransport(transport SyncTransport) {
	if a == nil {
		return
	}
	a.transport = transport
}

func (a *App) SyncNow(ctx context.Context) error {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	return a.syncNowForLocalContext(ctx, local, nil)
}

func (a *App) StartSyncNow(ctx context.Context) (JobSnapshot, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}
	if a.transport == nil {
		return JobSnapshot{}, fmt.Errorf("peer transport is not configured")
	}

	jobID := "sync:" + local.LibraryID
	snapshot, started := a.jobs.Begin(jobID, jobKindSyncNow, local.LibraryID, "queued manual sync")
	if !started {
		return snapshot, nil
	}

	runCtx, cleanup, err := a.activeLibraryTaskContext(ctx, local.LibraryID)
	if err != nil {
		return JobSnapshot{}, err
	}
	go func() {
		defer cleanup()
		_ = a.syncNowForLocalContext(runCtx, local, a.jobs.Track(jobID, jobKindSyncNow, local.LibraryID))
	}()
	return snapshot, nil
}

func (a *App) syncNowForLocalContext(ctx context.Context, local apitypes.LocalContext, job *JobTracker) error {
	local, err := a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "manual sync canceled because the library is no longer active", nil)
				return err
			}
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}
	if a.transport == nil {
		err := fmt.Errorf("peer transport is not configured")
		if job != nil {
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}

	if job != nil {
		job.Queued(0, "queued manual sync")
		job.Running(0.05, "discovering peers")
	}

	peers, err := a.transport.ListPeers(ctx, local)
	if err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "manual sync canceled because the library is no longer active", nil)
				return err
			}
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}
	if len(peers) == 0 {
		err := fmt.Errorf("no connected peers")
		if job != nil {
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}

	successes := 0
	failures := 0
	var firstErr error
	seen := make(map[string]struct{}, len(peers))
	processed := 0
	totalPeers := len(peers)
	for _, peer := range peers {
		key := strings.TrimSpace(peer.Address())
		if key == "" {
			key = strings.TrimSpace(peer.PeerID())
		}
		if key == "" {
			key = strings.TrimSpace(peer.DeviceID())
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		processed++
		if job != nil {
			progress := 0.1
			if totalPeers > 0 {
				progress += (float64(processed-1) / float64(totalPeers)) * 0.8
			}
			job.Running(progress, syncPeerJobMessage(processed, totalPeers, peer))
		}
		if _, err := a.syncPeerCatchup(ctx, local, peer, apitypes.NetworkSyncReasonManual); err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		successes++
	}

	if successes == 0 {
		if firstErr != nil {
			if job != nil {
				if errors.Is(firstErr, context.Canceled) {
					job.Fail(1, "manual sync canceled because the library is no longer active", nil)
					return firstErr
				}
				job.Fail(1, "manual sync failed", firstErr)
			}
			return firstErr
		}
		err := fmt.Errorf("all peer sync attempts failed")
		if job != nil {
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}
	if failures > 0 && firstErr != nil {
		err := fmt.Errorf("%d peer sync attempts failed: %w", failures, firstErr)
		if job != nil {
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}
	if job != nil {
		job.Complete(1, syncJobCompletionMessage(successes, failures))
	}
	return nil
}

func syncPeerJobMessage(index, total int, peer SyncPeer) string {
	if total <= 0 {
		total = 1
	}
	target := strings.TrimSpace(peer.Address())
	if target == "" {
		target = strings.TrimSpace(peer.PeerID())
	}
	if target == "" {
		target = strings.TrimSpace(peer.DeviceID())
	}
	if target == "" {
		return fmt.Sprintf("syncing peer %d of %d", index, total)
	}
	return fmt.Sprintf("syncing peer %d of %d: %s", index, total, target)
}

func syncJobCompletionMessage(successes, failures int) string {
	switch {
	case successes > 0 && failures > 0:
		return fmt.Sprintf("synced %d peer(s), %d failed", successes, failures)
	case successes > 0:
		return fmt.Sprintf("synced %d peer(s)", successes)
	default:
		return "manual sync completed"
	}
}

func (a *App) ConnectPeer(ctx context.Context, peerAddr string) error {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return err
	}
	peerAddr = strings.TrimSpace(peerAddr)
	if peerAddr == "" {
		return fmt.Errorf("peer address is required")
	}
	if a.transport == nil {
		return fmt.Errorf("peer transport is not configured")
	}

	peer, err := a.transport.ResolvePeer(ctx, local, peerAddr)
	if err != nil {
		return err
	}
	_, err = a.syncPeerCatchup(ctx, local, peer, apitypes.NetworkSyncReasonConnect)
	return err
}

func (a *App) syncPeerCatchup(ctx context.Context, local apitypes.LocalContext, peer SyncPeer, reason apitypes.NetworkSyncReason) (int, error) {
	if peer == nil {
		return 0, fmt.Errorf("sync peer is required")
	}
	startedAt := time.Now().UTC()
	totalApplied := 0
	remoteDeviceID := strings.TrimSpace(peer.DeviceID())
	remotePeerID := strings.TrimSpace(peer.PeerID())

	for round := 0; round < maxSyncCatchupRounds; round++ {
		if err := ctx.Err(); err != nil {
			if !errors.Is(err, context.Canceled) {
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			}
			return totalApplied, err
		}

		req, err := a.buildSyncRequest(ctx, local.LibraryID, local.DeviceID, local.PeerID, defaultSyncBatchSize)
		if err != nil {
			a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			return totalApplied, err
		}

		resp, err := peer.Sync(ctx, req)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			}
			return totalApplied, err
		}
		if strings.TrimSpace(resp.LibraryID) != strings.TrimSpace(local.LibraryID) {
			err := fmt.Errorf("remote library mismatch")
			a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			return totalApplied, err
		}
		remoteDeviceID = firstNonEmpty(resp.DeviceID, remoteDeviceID)
		remotePeerID = firstNonEmpty(resp.PeerID, remotePeerID)

		if resp.NeedCheckpoint {
			if resp.Checkpoint == nil || strings.TrimSpace(resp.Checkpoint.CheckpointID) == "" {
				err := fmt.Errorf("remote checkpoint response missing checkpoint summary")
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				return totalApplied, err
			}
			record, err := peer.FetchCheckpoint(ctx, local.LibraryID, resp.Checkpoint.CheckpointID)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				}
				return totalApplied, err
			}
			applied, err := a.installCheckpointRecord(ctx, local.DeviceID, record)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				}
				return totalApplied, err
			}
			totalApplied += applied
			a.recordPeerSyncSuccess(ctx, local.LibraryID, remoteDeviceID, remotePeerID, int64(applied))
			continue
		}

		applied, err := a.applyRemoteOps(ctx, local.LibraryID, resp.Ops)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			}
			return totalApplied, err
		}
		totalApplied += applied
		a.recordPeerSyncSuccess(ctx, local.LibraryID, remoteDeviceID, remotePeerID, int64(applied))

		if !resp.HasMore || (len(resp.Ops) == 0 && applied == 0) {
			_ = startedAt
			_ = reason
			return totalApplied, nil
		}
	}

	err := fmt.Errorf("catch-up session budget exhausted")
	a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
	return totalApplied, err
}

func (a *App) ensureLocalPeerContext(ctx context.Context, local apitypes.LocalContext) (apitypes.LocalContext, error) {
	if strings.TrimSpace(local.PeerID) != "" {
		return local, nil
	}
	peerID, err := a.ensureDevicePeerID(ctx, local.DeviceID, local.Device)
	if err != nil {
		return apitypes.LocalContext{}, err
	}
	local.PeerID = peerID
	return local, nil
}

func (a *App) buildSyncRequest(ctx context.Context, libraryID, deviceID, peerID string, maxOps int) (SyncRequest, error) {
	clocks, err := a.listDeviceClocks(ctx, libraryID)
	if err != nil {
		return SyncRequest{}, fmt.Errorf("list device clocks: %w", err)
	}
	ack, ok, err := a.checkpointAckForDevice(ctx, libraryID, deviceID)
	if err != nil {
		return SyncRequest{}, fmt.Errorf("load checkpoint ack: %w", err)
	}
	if maxOps <= 0 {
		maxOps = defaultSyncBatchSize
	}

	req := SyncRequest{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		PeerID:    peerID,
		Clocks:    clocksToMap(clocks),
		MaxOps:    maxOps,
	}
	if ok {
		req.InstalledCheckpointID = strings.TrimSpace(ack.CheckpointID)
	}
	return req, nil
}

func (a *App) buildSyncResponse(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return SyncResponse{}, err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return SyncResponse{}, err
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return SyncResponse{}, fmt.Errorf("remote library mismatch")
	}
	if !a.isLibraryMember(ctx, req.LibraryID, req.DeviceID) {
		return SyncResponse{}, fmt.Errorf("device not allowed")
	}

	published, hasPublished, err := a.loadCheckpointTransferRecord(ctx, req.LibraryID, "", true)
	if err != nil {
		return SyncResponse{}, fmt.Errorf("load published checkpoint: %w", err)
	}
	if hasPublished {
		now := time.Now().UTC()
		switch {
		case strings.TrimSpace(req.InstalledCheckpointID) == strings.TrimSpace(published.Manifest.CheckpointID):
			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return recordCheckpointAckTx(tx, req.LibraryID, req.DeviceID, published.Manifest.CheckpointID, checkpointAckSourceInstalled, now)
			}); err != nil {
				return SyncResponse{}, err
			}
		case clocksCoverCheckpoint(req.Clocks, published.Manifest.BaseClocks):
			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return recordCheckpointAckTx(tx, req.LibraryID, req.DeviceID, published.Manifest.CheckpointID, checkpointAckSourceCovered, now)
			}); err != nil {
				return SyncResponse{}, err
			}
		}
	}

	batch, err := a.selectSyncBatch(ctx, req.LibraryID, req.Clocks, req.MaxOps)
	if err != nil {
		return SyncResponse{}, fmt.Errorf("select sync batch: %w", err)
	}

	resp := SyncResponse{
		LibraryID:    req.LibraryID,
		DeviceID:     local.DeviceID,
		PeerID:       local.PeerID,
		Ops:          batch.Ops,
		HasMore:      batch.HasMore,
		RemainingOps: batch.RemainingOps,
	}
	if hasPublished && (requesterNeedsCheckpoint(req.Clocks, published.Manifest.BaseClocks) || batch.TotalMissing >= incrementalSyncBacklogCutover) {
		resp.Ops = nil
		resp.HasMore = true
		resp.NeedCheckpoint = true
		resp.Checkpoint = &published.Manifest
		resp.RemainingOps = 0
	}
	return resp, nil
}

func (a *App) listDeviceClocks(ctx context.Context, libraryID string) ([]DeviceClock, error) {
	var rows []DeviceClock
	if err := a.db.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("device_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func clocksToMap(rows []DeviceClock) map[string]int64 {
	if len(rows) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.DeviceID)] = row.LastSeqSeen
	}
	return out
}

func requesterNeedsCheckpoint(requestClocks, baseClocks map[string]int64) bool {
	for deviceID, seq := range baseClocks {
		if seq > 0 && requestClocks[strings.TrimSpace(deviceID)] < seq {
			return true
		}
	}
	return false
}

func clocksCoverCheckpoint(clocks, baseClocks map[string]int64) bool {
	for deviceID, seq := range baseClocks {
		if clocks[strings.TrimSpace(deviceID)] < seq {
			return false
		}
	}
	return true
}

func (a *App) selectSyncBatch(ctx context.Context, libraryID string, clocks map[string]int64, limit int) (syncBatch, error) {
	if limit <= 0 {
		limit = defaultSyncBatchSize
	}
	deviceClocks, err := a.listDeviceClocks(ctx, libraryID)
	if err != nil {
		return syncBatch{}, err
	}

	type deviceBacklog struct {
		entries    []checkpointOplogEntry
		nextCursor int
	}

	backlogs := make([]deviceBacklog, 0, len(deviceClocks))
	totalMissing := int64(0)
	for _, clock := range deviceClocks {
		deviceID := strings.TrimSpace(clock.DeviceID)
		since := int64(0)
		if clocks != nil {
			since = clocks[deviceID]
		}
		missing := clock.LastSeqSeen - since
		if missing <= 0 {
			continue
		}
		totalMissing += missing
		entries, err := a.listOplogByDevice(ctx, libraryID, deviceID, since, limit)
		if err != nil {
			return syncBatch{}, err
		}
		backlogs = append(backlogs, deviceBacklog{entries: entries})
	}

	ops := make([]checkpointOplogEntry, 0, limit)
	for len(ops) < limit {
		progress := false
		for i := range backlogs {
			if len(ops) >= limit {
				break
			}
			if backlogs[i].nextCursor >= len(backlogs[i].entries) {
				continue
			}
			ops = append(ops, backlogs[i].entries[backlogs[i].nextCursor])
			backlogs[i].nextCursor++
			progress = true
		}
		if !progress {
			break
		}
	}
	sortCheckpointEntries(ops)

	remaining := totalMissing - int64(len(ops))
	if remaining < 0 {
		remaining = 0
	}
	return syncBatch{
		Ops:          ops,
		HasMore:      remaining > 0,
		RemainingOps: remaining,
		TotalMissing: totalMissing,
	}, nil
}

func (a *App) listOplogByDevice(ctx context.Context, libraryID, deviceID string, sinceSeq int64, limit int) ([]checkpointOplogEntry, error) {
	if limit <= 0 {
		limit = defaultSyncBatchSize
	}
	var rows []OplogEntry
	query := a.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Order("seq ASC").
		Limit(limit)
	if sinceSeq > 0 {
		query = query.Where("seq > ?", sinceSeq)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]checkpointOplogEntry, 0, len(rows))
	for _, row := range rows {
		entry := checkpointOplogEntry{
			OpID:       strings.TrimSpace(row.OpID),
			DeviceID:   strings.TrimSpace(row.DeviceID),
			Seq:        row.Seq,
			TSNS:       row.TSNS,
			EntityType: strings.TrimSpace(row.EntityType),
			EntityID:   strings.TrimSpace(row.EntityID),
			OpKind:     strings.TrimSpace(row.OpKind),
		}
		if payload := strings.TrimSpace(row.PayloadJSON); payload != "" {
			entry.PayloadJSON = json.RawMessage(payload)
		}
		out = append(out, entry)
	}
	return out, nil
}

func sortCheckpointEntries(entries []checkpointOplogEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TSNS == entries[j].TSNS {
			if entries[i].DeviceID == entries[j].DeviceID {
				if entries[i].Seq == entries[j].Seq {
					return entries[i].OpID < entries[j].OpID
				}
				return entries[i].Seq < entries[j].Seq
			}
			return entries[i].DeviceID < entries[j].DeviceID
		}
		return entries[i].TSNS < entries[j].TSNS
	})
}

func (a *App) checkpointAckForDevice(ctx context.Context, libraryID, deviceID string) (DeviceCheckpointAck, bool, error) {
	var row DeviceCheckpointAck
	err := a.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return DeviceCheckpointAck{}, false, nil
		}
		return DeviceCheckpointAck{}, false, err
	}
	return row, true, nil
}

func (a *App) loadCheckpointTransferRecord(ctx context.Context, libraryID, checkpointID string, publishedOnly bool) (checkpointTransferRecord, bool, error) {
	libraryID = strings.TrimSpace(libraryID)
	checkpointID = strings.TrimSpace(checkpointID)
	if libraryID == "" {
		return checkpointTransferRecord{}, false, fmt.Errorf("library id is required")
	}

	var row LibraryCheckpoint
	query := a.db.WithContext(ctx).Where("library_id = ?", libraryID)
	if publishedOnly {
		query = query.Where("published_at IS NOT NULL").Order("published_at DESC")
	} else {
		query = query.Where("checkpoint_id = ?", checkpointID)
	}
	if err := query.Limit(1).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return checkpointTransferRecord{}, false, nil
		}
		return checkpointTransferRecord{}, false, err
	}

	manifest, err := checkpointManifestFromRow(row)
	if err != nil {
		return checkpointTransferRecord{}, false, err
	}

	var chunkRows []LibraryCheckpointChunk
	if err := a.db.WithContext(ctx).
		Where("library_id = ? AND checkpoint_id = ?", row.LibraryID, row.CheckpointID).
		Order("chunk_index ASC").
		Find(&chunkRows).Error; err != nil {
		return checkpointTransferRecord{}, false, err
	}
	chunks := make([]checkpointChunk, 0, len(chunkRows))
	for _, chunkRow := range chunkRows {
		var entries []checkpointOplogEntry
		if strings.TrimSpace(chunkRow.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(chunkRow.PayloadJSON), &entries); err != nil {
				return checkpointTransferRecord{}, false, fmt.Errorf("decode checkpoint chunk %d: %w", chunkRow.ChunkIndex, err)
			}
		}
		chunks = append(chunks, checkpointChunk{
			ChunkIndex:  chunkRow.ChunkIndex,
			EntryCount:  chunkRow.EntryCount,
			ContentHash: strings.TrimSpace(chunkRow.ContentHash),
			Entries:     entries,
		})
	}
	return checkpointTransferRecord{Manifest: manifest, Chunks: chunks}, true, nil
}

func checkpointManifestFromRow(row LibraryCheckpoint) (apitypes.LibraryCheckpointManifest, error) {
	baseClocks := make(map[string]int64)
	if strings.TrimSpace(row.BaseClocksJSON) != "" {
		if err := json.Unmarshal([]byte(row.BaseClocksJSON), &baseClocks); err != nil {
			return apitypes.LibraryCheckpointManifest{}, fmt.Errorf("decode checkpoint clocks: %w", err)
		}
	}
	return apitypes.LibraryCheckpointManifest{
		LibraryID:         strings.TrimSpace(row.LibraryID),
		CheckpointID:      strings.TrimSpace(row.CheckpointID),
		CreatedByDeviceID: strings.TrimSpace(row.CreatedByDeviceID),
		CreatedAt:         row.CreatedAt,
		BaseClocks:        baseClocks,
		ChunkCount:        row.ChunkCount,
		EntryCount:        row.EntryCount,
		ContentHash:       strings.TrimSpace(row.ContentHash),
		Status:            strings.TrimSpace(row.Status),
		PublishedAt:       cloneTimePtr(row.PublishedAt),
	}, nil
}

func (a *App) installCheckpointRecord(ctx context.Context, localDeviceID string, record checkpointTransferRecord) (int, error) {
	totalEntries := 0
	for _, chunk := range record.Chunks {
		totalEntries += len(chunk.Entries)
	}

	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		preservedTail, err := selectCheckpointTailOpsTx(tx, record.Manifest.LibraryID, record.Manifest.BaseClocks)
		if err != nil {
			return err
		}
		if err := savePublishedCheckpointTx(tx, record.Manifest, record.Chunks); err != nil {
			return err
		}
		if err := clearCheckpointManagedStateTx(tx, record.Manifest.LibraryID); err != nil {
			return err
		}
		if err := insertCheckpointOpsTx(tx, record.Manifest.LibraryID, record.Chunks); err != nil {
			return err
		}

		deviceIDs := make([]string, 0, len(record.Manifest.BaseClocks))
		for deviceID := range record.Manifest.BaseClocks {
			deviceIDs = append(deviceIDs, strings.TrimSpace(deviceID))
		}
		sort.Strings(deviceIDs)
		for _, deviceID := range deviceIDs {
			if _, err := applyBufferedDeviceOplogTx(tx, record.Manifest.LibraryID, deviceID, 1); err != nil {
				return fmt.Errorf("replay checkpoint device %s: %w", deviceID, err)
			}
		}

		restoreDevices := make(map[string]struct{}, len(preservedTail))
		for _, op := range preservedTail {
			if err := tx.Create(&op).Error; err != nil {
				return fmt.Errorf("restore checkpoint tail op %s: %w", op.OpID, err)
			}
			deviceID := strings.TrimSpace(op.DeviceID)
			if deviceID != "" {
				restoreDevices[deviceID] = struct{}{}
			}
		}
		for deviceID := range restoreDevices {
			startSeq := record.Manifest.BaseClocks[deviceID] + 1
			if _, err := applyBufferedDeviceOplogTx(tx, record.Manifest.LibraryID, deviceID, startSeq); err != nil {
				return fmt.Errorf("replay checkpoint tail device %s: %w", deviceID, err)
			}
		}

		if strings.TrimSpace(localDeviceID) != "" {
			if err := ensureLikedPlaylistTx(tx, record.Manifest.LibraryID, localDeviceID, time.Now().UTC()); err != nil {
				return err
			}
			if err := recordCheckpointAckTx(tx, record.Manifest.LibraryID, localDeviceID, record.Manifest.CheckpointID, checkpointAckSourceInstalled, time.Now().UTC()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return totalEntries, nil
}

func selectCheckpointTailOpsTx(tx *gorm.DB, libraryID string, baseClocks map[string]int64) ([]OplogEntry, error) {
	var rows []OplogEntry
	if err := tx.Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("tsns ASC, device_id ASC, seq ASC, op_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]OplogEntry, 0, len(rows))
	for _, row := range rows {
		if row.Seq <= baseClocks[strings.TrimSpace(row.DeviceID)] {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func clearCheckpointManagedStateTx(tx *gorm.DB, libraryID string) error {
	models := []any{
		&ScanRoot{},
		&OfflinePin{},
		&Artist{},
		&Credit{},
		&AlbumVariantModel{},
		&TrackVariantModel{},
		&AlbumTrack{},
		&DeviceVariantPreference{},
		&SourceFileModel{},
		&OptimizedAssetModel{},
		&DeviceAssetCacheModel{},
		&ArtworkVariant{},
		&PlaylistItem{},
		&Playlist{},
		&DeviceClock{},
		&OplogEntry{},
	}
	for _, model := range models {
		if err := tx.Where("library_id = ?", strings.TrimSpace(libraryID)).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}

func insertCheckpointOpsTx(tx *gorm.DB, libraryID string, chunks []checkpointChunk) error {
	for _, chunk := range chunks {
		entries := append([]checkpointOplogEntry(nil), chunk.Entries...)
		sortCheckpointEntries(entries)
		for _, entry := range entries {
			payloadJSON := "{}"
			if len(entry.PayloadJSON) > 0 {
				payloadJSON = string(entry.PayloadJSON)
			}
			if err := tx.Create(&OplogEntry{
				LibraryID:   strings.TrimSpace(libraryID),
				OpID:        strings.TrimSpace(entry.OpID),
				DeviceID:    strings.TrimSpace(entry.DeviceID),
				Seq:         entry.Seq,
				TSNS:        entry.TSNS,
				EntityType:  strings.TrimSpace(entry.EntityType),
				EntityID:    strings.TrimSpace(entry.EntityID),
				OpKind:      strings.TrimSpace(entry.OpKind),
				PayloadJSON: payloadJSON,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) applyRemoteOps(ctx context.Context, libraryID string, ops []checkpointOplogEntry) (int, error) {
	applied := 0
	for _, op := range ops {
		inserted, err := a.applyRemoteOp(ctx, libraryID, op)
		applied += inserted
		if err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func (a *App) applyRemoteOp(ctx context.Context, libraryID string, op checkpointOplogEntry) (int, error) {
	inserted := 0
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&OplogEntry{}).
			Where("library_id = ? AND op_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(op.OpID)).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}

		lastSeq, err := deviceClockSeqTx(tx, libraryID, op.DeviceID)
		if err != nil {
			return err
		}
		payloadJSON := "{}"
		if len(op.PayloadJSON) > 0 {
			payloadJSON = string(op.PayloadJSON)
		}
		entry := OplogEntry{
			LibraryID:   strings.TrimSpace(libraryID),
			OpID:        strings.TrimSpace(op.OpID),
			DeviceID:    strings.TrimSpace(op.DeviceID),
			Seq:         op.Seq,
			TSNS:        op.TSNS,
			EntityType:  strings.TrimSpace(op.EntityType),
			EntityID:    strings.TrimSpace(op.EntityID),
			OpKind:      strings.TrimSpace(op.OpKind),
			PayloadJSON: payloadJSON,
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		inserted = 1
		if entry.Seq != lastSeq+1 {
			return nil
		}
		_, err = applyBufferedDeviceOplogTx(tx, libraryID, entry.DeviceID, lastSeq+1)
		return err
	})
	return inserted, err
}

func deviceClockSeqTx(tx *gorm.DB, libraryID, deviceID string) (int64, error) {
	var row DeviceClock
	err := tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return row.LastSeqSeen, nil
}

func applyBufferedDeviceOplogTx(tx *gorm.DB, libraryID, deviceID string, nextSeq int64) (int64, error) {
	appliedUntil := nextSeq - 1
	for seq := nextSeq; ; seq++ {
		var row OplogEntry
		err := tx.Where("library_id = ? AND device_id = ? AND seq = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), seq).Take(&row).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				break
			}
			return appliedUntil, err
		}
		if err := applyOplogEntryTx(tx, row); err != nil {
			return appliedUntil, err
		}
		appliedUntil = seq
	}
	if appliedUntil >= nextSeq {
		if err := upsertDeviceClockTx(tx, libraryID, deviceID, appliedUntil); err != nil {
			return nextSeq - 1, err
		}
	}
	return appliedUntil, nil
}

func upsertDeviceClockTx(tx *gorm.DB, libraryID, deviceID string, seq int64) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{"last_seq_seen": seq}),
	}).Create(&DeviceClock{
		LibraryID:   strings.TrimSpace(libraryID),
		DeviceID:    strings.TrimSpace(deviceID),
		LastSeqSeen: seq,
	}).Error
}

func (a *App) recordPeerSyncSuccess(ctx context.Context, libraryID, deviceID, peerID string, applied int64) {
	if applied == 0 {
		if existing, ok, err := a.peerSyncState(ctx, libraryID, deviceID); err == nil && ok && existing.LastApplied > 0 {
			applied = existing.LastApplied
		}
	}
	now := time.Now().UTC()
	a.upsertPeerSyncState(ctx, libraryID, deviceID, peerID, &now, &now, "", applied)
}

func (a *App) recordPeerSyncFailure(ctx context.Context, libraryID, deviceID, peerID string, syncErr error) {
	if syncErr == nil {
		return
	}
	now := time.Now().UTC()
	a.upsertPeerSyncState(ctx, libraryID, deviceID, peerID, &now, nil, syncErr.Error(), 0)
}

func (a *App) upsertPeerSyncState(ctx context.Context, libraryID, deviceID, peerID string, attemptedAt, successAt *time.Time, lastError string, applied int64) {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if libraryID == "" || deviceID == "" {
		return
	}
	row := PeerSyncState{
		LibraryID:     libraryID,
		DeviceID:      deviceID,
		PeerID:        peerID,
		LastAttemptAt: cloneTimePtr(attemptedAt),
		LastSuccessAt: cloneTimePtr(successAt),
		LastError:     strings.TrimSpace(lastError),
		LastApplied:   applied,
		UpdatedAt:     time.Now().UTC(),
	}
	_ = a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"peer_id":         row.PeerID,
			"last_attempt_at": row.LastAttemptAt,
			"last_success_at": row.LastSuccessAt,
			"last_error":      row.LastError,
			"last_applied":    row.LastApplied,
			"updated_at":      row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (a *App) isLibraryMember(ctx context.Context, libraryID, deviceID string) bool {
	var count int64
	if err := a.db.WithContext(ctx).Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (a *App) peerSyncState(ctx context.Context, libraryID, deviceID string) (PeerSyncState, bool, error) {
	var row PeerSyncState
	err := a.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return PeerSyncState{}, false, nil
		}
		return PeerSyncState{}, false, err
	}
	return row, true, nil
}
