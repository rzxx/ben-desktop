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
	case "playlist":
		return applyPlaylistOplogEntryTx(tx, entry)
	case "playlist_item":
		return applyPlaylistItemOplogEntryTx(tx, entry)
	default:
		return fmt.Errorf("unsupported entity type %q", strings.TrimSpace(entry.EntityType))
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
