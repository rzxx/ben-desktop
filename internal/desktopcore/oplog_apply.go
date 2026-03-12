package desktopcore

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type playlistOplogPayload struct {
	PlaylistID string `json:"playlistId"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	CreatedBy  string `json:"createdBy"`
}

type playlistDeleteOplogPayload struct {
	PlaylistID string `json:"playlistId"`
}

type playlistItemOplogPayload struct {
	PlaylistID  string `json:"playlistId"`
	ItemID      string `json:"itemId"`
	RecordingID string `json:"recordingId"`
	PositionKey string `json:"positionKey"`
	Liked       bool   `json:"liked"`
}

type playlistItemDeleteOplogPayload struct {
	PlaylistID  string `json:"playlistId"`
	ItemID      string `json:"itemId"`
	RecordingID string `json:"recordingId"`
	Liked       bool   `json:"liked"`
}

func applyOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	switch strings.TrimSpace(entry.EntityType) {
	case entityTypeLibrary:
		return applyLibraryOplogEntryTx(tx, entry)
	case entityTypeScanRoots:
		return applyScanRootsOplogEntryTx(tx, entry)
	case entityTypeSourceFile:
		return applySourceFileOplogEntryTx(tx, entry)
	case entityTypePlaylist:
		return applyPlaylistOplogEntryTx(tx, entry)
	case entityTypePlaylistItem:
		return applyPlaylistItemOplogEntryTx(tx, entry)
	default:
		return fmt.Errorf("unsupported entity type %q", strings.TrimSpace(entry.EntityType))
	}
}

func applyLibraryOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	if strings.TrimSpace(entry.OpKind) != "upsert" {
		return fmt.Errorf("unsupported library op kind %q", strings.TrimSpace(entry.OpKind))
	}

	var payload libraryOplogPayload
	if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode library oplog payload: %w", err)
	}
	libraryID := firstNonEmpty(payload.LibraryID, entry.EntityID)
	if strings.TrimSpace(libraryID) == "" {
		return fmt.Errorf("library id is required")
	}
	row := Library{
		LibraryID: libraryID,
		Name:      strings.TrimSpace(payload.Name),
		CreatedAt: oplogMutationTime(entry),
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}},
		DoUpdates: clause.Assignments(map[string]any{"name": row.Name}),
	}).Create(&row).Error
}

func applyScanRootsOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	if strings.TrimSpace(entry.OpKind) != "replace" {
		return fmt.Errorf("unsupported scan roots op kind %q", strings.TrimSpace(entry.OpKind))
	}

	var payload scanRootsOplogPayload
	if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode scan roots oplog payload: %w", err)
	}
	deviceID := firstNonEmpty(payload.DeviceID, entry.EntityID)
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("scan roots device id is required")
	}
	roots, err := normalizeScanRoots(payload.Roots)
	if err != nil {
		return fmt.Errorf("normalize replayed scan roots: %w", err)
	}
	return setLibraryScanRootsTx(tx, entry.LibraryID, deviceID, roots)
}

func applySourceFileOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	switch strings.TrimSpace(entry.OpKind) {
	case "upsert":
		var payload sourceFileOplogPayload
		if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode source file oplog payload: %w", err)
		}
		if strings.TrimSpace(payload.DeviceID) == "" {
			return fmt.Errorf("source file device id is required")
		}
		if strings.TrimSpace(payload.SourceFileID) == "" {
			return fmt.Errorf("source file id is required")
		}
		return upsertIngestTx(tx, ingestRecord{
			LibraryID:    entry.LibraryID,
			DeviceID:     payload.DeviceID,
			Path:         payload.LocalPath,
			MTimeNS:      payload.MTimeNS,
			SizeBytes:    payload.SizeBytes,
			HashAlgo:     payload.HashAlgo,
			HashHex:      payload.HashHex,
			SourceFileID: payload.SourceFileID,
			Tags:         payload.Tags,
		}, oplogMutationTime(entry), payload.IsPresent)
	case "delete":
		var payload sourceFileOplogPayload
		if strings.TrimSpace(entry.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode source file delete payload: %w", err)
			}
		}
		deviceID, sourceFileID := sourceFileIdentityForEntry(entry, payload)
		if deviceID == "" || sourceFileID == "" {
			return fmt.Errorf("source file device id and source file id are required")
		}
		return tx.Where("library_id = ? AND device_id = ? AND source_file_id = ?", entry.LibraryID, deviceID, sourceFileID).Delete(&SourceFileModel{}).Error
	default:
		return fmt.Errorf("unsupported source file op kind %q", strings.TrimSpace(entry.OpKind))
	}
}

func applyPlaylistOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	switch strings.TrimSpace(entry.OpKind) {
	case "upsert":
		var payload playlistOplogPayload
		if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode playlist oplog payload: %w", err)
		}
		playlistID := firstNonEmpty(payload.PlaylistID, entry.EntityID)
		if strings.TrimSpace(playlistID) == "" {
			return fmt.Errorf("playlist id is required")
		}
		kind := strings.TrimSpace(payload.Kind)
		if kind == "" && playlistID == likedPlaylistIDForLibrary(entry.LibraryID) {
			kind = playlistKindLiked
		}
		if kind == "" {
			kind = playlistKindNormal
		}
		mutatedAt := oplogMutationTime(entry)

		var existing Playlist
		err := tx.Where("library_id = ? AND playlist_id = ?", entry.LibraryID, playlistID).Take(&existing).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}

		row := Playlist{
			LibraryID:  entry.LibraryID,
			PlaylistID: playlistID,
			Name:       strings.TrimSpace(payload.Name),
			Kind:       kind,
			CreatedBy:  firstNonEmpty(payload.CreatedBy, entry.DeviceID),
			CreatedAt:  mutatedAt,
			UpdatedAt:  mutatedAt,
		}
		if err == nil && !existing.CreatedAt.IsZero() {
			row.CreatedAt = existing.CreatedAt
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "playlist_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"name":       row.Name,
				"kind":       row.Kind,
				"created_by": row.CreatedBy,
				"updated_at": row.UpdatedAt,
				"deleted_at": nil,
			}),
		}).Create(&row).Error
	case "delete":
		var payload playlistDeleteOplogPayload
		if strings.TrimSpace(entry.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode playlist delete payload: %w", err)
			}
		}
		playlistID := firstNonEmpty(payload.PlaylistID, entry.EntityID)
		if strings.TrimSpace(playlistID) == "" {
			return fmt.Errorf("playlist id is required")
		}
		mutatedAt := oplogMutationTime(entry)
		return tx.Model(&Playlist{}).
			Where("library_id = ? AND playlist_id = ?", entry.LibraryID, playlistID).
			Updates(map[string]any{
				"deleted_at": &mutatedAt,
				"updated_at": mutatedAt,
			}).Error
	default:
		return fmt.Errorf("unsupported playlist op kind %q", strings.TrimSpace(entry.OpKind))
	}
}

func sourceFileIdentityForEntry(entry OplogEntry, payload sourceFileOplogPayload) (string, string) {
	deviceID := strings.TrimSpace(payload.DeviceID)
	sourceFileID := strings.TrimSpace(payload.SourceFileID)
	if deviceID != "" && sourceFileID != "" {
		return deviceID, sourceFileID
	}
	parts := strings.SplitN(strings.TrimSpace(entry.EntityID), ":", 2)
	if len(parts) == 2 {
		if deviceID == "" {
			deviceID = strings.TrimSpace(parts[0])
		}
		if sourceFileID == "" {
			sourceFileID = strings.TrimSpace(parts[1])
		}
	}
	return deviceID, sourceFileID
}

func applyPlaylistItemOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	switch strings.TrimSpace(entry.OpKind) {
	case "upsert", "move":
		var payload playlistItemOplogPayload
		if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode playlist item oplog payload: %w", err)
		}
		playlistID := firstNonEmpty(payload.PlaylistID, likedPlaylistIDForLibrary(entry.LibraryID))
		itemID := firstNonEmpty(payload.ItemID, entry.EntityID)
		recordingID := strings.TrimSpace(payload.RecordingID)
		if strings.TrimSpace(itemID) == "" || recordingID == "" {
			return fmt.Errorf("playlist item id and recording id are required")
		}
		if payload.Liked || playlistID == likedPlaylistIDForLibrary(entry.LibraryID) {
			if err := ensureLikedPlaylistTx(tx, entry.LibraryID, entry.DeviceID, oplogMutationTime(entry)); err != nil {
				return err
			}
			playlistID = likedPlaylistIDForLibrary(entry.LibraryID)
		}
		var playlistCount int64
		if err := tx.Model(&Playlist{}).
			Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", entry.LibraryID, playlistID).
			Count(&playlistCount).Error; err != nil {
			return err
		}
		if playlistCount == 0 {
			return fmt.Errorf("playlist %q does not exist for playlist item op", playlistID)
		}

		mutatedAt := oplogMutationTime(entry)
		row := PlaylistItem{
			LibraryID:      entry.LibraryID,
			PlaylistID:     playlistID,
			ItemID:         itemID,
			TrackVariantID: recordingID,
			AddedAt:        mutatedAt,
			UpdatedAt:      mutatedAt,
			PositionKey:    strings.TrimSpace(payload.PositionKey),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "playlist_id"},
				{Name: "item_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"track_variant_id": row.TrackVariantID,
				"position_key":     row.PositionKey,
				"updated_at":       row.UpdatedAt,
				"deleted_at":       nil,
			}),
		}).Create(&row).Error
	case "delete":
		var payload playlistItemDeleteOplogPayload
		if strings.TrimSpace(entry.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode playlist item delete payload: %w", err)
			}
		}
		playlistID := strings.TrimSpace(payload.PlaylistID)
		if payload.Liked || playlistID == "" {
			playlistID = likedPlaylistIDForLibrary(entry.LibraryID)
		}
		itemID := firstNonEmpty(payload.ItemID, entry.EntityID)
		if strings.TrimSpace(itemID) == "" {
			return fmt.Errorf("playlist item id is required")
		}
		mutatedAt := oplogMutationTime(entry)
		return tx.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND item_id = ?", entry.LibraryID, playlistID, itemID).
			Updates(map[string]any{
				"deleted_at": &mutatedAt,
				"updated_at": mutatedAt,
			}).Error
	default:
		return fmt.Errorf("unsupported playlist item op kind %q", strings.TrimSpace(entry.OpKind))
	}
}

func shouldApplyLatestMutationTx(tx *gorm.DB, entry OplogEntry) (bool, error) {
	type latestRow struct {
		TSNS int64
		OpID string
	}
	var latest latestRow
	if err := tx.Model(&OplogEntry{}).
		Select("tsns, op_id").
		Where("library_id = ? AND entity_type = ? AND entity_id = ?", entry.LibraryID, entry.EntityType, entry.EntityID).
		Order("tsns DESC, op_id DESC").
		Limit(1).
		Scan(&latest).Error; err != nil {
		return false, err
	}
	return latest.TSNS == entry.TSNS && strings.TrimSpace(latest.OpID) == strings.TrimSpace(entry.OpID), nil
}

func oplogMutationTime(entry OplogEntry) time.Time {
	if entry.TSNS <= 0 {
		return time.Now().UTC()
	}
	return time.Unix(0, entry.TSNS).UTC()
}
