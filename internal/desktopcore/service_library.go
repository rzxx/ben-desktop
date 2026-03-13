package desktopcore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	apitypes "ben/core/api/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LibraryService struct {
	app *App
}

type deviceLibraryRow struct {
	LibraryID string
	Name      string
	Role      string
	JoinedAt  time.Time
	IsActive  bool
}

func (s *LibraryService) ListLibraries(ctx context.Context) ([]apitypes.LibrarySummary, error) {
	device, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.app.listLibrariesForDevice(ctx, device.DeviceID)
	if err != nil {
		return nil, err
	}
	out := make([]apitypes.LibrarySummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, apitypes.LibrarySummary{
			LibraryID: strings.TrimSpace(row.LibraryID),
			Name:      strings.TrimSpace(row.Name),
			Role:      strings.TrimSpace(row.Role),
			JoinedAt:  row.JoinedAt,
			IsActive:  row.IsActive,
		})
	}
	return out, nil
}

func (s *LibraryService) ActiveLibrary(ctx context.Context) (apitypes.LibrarySummary, bool, error) {
	device, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.LibrarySummary{}, false, err
	}
	row, ok, err := s.app.activeLibraryForDevice(ctx, device.DeviceID)
	if err != nil || !ok {
		return apitypes.LibrarySummary{}, ok, err
	}
	return apitypes.LibrarySummary{
		LibraryID: strings.TrimSpace(row.LibraryID),
		Name:      strings.TrimSpace(row.Name),
		Role:      strings.TrimSpace(row.Role),
		JoinedAt:  row.JoinedAt,
		IsActive:  row.IsActive,
	}, true, nil
}

func (s *LibraryService) CreateLibrary(ctx context.Context, name string) (apitypes.LibrarySummary, error) {
	device, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.LibrarySummary{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultLibraryName
	}

	now := time.Now().UTC()
	libraryID := uuid.NewString()
	if err := s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&Library{
			LibraryID: libraryID,
			Name:      name,
			CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Membership{
			LibraryID:        libraryID,
			DeviceID:         device.DeviceID,
			Role:             roleAdmin,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Device{}).
			Where("device_id = ?", device.DeviceID).
			Update("active_library_id", libraryID).Error; err != nil {
			return err
		}
		return tx.Create(&Playlist{
			LibraryID:  libraryID,
			PlaylistID: likedPlaylistIDForLibrary(libraryID),
			Name:       "Liked",
			Kind:       playlistKindLiked,
			CreatedBy:  device.DeviceID,
			CreatedAt:  now,
			UpdatedAt:  now,
		}).Error
	}); err != nil {
		return apitypes.LibrarySummary{}, err
	}
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		return apitypes.LibrarySummary{}, err
	}
	return apitypes.LibrarySummary{
		LibraryID: libraryID,
		Name:      name,
		Role:      roleAdmin,
		JoinedAt:  now,
		IsActive:  true,
	}, nil
}

func (s *LibraryService) SelectLibrary(ctx context.Context, libraryID string) (apitypes.LibrarySummary, error) {
	device, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.LibrarySummary{}, err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return apitypes.LibrarySummary{}, fmt.Errorf("library id is required")
	}

	var membership Membership
	if err := s.app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", libraryID, device.DeviceID).
		Take(&membership).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return apitypes.LibrarySummary{}, fmt.Errorf("device is not a member of library %s", libraryID)
		}
		return apitypes.LibrarySummary{}, err
	}
	if err := s.app.db.WithContext(ctx).Model(&Device{}).
		Where("device_id = ?", device.DeviceID).
		Update("active_library_id", libraryID).Error; err != nil {
		return apitypes.LibrarySummary{}, err
	}

	var library Library
	if err := s.app.db.WithContext(ctx).Where("library_id = ?", libraryID).Take(&library).Error; err != nil {
		return apitypes.LibrarySummary{}, err
	}
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		return apitypes.LibrarySummary{}, err
	}
	return apitypes.LibrarySummary{
		LibraryID: library.LibraryID,
		Name:      strings.TrimSpace(library.Name),
		Role:      strings.TrimSpace(membership.Role),
		JoinedAt:  membership.JoinedAt,
		IsActive:  true,
	}, nil
}

