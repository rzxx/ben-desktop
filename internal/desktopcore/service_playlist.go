package desktopcore

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	positionKeyWidth  = 30
	positionKeyStride = int64(1_000_000_000_000)
)

type PlaylistService struct {
	app *App
}

func (s *PlaylistService) CreatePlaylist(ctx context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaylistRecord{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return apitypes.PlaylistRecord{}, fmt.Errorf("playlist name is required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = playlistKindNormal
	}
	if kind != playlistKindNormal {
		return apitypes.PlaylistRecord{}, fmt.Errorf("playlist kind %q is not supported", kind)
	}

	now := time.Now().UTC()
	row := Playlist{
		LibraryID:  local.LibraryID,
		PlaylistID: uuid.NewString(),
		Name:       name,
		Kind:       kind,
		CreatedBy:  local.DeviceID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist", row.PlaylistID, "upsert", map[string]any{
			"playlistId": row.PlaylistID,
			"name":       row.Name,
			"kind":       row.Kind,
			"createdBy":  row.CreatedBy,
			"deleted":    false,
		})
		return err
	}); err != nil {
		return apitypes.PlaylistRecord{}, err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityPlaylists,
		QueryKey:      "playlists",
		EntityID:      row.PlaylistID,
		InvalidateAll: true,
	})
	return s.toPlaylistRecord(ctx, row)
}

func (s *PlaylistService) RenamePlaylist(ctx context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaylistRecord{}, err
	}
	playlistID = strings.TrimSpace(playlistID)
	name = strings.TrimSpace(name)
	if playlistID == "" {
		return apitypes.PlaylistRecord{}, fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return apitypes.PlaylistRecord{}, fmt.Errorf("reserved playlists are not renameable")
	}
	if name == "" {
		return apitypes.PlaylistRecord{}, fmt.Errorf("playlist name is required")
	}

	row, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistRecord{}, err
	}
	if !ok {
		return apitypes.PlaylistRecord{}, fmt.Errorf("playlist %q does not exist", playlistID)
	}
	if isReservedPlaylist(row) {
		return apitypes.PlaylistRecord{}, fmt.Errorf("reserved playlists are not renameable")
	}
	row.Name = name
	row.UpdatedAt = time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&Playlist{}).
			Where("library_id = ? AND playlist_id = ?", local.LibraryID, playlistID).
			Updates(map[string]any{"name": row.Name, "updated_at": row.UpdatedAt}).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist", row.PlaylistID, "upsert", map[string]any{
			"playlistId": row.PlaylistID,
			"name":       row.Name,
			"kind":       row.Kind,
			"createdBy":  row.CreatedBy,
			"deleted":    false,
		})
		return err
	}); err != nil {
		return apitypes.PlaylistRecord{}, err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityPlaylists,
		QueryKey:      "playlists",
		EntityID:      playlistID,
		InvalidateAll: true,
	})
	return s.toPlaylistRecord(ctx, row)
}

func (s *PlaylistService) DeletePlaylist(ctx context.Context, playlistID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return fmt.Errorf("reserved playlists are not deletable")
	}
	row, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("playlist %q does not exist", playlistID)
	}
	if isReservedPlaylist(row) {
		return fmt.Errorf("reserved playlists are not deletable")
	}
	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&Playlist{}).
			Where("library_id = ? AND playlist_id = ?", local.LibraryID, playlistID).
			Updates(map[string]any{"deleted_at": &now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := s.app.deleteArtworkScopeTx(tx, local, "playlist", playlistID); err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist", playlistID, "delete", map[string]any{
			"playlistId": playlistID,
			"deletedAt":  now,
		})
		return err
	}); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityPlaylists,
		QueryKey:      "playlists",
		EntityID:      playlistID,
		InvalidateAll: true,
	})
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateBase,
		Entity:   apitypes.CatalogChangeEntityPlaylistTracks,
		QueryKey: "playlistTracks:" + playlistID,
		EntityID: playlistID,
	})
	return nil
}

