package desktopcore

import (
	"context"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	scanRepairReasonScopedImpactEmpty   = "scoped-impact-empty"
	scanRepairReasonScopedImpactCorrupt = "scoped-impact-corrupt"
)

func (a *App) loadScanMaintenanceStatus(ctx context.Context, libraryID, deviceID string) (apitypes.ScanMaintenanceStatus, error) {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	if a == nil || a.storage == nil || libraryID == "" || deviceID == "" {
		return apitypes.ScanMaintenanceStatus{}, nil
	}

	var state ScanMaintenanceState
	err := a.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Take(&state).Error
	switch {
	case err == nil:
		return apitypes.ScanMaintenanceStatus{
			RepairRequired: state.RepairRequired,
			Reason:         strings.TrimSpace(state.Reason),
			Detail:         strings.TrimSpace(state.Detail),
			UpdatedAt:      state.UpdatedAt,
		}, nil
	case err == gorm.ErrRecordNotFound:
		return apitypes.ScanMaintenanceStatus{}, nil
	default:
		return apitypes.ScanMaintenanceStatus{}, err
	}
}

func (a *App) markScanRepairRequired(ctx context.Context, libraryID, deviceID, reason, detail string) error {
	if a == nil || a.storage == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	if libraryID == "" || deviceID == "" {
		return nil
	}
	now := time.Now().UTC()
	if err := a.storage.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"repair_required", "reason", "detail", "updated_at"}),
	}).Create(&ScanMaintenanceState{
		LibraryID:      libraryID,
		DeviceID:       deviceID,
		RepairRequired: true,
		Reason:         strings.TrimSpace(reason),
		Detail:         strings.TrimSpace(detail),
		UpdatedAt:      now,
	}).Error; err != nil {
		return err
	}
	a.setScanMaintenanceStatus(apitypes.ScanMaintenanceStatus{
		RepairRequired: true,
		Reason:         strings.TrimSpace(reason),
		Detail:         strings.TrimSpace(detail),
		UpdatedAt:      now,
	})
	return nil
}

func (a *App) clearScanRepairRequired(ctx context.Context, libraryID, deviceID string) error {
	if a == nil || a.storage == nil {
		return nil
	}
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	if libraryID == "" || deviceID == "" {
		return nil
	}
	now := time.Now().UTC()
	if err := a.storage.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"repair_required", "reason", "detail", "updated_at"}),
	}).Create(&ScanMaintenanceState{
		LibraryID:      libraryID,
		DeviceID:       deviceID,
		RepairRequired: false,
		Reason:         "",
		Detail:         "",
		UpdatedAt:      now,
	}).Error; err != nil {
		return err
	}
	a.setScanMaintenanceStatus(apitypes.ScanMaintenanceStatus{
		RepairRequired: false,
		Reason:         "",
		Detail:         "",
		UpdatedAt:      now,
	})
	return nil
}
