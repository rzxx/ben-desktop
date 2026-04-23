package desktopcore

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const localOplogMutationStateKey = "desktopcore:local-oplog-mutation-state"

type localOplogMutationState struct {
	mu        sync.Mutex
	libraries map[string]struct{}
	armed     bool
}

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
	if a != nil {
		a.armLocalOplogMutationNotification(tx, libraryID)
	}
	return entry, nil
}

func (a *App) armLocalOplogMutationNotification(tx *gorm.DB, libraryID string) {
	if a == nil || a.storage == nil || tx == nil {
		return
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return
	}

	state := a.localOplogMutationState(tx)
	state.mu.Lock()
	if state.libraries == nil {
		state.libraries = make(map[string]struct{}, 1)
	}
	state.libraries[libraryID] = struct{}{}
	if state.armed {
		state.mu.Unlock()
		return
	}
	state.armed = true
	state.mu.Unlock()

	registerTxCommitHook(tx, func() {
		a.notifyCommittedLocalOplogMutation(state)
	})
}

func (a *App) localOplogMutationState(tx *gorm.DB) *localOplogMutationState {
	if state := transactionState(tx); state != nil {
		if existing, ok := state.oplogMutation.(*localOplogMutationState); ok && existing != nil {
			return existing
		}
		mutationState := &localOplogMutationState{}
		state.oplogMutation = mutationState
		return mutationState
	}
	if tx == nil {
		return &localOplogMutationState{}
	}
	if existing, ok := tx.InstanceGet(localOplogMutationStateKey); ok {
		if state, stateOK := existing.(*localOplogMutationState); stateOK && state != nil {
			return state
		}
	}
	state := &localOplogMutationState{}
	tx.InstanceSet(localOplogMutationStateKey, state)
	return state
}

func (a *App) notifyCommittedLocalOplogMutation(state *localOplogMutationState) {
	if a == nil || a.storage == nil || a.transportService == nil || state == nil {
		return
	}

	state.mu.Lock()
	libraries := make([]string, 0, len(state.libraries))
	for mutatedLibraryID := range state.libraries {
		libraries = append(libraries, mutatedLibraryID)
	}
	state.mu.Unlock()

	for _, mutatedLibraryID := range libraries {
		a.transportService.noteLocalLibraryMutation(mutatedLibraryID)
	}
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
