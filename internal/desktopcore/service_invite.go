package desktopcore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	apitypes "ben/core/api/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	inviteJoinStatusPending  = "pending"
	inviteJoinStatusApproved = "approved"
	inviteJoinStatusRejected = "rejected"
	inviteJoinStatusExpired  = "expired"

	issuedInviteStatusActive   = "active"
	issuedInviteStatusRevoked  = "revoked"
	issuedInviteStatusExpired  = "expired"
	issuedInviteStatusConsumed = "consumed"

	joinSessionStatusPending   = "pending"
	joinSessionStatusApproved  = "approved"
	joinSessionStatusRejected  = "rejected"
	joinSessionStatusExpired   = "expired"
	joinSessionStatusCompleted = "completed"
	joinSessionStatusFailed    = "failed"

	defaultInviteExpiry = 24 * time.Hour

	jobKindJoinSession         = "join-session"
	jobKindFinalizeJoinSession = "finalize-join-session"
)

type InviteService struct {
	app *App
}

type inviteCodePayload struct {
	TokenID    string `json:"tokenId"`
	LibraryID  string `json:"libraryId"`
	ServiceTag string `json:"serviceTag"`
	Role       string `json:"role"`
	MaxUses    int    `json:"maxUses"`
	ExpiresAt  int64  `json:"expiresAt"`
}

type joinSessionMaterial struct {
	LibraryName        string                        `json:"libraryName"`
	RootPublicKey      string                        `json:"rootPublicKey"`
	LibraryKey         string                        `json:"libraryKey"`
	AdmissionAuthority *joinSessionAuthorityMaterial `json:"admissionAuthority,omitempty"`
	RecoveryToken      string                        `json:"recoveryToken"`
	MembershipCert     membershipCertEnvelope        `json:"membershipCert"`
}

type membershipCertEnvelope struct {
	LibraryID        string `json:"libraryId"`
	DeviceID         string `json:"deviceId"`
	PeerID           string `json:"peerId"`
	Role             string `json:"role"`
	AuthorityVersion int64  `json:"authorityVersion"`
	Serial           int64  `json:"serial"`
	IssuedAt         int64  `json:"issuedAt"`
	ExpiresAt        int64  `json:"expiresAt"`
	Sig              []byte `json:"sig"`
}

func (s *InviteService) CreateInviteCode(ctx context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.InviteCodeResult{}, err
	}
	if !canManageLibrary(local.Role) {
		return apitypes.InviteCodeResult{}, fmt.Errorf("invite creation requires owner or admin role")
	}

	role := normalizeRole(req.Role)
	uses := req.Uses
	if uses <= 0 {
		uses = 1
	}
	expires := req.Expires
	if expires <= 0 {
		expires = defaultInviteExpiry
	}

	now := time.Now().UTC()
	expiresAt := now.Add(expires)
	tokenID := uuid.NewString()
	serviceTag := serviceTagForLibrary(local.LibraryID)
	code, err := encodeInviteCode(inviteCodePayload{
		TokenID:    tokenID,
		LibraryID:  local.LibraryID,
		ServiceTag: serviceTag,
		Role:       role,
		MaxUses:    uses,
		ExpiresAt:  expiresAt.Unix(),
	})
	if err != nil {
		return apitypes.InviteCodeResult{}, err
	}

	if err := s.app.db.WithContext(ctx).Create(&IssuedInvite{
		InviteID:   tokenID,
		LibraryID:  local.LibraryID,
		TokenID:    tokenID,
		ServiceTag: serviceTag,
		InviteCode: code,
		Role:       role,
		MaxUses:    uses,
		ExpiresAt:  expiresAt,
		CreatedAt:  now,
	}).Error; err != nil {
		return apitypes.InviteCodeResult{}, err
	}

	return apitypes.InviteCodeResult{
		LibraryID:  local.LibraryID,
		ServiceTag: serviceTag,
		InviteCode: code,
		InviteLink: inviteLinkForCode(code),
		Role:       role,
		Uses:       uses,
		ExpiresAt:  expiresAt,
	}, nil
}

func (s *InviteService) ListIssuedInvites(ctx context.Context, status string) ([]apitypes.IssuedInviteRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	status, ok := normalizeIssuedInviteStatus(status)
	if !ok {
		return nil, fmt.Errorf("unsupported issued invite status %q", strings.TrimSpace(status))
	}

	var rows []IssuedInvite
	if err := s.app.db.WithContext(ctx).
		Where("library_id = ?", local.LibraryID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]apitypes.IssuedInviteRecord, 0, len(rows))
	for _, row := range rows {
		record, err := s.toIssuedInviteRecord(ctx, row, now)
		if err != nil {
			return nil, err
		}
		if status != "" && record.Status != status {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *InviteService) RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("invite revocation requires owner or admin role")
	}

	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return fmt.Errorf("invite id is required")
	}

	var row IssuedInvite
	if err := s.app.db.WithContext(ctx).
		Where("library_id = ? AND invite_id = ?", local.LibraryID, inviteID).
		Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("issued invite not found")
		}
		return err
	}

	redemptions, err := countInviteRedemptions(ctx, s.app.db, row.LibraryID, row.TokenID)
	if err != nil {
		return err
	}
	if deriveIssuedInviteStatus(row, redemptions, time.Now().UTC()) == issuedInviteStatusConsumed {
		return fmt.Errorf("invite is already consumed")
	}

	now := time.Now().UTC()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "revoked"
	}
	return s.app.db.WithContext(ctx).Model(&IssuedInvite{}).
		Where("library_id = ? AND invite_id = ?", local.LibraryID, inviteID).
		Updates(map[string]any{
			"revoked_at":    &now,
			"revoke_reason": reason,
		}).Error
}