func (s *PlaylistService) AddPlaylistItem(ctx context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	playlistID := strings.TrimSpace(req.PlaylistID)
	recordingID := firstNonEmpty(strings.TrimSpace(req.LibraryRecordingID), strings.TrimSpace(req.RecordingID))
	if playlistID == "" {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("reserved playlists are not editable")
	}
	if recordingID == "" {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("recording id is required")
	}
	playlist, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	if !ok {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("playlist %q does not exist", playlistID)
	}
	libraryRecordingID, ok, err := s.resolvePlaylistLibraryRecordingID(ctx, local.LibraryID, recordingID)
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	if !ok {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("recording %q does not exist", recordingID)
	}
	if exists, err := s.recordingExists(ctx, local.LibraryID, libraryRecordingID); err != nil {
		return apitypes.PlaylistItemRecord{}, err
	} else if !exists {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("recording %q does not exist", libraryRecordingID)
	}
	if isReservedPlaylist(playlist) {
		if err := s.LikeRecording(ctx, libraryRecordingID); err != nil {
			return apitypes.PlaylistItemRecord{}, err
		}
		item, ok, err := s.playlistItemByLibraryRecordingID(ctx, local.LibraryID, playlistID, libraryRecordingID)
		if err != nil {
			return apitypes.PlaylistItemRecord{}, err
		}
		if !ok {
			return apitypes.PlaylistItemRecord{}, fmt.Errorf("liked recording %q was not materialized", recordingID)
		}
		return toPlaylistItemRecord(item), nil
	}

	var item PlaylistItem
	now := time.Now().UTC()
	err = s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		positionKey, err := playlistItemPositionKeyTx(tx, local.LibraryID, playlistID, req.AfterItemID, req.BeforeItemID, "")
		if err != nil {
			return err
		}
		item = PlaylistItem{
			LibraryID:      local.LibraryID,
			PlaylistID:     playlistID,
			ItemID:         uuid.NewString(),
			TrackVariantID: libraryRecordingID,
			AddedAt:        now,
			UpdatedAt:      now,
			PositionKey:    positionKey,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		_, err = s.app.appendLocalOplogTx(tx, local, "playlist_item", item.ItemID, "upsert", map[string]any{
			"playlistId":   item.PlaylistID,
			"itemId":       item.ItemID,
			"recordingId":  item.TrackVariantID,
			"positionKey":  item.PositionKey,
			"afterItemId":  strings.TrimSpace(req.AfterItemID),
			"beforeItemId": strings.TrimSpace(req.BeforeItemID),
			"deleted":      false,
		})
		return err
	})
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	s.emitPlaylistMutationEvents(playlistID, libraryRecordingID, isReservedPlaylist(playlist))
	return toPlaylistItemRecord(item), nil
}

func (s *PlaylistService) MovePlaylistItem(ctx context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	playlistID := strings.TrimSpace(req.PlaylistID)
	itemID := strings.TrimSpace(req.ItemID)
	if playlistID == "" || itemID == "" {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("playlist id and item id are required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("reserved playlists are not reorderable")
	}
	playlist, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	if !ok {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("playlist %q does not exist", playlistID)
	}
	if isReservedPlaylist(playlist) {
		return apitypes.PlaylistItemRecord{}, fmt.Errorf("reserved playlists are not reorderable")
	}

	var item PlaylistItem
	err = s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		current, ok, err := playlistItemByIDTx(tx, local.LibraryID, playlistID, itemID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("playlist item %q does not exist", itemID)
		}
		positionKey, err := playlistItemPositionKeyTx(tx, local.LibraryID, playlistID, req.AfterItemID, req.BeforeItemID, itemID)
		if err != nil {
			return err
		}
		current.PositionKey = positionKey
		current.UpdatedAt = time.Now().UTC()
		if err := tx.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND item_id = ?", local.LibraryID, playlistID, itemID).
			Updates(map[string]any{"position_key": current.PositionKey, "updated_at": current.UpdatedAt}).Error; err != nil {
			return err
		}
		item = current
		_, err = s.app.appendLocalOplogTx(tx, local, "playlist_item", item.ItemID, "move", map[string]any{
			"playlistId":   item.PlaylistID,
			"itemId":       item.ItemID,
			"recordingId":  item.TrackVariantID,
			"positionKey":  item.PositionKey,
			"afterItemId":  strings.TrimSpace(req.AfterItemID),
			"beforeItemId": strings.TrimSpace(req.BeforeItemID),
		})
		return err
	})
	if err != nil {
		return apitypes.PlaylistItemRecord{}, err
	}
	s.emitPlaylistMutationEvents(playlistID, item.TrackVariantID, false)
	return toPlaylistItemRecord(item), nil
}