func (s *LibraryService) RenameLibrary(ctx context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.LibrarySummary{}, err
	}
	libraryID = strings.TrimSpace(libraryID)
	name = strings.TrimSpace(name)
	if libraryID == "" || name == "" {
		return apitypes.LibrarySummary{}, fmt.Errorf("library id and name are required")
	}
	if !canManageLibrary(local.Role) {
		return apitypes.LibrarySummary{}, fmt.Errorf("library rename requires admin role")
	}
	if err := s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Library{}).
			Where("library_id = ?", libraryID).
			Update("name", name).Error; err != nil {
			return err
		}
		_, err := s.app.appendLocalOplogTx(tx, local, entityTypeLibrary, libraryID, "upsert", libraryOplogPayload{
			LibraryID: libraryID,
			Name:      name,
		})
		return err
	}); err != nil {
		return apitypes.LibrarySummary{}, err
	}
	return s.SelectLibrary(ctx, libraryID)
}

func (s *LibraryService) LeaveLibrary(ctx context.Context, libraryID string) error {
	device, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library id is required")
	}
	if err := s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ? AND device_id = ?", libraryID, device.DeviceID).Delete(&Membership{}).Error; err != nil {
			return err
		}
		if err := tx.Where("library_id = ? AND device_id = ?", libraryID, device.DeviceID).Delete(&DeviceCheckpointAck{}).Error; err != nil {
			return err
		}
		return tx.Model(&Device{}).
			Where("device_id = ? AND active_library_id = ?", device.DeviceID, libraryID).
			Update("active_library_id", nil).Error
	}); err != nil {
		return err
	}
	return s.app.syncActiveRuntimeServices(ctx)
}

func (s *LibraryService) DeleteLibrary(ctx context.Context, libraryID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library id is required")
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("library delete requires admin role")
	}
	if err := s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Device{}).Where("active_library_id = ?", libraryID).Update("active_library_id", nil).Error; err != nil {
			return err
		}
		models := []any{
			&Membership{}, &ScanRoot{}, &OfflinePin{}, &AdmissionAuthority{}, &MembershipCert{}, &MembershipCertRevocation{},
			&MembershipRecovery{}, &InviteJoinRequest{}, &InviteTokenRedemption{}, &IssuedInvite{}, &JoinSession{},
			&Artist{}, &Credit{}, &AlbumVariantModel{}, &TrackVariantModel{}, &AlbumTrack{}, &DeviceVariantPreference{},
			&SourceFileModel{}, &OptimizedAssetModel{}, &DeviceAssetCacheModel{}, &ArtworkVariant{}, &Playlist{},
			&PlaylistItem{}, &OplogEntry{}, &DeviceClock{}, &PeerSyncState{}, &LibraryCheckpoint{}, &LibraryCheckpointChunk{},
			&DeviceCheckpointAck{},
		}
		for _, model := range models {
			if err := tx.Where("library_id = ?", libraryID).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Where("library_id = ?", libraryID).Delete(&Library{}).Error
	}); err != nil {
		return err
	}
	return s.app.syncActiveRuntimeServices(ctx)
}

func (s *LibraryService) ListLibraryMembers(ctx context.Context) ([]apitypes.LibraryMemberStatus, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	type row struct {
		LibraryID     string
		DeviceID      string
		Role          string
		PeerID        string
		LastSeenAt    *time.Time
		LastAttemptAt *time.Time
		LastSuccessAt *time.Time
		LastError     string
		LastApplied   int64
	}
	var rows []row
	query := `
SELECT
	m.library_id,
	m.device_id,
	m.role,
	COALESCE(d.peer_id, '') AS peer_id,
	d.last_seen_at,
	ps.last_attempt_at,
	ps.last_success_at,
	COALESCE(ps.last_error, '') AS last_error,
	COALESCE(ps.last_applied, 0) AS last_applied
FROM memberships m
LEFT JOIN devices d ON d.device_id = m.device_id
LEFT JOIN peer_sync_states ps ON ps.library_id = m.library_id AND ps.device_id = m.device_id
WHERE m.library_id = ?
ORDER BY CASE WHEN m.device_id = ? THEN 0 ELSE 1 END, m.device_id ASC`
	if err := s.app.db.WithContext(ctx).Raw(query, local.LibraryID, local.DeviceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]apitypes.LibraryMemberStatus, 0, len(rows))
	for _, row := range rows {
		out = append(out, apitypes.LibraryMemberStatus{
			LibraryID:         strings.TrimSpace(row.LibraryID),
			DeviceID:          strings.TrimSpace(row.DeviceID),
			Role:              strings.TrimSpace(row.Role),
			PeerID:            strings.TrimSpace(row.PeerID),
			LastSeenAt:        row.LastSeenAt,
			LastSeq:           row.LastApplied,
			LastSyncAttemptAt: row.LastAttemptAt,
			LastSyncSuccessAt: row.LastSuccessAt,
			LastSyncError:     strings.TrimSpace(row.LastError),
		})
	}
	return out, nil
}