func (s *InviteService) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
	payload, err := decodeInviteCode(req.InviteCode)
	if err != nil {
		return apitypes.JoinSession{}, err
	}
	now := time.Now().UTC()
	if payload.ExpiresAt > 0 && time.Unix(payload.ExpiresAt, 0).UTC().Before(now) {
		return apitypes.JoinSession{}, fmt.Errorf("invite expired")
	}

	current, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.JoinSession{}, fmt.Errorf("ensure current device: %w", err)
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		deviceID = current.DeviceID
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = strings.TrimSpace(current.Name)
	}
	if deviceName == "" {
		deviceName = deviceID
	}
	peerID, err := s.app.ensureDevicePeerID(ctx, deviceID, deviceName)
	if err != nil {
		return apitypes.JoinSession{}, err
	}

	issued, ok, err := s.issuedInviteByToken(ctx, payload.LibraryID, payload.TokenID)
	if err != nil {
		return apitypes.JoinSession{}, err
	}
	if !ok {
		return apitypes.JoinSession{}, fmt.Errorf("invite not found")
	}
	redemptions, err := countInviteRedemptions(ctx, s.app.db, issued.LibraryID, issued.TokenID)
	if err != nil {
		return apitypes.JoinSession{}, err
	}
	if deriveIssuedInviteStatus(issued, redemptions, now) != issuedInviteStatusActive {
		return apitypes.JoinSession{}, fmt.Errorf("invite is not active")
	}

	requestID := uuid.NewString()
	sessionID := uuid.NewString()
	job := s.app.jobs.Track(sessionID, jobKindJoinSession, payload.LibraryID)
	job.Queued(0, "queued join session start")
	job.Running(0.1, "validating invite")
	joinPubKey, err := randomBytes(32)
	if err != nil {
		job.Fail(1, "failed to generate join session material", err)
		return apitypes.JoinSession{}, fmt.Errorf("generate join public key: %w", err)
	}
	fingerprint := fingerprintForDevice(deviceID, peerID)
	job.Running(0.25, "creating join session")

	err = s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&InviteJoinRequest{
			RequestID:         requestID,
			LibraryID:         issued.LibraryID,
			TokenID:           issued.TokenID,
			MaxUses:           issued.MaxUses,
			DeviceID:          deviceID,
			DeviceName:        deviceName,
			PeerID:            peerID,
			DeviceFingerprint: fingerprint,
			RequestedRole:     issued.Role,
			Status:            inviteJoinStatusPending,
			Message:           "join request pending approval",
			JoinPubKey:        joinPubKey,
			ExpiresAt:         issued.ExpiresAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&JoinSession{
			SessionID:         sessionID,
			InviteCode:        strings.TrimSpace(req.InviteCode),
			InviteToken:       issued.TokenID,
			LibraryID:         issued.LibraryID,
			ServiceTag:        issued.ServiceTag,
			DeviceID:          deviceID,
			DeviceName:        deviceName,
			RequestID:         requestID,
			Status:            joinSessionStatusPending,
			Message:           "join request pending approval",
			Role:              issued.Role,
			LocalPeerID:       peerID,
			DeviceFingerprint: fingerprint,
			ExpiresAt:         issued.ExpiresAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		}).Error
	})
	if err != nil {
		job.Fail(1, "failed to create join session", err)
		return apitypes.JoinSession{}, err
	}
	return s.GetJoinSession(ctx, sessionID)
}

func (s *InviteService) GetJoinSession(ctx context.Context, sessionID string) (apitypes.JoinSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return apitypes.JoinSession{}, fmt.Errorf("session id is required")
	}
	session, err := s.loadJoinSession(ctx, sessionID)
	if err != nil {
		return apitypes.JoinSession{}, err
	}
	session, err = s.refreshJoinSession(ctx, session)
	if err != nil {
		return apitypes.JoinSession{}, err
	}
	s.syncJoinSessionJob(session)
	return toJoinSessionRecord(session), nil
}