func (s *PlaylistService) RemovePlaylistItem(ctx context.Context, playlistID, itemID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	playlistID = strings.TrimSpace(playlistID)
	itemID = strings.TrimSpace(itemID)
	if playlistID == "" {
		return fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return fmt.Errorf("reserved playlists are not editable")
	}
	if itemID == "" {
		return fmt.Errorf("item id is required")
	}
	playlist, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("playlist %q does not exist", playlistID)
	}
	item, ok, err := s.playlistItemByID(ctx, local.LibraryID, playlistID, itemID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("playlist item %q does not exist", itemID)
	}
	if isReservedPlaylist(playlist) {
		return s.UnlikeRecording(ctx, item.TrackVariantID)
	}
	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND item_id = ?", local.LibraryID, playlistID, itemID).
			Updates(map[string]any{"deleted_at": &now, "updated_at": now}).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist_item", item.ItemID, "delete", map[string]any{
			"playlistId":  item.PlaylistID,
			"itemId":      item.ItemID,
			"recordingId": item.TrackVariantID,
			"deletedAt":   now,
		})
		return err
	}); err != nil {
		return err
	}
	s.emitPlaylistMutationEvents(playlistID, item.TrackVariantID, false)
	return nil
}

func (s *PlaylistService) GetPlaylistCover(ctx context.Context, playlistID string) (apitypes.PlaylistCoverRecord, bool, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, false, err
	}
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return apitypes.PlaylistCoverRecord{}, false, fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return apitypes.PlaylistCoverRecord{
			PlaylistID: strings.TrimSpace(playlistID),
		}, false, nil
	}
	playlist, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, false, err
	}
	if !ok {
		return apitypes.PlaylistCoverRecord{}, false, fmt.Errorf("playlist %q does not exist", playlistID)
	}
	if isReservedPlaylist(playlist) {
		return apitypes.PlaylistCoverRecord{
			PlaylistID: strings.TrimSpace(playlistID),
			UpdatedAt:  playlist.UpdatedAt,
		}, false, nil
	}
	record, found, err := s.loadPlaylistCoverRecord(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, false, err
	}
	if !record.UpdatedAt.IsZero() {
		return record, found, nil
	}
	record.UpdatedAt = playlist.UpdatedAt
	return record, found, nil
}

func (s *PlaylistService) SetPlaylistCover(ctx context.Context, req apitypes.PlaylistCoverUploadRequest) (apitypes.PlaylistCoverRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, err
	}
	playlistID := strings.TrimSpace(req.PlaylistID)
	if playlistID == "" {
		return apitypes.PlaylistCoverRecord{}, fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return apitypes.PlaylistCoverRecord{}, fmt.Errorf("reserved playlists do not support custom covers")
	}
	sourcePath := filepath.Clean(strings.TrimSpace(req.SourcePath))
	if sourcePath == "" {
		return apitypes.PlaylistCoverRecord{}, fmt.Errorf("artwork source path is required")
	}

	playlist, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, err
	}
	if !ok {
		return apitypes.PlaylistCoverRecord{}, fmt.Errorf("playlist %q does not exist", playlistID)
	}
	if isReservedPlaylist(playlist) {
		return apitypes.PlaylistCoverRecord{}, fmt.Errorf("reserved playlists do not support custom covers")
	}

	built, err := s.app.artwork.buildArtworkFromImagePath(ctx, sourcePath)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, err
	}

	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.app.deleteArtworkScopeTx(tx, local, "playlist", playlistID); err != nil {
			return err
		}
		for _, variant := range built.Variants {
			blobID, err := s.app.blobs.StoreArtworkBytes(variant.Bytes, variant.FileExt)
			if err != nil {
				return err
			}
			if err := s.app.upsertArtworkVariantTx(tx, local, ArtworkVariant{
				LibraryID:       local.LibraryID,
				ScopeType:       "playlist",
				ScopeID:         playlistID,
				Variant:         strings.TrimSpace(variant.Variant),
				BlobID:          blobID,
				MIME:            strings.TrimSpace(variant.MIME),
				FileExt:         normalizeArtworkFileExt(variant.FileExt, variant.MIME),
				W:               variant.W,
				H:               variant.H,
				Bytes:           int64(len(variant.Bytes)),
				ChosenSource:    strings.TrimSpace(built.SourceKind),
				ChosenSourceRef: strings.TrimSpace(built.SourceRef),
				UpdatedAt:       now,
			}); err != nil {
				return err
			}
		}
		if err := tx.Model(&Playlist{}).
			Where("library_id = ? AND playlist_id = ?", local.LibraryID, playlistID).
			Update("updated_at", now).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist", playlistID, "upsert", map[string]any{
			"playlistId": playlistID,
			"name":       playlist.Name,
			"kind":       playlist.Kind,
			"createdBy":  playlist.CreatedBy,
			"deleted":    false,
		})
		return err
	}); err != nil {
		return apitypes.PlaylistCoverRecord{}, err
	}
	s.emitPlaylistCoverMutationEvents(playlistID)

	record, _, err := s.loadPlaylistCoverRecord(ctx, local.LibraryID, playlistID)
	if err != nil {
		return apitypes.PlaylistCoverRecord{}, err
	}
	return record, nil
}

