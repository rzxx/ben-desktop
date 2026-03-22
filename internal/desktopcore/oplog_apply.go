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
		return nil
	case entityTypeSourceFile:
		return applySourceFileOplogEntryTx(tx, entry)
	case entityTypePlaylist:
		return applyPlaylistOplogEntryTx(tx, entry)
	case entityTypePlaylistItem:
		return applyPlaylistItemOplogEntryTx(tx, entry)
	case entityTypeDeviceVariantPreference:
		return applyDeviceVariantPreferenceOplogEntryTx(tx, entry)
	case entityTypeOfflinePin:
		return applyOfflinePinOplogEntryTx(tx, entry)
	case entityTypeOptimizedAsset:
		return applyOptimizedAssetOplogEntryTx(tx, entry)
	case entityTypeDeviceAssetCache:
		return applyDeviceAssetCacheOplogEntryTx(tx, entry)
	case entityTypeArtworkVariant:
		return applyArtworkVariantOplogEntryTx(tx, entry)
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
			LibraryID:       entry.LibraryID,
			DeviceID:        payload.DeviceID,
			Path:            "",
			MTimeNS:         payload.MTimeNS,
			SizeBytes:       payload.SizeBytes,
			HashAlgo:        payload.HashAlgo,
			HashHex:         payload.HashHex,
			SourceFileID:    payload.SourceFileID,
			EditionScopeKey: payload.EditionScopeKey,
			Tags:            payload.Tags,
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

func applyDeviceVariantPreferenceOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}
	if strings.TrimSpace(entry.OpKind) != "upsert" {
		return fmt.Errorf("unsupported device variant preference op kind %q", strings.TrimSpace(entry.OpKind))
	}

	var payload deviceVariantPreferenceOplogPayload
	if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode device variant preference payload: %w", err)
	}
	deviceID, scopeType, clusterID := deviceVariantPreferenceIdentityForEntry(entry, payload)
	if deviceID == "" || scopeType == "" || clusterID == "" {
		return fmt.Errorf("device variant preference identity is required")
	}
	row := DeviceVariantPreference{
		LibraryID:       entry.LibraryID,
		DeviceID:        deviceID,
		ScopeType:       scopeType,
		ClusterID:       clusterID,
		ChosenVariantID: strings.TrimSpace(payload.ChosenVariantID),
		UpdatedAt:       oplogPayloadTime(payload.UpdatedAtNS, entry),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
			{Name: "scope_type"},
			{Name: "cluster_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"chosen_variant_id", "updated_at"}),
	}).Create(&row).Error
}

func applyOfflinePinOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	switch strings.TrimSpace(entry.OpKind) {
	case "upsert":
		var payload offlinePinOplogPayload
		if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode offline pin payload: %w", err)
		}
		deviceID, scope, scopeID := offlinePinIdentityForEntry(entry, payload)
		if deviceID == "" || scope == "" || scopeID == "" {
			return fmt.Errorf("offline pin identity is required")
		}
		row := OfflinePin{
			LibraryID: entry.LibraryID,
			DeviceID:  deviceID,
			Scope:     scope,
			ScopeID:   scopeID,
			Profile:   strings.TrimSpace(payload.Profile),
			CreatedAt: oplogPayloadTime(payload.UpdatedAtNS, entry),
			UpdatedAt: oplogPayloadTime(payload.UpdatedAtNS, entry),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "device_id"},
				{Name: "scope"},
				{Name: "scope_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"profile", "updated_at"}),
		}).Create(&row).Error
	case "delete":
		var payload offlinePinOplogPayload
		if strings.TrimSpace(entry.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode offline pin delete payload: %w", err)
			}
		}
		deviceID, scope, scopeID := offlinePinIdentityForEntry(entry, payload)
		if deviceID == "" || scope == "" || scopeID == "" {
			return fmt.Errorf("offline pin identity is required")
		}
		return tx.Where("library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?", entry.LibraryID, deviceID, scope, scopeID).
			Delete(&OfflinePin{}).Error
	default:
		return fmt.Errorf("unsupported offline pin op kind %q", strings.TrimSpace(entry.OpKind))
	}
}

func applyOptimizedAssetOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	switch strings.TrimSpace(entry.OpKind) {
	case "upsert":
		var payload optimizedAssetOplogPayload
		if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode optimized asset payload: %w", err)
		}
		optimizedAssetID := firstNonEmpty(payload.OptimizedAssetID, entry.EntityID)
		if optimizedAssetID == "" {
			return fmt.Errorf("optimized asset id is required")
		}
		row := OptimizedAssetModel{
			LibraryID:         entry.LibraryID,
			OptimizedAssetID:  optimizedAssetID,
			SourceFileID:      strings.TrimSpace(payload.SourceFileID),
			TrackVariantID:    strings.TrimSpace(payload.TrackVariantID),
			Profile:           strings.TrimSpace(payload.Profile),
			BlobID:            strings.TrimSpace(payload.BlobID),
			MIME:              strings.TrimSpace(payload.MIME),
			DurationMS:        payload.DurationMS,
			Bitrate:           payload.Bitrate,
			Codec:             strings.TrimSpace(payload.Codec),
			Container:         strings.TrimSpace(payload.Container),
			CreatedByDeviceID: strings.TrimSpace(payload.CreatedByDeviceID),
			CreatedAt:         oplogPayloadTime(payload.CreatedAtNS, entry),
			UpdatedAt:         oplogPayloadTime(payload.UpdatedAtNS, entry),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "optimized_asset_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"source_file_id", "track_variant_id", "profile", "blob_id", "mime", "duration_ms", "bitrate", "codec", "container", "created_by_device_id", "updated_at"}),
		}).Create(&row).Error
	case "delete":
		var payload optimizedAssetDeleteOplogPayload
		if strings.TrimSpace(entry.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode optimized asset delete payload: %w", err)
			}
		}
		optimizedAssetID := firstNonEmpty(payload.OptimizedAssetID, entry.EntityID)
		if optimizedAssetID == "" {
			return fmt.Errorf("optimized asset id is required")
		}
		if err := tx.Where("library_id = ? AND optimized_asset_id = ?", entry.LibraryID, optimizedAssetID).Delete(&DeviceAssetCacheModel{}).Error; err != nil {
			return err
		}
		return tx.Where("library_id = ? AND optimized_asset_id = ?", entry.LibraryID, optimizedAssetID).Delete(&OptimizedAssetModel{}).Error
	default:
		return fmt.Errorf("unsupported optimized asset op kind %q", strings.TrimSpace(entry.OpKind))
	}
}

func applyDeviceAssetCacheOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}
	if strings.TrimSpace(entry.OpKind) != "upsert" {
		return fmt.Errorf("unsupported device asset cache op kind %q", strings.TrimSpace(entry.OpKind))
	}

	var payload deviceAssetCacheOplogPayload
	if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode device asset cache payload: %w", err)
	}
	deviceID, optimizedAssetID := deviceAssetCacheIdentityForEntry(entry, payload)
	if deviceID == "" || optimizedAssetID == "" {
		return fmt.Errorf("device asset cache identity is required")
	}
	var lastVerifiedAt *time.Time
	if payload.HasLastVerifiedAt {
		at := oplogPayloadTime(payload.LastVerifiedAtNS, entry)
		lastVerifiedAt = &at
	}
	row := DeviceAssetCacheModel{
		LibraryID:        entry.LibraryID,
		DeviceID:         deviceID,
		OptimizedAssetID: optimizedAssetID,
		IsCached:         payload.IsCached,
		LastVerifiedAt:   lastVerifiedAt,
		UpdatedAt:        oplogPayloadTime(payload.UpdatedAtNS, entry),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
			{Name: "optimized_asset_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"is_cached", "last_verified_at", "updated_at"}),
	}).Create(&row).Error
}

