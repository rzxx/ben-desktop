package desktopcore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	localSettingPlaylistCoverEpoch = "playlist_cover_epoch"
	playlistCoverEpoch             = "1"
)

type artworkBlobRecord struct {
	ScopeType string
	ScopeID   string
	Variant   string
	BlobID    string
	MIME      string
	FileExt   string
	UpdatedAt time.Time
}

type legacyPlaylistCoverGroup struct {
	libraryID  string
	playlistID string
	rows       []ArtworkVariant
}

func playlistCoverRefFromRow(row PlaylistCover) apitypes.ArtworkRef {
	if strings.TrimSpace(row.BlobID) == "" {
		return apitypes.ArtworkRef{}
	}
	return apitypes.ArtworkRef{
		BlobID:  strings.TrimSpace(row.BlobID),
		MIME:    strings.TrimSpace(row.MIME),
		FileExt: normalizeArtworkFileExt(row.FileExt, row.MIME),
		Variant: playlistCoverVariantCanonical,
		Width:   row.W,
		Height:  row.H,
		Bytes:   row.Bytes,
	}
}

func playlistCoverBlobRecordFromRow(row PlaylistCover) artworkBlobRecord {
	return artworkBlobRecord{
		ScopeType: "playlist",
		ScopeID:   strings.TrimSpace(row.PlaylistID),
		Variant:   playlistCoverVariantCanonical,
		BlobID:    strings.TrimSpace(row.BlobID),
		MIME:      strings.TrimSpace(row.MIME),
		FileExt:   normalizeArtworkFileExt(row.FileExt, row.MIME),
		UpdatedAt: row.UpdatedAt,
	}
}

func (a *App) loadPlaylistCoverRow(ctx context.Context, libraryID, playlistID string) (PlaylistCover, bool, error) {
	var row PlaylistCover
	err := a.storage.WithContext(ctx).
		Where("library_id = ? AND playlist_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(playlistID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return PlaylistCover{}, false, nil
		}
		return PlaylistCover{}, false, err
	}
	return row, true, nil
}

func chooseLegacyPlaylistCover(rows []ArtworkVariant) (ArtworkVariant, bool) {
	var best ArtworkVariant
	hasBest := false
	for _, row := range rows {
		if !hasBest || legacyPlaylistCoverBetter(row, best) {
			best = row
			hasBest = true
		}
	}
	return best, hasBest
}

func legacyPlaylistCoverBetter(left, right ArtworkVariant) bool {
	leftArea := int64(left.W) * int64(left.H)
	rightArea := int64(right.W) * int64(right.H)
	if leftArea != rightArea {
		return leftArea > rightArea
	}
	if left.Bytes != right.Bytes {
		return left.Bytes > right.Bytes
	}
	if left.UpdatedAt.Equal(right.UpdatedAt) {
		return strings.TrimSpace(left.Variant) > strings.TrimSpace(right.Variant)
	}
	return left.UpdatedAt.After(right.UpdatedAt)
}

func (a *App) runPlaylistCoverMigration(ctx context.Context) error {
	if a == nil || a.storage == nil {
		return nil
	}

	var setting LocalSetting
	err := a.storage.WithContext(ctx).Where("key = ?", localSettingPlaylistCoverEpoch).Take(&setting).Error
	switch {
	case err == nil && strings.TrimSpace(setting.Value) == playlistCoverEpoch:
		return nil
	case err != nil && err != gorm.ErrRecordNotFound:
		return err
	}

	current, err := a.ensureCurrentDevice(ctx)
	if err != nil {
		return fmt.Errorf("ensure current device for playlist cover migration: %w", err)
	}

	return a.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var legacyRows []ArtworkVariant
		if err := tx.Where("scope_type = ?", "playlist").
			Order("library_id ASC, scope_id ASC, updated_at DESC, variant ASC").
			Find(&legacyRows).Error; err != nil {
			return err
		}

		grouped := make(map[string]*legacyPlaylistCoverGroup)
		order := make([]string, 0, len(legacyRows))
		for _, row := range legacyRows {
			key := strings.TrimSpace(row.LibraryID) + "|" + strings.TrimSpace(row.ScopeID)
			group, ok := grouped[key]
			if !ok {
				group = &legacyPlaylistCoverGroup{
					libraryID:  strings.TrimSpace(row.LibraryID),
					playlistID: strings.TrimSpace(row.ScopeID),
				}
				grouped[key] = group
				order = append(order, key)
			}
			group.rows = append(group.rows, row)
		}
		sort.Strings(order)

		for _, key := range order {
			group := grouped[key]
			if group == nil || group.libraryID == "" || group.playlistID == "" {
				continue
			}
			if err := migrateLegacyPlaylistCoverGroup(ctx, tx, a, current, *group); err != nil {
				return err
			}
		}

		if err := tx.Where("scope_type = ?", "playlist").Delete(&ArtworkVariant{}).Error; err != nil {
			return err
		}
		if err := tx.Where("scope_type = ? AND variant <> ?", "playlist", playlistCoverVariantCanonical).
			Delete(&LocalArtworkSourceRef{}).Error; err != nil {
			return err
		}
		return upsertLocalSettingTx(tx, localSettingPlaylistCoverEpoch, playlistCoverEpoch, time.Now().UTC())
	})
}