func (s *LibraryService) UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	deviceID = strings.TrimSpace(deviceID)
	role = normalizeRole(role)
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("member role update requires admin role")
	}
	if err := s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := loadManagedMembershipTx(tx, local.LibraryID, local.DeviceID)
		if err != nil {
			return err
		}
		target, err := loadManagedMembershipTx(tx, local.LibraryID, deviceID)
		if err != nil {
			return err
		}
		if err := authorizeManagedMembershipMutation(actor, target, local.DeviceID, deviceID); err != nil {
			return err
		}

		previousRole := normalizeRole(target.Role)
		adminChanged := canManageLibrary(previousRole) != canManageLibrary(role)
		if previousRole == role && !adminChanged {
			return nil
		}

		var existingCert MembershipCert
		peerID := ""
		err = tx.Where("library_id = ? AND device_id = ?", local.LibraryID, deviceID).Take(&existingCert).Error
		switch {
		case err == nil:
			peerID = strings.TrimSpace(existingCert.PeerID)
		case err != nil && err != gorm.ErrRecordNotFound:
			return err
		}

		if err := tx.Model(&Membership{}).
			Where("library_id = ? AND device_id = ?", local.LibraryID, deviceID).
			Update("role", role).Error; err != nil {
			return err
		}
		if err := revokeMembershipCertTx(tx, local.LibraryID, deviceID, "membership role updated", false); err != nil {
			return err
		}
		if adminChanged {
			if _, _, _, err := rotateAdmissionAuthorityTx(tx, local.LibraryID); err != nil {
				return fmt.Errorf("rotate admission authority: %w", err)
			}
		}
		peerID, err = loadMembershipPeerIDTx(tx, deviceID, peerID)
		if err != nil {
			return err
		}
		if peerID == "" {
			return nil
		}
		_, err = issueMembershipCertTx(tx, local.LibraryID, deviceID, peerID, role, defaultMembershipCertTTL)
		return err
	}); err != nil {
		return err
	}
	return s.app.syncActiveRuntimeServices(ctx)
}

func (s *LibraryService) RemoveLibraryMember(ctx context.Context, deviceID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("member removal requires admin role")
	}
	if err := s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := loadManagedMembershipTx(tx, local.LibraryID, local.DeviceID)
		if err != nil {
			return err
		}
		target, err := loadManagedMembershipTx(tx, local.LibraryID, deviceID)
		if err != nil {
			return err
		}
		if err := authorizeManagedMembershipMutation(actor, target, local.DeviceID, deviceID); err != nil {
			return err
		}
		wasAdmin := canManageLibrary(target.Role)
		if err := revokeMembershipCertTx(tx, local.LibraryID, deviceID, "membership removed", true); err != nil {
			return err
		}
		if err := deleteMembershipSecretsTx(tx, local.LibraryID, deviceID); err != nil {
			return err
		}
		if err := tx.Where("library_id = ? AND device_id = ?", local.LibraryID, deviceID).Delete(&Membership{}).Error; err != nil {
			return err
		}
		if err := tx.Where("library_id = ? AND device_id = ?", local.LibraryID, deviceID).Delete(&DeviceCheckpointAck{}).Error; err != nil {
			return err
		}
		if wasAdmin {
			if _, _, _, err := rotateAdmissionAuthorityTx(tx, local.LibraryID); err != nil {
				return fmt.Errorf("rotate admission authority: %w", err)
			}
		}
		return tx.Model(&Device{}).
			Where("device_id = ? AND active_library_id = ?", deviceID, local.LibraryID).
			Update("active_library_id", nil).Error
	}); err != nil {
		return err
	}
	return s.app.syncActiveRuntimeServices(ctx)
}