func applyArtworkVariantOplogEntryTx(tx *gorm.DB, entry OplogEntry) error {
	apply, err := shouldApplyLatestMutationTx(tx, entry)
	if err != nil || !apply {
		return err
	}

	switch strings.TrimSpace(entry.OpKind) {
	case "upsert":
		var payload artworkVariantOplogPayload
		if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode artwork variant payload: %w", err)
		}
		scopeType, scopeID, variant := artworkVariantIdentityForEntry(entry, payload.ScopeType, payload.ScopeID, payload.Variant)
		if scopeType == "" || scopeID == "" || variant == "" {
			return fmt.Errorf("artwork variant identity is required")
		}
		row := ArtworkVariant{
			LibraryID:       entry.LibraryID,
			ScopeType:       scopeType,
			ScopeID:         scopeID,
			Variant:         variant,
			BlobID:          strings.TrimSpace(payload.BlobID),
			MIME:            strings.TrimSpace(payload.MIME),
			FileExt:         normalizeArtworkFileExt(payload.FileExt, payload.MIME),
			W:               payload.W,
			H:               payload.H,
			Bytes:           payload.Bytes,
			ChosenSource:    strings.TrimSpace(payload.ChosenSource),
			ChosenSourceRef: "",
			UpdatedAt:       oplogPayloadTime(payload.UpdatedAtNS, entry),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "scope_type"},
				{Name: "scope_id"},
				{Name: "variant"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"blob_id", "mime", "file_ext", "w", "h", "bytes", "chosen_source", "chosen_source_ref", "updated_at"}),
		}).Create(&row).Error
	case "delete":
		var payload artworkVariantDeleteOplogPayload
		if strings.TrimSpace(entry.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(entry.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode artwork variant delete payload: %w", err)
			}
		}
		scopeType, scopeID, variant := artworkVariantIdentityForEntry(entry, payload.ScopeType, payload.ScopeID, payload.Variant)
		if scopeType == "" || scopeID == "" || variant == "" {
			return fmt.Errorf("artwork variant identity is required")
		}
		return tx.Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", entry.LibraryID, scopeType, scopeID, variant).
			Delete(&ArtworkVariant{}).Error
	default:
		return fmt.Errorf("unsupported artwork variant op kind %q", strings.TrimSpace(entry.OpKind))
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

func oplogPayloadTime(payloadNS int64, entry OplogEntry) time.Time {
	if payloadNS > 0 {
		return time.Unix(0, payloadNS).UTC()
	}
	return oplogMutationTime(entry)
}

func deviceVariantPreferenceIdentityForEntry(entry OplogEntry, payload deviceVariantPreferenceOplogPayload) (string, string, string) {
	deviceID := strings.TrimSpace(payload.DeviceID)
	scopeType := strings.TrimSpace(payload.ScopeType)
	clusterID := strings.TrimSpace(payload.ClusterID)
	parts := strings.SplitN(strings.TrimSpace(entry.EntityID), ":", 3)
	if len(parts) == 3 {
		if deviceID == "" {
			deviceID = strings.TrimSpace(parts[0])
		}
		if scopeType == "" {
			scopeType = strings.TrimSpace(parts[1])
		}
		if clusterID == "" {
			clusterID = strings.TrimSpace(parts[2])
		}
	}
	return deviceID, scopeType, clusterID
}

func offlinePinIdentityForEntry(entry OplogEntry, payload offlinePinOplogPayload) (string, string, string) {
	deviceID := strings.TrimSpace(payload.DeviceID)
	scope := strings.TrimSpace(payload.Scope)
	scopeID := strings.TrimSpace(payload.ScopeID)
	parts := strings.SplitN(strings.TrimSpace(entry.EntityID), ":", 3)
	if len(parts) == 3 {
		if deviceID == "" {
			deviceID = strings.TrimSpace(parts[0])
		}
		if scope == "" {
			scope = strings.TrimSpace(parts[1])
		}
		if scopeID == "" {
			scopeID = strings.TrimSpace(parts[2])
		}
	}
	return deviceID, scope, scopeID
}

func deviceAssetCacheIdentityForEntry(entry OplogEntry, payload deviceAssetCacheOplogPayload) (string, string) {
	deviceID := strings.TrimSpace(payload.DeviceID)
	optimizedAssetID := strings.TrimSpace(payload.OptimizedAssetID)
	parts := strings.SplitN(strings.TrimSpace(entry.EntityID), ":", 2)
	if len(parts) == 2 {
		if deviceID == "" {
			deviceID = strings.TrimSpace(parts[0])
		}
		if optimizedAssetID == "" {
			optimizedAssetID = strings.TrimSpace(parts[1])
		}
	}
	return deviceID, optimizedAssetID
}

func artworkVariantIdentityForEntry(entry OplogEntry, scopeType, scopeID, variant string) (string, string, string) {
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	variant = strings.TrimSpace(variant)
	parts := strings.SplitN(strings.TrimSpace(entry.EntityID), ":", 3)
	if len(parts) == 3 {
		if scopeType == "" {
			scopeType = strings.TrimSpace(parts[0])
		}
		if scopeID == "" {
			scopeID = strings.TrimSpace(parts[1])
		}
		if variant == "" {
			variant = strings.TrimSpace(parts[2])
		}
	}
	return scopeType, scopeID, variant
}