func (s *InviteService) FinalizeJoinSession(ctx context.Context, sessionID string) (apitypes.JoinLibraryResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return apitypes.JoinLibraryResult{}, fmt.Errorf("session id is required")
	}

	session, err := s.loadJoinSession(ctx, sessionID)
	if err != nil {
		return apitypes.JoinLibraryResult{}, err
	}
	session, err = s.refreshJoinSession(ctx, session)
	if err != nil {
		return apitypes.JoinLibraryResult{}, err
	}
	job := s.app.jobs.Track(session.SessionID, jobKindJoinSession, session.LibraryID)

	switch strings.TrimSpace(session.Status) {
	case joinSessionStatusCompleted:
		s.syncJoinSessionJob(session)
		return joinLibraryResultFromSession(session), nil
	case joinSessionStatusPending:
		s.syncJoinSessionJob(session)
		return apitypes.JoinLibraryResult{}, fmt.Errorf("join request is still pending")
	case joinSessionStatusRejected, joinSessionStatusExpired, joinSessionStatusFailed:
		s.syncJoinSessionJob(session)
		return apitypes.JoinLibraryResult{}, fmt.Errorf("join session is %s", strings.TrimSpace(session.Status))
	case joinSessionStatusApproved:
	default:
		s.syncJoinSessionJob(session)
		return apitypes.JoinLibraryResult{}, fmt.Errorf("join session is %s", strings.TrimSpace(session.Status))
	}
	job.Running(0.85, "finalizing join session")

	var material joinSessionMaterial
	if err := json.Unmarshal([]byte(strings.TrimSpace(session.MaterialJSON)), &material); err != nil {
		job.Fail(1, "failed to decode join session material", err)
		return apitypes.JoinLibraryResult{}, fmt.Errorf("decode join session material: %w", err)
	}

	now := time.Now().UTC()
	err = s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := restoreJoinSessionMaterialTx(tx, session, material, now); err != nil {
			return err
		}

		if err := upsertDeviceMembershipTx(tx, session.LibraryID, session.DeviceID, session.DeviceName, session.LocalPeerID, session.Role, now); err != nil {
			return err
		}

		if material.RecoveryToken != "" {
			tokenHash := sha256.Sum256([]byte(material.RecoveryToken))
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"token_hash":          hex.EncodeToString(tokenHash[:]),
					"issued_by_device_id": strings.TrimSpace(session.OwnerDeviceID),
					"updated_at":          now,
				}),
			}).Create(&MembershipRecovery{
				LibraryID:        session.LibraryID,
				DeviceID:         session.DeviceID,
				TokenHash:        hex.EncodeToString(tokenHash[:]),
				IssuedByDeviceID: strings.TrimSpace(session.OwnerDeviceID),
				CreatedAt:        now,
				UpdatedAt:        now,
			}).Error; err != nil {
				return err
			}
		}

		if len(material.MembershipCert.Sig) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"peer_id", "role", "authority_version", "serial", "issued_at", "expires_at", "sig"}),
			}).Create(&MembershipCert{
				LibraryID:        material.MembershipCert.LibraryID,
				DeviceID:         material.MembershipCert.DeviceID,
				PeerID:           material.MembershipCert.PeerID,
				Role:             material.MembershipCert.Role,
				AuthorityVersion: material.MembershipCert.AuthorityVersion,
				Serial:           material.MembershipCert.Serial,
				IssuedAt:         material.MembershipCert.IssuedAt,
				ExpiresAt:        material.MembershipCert.ExpiresAt,
				Sig:              append([]byte(nil), material.MembershipCert.Sig...),
			}).Error; err != nil {
				return err
			}
		}

		if err := upsertDeviceMembershipTx(tx, session.LibraryID, session.OwnerDeviceID, session.OwnerDeviceID, session.OwnerPeerID, session.OwnerRole, now); err != nil {
			return err
		}

		current, err := s.app.ensureCurrentDeviceTx(tx)
		if err != nil {
			return err
		}
		if session.DeviceID == current.DeviceID {
			if err := tx.Model(&Device{}).
				Where("device_id = ?", current.DeviceID).
				Update("active_library_id", session.LibraryID).Error; err != nil {
				return err
			}
			if err := upsertLocalSettingTx(tx, membershipRecoveryLocalSettingKey(session.LibraryID, session.DeviceID), material.RecoveryToken, now); err != nil {
				return err
			}
		}

		if err := ensureLikedPlaylistTx(tx, session.LibraryID, session.DeviceID, now); err != nil {
			return err
		}

		return tx.Model(&JoinSession{}).
			Where("session_id = ?", session.SessionID).
			Updates(map[string]any{
				"status":     joinSessionStatusCompleted,
				"message":    "join completed",
				"updated_at": now,
			}).Error
	})
	if err != nil {
		job.Fail(1, "failed to finalize join session", err)
		return apitypes.JoinLibraryResult{}, err
	}
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		job.Fail(1, "failed to refresh active runtime services", err)
		return apitypes.JoinLibraryResult{}, err
	}

	session, err = s.loadJoinSession(ctx, sessionID)
	if err != nil {
		job.Fail(1, "failed to reload finalized join session", err)
		return apitypes.JoinLibraryResult{}, err
	}
	s.syncJoinSessionJob(session)
	return joinLibraryResultFromSession(session), nil
}

