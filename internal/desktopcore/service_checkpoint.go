package desktopcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/core/api/types"
	"gorm.io/gorm"
)

const (
	checkpointStatusPublished  = "published"
	defaultCheckpointChunkSize = 1000

	jobKindPublishCheckpoint = "publish-checkpoint"
	jobKindCompactCheckpoint = "compact-checkpoint"
)

type checkpointOplogEntry struct {
	OpID        string          `json:"opId"`
	DeviceID    string          `json:"deviceId"`
	Seq         int64           `json:"seq"`
	TSNS        int64           `json:"tsns"`
	EntityType  string          `json:"entityType"`
	EntityID    string          `json:"entityId"`
	OpKind      string          `json:"opKind"`
	PayloadJSON json.RawMessage `json:"payloadJson,omitempty"`
}

type checkpointChunk struct {
	ChunkIndex  int
	EntryCount  int
	ContentHash string
	Entries     []checkpointOplogEntry
}

func (a *App) SyncNow(ctx context.Context) error {
	if _, err := a.requireActiveContext(ctx); err != nil {
		return err
	}
	return fmt.Errorf("sync service is not implemented in desktop core yet")
}

func (a *App) ConnectPeer(ctx context.Context, peerAddr string) error {
	if _, err := a.requireActiveContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(peerAddr) == "" {
		return fmt.Errorf("peer address is required")
	}
	return fmt.Errorf("peer transport is not implemented in desktop core yet")
}

func (a *App) PublishCheckpoint(ctx context.Context) (apitypes.LibraryCheckpointManifest, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return apitypes.LibraryCheckpointManifest{}, err
	}
	if !canManageLibrary(local.Role) {
		return apitypes.LibraryCheckpointManifest{}, fmt.Errorf("checkpoint publish requires admin role")
	}

	job := a.jobs.Track("checkpoint:publish:"+local.LibraryID, jobKindPublishCheckpoint, local.LibraryID)
	if job != nil {
		job.Queued(0, "queued checkpoint publish")
		job.Running(0.1, "building checkpoint manifest")
	}

	manifest, err := a.publishCheckpoint(ctx, local.LibraryID, local.DeviceID, job)
	if err != nil {
		if job != nil {
			job.Fail(1, "checkpoint publish failed", err)
		}
		return apitypes.LibraryCheckpointManifest{}, err
	}
	if job != nil {
		job.Complete(1, "published checkpoint "+manifest.CheckpointID)
	}
	return manifest, nil
}

func (a *App) CompactCheckpoint(ctx context.Context, force bool) (apitypes.CheckpointCompactionResult, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return apitypes.CheckpointCompactionResult{}, err
	}
	if !canManageLibrary(local.Role) {
		return apitypes.CheckpointCompactionResult{}, fmt.Errorf("checkpoint compaction requires admin role")
	}

	job := a.jobs.Track("checkpoint:compact:"+local.LibraryID, jobKindCompactCheckpoint, local.LibraryID)
	if job != nil {
		job.Queued(0, "queued checkpoint compaction")
		job.Running(0.15, "loading published checkpoint")
	}

	result, err := a.compactCheckpoint(ctx, local.LibraryID, force, job)
	if err != nil {
		if job != nil {
			job.Fail(1, "checkpoint compaction failed", err)
		}
		return apitypes.CheckpointCompactionResult{}, err
	}
	if job != nil {
		message := fmt.Sprintf("compacted checkpoint %s, deleted %d ops", result.CheckpointID, result.DeletedOps)
		if !result.Compactable && !force {
			message = "checkpoint compaction blocked by pending devices"
		}
		job.Complete(1, message)
	}
	return result, nil
}