func (a *App) ensureCurrentDevice(ctx context.Context) (Device, error) {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}

	var setting LocalSetting
	if err := a.db.WithContext(ctx).Where("key = ?", localSettingCurrentDevice).Take(&setting).Error; err == nil {
		var device Device
		if err := a.db.WithContext(ctx).Where("device_id = ?", strings.TrimSpace(setting.Value)).Take(&device).Error; err == nil {
			now := time.Now().UTC()
			updates := map[string]any{"last_seen_at": &now}
			if strings.TrimSpace(host) != "" && device.Name != host {
				updates["name"] = host
				device.Name = host
			}
			if err := a.db.WithContext(ctx).Model(&Device{}).Where("device_id = ?", device.DeviceID).Updates(updates).Error; err != nil {
				return Device{}, err
			}
			device.LastSeenAt = &now
			return device, nil
		}
	}

	now := time.Now().UTC()
	device := Device{
		DeviceID:   uuid.NewString(),
		Name:       host,
		JoinedAt:   now,
		LastSeenAt: &now,
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&device).Error; err != nil {
			return err
		}
		return tx.Save(&LocalSetting{
			Key:       localSettingCurrentDevice,
			Value:     device.DeviceID,
			UpdatedAt: now,
		}).Error
	}); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (a *App) activeLibraryForDevice(ctx context.Context, deviceID string) (deviceLibraryRow, bool, error) {
	var device Device
	if err := a.db.WithContext(ctx).Select("active_library_id").Where("device_id = ?", deviceID).Take(&device).Error; err != nil {
		return deviceLibraryRow{}, false, err
	}
	if device.ActiveLibraryID == nil || strings.TrimSpace(*device.ActiveLibraryID) == "" {
		return deviceLibraryRow{}, false, nil
	}
	activeLibraryID := strings.TrimSpace(*device.ActiveLibraryID)

	var row deviceLibraryRow
	err := a.db.WithContext(ctx).
		Table("memberships AS m").
		Select("l.library_id, l.name, m.role, m.joined_at").
		Joins("JOIN libraries l ON l.library_id = m.library_id").
		Where("m.device_id = ? AND m.library_id = ?", deviceID, activeLibraryID).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return deviceLibraryRow{}, false, nil
		}
		return deviceLibraryRow{}, false, err
	}
	row.IsActive = true
	return row, true, nil
}

func (a *App) listLibrariesForDevice(ctx context.Context, deviceID string) ([]deviceLibraryRow, error) {
	var device Device
	if err := a.db.WithContext(ctx).Select("active_library_id").Where("device_id = ?", deviceID).Take(&device).Error; err != nil {
		return nil, err
	}
	activeLibraryID := ""
	if device.ActiveLibraryID != nil {
		activeLibraryID = strings.TrimSpace(*device.ActiveLibraryID)
	}

	var rows []deviceLibraryRow
	if err := a.db.WithContext(ctx).
		Table("memberships AS m").
		Select("l.library_id, l.name, m.role, m.joined_at").
		Joins("JOIN libraries l ON l.library_id = m.library_id").
		Where("m.device_id = ?", deviceID).
		Order("m.joined_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		rows[index].IsActive = strings.TrimSpace(rows[index].LibraryID) != "" && rows[index].LibraryID == activeLibraryID
	}
	return rows, nil
}

func (a *App) requireActiveContext(ctx context.Context) (apitypes.LocalContext, error) {
	device, err := a.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.LocalContext{}, err
	}
	active, ok, err := a.activeLibraryForDevice(ctx, device.DeviceID)
	if err != nil {
		return apitypes.LocalContext{}, err
	}
	if !ok || strings.TrimSpace(active.LibraryID) == "" {
		return apitypes.LocalContext{}, apitypes.ErrNoActiveLibrary
	}
	return apitypes.LocalContext{
		LibraryID: active.LibraryID,
		DeviceID:  device.DeviceID,
		Device:    device.Name,
		Role:      active.Role,
		PeerID:    device.PeerID,
	}, nil
}
