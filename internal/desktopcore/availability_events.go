package desktopcore

import (
	"context"
	"strings"

	apitypes "ben/desktop/api/types"
)

func (a *App) emitAvailabilityInvalidateAll() {
	if a == nil {
		return
	}
	a.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateAvailability,
		InvalidateAll: true,
	})
}

func (a *App) emitAvailabilityInvalidateAllForActiveLibrary(libraryID string) {
	if a == nil {
		return
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return
	}
	local, err := a.EnsureLocalContext(context.Background())
	if err != nil || strings.TrimSpace(local.LibraryID) != libraryID {
		return
	}
	a.emitAvailabilityInvalidateAll()
}

func (a *App) emitAvailabilityInvalidateAllForActiveMembership(
	ctx context.Context,
	deviceID string,
) {
	if a == nil || a.storage == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	local, err := a.EnsureLocalContext(context.Background())
	if err != nil || strings.TrimSpace(local.LibraryID) == "" {
		return
	}
	var count int64
	if err := a.storage.WithContext(ctx).
		Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", local.LibraryID, deviceID).
		Count(&count).Error; err != nil || count == 0 {
		return
	}
	a.emitAvailabilityInvalidateAll()
}