func (a *App) publishCheckpoint(ctx context.Context, libraryID, deviceID string, job *JobTracker) (apitypes.LibraryCheckpointManifest, error) {
	entries, err := a.listCheckpointEntries(ctx, libraryID)
	if err != nil {
		return apitypes.LibraryCheckpointManifest{}, err
	}
	if job != nil {
		job.Running(0.3, "collecting checkpoint clocks")
	}
	baseClocks, err := a.checkpointBaseClocks(ctx, libraryID)
	if err != nil {
		return apitypes.LibraryCheckpointManifest{}, err
	}
	chunks, err := buildCheckpointChunks(entries, defaultCheckpointChunkSize)
	if err != nil {
		return apitypes.LibraryCheckpointManifest{}, err
	}
	contentHash, err := checkpointContentHash(baseClocks, chunks)
	if err != nil {
		return apitypes.LibraryCheckpointManifest{}, err
	}

	now := time.Now().UTC()
	manifest := apitypes.LibraryCheckpointManifest{
		LibraryID:         strings.TrimSpace(libraryID),
		CheckpointID:      contentHash,
		CreatedByDeviceID: strings.TrimSpace(deviceID),
		CreatedAt:         now,
		BaseClocks:        baseClocks,
		ChunkCount:        len(chunks),
		EntryCount:        len(entries),
		ContentHash:       contentHash,
		Status:            checkpointStatusPublished,
		PublishedAt:       cloneTimePtr(&now),
	}

	if job != nil {
		job.Running(0.7, "persisting published checkpoint")
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := savePublishedCheckpointTx(tx, manifest, chunks); err != nil {
			return err
		}
		return recordCheckpointAckTx(tx, libraryID, deviceID, manifest.CheckpointID, checkpointAckSourceCovered, now)
	}); err != nil {
		return apitypes.LibraryCheckpointManifest{}, err
	}
	return manifest, nil
}

func (a *App) compactCheckpoint(ctx context.Context, libraryID string, force bool, job *JobTracker) (apitypes.CheckpointCompactionResult, error) {
	checkpoint, baseClocks, ok, err := a.loadPublishedCheckpoint(ctx, libraryID)
	if err != nil {
		return apitypes.CheckpointCompactionResult{}, err
	}
	if !ok {
		return apitypes.CheckpointCompactionResult{}, fmt.Errorf("published checkpoint not found")
	}

	devices, compactable, err := a.pendingCheckpointDevices(ctx, libraryID, checkpoint.CheckpointID)
	if err != nil {
		return apitypes.CheckpointCompactionResult{}, err
	}
	result := apitypes.CheckpointCompactionResult{
		LibraryID:    strings.TrimSpace(libraryID),
		CheckpointID: strings.TrimSpace(checkpoint.CheckpointID),
		Compactable:  compactable,
		Forced:       force,
	}
	for _, device := range devices {
		if strings.TrimSpace(device.State) == "pending" {
			result.PendingDevices = append(result.PendingDevices, device)
		}
	}
	if !compactable && !force {
		return result, nil
	}

	if job != nil {
		job.Running(0.65, "deleting checkpoint-covered oplog entries")
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for deviceID, seq := range baseClocks {
			deviceID = strings.TrimSpace(deviceID)
			if deviceID == "" {
				continue
			}
			deleted := tx.Where("library_id = ? AND device_id = ? AND seq <= ?", libraryID, deviceID, seq).Delete(&OplogEntry{})
			if deleted.Error != nil {
				return deleted.Error
			}
			result.DeletedOps += deleted.RowsAffected
		}
		return pruneSupersededCheckpointsTx(tx, libraryID, checkpoint.CheckpointID)
	}); err != nil {
		return apitypes.CheckpointCompactionResult{}, err
	}

	_ = reclaimSQLiteSpace(ctx, a.db)
	result.Compactable = true
	return result, nil
}

func (a *App) loadPublishedCheckpoint(ctx context.Context, libraryID string) (LibraryCheckpoint, map[string]int64, bool, error) {
	var checkpoint LibraryCheckpoint
	err := a.db.WithContext(ctx).
		Where("library_id = ? AND published_at IS NOT NULL", strings.TrimSpace(libraryID)).
		Order("published_at DESC").
		Limit(1).
		Take(&checkpoint).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return LibraryCheckpoint{}, nil, false, nil
		}
		return LibraryCheckpoint{}, nil, false, err
	}
	baseClocks := make(map[string]int64)
	if strings.TrimSpace(checkpoint.BaseClocksJSON) != "" {
		if err := json.Unmarshal([]byte(checkpoint.BaseClocksJSON), &baseClocks); err != nil {
			return LibraryCheckpoint{}, nil, false, fmt.Errorf("decode checkpoint clocks: %w", err)
		}
	}
	return checkpoint, baseClocks, true, nil
}