func (s *PlaylistService) ClearPlaylistCover(ctx context.Context, playlistID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return fmt.Errorf("playlist id is required")
	}
	if s.app.offline != nil && s.app.offline.matchesPlaylistID(local, playlistID) {
		return fmt.Errorf("reserved playlists do not support custom covers")
	}
	playlist, ok, err := s.playlistByID(ctx, local.LibraryID, playlistID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("playlist %q does not exist", playlistID)
	}
	if isReservedPlaylist(playlist) {
		return fmt.Errorf("reserved playlists do not support custom covers")
	}

	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.app.deleteArtworkScopeTx(tx, local, "playlist", playlistID); err != nil {
			return err
		}
		if err := tx.Model(&Playlist{}).
			Where("library_id = ? AND playlist_id = ?", local.LibraryID, playlistID).
			Update("updated_at", now).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist", playlistID, "upsert", map[string]any{
			"playlistId": playlistID,
			"name":       playlist.Name,
			"kind":       playlist.Kind,
			"createdBy":  playlist.CreatedBy,
			"deleted":    false,
		})
		return err
	}); err != nil {
		return err
	}
	s.emitPlaylistCoverMutationEvents(playlistID)
	return nil
}

func (s *PlaylistService) LikeRecording(ctx context.Context, recordingID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return fmt.Errorf("recording id is required")
	}
	libraryRecordingID, ok, err := s.resolvePlaylistLibraryRecordingID(ctx, local.LibraryID, recordingID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("recording %q does not exist", recordingID)
	}
	if exists, err := s.recordingExists(ctx, local.LibraryID, libraryRecordingID); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("recording %q does not exist", libraryRecordingID)
	}

	likedPlaylistID := likedPlaylistIDForLibrary(local.LibraryID)
	active, err := s.isRecordingLikedInLibrary(ctx, local.LibraryID, libraryRecordingID)
	if err != nil {
		return err
	}
	if active {
		return nil
	}

	now := time.Now().UTC()
	item := PlaylistItem{
		LibraryID:      local.LibraryID,
		PlaylistID:     likedPlaylistID,
		ItemID:         likedItemID(likedPlaylistID, libraryRecordingID),
		TrackVariantID: libraryRecordingID,
		AddedAt:        now,
		UpdatedAt:      now,
		PositionKey:    defaultPositionKey(),
	}
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := ensureLikedPlaylistTx(tx, local.LibraryID, local.DeviceID, now); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "library_id"},
				{Name: "playlist_id"},
				{Name: "item_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"track_variant_id": item.TrackVariantID,
				"added_at":         item.AddedAt,
				"updated_at":       item.UpdatedAt,
				"position_key":     item.PositionKey,
				"deleted_at":       nil,
			}),
		}).Create(&item).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist_item", item.ItemID, "upsert", map[string]any{
			"playlistId":  item.PlaylistID,
			"itemId":      item.ItemID,
			"recordingId": item.TrackVariantID,
			"positionKey": item.PositionKey,
			"liked":       true,
			"deleted":     false,
		})
		return err
	}); err != nil {
		return err
	}
	s.emitPlaylistMutationEvents(likedPlaylistID, libraryRecordingID, true)
	return nil
}