func (s *InviteService) StartFinalizeJoinSession(ctx context.Context, sessionID string) (JobSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return JobSnapshot{}, fmt.Errorf("session id is required")
	}

	session, err := s.loadJoinSession(ctx, sessionID)
	if err != nil {
		return JobSnapshot{}, err
	}
	session, err = s.refreshJoinSession(ctx, session)
	if err != nil {
		return JobSnapshot{}, err
	}

	jobID := finalizeJoinSessionJobID(session.SessionID)
	snapshot, started := s.app.jobs.Begin(jobID, jobKindFinalizeJoinSession, session.LibraryID, "queued join session finalization")
	if !started {
		return snapshot, nil
	}

	runCtx := context.WithoutCancel(ctx)
	go func() {
		job := s.app.jobs.Track(jobID, jobKindFinalizeJoinSession, session.LibraryID)
		if job != nil {
			job.Running(0.1, "finalizing join session")
		}
		_, err := s.FinalizeJoinSession(runCtx, sessionID)
		if err != nil {
			if job != nil {
				job.Fail(1, "join session finalization failed", err)
			}
			return
		}
		if job != nil {
			job.Complete(1, "join session finalized")
		}
	}()

	return snapshot, nil
}

func (s *InviteService) CancelJoinSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	session, err := s.loadJoinSession(ctx, sessionID)
	if err != nil {
		return err
	}
	job := s.app.jobs.Track(session.SessionID, jobKindJoinSession, session.LibraryID)
	if strings.TrimSpace(session.Status) == joinSessionStatusCompleted {
		s.syncJoinSessionJob(session)
		return fmt.Errorf("cannot cancel a completed join session")
	}
	now := time.Now().UTC()
	if err := s.app.db.WithContext(ctx).Model(&JoinSession{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"status":     joinSessionStatusFailed,
			"message":    "canceled by user",
			"updated_at": now,
		}).Error; err != nil {
		job.Fail(1, "failed to cancel join session", err)
		return err
	}
	job.Fail(1, "canceled by user", nil)
	return nil
}

func (s *InviteService) ListJoinRequests(ctx context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	status, ok := normalizeJoinRequestStatus(status)
	if !ok {
		return nil, fmt.Errorf("unsupported join request status %q", strings.TrimSpace(status))
	}

	var rows []InviteJoinRequest
	if err := s.app.db.WithContext(ctx).
		Where("library_id = ?", local.LibraryID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]apitypes.InviteJoinRequestRecord, 0, len(rows))
	for _, row := range rows {
		record, changed := normalizeJoinRequestRecord(row, now)
		if changed {
			if err := s.app.db.WithContext(ctx).Model(&InviteJoinRequest{}).
				Where("request_id = ?", row.RequestID).
				Updates(map[string]any{
					"status":     record.Status,
					"message":    record.Message,
					"updated_at": record.UpdatedAt,
				}).Error; err != nil {
				return nil, err
			}
		}
		if status != "" && record.Status != status {
			continue
		}
		out = append(out, toInviteJoinRequestRecord(record))
	}
	return out, nil
}

func (s *InviteService) ApproveJoinRequest(ctx context.Context, requestID, role string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("join approval requires owner or admin role")
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request id is required")
	}
	session, sessionFound, err := s.loadJoinSessionByRequestID(ctx, requestID)
	if err != nil {
		return err
	}
	var job *JobTracker
	if sessionFound {
		job = s.app.jobs.Track(session.SessionID, jobKindJoinSession, session.LibraryID)
		job.Running(0.6, "approving join request")
	}
	ownerPeerID, err := s.app.ensureDevicePeerID(ctx, local.DeviceID, local.Device)
	if err != nil {
		if job != nil {
			job.Fail(1, "failed to resolve owner peer id", err)
		}
		return err
	}

	err = s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req InviteJoinRequest
		if err := tx.Where("request_id = ?", requestID).Take(&req).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("invite request not found")
			}
			return err
		}
		if req.LibraryID != local.LibraryID {
			return fmt.Errorf("invite request belongs to a different library")
		}
		req, changed := normalizeJoinRequestRecord(req, time.Now().UTC())
		if changed {
			if err := tx.Model(&InviteJoinRequest{}).
				Where("request_id = ?", req.RequestID).
				Updates(map[string]any{
					"status":     req.Status,
					"message":    req.Message,
					"updated_at": req.UpdatedAt,
				}).Error; err != nil {
				return err
			}
		}
		if req.Status != inviteJoinStatusPending {
			return fmt.Errorf("invite request is %s, expected pending", req.Status)
		}

		consumed, err := consumeInviteTokenRedemptionTx(tx, req.LibraryID, req.TokenID, req.RequestID, req.MaxUses)
		if err != nil {
			return err
		}
		if !consumed {
			now := time.Now().UTC()
			if err := tx.Model(&InviteJoinRequest{}).
				Where("request_id = ?", req.RequestID).
				Updates(map[string]any{
					"status":     inviteJoinStatusRejected,
					"message":    "invite has no remaining uses",
					"updated_at": now,
				}).Error; err != nil {
				return err
			}
			return fmt.Errorf("invite has no remaining uses")
		}

		approvedRole := normalizeRole(role)
		if strings.TrimSpace(role) == "" {
			approvedRole = normalizeRole(req.RequestedRole)
		}
		now := time.Now().UTC()
		material, err := s.buildJoinSessionMaterialTx(tx, req.LibraryID, req.DeviceID, req.PeerID, approvedRole, now)
		if err != nil {
			return err
		}
		encodedCert, err := json.Marshal(material.MembershipCert)
		if err != nil {
			return err
		}
		materialJSON, err := json.Marshal(material)
		if err != nil {
			return err
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":         req.DeviceName,
				"peer_id":      req.PeerID,
				"last_seen_at": &now,
			}),
		}).Create(&Device{
			DeviceID:   req.DeviceID,
			Name:       req.DeviceName,
			PeerID:     req.PeerID,
			JoinedAt:   now,
			LastSeenAt: &now,
		}).Error; err != nil {
			return err
		}

		if err := tx.Model(&InviteJoinRequest{}).
			Where("request_id = ?", req.RequestID).
			Updates(map[string]any{
				"approved_role":        approvedRole,
				"status":               inviteJoinStatusApproved,
				"message":              "join request approved",
				"membership_cert_json": string(encodedCert),
				"encrypted_material":   []byte(string(materialJSON)),
				"updated_at":           now,
			}).Error; err != nil {
			return err
		}

		return tx.Model(&JoinSession{}).
			Where("request_id = ?", req.RequestID).
			Updates(map[string]any{
				"status":            joinSessionStatusApproved,
				"message":           "join request approved",
				"role":              approvedRole,
				"owner_device_id":   local.DeviceID,
				"owner_role":        local.Role,
				"owner_peer_id":     ownerPeerID,
				"owner_fingerprint": fingerprintForDevice(local.DeviceID, ownerPeerID),
				"material_json":     string(materialJSON),
				"updated_at":        now,
			}).Error
	})
	if err != nil {
		if job != nil {
			job.Fail(1, "join request approval failed", err)
		}
		return err
	}
	if sessionFound {
		session, _, err = s.loadJoinSessionByRequestID(ctx, requestID)
		if err != nil {
			job.Fail(1, "failed to reload approved join session", err)
			return err
		}
		s.syncJoinSessionJob(session)
	}
	return nil
}