func (a *App) pendingCheckpointDevices(ctx context.Context, libraryID, checkpointID string) ([]apitypes.CheckpointDeviceCoverage, bool, error) {
	type memberRow struct {
		DeviceID string
		Role     string
	}

	var members []memberRow
	if err := a.db.WithContext(ctx).
		Table("memberships").
		Select("device_id, role").
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("device_id ASC").
		Scan(&members).Error; err != nil {
		return nil, false, err
	}

	var acks []DeviceCheckpointAck
	if err := a.db.WithContext(ctx).
		Where("library_id = ? AND checkpoint_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(checkpointID)).
		Find(&acks).Error; err != nil {
		return nil, false, err
	}
	ackByDevice := make(map[string]DeviceCheckpointAck, len(acks))
	for _, ack := range acks {
		ackByDevice[strings.TrimSpace(ack.DeviceID)] = ack
	}

	devices := make([]apitypes.CheckpointDeviceCoverage, 0, len(members))
	compactable := len(members) > 0
	for _, member := range members {
		state := "pending"
		if ack, ok := ackByDevice[strings.TrimSpace(member.DeviceID)]; ok {
			state = strings.TrimSpace(ack.Source)
			if state == "" {
				state = checkpointAckSourceCovered
			}
		}
		if state == "pending" {
			compactable = false
		}
		devices = append(devices, apitypes.CheckpointDeviceCoverage{
			DeviceID: strings.TrimSpace(member.DeviceID),
			Role:     strings.TrimSpace(member.Role),
			State:    state,
		})
	}
	return devices, compactable, nil
}

func (a *App) listCheckpointEntries(ctx context.Context, libraryID string) ([]checkpointOplogEntry, error) {
	var rows []OplogEntry
	if err := a.db.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("tsns ASC, device_id ASC, seq ASC, op_id ASC").
		Find(&rows).Error; err != nil {
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

func (a *App) checkpointBaseClocks(ctx context.Context, libraryID string) (map[string]int64, error) {
	baseClocks := make(map[string]int64)

	var clocks []DeviceClock
	if err := a.db.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Find(&clocks).Error; err != nil {
		return nil, err
	}
	for _, clock := range clocks {
		deviceID := strings.TrimSpace(clock.DeviceID)
		if deviceID == "" {
			continue
		}
		baseClocks[deviceID] = clock.LastSeqSeen
	}

	type row struct {
		DeviceID string
		MaxSeq   int64
	}
	var rows []row
	if err := a.db.WithContext(ctx).
		Model(&OplogEntry{}).
		Select("device_id, MAX(seq) AS max_seq").
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Group("device_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		deviceID := strings.TrimSpace(row.DeviceID)
		if deviceID == "" {
			continue
		}
		if row.MaxSeq > baseClocks[deviceID] {
			baseClocks[deviceID] = row.MaxSeq
		}
	}
	return baseClocks, nil
}

func buildCheckpointChunks(entries []checkpointOplogEntry, chunkSize int) ([]checkpointChunk, error) {
	if chunkSize <= 0 {
		chunkSize = defaultCheckpointChunkSize
	}
	if len(entries) == 0 {
		hash, err := hashCheckpointChunk(nil)
		if err != nil {
			return nil, err
		}
		return []checkpointChunk{{
			ChunkIndex:  0,
			EntryCount:  0,
			ContentHash: hash,
		}}, nil
	}

	chunks := make([]checkpointChunk, 0, (len(entries)+chunkSize-1)/chunkSize)
	for start, index := 0, 0; start < len(entries); start, index = start+chunkSize, index+1 {
		end := start + chunkSize
		if end > len(entries) {
			end = len(entries)
		}
		chunkEntries := append([]checkpointOplogEntry(nil), entries[start:end]...)
		hash, err := hashCheckpointChunk(chunkEntries)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, checkpointChunk{
			ChunkIndex:  index,
			EntryCount:  len(chunkEntries),
			ContentHash: hash,
			Entries:     chunkEntries,
		})
	}
	return chunks, nil
}

func hashCheckpointChunk(entries []checkpointOplogEntry) (string, error) {
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal checkpoint chunk: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func checkpointContentHash(baseClocks map[string]int64, chunks []checkpointChunk) (string, error) {
	baseJSON, err := json.Marshal(baseClocks)
	if err != nil {
		return "", fmt.Errorf("marshal checkpoint clocks: %w", err)
	}
	hash := sha256.New()
	_, _ = hash.Write(baseJSON)
	for _, chunk := range chunks {
		_, _ = hash.Write([]byte(strings.TrimSpace(chunk.ContentHash)))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func savePublishedCheckpointTx(tx *gorm.DB, manifest apitypes.LibraryCheckpointManifest, chunks []checkpointChunk) error {
	baseJSON, err := json.Marshal(manifest.BaseClocks)
	if err != nil {
		return fmt.Errorf("marshal checkpoint clocks: %w", err)
	}

	if err := tx.Where("library_id = ? AND checkpoint_id = ?", manifest.LibraryID, manifest.CheckpointID).Delete(&LibraryCheckpointChunk{}).Error; err != nil {
		return err
	}

	row := LibraryCheckpoint{
		LibraryID:         strings.TrimSpace(manifest.LibraryID),
		CheckpointID:      strings.TrimSpace(manifest.CheckpointID),
		CreatedByDeviceID: strings.TrimSpace(manifest.CreatedByDeviceID),
		BaseClocksJSON:    string(baseJSON),
		ChunkCount:        manifest.ChunkCount,
		EntryCount:        manifest.EntryCount,
		ContentHash:       strings.TrimSpace(manifest.ContentHash),
		Status:            checkpointStatusPublished,
		CreatedAt:         manifest.CreatedAt.UTC(),
		UpdatedAt:         time.Now().UTC(),
		PublishedAt:       cloneTimePtr(manifest.PublishedAt),
	}
	if err := tx.Save(&row).Error; err != nil {
		return err
	}
	for _, chunk := range chunks {
		payloadJSON, err := json.Marshal(chunk.Entries)
		if err != nil {
			return fmt.Errorf("marshal checkpoint chunk %d: %w", chunk.ChunkIndex, err)
		}
		if err := tx.Create(&LibraryCheckpointChunk{
			LibraryID:    row.LibraryID,
			CheckpointID: row.CheckpointID,
			ChunkIndex:   chunk.ChunkIndex,
			EntryCount:   chunk.EntryCount,
			ContentHash:  strings.TrimSpace(chunk.ContentHash),
			PayloadJSON:  string(payloadJSON),
		}).Error; err != nil {
			return err
		}
	}
	return pruneSupersededCheckpointsTx(tx, row.LibraryID, row.CheckpointID)
}

func recordCheckpointAckTx(tx *gorm.DB, libraryID, deviceID, checkpointID, source string, ackedAt time.Time) error {
	source = strings.TrimSpace(source)
	if source == "" {
		source = checkpointAckSourceCovered
	}
	return tx.Save(&DeviceCheckpointAck{
		LibraryID:    strings.TrimSpace(libraryID),
		DeviceID:     strings.TrimSpace(deviceID),
		CheckpointID: strings.TrimSpace(checkpointID),
		Source:       source,
		AckedAt:      ackedAt.UTC(),
	}).Error
}

func pruneSupersededCheckpointsTx(tx *gorm.DB, libraryID, keepCheckpointID string) error {
	libraryID = strings.TrimSpace(libraryID)
	keepCheckpointID = strings.TrimSpace(keepCheckpointID)
	if err := tx.Where("library_id = ? AND checkpoint_id <> ?", libraryID, keepCheckpointID).Delete(&LibraryCheckpointChunk{}).Error; err != nil {
		return err
	}
	if err := tx.Where("library_id = ? AND checkpoint_id <> ?", libraryID, keepCheckpointID).Delete(&LibraryCheckpoint{}).Error; err != nil {
		return err
	}
	return tx.Where("library_id = ? AND checkpoint_id <> ?", libraryID, keepCheckpointID).Delete(&DeviceCheckpointAck{}).Error
}

func sortCheckpointDevices(devices []apitypes.CheckpointDeviceCoverage) {
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].DeviceID < devices[j].DeviceID
	})
}