func (s *PlaylistService) UnlikeRecording(ctx context.Context, recordingID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return fmt.Errorf("recording id is required")
	}
	libraryRecordingID, ok, err := s.resolvePlaylistLibraryRecordingID(ctx, local.LibraryID, recordingID)
	if err != nil {
		return err
	}
	if !ok {
		if err := s.ensureLikedPlaylist(ctx, local.LibraryID, local.DeviceID); err != nil {
			return err
		}
		return nil
	}
	item, ok, err := s.playlistItemByLibraryRecordingID(ctx, local.LibraryID, likedPlaylistIDForLibrary(local.LibraryID), libraryRecordingID)
	if err != nil {
		return err
	}
	if !ok {
		if err := s.ensureLikedPlaylist(ctx, local.LibraryID, local.DeviceID); err != nil {
			return err
		}
		return nil
	}
	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND item_id = ?", local.LibraryID, item.PlaylistID, item.ItemID).
			Updates(map[string]any{"deleted_at": &now, "updated_at": now}).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, "playlist_item", item.ItemID, "delete", map[string]any{
			"playlistId":  item.PlaylistID,
			"itemId":      item.ItemID,
			"recordingId": item.TrackVariantID,
			"liked":       true,
			"deletedAt":   now,
		})
		return err
	}); err != nil {
		return err
	}
	s.emitPlaylistMutationEvents(item.PlaylistID, item.TrackVariantID, true)
	return nil
}

func (s *PlaylistService) IsRecordingLiked(ctx context.Context, recordingID string) (bool, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return false, err
	}
	libraryRecordingID, ok, err := s.resolvePlaylistLibraryRecordingID(ctx, local.LibraryID, recordingID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return s.isRecordingLikedInLibrary(ctx, local.LibraryID, libraryRecordingID)
}

func (s *PlaylistService) emitPlaylistMutationEvents(playlistID, recordingID string, liked bool) {
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateBase,
		Entity:   apitypes.CatalogChangeEntityPlaylists,
		QueryKey: "playlists",
		EntityID: strings.TrimSpace(playlistID),
	})
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:         apitypes.CatalogChangeInvalidateBase,
		Entity:       apitypes.CatalogChangeEntityPlaylistTracks,
		QueryKey:     "playlistTracks:" + strings.TrimSpace(playlistID),
		EntityID:     strings.TrimSpace(playlistID),
		RecordingIDs: []string{strings.TrimSpace(recordingID)},
	})
	if liked {
		s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
			Kind:         apitypes.CatalogChangeInvalidateBase,
			Entity:       apitypes.CatalogChangeEntityLiked,
			QueryKey:     "liked",
			EntityID:     strings.TrimSpace(playlistID),
			RecordingIDs: []string{strings.TrimSpace(recordingID)},
		})
	}
}

func (s *PlaylistService) emitPlaylistCoverMutationEvents(playlistID string) {
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:     apitypes.CatalogChangeInvalidateBase,
		Entity:   apitypes.CatalogChangeEntityPlaylists,
		QueryKey: "playlists",
		EntityID: strings.TrimSpace(playlistID),
	})
}