func (s *InviteService) RejectJoinRequest(ctx context.Context, requestID, reason string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("join rejection requires owner or admin role")
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request id is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "join request rejected"
	}
	session, sessionFound, err := s.loadJoinSessionByRequestID(ctx, requestID)
	if err != nil {
		return err
	}
	var job *JobTracker
	if sessionFound {
		job = s.app.jobs.Track(session.SessionID, jobKindJoinSession, session.LibraryID)
		job.Running(0.6, "rejecting join request")
	}

	now := time.Now().UTC()
	err = s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req InviteJoinRequest
		if err := tx.Where("request_id = ?", requestID).Take(&req).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("invite request not found")
			}
			return err
		}
		if req.LibraryID != local.LibraryID {
			return fmt.Errorf("invite request belongs to a different library")
		}
		req, changed := normalizeJoinRequestRecord(req, now)
		if changed {
			if err := tx.Model(&InviteJoinRequest{}).
				Where("request_id = ?", req.RequestID).
				Updates(map[string]any{
					"status":     req.Status,
					"message":    req.Message,
					"updated_at": req.UpdatedAt,
				}).Error; err != nil {
				return err
			}
		}
		if req.Status != inviteJoinStatusPending {
			return fmt.Errorf("invite request is %s, expected pending", req.Status)
		}
		if err := tx.Model(&InviteJoinRequest{}).
			Where("request_id = ?", requestID).
			Updates(map[string]any{
				"status":     inviteJoinStatusRejected,
				"message":    reason,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&JoinSession{}).
			Where("request_id = ?", requestID).
			Updates(map[string]any{
				"status":     joinSessionStatusRejected,
				"message":    reason,
				"updated_at": now,
			}).Error
	})
	if err != nil {
		if job != nil {
			job.Fail(1, "join request rejection failed", err)
		}
		return err
	}
	if sessionFound {
		session, _, err = s.loadJoinSessionByRequestID(ctx, requestID)
		if err != nil {
			job.Fail(1, "failed to reload rejected join session", err)
			return err
		}
		s.syncJoinSessionJob(session)
	}
	return nil
}

func (s *InviteService) toIssuedInviteRecord(ctx context.Context, row IssuedInvite, now time.Time) (apitypes.IssuedInviteRecord, error) {
	redemptions, err := countInviteRedemptions(ctx, s.app.db, row.LibraryID, row.TokenID)
	if err != nil {
		return apitypes.IssuedInviteRecord{}, err
	}
	return apitypes.IssuedInviteRecord{
		InviteID:        row.InviteID,
		LibraryID:       row.LibraryID,
		InviteCode:      row.InviteCode,
		InviteLink:      inviteLinkForCode(row.InviteCode),
		Role:            row.Role,
		MaxUses:         row.MaxUses,
		RedemptionCount: redemptions,
		Status:          deriveIssuedInviteStatus(row, redemptions, now),
		ExpiresAt:       row.ExpiresAt,
		CreatedAt:       row.CreatedAt,
		RevokedAt:       cloneTimePtr(row.RevokedAt),
		RevokeReason:    strings.TrimSpace(row.RevokeReason),
	}, nil
}

