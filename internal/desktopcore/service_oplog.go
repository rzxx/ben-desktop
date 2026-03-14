package desktopcore

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) appendLocalOplogTx(tx *gorm.DB, local apitypes.LocalContext, entityType, entityID, opKind string, payload any) (OplogEntry, error) {
	libraryID := strings.TrimSpace(local.LibraryID)
	deviceID := strings.TrimSpace(local.DeviceID)
	entityType = strings.TrimSpace(entityType)
	entityID = strings.TrimSpace(entityID)
	opKind = strings.TrimSpace(opKind)
	if libraryID == "" || deviceID == "" {
		return OplogEntry{}, fmt.Errorf("local library and device context are required")
	}
	if entityType == "" || entityID == "" || opKind == "" {
		return OplogEntry{}, fmt.Errorf("oplog entity type, entity id, and op kind are required")
	}

	payloadJSON := "{}"
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return OplogEntry{}, fmt.Errorf("marshal oplog payload: %w", err)
		}
		payloadJSON = string(raw)
	}

	seq, err := nextLibraryDeviceSeqTx(tx, libraryID, deviceID)
	if err != nil {
		return OplogEntry{}, err
	}
	now := time.Now().UTC()
	entry := OplogEntry{
		LibraryID:   libraryID,
		OpID:        deviceID + ":" + fmt.Sprintf("%d", seq),
		DeviceID:    deviceID,
		Seq:         seq,
		TSNS:        now.UnixNano(),
		EntityType:  entityType,
		EntityID:    entityID,
		OpKind:      opKind,
		PayloadJSON: payloadJSON,
	}
	if err := tx.Create(&entry).Error; err != nil {
		return OplogEntry{}, err
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seq_seen"}),
	}).Create(&DeviceClock{
		LibraryID:   libraryID,
		DeviceID:    deviceID,
		LastSeqSeen: seq,
	}).Error; err != nil {
		return OplogEntry{}, err
	}
	return entry, nil
}

func nextLibraryDeviceSeqTx(tx *gorm.DB, libraryID, deviceID string) (int64, error) {
	var clock DeviceClock
	lastSeq := int64(0)
	if err := tx.Where("library_id = ? AND device_id = ?", libraryID, deviceID).Take(&clock).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return 0, err
		}
	} else {
		lastSeq = clock.LastSeqSeen
	}

	type row struct {
		MaxSeq int64
	}
	var oplogRow row
	if err := tx.Model(&OplogEntry{}).
		Select("COALESCE(MAX(seq), 0) AS max_seq").
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Scan(&oplogRow).Error; err != nil {
		return 0, err
	}
	if oplogRow.MaxSeq > lastSeq {
		lastSeq = oplogRow.MaxSeq
	}
	return lastSeq + 1, nil
}