func (s *PlaylistService) toPlaylistRecord(ctx context.Context, row Playlist) (apitypes.PlaylistRecord, error) {
	record := apitypes.PlaylistRecord{
		LibraryID:  strings.TrimSpace(row.LibraryID),
		PlaylistID: strings.TrimSpace(row.PlaylistID),
		Name:       strings.TrimSpace(row.Name),
		Kind:       apitypes.PlaylistKind(strings.TrimSpace(row.Kind)),
		IsReserved: isReservedPlaylist(row),
		CreatedBy:  strings.TrimSpace(row.CreatedBy),
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
	if record.IsReserved {
		return record, nil
	}
	thumb, ok, err := s.app.catalog.loadPlaylistArtworkRef(ctx, row.LibraryID, row.PlaylistID)
	if err != nil {
		return apitypes.PlaylistRecord{}, err
	}
	record.Thumb = thumb
	record.HasCustomCover = ok
	return record, nil
}

func (s *PlaylistService) loadPlaylistCoverRecord(ctx context.Context, libraryID, playlistID string) (apitypes.PlaylistCoverRecord, bool, error) {
	var rows []ArtworkVariant
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND scope_type = ? AND scope_id = ?", libraryID, "playlist", playlistID).
		Order("variant ASC").
		Find(&rows).Error; err != nil {
		return apitypes.PlaylistCoverRecord{}, false, err
	}
	record := apitypes.PlaylistCoverRecord{
		PlaylistID: strings.TrimSpace(playlistID),
	}
	if len(rows) == 0 {
		return record, false, nil
	}

	record.HasCustomCover = true
	record.Variants = make([]apitypes.PlaylistCoverVariant, 0, len(rows))
	for _, row := range rows {
		record.Variants = append(record.Variants, apitypes.PlaylistCoverVariant{
			Variant: strings.TrimSpace(row.Variant),
			BlobID:  strings.TrimSpace(row.BlobID),
			MIME:    strings.TrimSpace(row.MIME),
			FileExt: normalizeArtworkFileExt(row.FileExt, row.MIME),
			W:       row.W,
			H:       row.H,
			Bytes:   row.Bytes,
		})
		if row.Variant == defaultArtworkVariant320 {
			record.Thumb = artworkRefFromRow(row)
		}
		if row.UpdatedAt.After(record.UpdatedAt) {
			record.UpdatedAt = row.UpdatedAt
		}
	}
	return record, true, nil
}

func (s *PlaylistService) playlistByID(ctx context.Context, libraryID, playlistID string) (Playlist, bool, error) {
	var row Playlist
	err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", libraryID, playlistID).
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return Playlist{}, false, nil
	}
	if err != nil {
		return Playlist{}, false, err
	}
	return row, true, nil
}

func (s *PlaylistService) playlistItemByID(ctx context.Context, libraryID, playlistID, itemID string) (PlaylistItem, bool, error) {
	return playlistItemByIDTx(s.app.storage.WithContext(ctx), libraryID, playlistID, itemID)
}

func (s *PlaylistService) playlistItemByLibraryRecordingID(ctx context.Context, libraryID, playlistID, recordingID string) (PlaylistItem, bool, error) {
	var row PlaylistItem
	err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ? AND track_variant_id = ? AND deleted_at IS NULL", libraryID, playlistID, recordingID).
		Order("updated_at DESC, added_at DESC, item_id DESC").
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return PlaylistItem{}, false, nil
	}
	if err != nil {
		return PlaylistItem{}, false, err
	}
	return row, true, nil
}

func (s *PlaylistService) recordingExists(ctx context.Context, libraryID, recordingID string) (bool, error) {
	var count int64
	if err := s.app.storage.WithContext(ctx).
		Model(&TrackVariantModel{}).
		Where("library_id = ? AND track_cluster_id = ?", libraryID, recordingID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// Playlist rows persist logical recording ids (track clusters) even though the
// legacy column name remains track_variant_id.
func resolvePlaylistLibraryRecordingIDTx(tx *gorm.DB, libraryID, recordingID string) (string, bool, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "", false, nil
	}

	var row TrackVariantModel
	if err := tx.
		Where("library_id = ? AND track_cluster_id = ?", libraryID, recordingID).
		Take(&row).Error; err == nil {
		return strings.TrimSpace(row.TrackClusterID), true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	if err := tx.
		Where("library_id = ? AND track_variant_id = ?", libraryID, recordingID).
		Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(row.TrackClusterID), true, nil
}

func (s *PlaylistService) resolvePlaylistLibraryRecordingID(ctx context.Context, libraryID, recordingID string) (string, bool, error) {
	return resolvePlaylistLibraryRecordingIDTx(s.app.storage.WithContext(ctx), libraryID, recordingID)
}

func (s *PlaylistService) ensureLikedPlaylist(ctx context.Context, libraryID, deviceID string) error {
	return s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		return ensureLikedPlaylistTx(tx, libraryID, deviceID, time.Now().UTC())
	})
}