func (s *InviteService) issuedInviteByToken(ctx context.Context, libraryID, tokenID string) (IssuedInvite, bool, error) {
	var row IssuedInvite
	err := s.app.db.WithContext(ctx).
		Where("library_id = ? AND token_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(tokenID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return IssuedInvite{}, false, nil
		}
		return IssuedInvite{}, false, err
	}
	return row, true, nil
}

func (s *InviteService) loadJoinSession(ctx context.Context, sessionID string) (JoinSession, error) {
	var session JoinSession
	if err := s.app.db.WithContext(ctx).Where("session_id = ?", sessionID).Take(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return JoinSession{}, fmt.Errorf("join session %s not found", sessionID)
		}
		return JoinSession{}, err
	}
	return session, nil
}

func (s *InviteService) loadJoinSessionByRequestID(ctx context.Context, requestID string) (JoinSession, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return JoinSession{}, false, nil
	}
	var session JoinSession
	err := s.app.db.WithContext(ctx).Where("request_id = ?", requestID).Take(&session).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return JoinSession{}, false, nil
		}
		return JoinSession{}, false, err
	}
	return session, true, nil
}

func (s *InviteService) refreshJoinSession(ctx context.Context, session JoinSession) (JoinSession, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(session.Status) == joinSessionStatusPending && !session.ExpiresAt.IsZero() && session.ExpiresAt.Before(now) {
		if err := s.app.db.WithContext(ctx).Model(&JoinSession{}).
			Where("session_id = ?", session.SessionID).
			Updates(map[string]any{
				"status":     joinSessionStatusExpired,
				"message":    "invite expired",
				"updated_at": now,
			}).Error; err != nil {
			return JoinSession{}, err
		}
	}

	if strings.TrimSpace(session.RequestID) != "" &&
		strings.TrimSpace(session.Status) != joinSessionStatusCompleted &&
		strings.TrimSpace(session.Status) != joinSessionStatusFailed {
		var req InviteJoinRequest
		if err := s.app.db.WithContext(ctx).Where("request_id = ?", session.RequestID).Take(&req).Error; err == nil {
			req, changed := normalizeJoinRequestRecord(req, now)
			if changed {
				if err := s.app.db.WithContext(ctx).Model(&InviteJoinRequest{}).
					Where("request_id = ?", req.RequestID).
					Updates(map[string]any{
						"status":     req.Status,
						"message":    req.Message,
						"updated_at": req.UpdatedAt,
					}).Error; err != nil {
					return JoinSession{}, err
				}
			}

			switch req.Status {
			case inviteJoinStatusApproved:
				nextRole := strings.TrimSpace(req.ApprovedRole)
				if nextRole == "" {
					nextRole = strings.TrimSpace(session.Role)
				}
				nextMaterial := strings.TrimSpace(session.MaterialJSON)
				if len(req.EncryptedMaterial) > 0 {
					nextMaterial = string(req.EncryptedMaterial)
				}
				if err := s.app.db.WithContext(ctx).Model(&JoinSession{}).
					Where("session_id = ?", session.SessionID).
					Updates(map[string]any{
						"status":        joinSessionStatusApproved,
						"message":       "join request approved",
						"role":          nextRole,
						"material_json": nextMaterial,
						"updated_at":    now,
					}).Error; err != nil {
					return JoinSession{}, err
				}
			case inviteJoinStatusRejected:
				if err := s.app.db.WithContext(ctx).Model(&JoinSession{}).
					Where("session_id = ?", session.SessionID).
					Updates(map[string]any{
						"status":     joinSessionStatusRejected,
						"message":    req.Message,
						"updated_at": now,
					}).Error; err != nil {
					return JoinSession{}, err
				}
			case inviteJoinStatusExpired:
				if err := s.app.db.WithContext(ctx).Model(&JoinSession{}).
					Where("session_id = ?", session.SessionID).
					Updates(map[string]any{
						"status":     joinSessionStatusExpired,
						"message":    req.Message,
						"updated_at": now,
					}).Error; err != nil {
					return JoinSession{}, err
				}
			}
		}
	}

	return s.loadJoinSession(ctx, session.SessionID)
}

func (s *InviteService) syncJoinSessionJob(session JoinSession) {
	job := s.app.jobs.Track(session.SessionID, jobKindJoinSession, session.LibraryID)
	if job == nil {
		return
	}
	message := strings.TrimSpace(session.Message)
	if message == "" {
		message = "join session in progress"
	}
	switch strings.TrimSpace(session.Status) {
	case joinSessionStatusPending:
		job.Running(0.25, message)
	case joinSessionStatusApproved:
		job.Running(0.75, message)
	case joinSessionStatusCompleted:
		job.Complete(1, message)
	case joinSessionStatusRejected, joinSessionStatusExpired, joinSessionStatusFailed:
		job.Fail(1, message, nil)
	default:
		job.Running(0.5, message)
	}
}

