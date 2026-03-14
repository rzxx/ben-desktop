package desktopcore

import (
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func opaqueSourcePathKey(sourceFileID string) string {
	sourceFileID = strings.TrimSpace(sourceFileID)
	if sourceFileID == "" {
		return "opaque:unknown"
	}
	return "opaque:" + sourceFileID
}

func localSourcePathTx(tx *gorm.DB, libraryID, deviceID, sourceFileID string) (LocalSourcePath, bool, error) {
	var row LocalSourcePath
	err := tx.Where("library_id = ? AND device_id = ? AND source_file_id = ?",
		strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), strings.TrimSpace(sourceFileID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return LocalSourcePath{}, false, nil
		}
		return LocalSourcePath{}, false, err
	}
	return row, true, nil
}

func upsertLocalSourcePathTx(tx *gorm.DB, libraryID, deviceID, sourceFileID, localPath string, updatedAt time.Time) error {
	localPath = filepath.Clean(strings.TrimSpace(localPath))
	if strings.TrimSpace(libraryID) == "" || strings.TrimSpace(deviceID) == "" || strings.TrimSpace(sourceFileID) == "" || localPath == "" {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
			{Name: "source_file_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"local_path", "path_key", "updated_at"}),
	}).Create(&LocalSourcePath{
		LibraryID:    strings.TrimSpace(libraryID),
		DeviceID:     strings.TrimSpace(deviceID),
		SourceFileID: strings.TrimSpace(sourceFileID),
		LocalPath:    localPath,
		PathKey:      localPathKey(localPath),
		UpdatedAt:    updatedAt.UTC(),
	}).Error
}

func resolveStoredSourcePathTx(tx *gorm.DB, libraryID, deviceID, sourceFileID string) (string, string, error) {
	row, ok, err := localSourcePathTx(tx, libraryID, deviceID, sourceFileID)
	if err != nil || !ok {
		return "", "", err
	}
	localPath := filepath.Clean(strings.TrimSpace(row.LocalPath))
	if localPath == "" {
		return "", "", nil
	}
	pathKey := strings.TrimSpace(row.PathKey)
	if pathKey == "" {
		pathKey = localPathKey(localPath)
	}
	return localPath, pathKey, nil
}

func upsertLocalArtworkSourceRefTx(tx *gorm.DB, libraryID, scopeType, scopeID, variant, chosenSource, chosenSourceRef string, updatedAt time.Time) error {
	chosenSourceRef = filepath.Clean(strings.TrimSpace(chosenSourceRef))
	if strings.TrimSpace(libraryID) == "" || strings.TrimSpace(scopeType) == "" || strings.TrimSpace(scopeID) == "" || strings.TrimSpace(variant) == "" || chosenSourceRef == "" {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "scope_type"},
			{Name: "scope_id"},
			{Name: "variant"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"chosen_source", "chosen_source_ref", "updated_at"}),
	}).Create(&LocalArtworkSourceRef{
		LibraryID:       strings.TrimSpace(libraryID),
		ScopeType:       strings.TrimSpace(scopeType),
		ScopeID:         strings.TrimSpace(scopeID),
		Variant:         strings.TrimSpace(variant),
		ChosenSource:    strings.TrimSpace(chosenSource),
		ChosenSourceRef: chosenSourceRef,
		UpdatedAt:       updatedAt.UTC(),
	}).Error
}

func localArtworkSourceRefForScopeTx(tx *gorm.DB, libraryID, scopeType, scopeID, variant string) (string, string, bool, error) {
	var row LocalArtworkSourceRef
	query := tx.Where("library_id = ? AND scope_type = ? AND scope_id = ?",
		strings.TrimSpace(libraryID), strings.TrimSpace(scopeType), strings.TrimSpace(scopeID))
	if strings.TrimSpace(variant) != "" {
		query = query.Where("variant = ?", strings.TrimSpace(variant))
	} else {
		query = query.Order("variant ASC")
	}
	err := query.Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return strings.TrimSpace(row.ChosenSource), filepath.Clean(strings.TrimSpace(row.ChosenSourceRef)), true, nil
}

func deleteLocalArtworkSourceScopeTx(tx *gorm.DB, libraryID, scopeType, scopeID string) error {
	return tx.Where("library_id = ? AND scope_type = ? AND scope_id = ?",
		strings.TrimSpace(libraryID), strings.TrimSpace(scopeType), strings.TrimSpace(scopeID)).
		Delete(&LocalArtworkSourceRef{}).Error
}