func (s *PlaylistService) isRecordingLikedInLibrary(ctx context.Context, libraryID, recordingID string) (bool, error) {
	var count int64
	if err := s.app.storage.WithContext(ctx).
		Table("playlist_items AS pi").
		Where("pi.library_id = ? AND pi.playlist_id = ? AND pi.track_variant_id = ? AND pi.deleted_at IS NULL",
			libraryID, likedPlaylistIDForLibrary(libraryID), recordingID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func toPlaylistItemRecord(row PlaylistItem) apitypes.PlaylistItemRecord {
	return apitypes.PlaylistItemRecord{
		LibraryID:          strings.TrimSpace(row.LibraryID),
		PlaylistID:         strings.TrimSpace(row.PlaylistID),
		ItemID:             strings.TrimSpace(row.ItemID),
		LibraryRecordingID: strings.TrimSpace(row.TrackVariantID),
		RecordingID:        strings.TrimSpace(row.TrackVariantID),
		AddedAt:            row.AddedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func ensureLikedPlaylistTx(tx *gorm.DB, libraryID, deviceID string, now time.Time) error {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	if libraryID == "" {
		return fmt.Errorf("library id is required")
	}
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}
	row := Playlist{
		LibraryID:  libraryID,
		PlaylistID: likedPlaylistIDForLibrary(libraryID),
		Name:       "Liked",
		Kind:       playlistKindLiked,
		CreatedBy:  deviceID,
		CreatedAt:  now,
		UpdatedAt:  now,
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
			"deleted_at": nil,
		}),
	}).Create(&row).Error
}

func isReservedPlaylist(row Playlist) bool {
	return isReservedPlaylistKind(row.Kind)
}

func likedItemID(playlistID, libraryRecordingID string) string {
	return stableNameID("liked_item", strings.TrimSpace(playlistID)+":"+strings.TrimSpace(libraryRecordingID))
}

func playlistItemByIDTx(tx *gorm.DB, libraryID, playlistID, itemID string) (PlaylistItem, bool, error) {
	var row PlaylistItem
	err := tx.
		Where("library_id = ? AND playlist_id = ? AND item_id = ? AND deleted_at IS NULL", libraryID, playlistID, itemID).
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return PlaylistItem{}, false, nil
	}
	if err != nil {
		return PlaylistItem{}, false, err
	}
	return row, true, nil
}

func playlistItemPositionKeyTx(tx *gorm.DB, libraryID, playlistID, afterItemID, beforeItemID, movingItemID string) (string, error) {
	items, err := orderedPlaylistItemsTx(tx, libraryID, playlistID, movingItemID)
	if err != nil {
		return "", err
	}
	return playlistItemPositionKeyFromItemsTx(tx, libraryID, playlistID, items, afterItemID, beforeItemID, movingItemID, true)
}

func playlistItemPositionKeyFromItemsTx(tx *gorm.DB, libraryID, playlistID string, items []PlaylistItem, afterItemID, beforeItemID, movingItemID string, allowRebalance bool) (string, error) {
	afterItemID = strings.TrimSpace(afterItemID)
	beforeItemID = strings.TrimSpace(beforeItemID)
	movingItemID = strings.TrimSpace(movingItemID)
	if afterItemID != "" && beforeItemID != "" && afterItemID == beforeItemID {
		return "", fmt.Errorf("after and before anchors must be different")
	}
	if movingItemID != "" && (afterItemID == movingItemID || beforeItemID == movingItemID) {
		return "", fmt.Errorf("playlist item cannot be anchored relative to itself")
	}

	indexByID := make(map[string]int, len(items))
	for i, item := range items {
		indexByID[strings.TrimSpace(item.ItemID)] = i
	}

	prevIdx := -1
	nextIdx := len(items)
	if afterItemID != "" {
		idx, ok := indexByID[afterItemID]
		if !ok {
			return "", fmt.Errorf("after item %q does not exist", afterItemID)
		}
		prevIdx = idx
		nextIdx = idx + 1
	}
	if beforeItemID != "" {
		idx, ok := indexByID[beforeItemID]
		if !ok {
			return "", fmt.Errorf("before item %q does not exist", beforeItemID)
		}
		nextIdx = idx
		if afterItemID == "" {
			prevIdx = idx - 1
		}
	}
	if afterItemID != "" && beforeItemID != "" && prevIdx >= nextIdx {
		return "", fmt.Errorf("after item must sort before before item")
	}
	if afterItemID == "" && beforeItemID == "" {
		prevIdx = len(items) - 1
		nextIdx = len(items)
	}

	prevKey := ""
	nextKey := ""
	if prevIdx >= 0 {
		prevKey = items[prevIdx].PositionKey
	}
	if nextIdx >= 0 && nextIdx < len(items) {
		nextKey = items[nextIdx].PositionKey
	}
	if key, ok := midpointPositionKey(prevKey, nextKey); ok {
		return key, nil
	}
	if !allowRebalance {
		return "", fmt.Errorf("unable to allocate playlist position")
	}
	if err := rebalancePlaylistPositionKeysTx(tx, libraryID, playlistID); err != nil {
		return "", err
	}
	itemsAfter, err := orderedPlaylistItemsTx(tx, libraryID, playlistID, movingItemID)
	if err != nil {
		return "", err
	}
	return playlistItemPositionKeyFromItemsTx(tx, libraryID, playlistID, itemsAfter, afterItemID, beforeItemID, movingItemID, false)
}