func migrateLegacyPlaylistCoverGroup(ctx context.Context, tx *gorm.DB, app *App, current Device, group legacyPlaylistCoverGroup) error {
	var existing PlaylistCover
	err := tx.Where("library_id = ? AND playlist_id = ?", group.libraryID, group.playlistID).Take(&existing).Error
	switch {
	case err == nil:
		return nil
	case err != nil && err != gorm.ErrRecordNotFound:
		return err
	}

	best, ok := chooseLegacyPlaylistCover(group.rows)
	if !ok {
		return nil
	}

	updatedAt := best.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	chosenSource, chosenSourceRef, _, err := localArtworkSourceRefForScopeTx(tx, group.libraryID, "playlist", group.playlistID, best.Variant)
	if err != nil {
		return err
	}

	row := PlaylistCover{
		LibraryID:    group.libraryID,
		PlaylistID:   group.playlistID,
		BlobID:       strings.TrimSpace(best.BlobID),
		MIME:         strings.TrimSpace(best.MIME),
		FileExt:      normalizeArtworkFileExt(best.FileExt, best.MIME),
		W:            best.W,
		H:            best.H,
		Bytes:        best.Bytes,
		ChosenSource: firstNonEmpty(chosenSource, strings.TrimSpace(best.ChosenSource)),
		UpdatedAt:    updatedAt,
	}

	if row.BlobID != "" {
		if imagePath, ok, err := app.blobs.ArtworkFilePath(row.BlobID, row.FileExt); err != nil {
			return err
		} else if ok {
			if built, err := app.artwork.buildCanonicalPlaylistCoverFromImagePath(ctx, imagePath); err == nil {
				blobID, err := app.blobs.StoreArtworkBytes(built.Bytes, built.FileExt)
				if err != nil {
					return err
				}
				row.BlobID = blobID
				row.MIME = built.MIME
				row.FileExt = normalizeArtworkFileExt(built.FileExt, built.MIME)
				row.W = built.W
				row.H = built.H
				row.Bytes = int64(len(built.Bytes))
				row.ChosenSource = firstNonEmpty(built.SourceKind, row.ChosenSource)
				chosenSourceRef = firstNonEmpty(strings.TrimSpace(built.SourceRef), chosenSourceRef)
			}
		}
	}

	if row.BlobID == "" {
		return nil
	}
	if err := upsertLocalArtworkSourceRefTx(tx, row.LibraryID, "playlist", row.PlaylistID, playlistCoverVariantCanonical, row.ChosenSource, chosenSourceRef, row.UpdatedAt); err != nil {
		return err
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "playlist_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"blob_id", "mime", "file_ext", "w", "h", "bytes", "chosen_source", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}

	local := apitypes.LocalContext{
		LibraryID: row.LibraryID,
		DeviceID:  strings.TrimSpace(current.DeviceID),
		Device:    strings.TrimSpace(current.Name),
	}
	peerID, err := app.ensureDevicePeerIDTx(tx, local.DeviceID, local.Device)
	if err == nil {
		local.PeerID = strings.TrimSpace(peerID)
	}
	if strings.TrimSpace(local.DeviceID) == "" {
		return nil
	}
	_, err = app.appendLocalOplogTx(tx, local, entityTypePlaylistCover, playlistCoverEntityID(row.PlaylistID), "upsert", playlistCoverOplogPayload{
		PlaylistID:   row.PlaylistID,
		BlobID:       row.BlobID,
		MIME:         row.MIME,
		FileExt:      row.FileExt,
		W:            row.W,
		H:            row.H,
		Bytes:        row.Bytes,
		ChosenSource: row.ChosenSource,
		UpdatedAtNS:  row.UpdatedAt.UTC().UnixNano(),
	})
	return err
}

func sortedArtworkBlobRecords(items []artworkBlobRecord) []artworkBlobRecord {
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			if items[i].ScopeType != items[j].ScopeType {
				return items[i].ScopeType < items[j].ScopeType
			}
			if items[i].ScopeID != items[j].ScopeID {
				return items[i].ScopeID < items[j].ScopeID
			}
			return items[i].Variant < items[j].Variant
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}