func toInviteJoinRequestRecord(row InviteJoinRequest) apitypes.InviteJoinRequestRecord {
	return apitypes.InviteJoinRequestRecord{
		RequestID:         strings.TrimSpace(row.RequestID),
		LibraryID:         strings.TrimSpace(row.LibraryID),
		DeviceID:          strings.TrimSpace(row.DeviceID),
		DeviceName:        strings.TrimSpace(row.DeviceName),
		PeerID:            strings.TrimSpace(row.PeerID),
		DeviceFingerprint: strings.TrimSpace(row.DeviceFingerprint),
		RequestedRole:     strings.TrimSpace(row.RequestedRole),
		ApprovedRole:      strings.TrimSpace(row.ApprovedRole),
		Status:            strings.TrimSpace(row.Status),
		Message:           strings.TrimSpace(row.Message),
		ExpiresAt:         row.ExpiresAt,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func toJoinSessionRecord(row JoinSession) apitypes.JoinSession {
	status := strings.TrimSpace(row.Status)
	return apitypes.JoinSession{
		SessionID:     strings.TrimSpace(row.SessionID),
		RequestID:     strings.TrimSpace(row.RequestID),
		Status:        status,
		Message:       strings.TrimSpace(row.Message),
		LibraryID:     strings.TrimSpace(row.LibraryID),
		Role:          strings.TrimSpace(row.Role),
		Pending:       status == joinSessionStatusPending,
		OwnerDeviceID: strings.TrimSpace(row.OwnerDeviceID),
		OwnerRole:     strings.TrimSpace(row.OwnerRole),
		OwnerPeerID:   strings.TrimSpace(row.OwnerPeerID),
		ExpiresAt:     row.ExpiresAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func joinLibraryResultFromSession(row JoinSession) apitypes.JoinLibraryResult {
	return apitypes.JoinLibraryResult{
		Pending:           strings.TrimSpace(row.Status) == joinSessionStatusPending,
		RequestID:         strings.TrimSpace(row.RequestID),
		LibraryID:         strings.TrimSpace(row.LibraryID),
		Role:              strings.TrimSpace(row.Role),
		DeviceID:          strings.TrimSpace(row.DeviceID),
		LocalPeerID:       strings.TrimSpace(row.LocalPeerID),
		DeviceFingerprint: strings.TrimSpace(row.DeviceFingerprint),
		OwnerDeviceID:     strings.TrimSpace(row.OwnerDeviceID),
		OwnerRole:         strings.TrimSpace(row.OwnerRole),
		OwnerPeerID:       strings.TrimSpace(row.OwnerPeerID),
		OwnerFingerprint:  strings.TrimSpace(row.OwnerFingerprint),
	}
}

func finalizeJoinSessionJobID(sessionID string) string {
	return "join-finalize:" + strings.TrimSpace(sessionID)
}

func normalizeIssuedInviteStatus(status string) (string, bool) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", issuedInviteStatusActive, issuedInviteStatusRevoked, issuedInviteStatusExpired, issuedInviteStatusConsumed:
		return status, true
	default:
		return "", false
	}
}

func normalizeJoinRequestStatus(status string) (string, bool) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", inviteJoinStatusPending, inviteJoinStatusApproved, inviteJoinStatusRejected, inviteJoinStatusExpired:
		return status, true
	default:
		return "", false
	}
}

func normalizeJoinRequestRecord(row InviteJoinRequest, now time.Time) (InviteJoinRequest, bool) {
	if strings.TrimSpace(row.Status) == inviteJoinStatusPending && !row.ExpiresAt.IsZero() && row.ExpiresAt.Before(now) {
		row.Status = inviteJoinStatusExpired
		row.Message = "invite request expired"
		row.UpdatedAt = now
		return row, true
	}
	return row, false
}

func deriveIssuedInviteStatus(row IssuedInvite, redemptions int64, now time.Time) string {
	if row.RevokedAt != nil && !row.RevokedAt.IsZero() {
		return issuedInviteStatusRevoked
	}
	if !row.ExpiresAt.IsZero() && row.ExpiresAt.Before(now) {
		return issuedInviteStatusExpired
	}
	if row.MaxUses > 0 && redemptions >= int64(row.MaxUses) {
		return issuedInviteStatusConsumed
	}
	return issuedInviteStatusActive
}

func countInviteRedemptions(ctx context.Context, db *gorm.DB, libraryID, tokenID string) (int64, error) {
	var count int64
	err := db.WithContext(ctx).
		Model(&InviteTokenRedemption{}).
		Where("library_id = ? AND token_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(tokenID)).
		Count(&count).Error
	return count, err
}

func consumeInviteTokenRedemptionTx(tx *gorm.DB, libraryID, tokenID, requestID string, maxUses int) (bool, error) {
	if maxUses <= 0 {
		return true, nil
	}

	var existing int64
	if err := tx.Model(&InviteTokenRedemption{}).
		Where("library_id = ? AND token_id = ? AND request_id = ?", libraryID, tokenID, requestID).
		Count(&existing).Error; err != nil {
		return false, err
	}
	if existing > 0 {
		return true, nil
	}

	var total int64
	if err := tx.Model(&InviteTokenRedemption{}).
		Where("library_id = ? AND token_id = ?", libraryID, tokenID).
		Count(&total).Error; err != nil {
		return false, err
	}
	if total >= int64(maxUses) {
		return false, nil
	}

	return true, tx.Create(&InviteTokenRedemption{
		LibraryID: strings.TrimSpace(libraryID),
		TokenID:   strings.TrimSpace(tokenID),
		RequestID: strings.TrimSpace(requestID),
		UsedAt:    time.Now().UTC(),
	}).Error
}

func encodeInviteCode(payload inviteCodePayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode invite code: %w", err)
	}
	return "ben-invite-v1." + base64.RawURLEncoding.EncodeToString(body), nil
}