func orderedPlaylistItemsTx(tx *gorm.DB, libraryID, playlistID, excludeItemID string) ([]PlaylistItem, error) {
	var items []PlaylistItem
	query := tx.Where("library_id = ? AND playlist_id = ? AND deleted_at IS NULL", libraryID, playlistID)
	if excludeItemID = strings.TrimSpace(excludeItemID); excludeItemID != "" {
		query = query.Where("item_id <> ?", excludeItemID)
	}
	if err := query.Order("position_key ASC, item_id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func rebalancePlaylistPositionKeysTx(tx *gorm.DB, libraryID, playlistID string) error {
	items, err := orderedPlaylistItemsTx(tx, libraryID, playlistID, "")
	if err != nil {
		return err
	}
	for i, item := range items {
		key := positionKeyForIndex(i + 1)
		if err := tx.Model(&PlaylistItem{}).
			Where("library_id = ? AND playlist_id = ? AND item_id = ?", libraryID, playlistID, item.ItemID).
			Update("position_key", key).Error; err != nil {
			return err
		}
	}
	return nil
}

func defaultPositionKey() string {
	return positionKeyForIndex(1)
}

func positionKeyForIndex(index int) string {
	if index < 1 {
		index = 1
	}
	value := big.NewInt(int64(index))
	value.Mul(value, big.NewInt(positionKeyStride))
	return formatPositionKey(value)
}

func midpointPositionKey(prevKey, nextKey string) (string, bool) {
	prev, ok := parsePositionKey(prevKey)
	if !ok {
		prev = big.NewInt(0)
	}
	next := maxPositionKeyValue()
	if strings.TrimSpace(nextKey) != "" {
		parsed, ok := parsePositionKey(nextKey)
		if !ok {
			return "", false
		}
		next = parsed
	}
	if next.Cmp(prev) <= 0 {
		return "", false
	}
	diff := new(big.Int).Sub(next, prev)
	if diff.Cmp(big.NewInt(1)) <= 0 {
		return "", false
	}
	half := new(big.Int).Div(diff, big.NewInt(2))
	mid := new(big.Int).Add(prev, half)
	if mid.Cmp(prev) <= 0 || mid.Cmp(next) >= 0 {
		return "", false
	}
	return formatPositionKey(mid), true
}

func parsePositionKey(v string) (*big.Int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, false
	}
	n, ok := new(big.Int).SetString(v, 10)
	return n, ok
}

func formatPositionKey(v *big.Int) string {
	s := strings.TrimSpace(v.String())
	if len(s) >= positionKeyWidth {
		return s
	}
	return strings.Repeat("0", positionKeyWidth-len(s)) + s
}

func maxPositionKeyValue() *big.Int {
	max := new(big.Int)
	max.Exp(big.NewInt(10), big.NewInt(positionKeyWidth), nil)
	max.Sub(max, big.NewInt(1))
	return max
}