func decodeInviteCode(code string) (inviteCodePayload, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return inviteCodePayload{}, fmt.Errorf("invite code is required")
	}
	parts := strings.SplitN(code, ".", 2)
	if len(parts) != 2 || parts[0] != "ben-invite-v1" {
		return inviteCodePayload{}, fmt.Errorf("invalid invite code")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return inviteCodePayload{}, fmt.Errorf("decode invite code: %w", err)
	}
	var payload inviteCodePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return inviteCodePayload{}, fmt.Errorf("decode invite code payload: %w", err)
	}
	if strings.TrimSpace(payload.TokenID) == "" || strings.TrimSpace(payload.LibraryID) == "" {
		return inviteCodePayload{}, fmt.Errorf("invalid invite code")
	}
	return payload, nil
}

func serviceTagForLibrary(libraryID string) string {
	sum := sha256.Sum256([]byte("service-tag:" + strings.TrimSpace(libraryID)))
	return "ben-" + hex.EncodeToString(sum[:6])
}

func inviteLinkForCode(code string) string {
	return "ben://invite?code=" + url.QueryEscape(strings.TrimSpace(code))
}

func fingerprintForDevice(deviceID, peerID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(deviceID) + ":" + strings.TrimSpace(peerID)))
	return hex.EncodeToString(sum[:16])
}

func randomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func randomToken() (string, error) {
	buf, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func membershipRecoveryLocalSettingKey(libraryID, deviceID string) string {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	if libraryID == "" || deviceID == "" {
		return ""
	}
	return "membership_recovery:" + libraryID + ":" + deviceID
}

func upsertLocalSettingTx(tx *gorm.DB, key, value string, updatedAt time.Time) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("local setting key is required")
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&LocalSetting{
		Key:       key,
		Value:     value,
		UpdatedAt: updatedAt,
	}).Error
}

func (a *App) ensureCurrentDeviceTx(tx *gorm.DB) (Device, error) {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}

	var setting LocalSetting
	if err := tx.Where("key = ?", localSettingCurrentDevice).Take(&setting).Error; err == nil {
		var device Device
		if err := tx.Where("device_id = ?", strings.TrimSpace(setting.Value)).Take(&device).Error; err == nil {
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
	if err := tx.Create(&device).Error; err != nil {
		return Device{}, err
	}
	if err := upsertLocalSettingTx(tx, localSettingCurrentDevice, device.DeviceID, now); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (a *App) ensureDevicePeerID(ctx context.Context, deviceID, deviceName string) (string, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", fmt.Errorf("device id is required")
	}

	expectedPeerID := ""
	current, currentErr := a.ensureCurrentDevice(ctx)
	if currentErr == nil && strings.TrimSpace(current.DeviceID) == deviceID {
		if peerID, err := a.transportIdentityPeerID(); err == nil {
			expectedPeerID = strings.TrimSpace(peerID)
		}
	}

	now := time.Now().UTC()
	var device Device
	err := a.db.WithContext(ctx).Where("device_id = ?", deviceID).Take(&device).Error
	if err == nil {
		if strings.TrimSpace(device.PeerID) != "" && (expectedPeerID == "" || strings.TrimSpace(device.PeerID) == expectedPeerID) {
			return strings.TrimSpace(device.PeerID), nil
		}
		peerID := firstNonEmpty(expectedPeerID, pseudoPeerID(deviceID))
		if err := a.db.WithContext(ctx).Model(&Device{}).
			Where("device_id = ?", deviceID).
			Updates(map[string]any{
				"name":         chooseDeviceName(device.Name, deviceName, deviceID),
				"peer_id":      peerID,
				"last_seen_at": &now,
			}).Error; err != nil {
			return "", err
		}
		return peerID, nil
	}
	if err != gorm.ErrRecordNotFound {
		return "", err
	}

	peerID := firstNonEmpty(expectedPeerID, pseudoPeerID(deviceID))
	if err := a.db.WithContext(ctx).Create(&Device{
		DeviceID:   deviceID,
		Name:       chooseDeviceName("", deviceName, deviceID),
		PeerID:     peerID,
		JoinedAt:   now,
		LastSeenAt: &now,
	}).Error; err != nil {
		return "", err
	}
	return peerID, nil
}

func pseudoPeerID(deviceID string) string {
	sum := sha256.Sum256([]byte("peer:" + strings.TrimSpace(deviceID)))
	return "peer-" + hex.EncodeToString(sum[:10])
}

func chooseDeviceName(existing, requested, fallback string) string {
	if strings.TrimSpace(existing) != "" {
		return strings.TrimSpace(existing)
	}
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	return strings.TrimSpace(fallback)
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	value := in.UTC()
	return &value
}
