package desktopcore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
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
	PeerID     string `json:"peerId,omitempty"`
	PeerAddr   string `json:"peerAddr,omitempty"`
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
	peerID, peerAddr, err := s.activeInviteTransportHints(ctx, local.LibraryID)
	if err != nil {
		return apitypes.InviteCodeResult{}, err
	}
	code, err := encodeInviteCode(inviteCodePayload{
		TokenID:    tokenID,
		LibraryID:  local.LibraryID,
		ServiceTag: serviceTag,
		PeerID:     peerID,
		PeerAddr:   peerAddr,
		Role:       role,
		MaxUses:    uses,
		ExpiresAt:  expiresAt.Unix(),
	})
	if err != nil {
		return apitypes.InviteCodeResult{}, err
	}

	if err := s.app.storage.WithContext(ctx).Create(&IssuedInvite{
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
	if err := s.app.storage.WithContext(ctx).
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
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND invite_id = ?", local.LibraryID, inviteID).
		Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("issued invite not found")
		}
		return err
	}

	redemptions, err := countInviteRedemptions(ctx, s.app.storage.DB(), row.LibraryID, row.TokenID)
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
	return s.app.storage.WithContext(ctx).Model(&IssuedInvite{}).
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

	sessionID := uuid.NewString()
	job := s.app.jobs.Track(sessionID, jobKindJoinSession, payload.LibraryID)
	job.Queued(0, "queued join session start")
	job.Running(0.1, "preparing join transport")

	joinPublicKey, joinPrivateKey, err := generateInviteJoinKeypair()
	if err != nil {
		job.Fail(1, "failed to generate join session keypair", err)
		return apitypes.JoinSession{}, fmt.Errorf("generate join public key: %w", err)
	}
	fingerprint := fingerprintForDevice(deviceID, peerID)
	discoverTimeout := req.DiscoverTimeout
	if discoverTimeout <= 0 {
		discoverTimeout = defaultInviteDiscoverTimeout
	}
	discoverCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	client, err := s.app.openInviteClientTransport(payload.ServiceTag)
	if err != nil {
		job.Fail(1, "failed to start invite transport", err)
		return apitypes.JoinSession{}, err
	}
	defer client.Close()

	job.Running(0.25, "contacting invite host")
	var startResp inviteJoinStartResponse
	resolvedPeerID, resolvedPeerAddr, err := client.roundTrip(
		discoverCtx,
		payload.PeerAddr,
		payload.PeerID,
		desktopInviteJoinStartProtocolID,
		inviteJoinStartRequest{
			InviteCode: strings.TrimSpace(req.InviteCode),
			DeviceID:   deviceID,
			DeviceName: deviceName,
			PeerID:     peerID,
			JoinPubKey: append([]byte(nil), joinPublicKey[:]...),
		},
		&startResp,
	)
	if err != nil {
		job.Fail(1, "failed to contact invite host", err)
		return apitypes.JoinSession{}, err
	}
	if msg := strings.TrimSpace(startResp.Error); msg != "" {
		err = fmt.Errorf("%s", msg)
		job.Fail(1, "invite host rejected join request", err)
		return apitypes.JoinSession{}, err
	}
	requestID := strings.TrimSpace(startResp.RequestID)
	if requestID == "" {
		err = fmt.Errorf("invite host response missing request id")
		job.Fail(1, "invite host response was incomplete", err)
		return apitypes.JoinSession{}, err
	}

	job.Running(0.5, "persisting join session")

	err = s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := saveJoinSessionKeypairTx(tx, sessionID, joinPublicKey, joinPrivateKey, now); err != nil {
			return err
		}
		return tx.Create(&JoinSession{
			SessionID:          sessionID,
			InviteCode:         strings.TrimSpace(req.InviteCode),
			InviteToken:        payload.TokenID,
			LibraryID:          payload.LibraryID,
			ServiceTag:         payload.ServiceTag,
			PeerAddrHint:       firstNonEmpty(strings.TrimSpace(resolvedPeerAddr), strings.TrimSpace(startResp.PeerAddrHint), strings.TrimSpace(payload.PeerAddr)),
			ExpectedPeerIDHint: firstNonEmpty(strings.TrimSpace(startResp.OwnerPeerID), strings.TrimSpace(resolvedPeerID), strings.TrimSpace(payload.PeerID)),
			DeviceID:           deviceID,
			DeviceName:         deviceName,
			RequestID:          requestID,
			Status:             firstNonEmpty(normalizeJoinSessionStatus(startResp.Status), joinSessionStatusPending),
			Message:            firstNonEmpty(strings.TrimSpace(startResp.Message), "join request pending approval"),
			Role:               firstNonEmpty(strings.TrimSpace(startResp.Role), normalizeRole(payload.Role)),
			LocalPeerID:        peerID,
			DeviceFingerprint:  fingerprint,
			OwnerDeviceID:      strings.TrimSpace(startResp.OwnerDeviceID),
			OwnerRole:          strings.TrimSpace(startResp.OwnerRole),
			OwnerPeerID:        strings.TrimSpace(startResp.OwnerPeerID),
			OwnerFingerprint: firstNonEmpty("", func() string {
				if strings.TrimSpace(startResp.OwnerDeviceID) == "" || strings.TrimSpace(startResp.OwnerPeerID) == "" {
					return ""
				}
				return fingerprintForDevice(startResp.OwnerDeviceID, startResp.OwnerPeerID)
			}()),
			ExpiresAt: func() time.Time {
				if startResp.ExpiresAt > 0 {
					return time.Unix(startResp.ExpiresAt, 0).UTC()
				}
				return time.Unix(payload.ExpiresAt, 0).UTC()
			}(),
			CreatedAt: now,
			UpdatedAt: now,
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
	err = s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if err := deleteJoinSessionKeypair(ctx, s.app.storage.DB(), session.SessionID); err != nil {
		job.Fail(1, "failed to clean join session keypair", err)
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
	if err := s.cancelRemoteJoinSession(ctx, session, "canceled by requester"); err != nil && s.app.cfg.Logger != nil {
		s.app.cfg.Logger.Errorf("desktopcore: cancel join session remote request failed for %s: %v", session.SessionID, err)
	}
	now := time.Now().UTC()
	if err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&JoinSession{}).
			Where("session_id = ?", sessionID).
			Updates(map[string]any{
				"status":     joinSessionStatusFailed,
				"message":    "canceled by user",
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		return deleteJoinSessionKeypair(ctx, tx, sessionID)
	}); err != nil {
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
	if err := s.app.storage.WithContext(ctx).
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
			if err := s.app.storage.WithContext(ctx).Model(&InviteJoinRequest{}).
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

	err = s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		encryptedMaterial, err := encryptJoinSessionMaterial(req.JoinPubKey, material)
		if err != nil {
			return err
		}
		encodedCert, err := json.Marshal(material.MembershipCert)
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
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"role": approvedRole,
			}),
		}).Create(&Membership{
			LibraryID:        req.LibraryID,
			DeviceID:         req.DeviceID,
			Role:             approvedRole,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		if strings.TrimSpace(material.RecoveryToken) != "" {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"token_hash":          hashMembershipRecoveryToken(material.RecoveryToken),
					"issued_by_device_id": local.DeviceID,
					"updated_at":          now,
				}),
			}).Create(&MembershipRecovery{
				LibraryID:        req.LibraryID,
				DeviceID:         req.DeviceID,
				TokenHash:        hashMembershipRecoveryToken(material.RecoveryToken),
				IssuedByDeviceID: local.DeviceID,
				CreatedAt:        now,
				UpdatedAt:        now,
			}).Error; err != nil {
				return err
			}
		}
		if err := saveMembershipCertTx(tx, material.MembershipCert); err != nil {
			return err
		}

		if err := tx.Model(&InviteJoinRequest{}).
			Where("request_id = ?", req.RequestID).
			Updates(map[string]any{
				"approved_role":        approvedRole,
				"owner_device_id":      local.DeviceID,
				"owner_role":           local.Role,
				"owner_peer_id":        ownerPeerID,
				"owner_fingerprint":    fingerprintForDevice(local.DeviceID, ownerPeerID),
				"status":               inviteJoinStatusApproved,
				"message":              "join request approved",
				"membership_cert_json": string(encodedCert),
				"encrypted_material":   encryptedMaterial,
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
	err = s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	redemptions, err := countInviteRedemptions(ctx, s.app.storage.DB(), row.LibraryID, row.TokenID)
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

func (s *InviteService) loadJoinSession(ctx context.Context, sessionID string) (JoinSession, error) {
	var session JoinSession
	if err := s.app.storage.WithContext(ctx).Where("session_id = ?", sessionID).Take(&session).Error; err != nil {
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
	err := s.app.storage.WithContext(ctx).Where("request_id = ?", requestID).Take(&session).Error
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
		if err := s.app.storage.WithContext(ctx).Model(&JoinSession{}).
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
		refreshed, ok, err := s.refreshJoinSessionRemote(ctx, session)
		if err != nil && s.app.cfg.Logger != nil {
			s.app.cfg.Logger.Errorf("desktopcore: refresh join session remote state failed for %s: %v", session.SessionID, err)
		}
		if ok {
			session = refreshed
		} else {
			session, err = s.refreshJoinSessionFromLocalRequest(ctx, session, now)
			if err != nil {
				return JoinSession{}, err
			}
		}
	}

	if isTerminalJoinSessionStatus(session.Status) && strings.TrimSpace(session.Status) != joinSessionStatusCompleted {
		if err := deleteJoinSessionKeypair(ctx, s.app.storage.DB(), session.SessionID); err != nil {
			return JoinSession{}, err
		}
	}
	return s.loadJoinSession(ctx, session.SessionID)
}

func (s *InviteService) activeInviteTransportHints(ctx context.Context, libraryID string) (string, string, error) {
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		return "", "", err
	}
	runtime := s.app.transportService.activeRuntimeForLibrary(libraryID)
	if runtime == nil || runtime.transport == nil {
		return "", "", fmt.Errorf("invite transport is not running")
	}
	peerID := strings.TrimSpace(runtime.transport.LocalPeerID())
	if peerID == "" {
		return "", "", fmt.Errorf("invite transport peer id is unavailable")
	}
	listenAddrs := runtime.transport.ListenAddrs()
	return peerID, preferredInvitePeerAddr(listenAddrs), nil
}

func (s *InviteService) refreshJoinSessionRemote(ctx context.Context, session JoinSession) (JoinSession, bool, error) {
	payload, err := decodeInviteCode(session.InviteCode)
	if err != nil {
		return JoinSession{}, false, err
	}
	client, err := s.app.openInviteClientTransport(firstNonEmpty(session.ServiceTag, payload.ServiceTag))
	if err != nil {
		return JoinSession{}, false, err
	}
	defer client.Close()

	refreshCtx, cancel := context.WithTimeout(ctx, defaultInviteDiscoverTimeout)
	defer cancel()

	var resp inviteJoinStatusResponse
	resolvedPeerID, resolvedPeerAddr, err := client.roundTrip(
		refreshCtx,
		firstNonEmpty(session.PeerAddrHint, payload.PeerAddr),
		firstNonEmpty(session.ExpectedPeerIDHint, session.OwnerPeerID, payload.PeerID),
		desktopInviteJoinStatusProtocolID,
		inviteJoinStatusRequest{
			LibraryID: session.LibraryID,
			RequestID: session.RequestID,
			DeviceID:  session.DeviceID,
			PeerID:    session.LocalPeerID,
		},
		&resp,
	)
	if err != nil {
		return JoinSession{}, false, err
	}
	if msg := strings.TrimSpace(resp.Error); msg != "" {
		return JoinSession{}, false, fmt.Errorf("%s", msg)
	}
	updated, err := s.applyRemoteJoinSessionStatus(ctx, session, resp, resolvedPeerID, resolvedPeerAddr)
	if err != nil {
		return JoinSession{}, false, err
	}
	return updated, true, nil
}

func (s *InviteService) applyRemoteJoinSessionStatus(ctx context.Context, session JoinSession, resp inviteJoinStatusResponse, resolvedPeerID, resolvedPeerAddr string) (JoinSession, error) {
	status := normalizeJoinSessionStatus(resp.Status)
	if status == "" {
		status = joinSessionStatusPending
	}
	now := time.Now().UTC()
	updatedAt := now
	if resp.UpdatedAt > 0 {
		updatedAt = time.Unix(0, resp.UpdatedAt).UTC()
	}

	updates := map[string]any{
		"status":          status,
		"message":         firstNonEmpty(strings.TrimSpace(resp.Message), session.Message),
		"role":            firstNonEmpty(strings.TrimSpace(resp.Role), session.Role),
		"owner_device_id": firstNonEmpty(strings.TrimSpace(resp.OwnerDeviceID), session.OwnerDeviceID),
		"owner_role":      firstNonEmpty(strings.TrimSpace(resp.OwnerRole), session.OwnerRole),
		"owner_peer_id":   firstNonEmpty(strings.TrimSpace(resp.OwnerPeerID), strings.TrimSpace(resolvedPeerID), session.OwnerPeerID),
		"owner_fingerprint": func() string {
			if strings.TrimSpace(resp.OwnerFingerprint) != "" {
				return strings.TrimSpace(resp.OwnerFingerprint)
			}
			if strings.TrimSpace(resp.OwnerDeviceID) != "" && strings.TrimSpace(resp.OwnerPeerID) != "" {
				return fingerprintForDevice(resp.OwnerDeviceID, resp.OwnerPeerID)
			}
			return session.OwnerFingerprint
		}(),
		"peer_addr_hint":        firstNonEmpty(strings.TrimSpace(resolvedPeerAddr), session.PeerAddrHint),
		"expected_peer_id_hint": firstNonEmpty(strings.TrimSpace(resp.OwnerPeerID), strings.TrimSpace(resolvedPeerID), session.ExpectedPeerIDHint),
		"updated_at":            updatedAt,
	}
	if resp.ExpiresAt > 0 {
		updates["expires_at"] = time.Unix(resp.ExpiresAt, 0).UTC()
	}

	if status == joinSessionStatusApproved {
		if len(resp.EncryptedMaterial) == 0 && strings.TrimSpace(session.MaterialJSON) == "" {
			return JoinSession{}, fmt.Errorf("approved join session is missing encrypted material")
		}
		if len(resp.EncryptedMaterial) > 0 {
			publicKey, privateKey, ok, err := loadJoinSessionKeypair(ctx, s.app.storage.DB(), session.SessionID)
			if err != nil {
				return JoinSession{}, err
			}
			if !ok {
				return JoinSession{}, fmt.Errorf("join session keypair is missing")
			}
			material, err := decryptJoinSessionMaterial(resp.EncryptedMaterial, publicKey, privateKey)
			if err != nil {
				return JoinSession{}, err
			}
			raw, err := json.Marshal(material)
			if err != nil {
				return JoinSession{}, err
			}
			updates["material_json"] = string(raw)
			updates["role"] = firstNonEmpty(strings.TrimSpace(resp.Role), strings.TrimSpace(material.MembershipCert.Role), session.Role)
		}
	}

	if err := s.app.storage.WithContext(ctx).Model(&JoinSession{}).
		Where("session_id = ?", session.SessionID).
		Updates(updates).Error; err != nil {
		return JoinSession{}, err
	}
	return s.loadJoinSession(ctx, session.SessionID)
}

func (s *InviteService) refreshJoinSessionFromLocalRequest(ctx context.Context, session JoinSession, now time.Time) (JoinSession, error) {
	var req InviteJoinRequest
	if err := s.app.storage.WithContext(ctx).Where("request_id = ?", session.RequestID).Take(&req).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return session, nil
		}
		return JoinSession{}, err
	}
	req, changed := normalizeJoinRequestRecord(req, now)
	if changed {
		if err := s.app.storage.WithContext(ctx).Model(&InviteJoinRequest{}).
			Where("request_id = ?", req.RequestID).
			Updates(map[string]any{
				"status":     req.Status,
				"message":    req.Message,
				"updated_at": req.UpdatedAt,
			}).Error; err != nil {
			return JoinSession{}, err
		}
	}

	updates := map[string]any{
		"updated_at":        now,
		"owner_device_id":   firstNonEmpty(req.OwnerDeviceID, session.OwnerDeviceID),
		"owner_role":        firstNonEmpty(req.OwnerRole, session.OwnerRole),
		"owner_peer_id":     firstNonEmpty(req.OwnerPeerID, session.OwnerPeerID),
		"owner_fingerprint": firstNonEmpty(req.OwnerFingerprint, session.OwnerFingerprint),
	}
	switch req.Status {
	case inviteJoinStatusApproved:
		updates["status"] = joinSessionStatusApproved
		updates["message"] = firstNonEmpty(strings.TrimSpace(req.Message), "join request approved")
		updates["role"] = firstNonEmpty(strings.TrimSpace(req.ApprovedRole), strings.TrimSpace(session.Role))
		if len(req.EncryptedMaterial) > 0 {
			publicKey, privateKey, ok, err := loadJoinSessionKeypair(ctx, s.app.storage.DB(), session.SessionID)
			if err != nil {
				return JoinSession{}, err
			}
			if ok {
				material, err := decryptJoinSessionMaterial(req.EncryptedMaterial, publicKey, privateKey)
				if err != nil {
					return JoinSession{}, err
				}
				raw, err := json.Marshal(material)
				if err != nil {
					return JoinSession{}, err
				}
				updates["material_json"] = string(raw)
				updates["role"] = firstNonEmpty(strings.TrimSpace(req.ApprovedRole), strings.TrimSpace(material.MembershipCert.Role), strings.TrimSpace(session.Role))
			}
		}
	case inviteJoinStatusRejected:
		updates["status"] = joinSessionStatusRejected
		updates["message"] = req.Message
	case inviteJoinStatusExpired:
		updates["status"] = joinSessionStatusExpired
		updates["message"] = req.Message
	default:
		return session, nil
	}

	if err := s.app.storage.WithContext(ctx).Model(&JoinSession{}).
		Where("session_id = ?", session.SessionID).
		Updates(updates).Error; err != nil {
		return JoinSession{}, err
	}
	return s.loadJoinSession(ctx, session.SessionID)
}

func (s *InviteService) cancelRemoteJoinSession(ctx context.Context, session JoinSession, reason string) error {
	if strings.TrimSpace(session.RequestID) == "" {
		return nil
	}
	payload, err := decodeInviteCode(session.InviteCode)
	if err != nil {
		return err
	}
	client, err := s.app.openInviteClientTransport(firstNonEmpty(session.ServiceTag, payload.ServiceTag))
	if err != nil {
		return err
	}
	defer client.Close()

	cancelCtx, cancel := context.WithTimeout(ctx, defaultInviteDiscoverTimeout)
	defer cancel()

	var resp inviteJoinCancelResponse
	_, _, err = client.roundTrip(
		cancelCtx,
		firstNonEmpty(session.PeerAddrHint, payload.PeerAddr),
		firstNonEmpty(session.ExpectedPeerIDHint, session.OwnerPeerID, payload.PeerID),
		desktopInviteJoinCancelProtocolID,
		inviteJoinCancelRequest{
			LibraryID: session.LibraryID,
			RequestID: session.RequestID,
			DeviceID:  session.DeviceID,
			PeerID:    session.LocalPeerID,
			Reason:    strings.TrimSpace(reason),
		},
		&resp,
	)
	if err != nil {
		return err
	}
	if msg := strings.TrimSpace(resp.Error); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (s *InviteService) handleInviteJoinStart(ctx context.Context, libraryID, localPeerID, actualPeerID string, req inviteJoinStartRequest) (inviteJoinStartResponse, error) {
	payload, err := decodeInviteCode(req.InviteCode)
	if err != nil {
		return inviteJoinStartResponse{}, err
	}
	now := time.Now().UTC()
	if payload.ExpiresAt > 0 && time.Unix(payload.ExpiresAt, 0).UTC().Before(now) {
		return inviteJoinStartResponse{}, fmt.Errorf("invite expired")
	}
	if strings.TrimSpace(payload.LibraryID) != strings.TrimSpace(libraryID) {
		return inviteJoinStartResponse{}, fmt.Errorf("invite library mismatch")
	}
	if strings.TrimSpace(payload.PeerID) != "" && strings.TrimSpace(payload.PeerID) != strings.TrimSpace(localPeerID) {
		return inviteJoinStartResponse{}, fmt.Errorf("invite owner peer mismatch")
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.DeviceName = chooseDeviceName("", req.DeviceName, req.DeviceID)
	req.PeerID = strings.TrimSpace(req.PeerID)
	if req.DeviceID == "" || req.PeerID == "" {
		return inviteJoinStartResponse{}, fmt.Errorf("device id and peer id are required")
	}
	if req.PeerID != strings.TrimSpace(actualPeerID) {
		return inviteJoinStartResponse{}, fmt.Errorf("join peer id mismatch")
	}
	if len(req.JoinPubKey) != 32 {
		return inviteJoinStartResponse{}, fmt.Errorf("join public key must be 32 bytes")
	}

	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return inviteJoinStartResponse{}, err
	}
	if strings.TrimSpace(local.LibraryID) != strings.TrimSpace(libraryID) {
		return inviteJoinStartResponse{}, fmt.Errorf("invite host is not serving the requested library")
	}

	var response inviteJoinStartResponse
	err = s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var issued IssuedInvite
		if err := tx.Where("library_id = ? AND token_id = ?", strings.TrimSpace(payload.LibraryID), strings.TrimSpace(payload.TokenID)).
			Take(&issued).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("invite not found")
			}
			return err
		}
		redemptions, err := countInviteRedemptions(ctx, tx, issued.LibraryID, issued.TokenID)
		if err != nil {
			return err
		}
		if deriveIssuedInviteStatus(issued, redemptions, now) != issuedInviteStatusActive {
			return fmt.Errorf("invite is not active")
		}

		var existing InviteJoinRequest
		existingErr := tx.Where("library_id = ? AND token_id = ? AND device_id = ? AND peer_id = ?", issued.LibraryID, issued.TokenID, req.DeviceID, req.PeerID).
			Order("created_at DESC").
			Limit(1).
			Take(&existing).Error
		switch {
		case existingErr == nil:
			existing, changed := normalizeJoinRequestRecord(existing, now)
			if changed {
				if err := tx.Model(&InviteJoinRequest{}).
					Where("request_id = ?", existing.RequestID).
					Updates(map[string]any{
						"status":     existing.Status,
						"message":    existing.Message,
						"updated_at": existing.UpdatedAt,
					}).Error; err != nil {
					return err
				}
			}
			if existing.Status == inviteJoinStatusPending {
				if err := tx.Model(&InviteJoinRequest{}).
					Where("request_id = ?", existing.RequestID).
					Updates(map[string]any{
						"device_name":  req.DeviceName,
						"join_pub_key": append([]byte(nil), req.JoinPubKey...),
						"updated_at":   now,
					}).Error; err != nil {
					return err
				}
				existing.UpdatedAt = now
				response = inviteJoinStartResponse{
					LibraryID:     existing.LibraryID,
					RequestID:     existing.RequestID,
					Status:        normalizeJoinSessionStatus(existing.Status),
					Message:       firstNonEmpty(existing.Message, "join request pending approval"),
					Role:          firstNonEmpty(existing.ApprovedRole, existing.RequestedRole),
					OwnerDeviceID: local.DeviceID,
					OwnerRole:     local.Role,
					OwnerPeerID:   localPeerID,
					PeerAddrHint:  payload.PeerAddr,
					ExpiresAt:     existing.ExpiresAt.UTC().Unix(),
				}
				return nil
			}
			if existing.Status == inviteJoinStatusApproved {
				response = inviteJoinStartResponse{
					LibraryID:     existing.LibraryID,
					RequestID:     existing.RequestID,
					Status:        normalizeJoinSessionStatus(existing.Status),
					Message:       firstNonEmpty(existing.Message, "join request approved"),
					Role:          firstNonEmpty(existing.ApprovedRole, existing.RequestedRole),
					OwnerDeviceID: firstNonEmpty(existing.OwnerDeviceID, local.DeviceID),
					OwnerRole:     firstNonEmpty(existing.OwnerRole, local.Role),
					OwnerPeerID:   firstNonEmpty(existing.OwnerPeerID, localPeerID),
					PeerAddrHint:  payload.PeerAddr,
					ExpiresAt:     existing.ExpiresAt.UTC().Unix(),
				}
				return nil
			}
		case !errors.Is(existingErr, gorm.ErrRecordNotFound):
			return existingErr
		}

		requestID := uuid.NewString()
		fingerprint := fingerprintForDevice(req.DeviceID, req.PeerID)
		if err := tx.Create(&InviteJoinRequest{
			RequestID:         requestID,
			LibraryID:         issued.LibraryID,
			TokenID:           issued.TokenID,
			MaxUses:           issued.MaxUses,
			DeviceID:          req.DeviceID,
			DeviceName:        req.DeviceName,
			PeerID:            req.PeerID,
			DeviceFingerprint: fingerprint,
			RequestedRole:     issued.Role,
			Status:            inviteJoinStatusPending,
			Message:           "join request pending approval",
			JoinPubKey:        append([]byte(nil), req.JoinPubKey...),
			ExpiresAt:         issued.ExpiresAt,
			CreatedAt:         now,
			UpdatedAt:         now,
		}).Error; err != nil {
			return err
		}
		response = inviteJoinStartResponse{
			LibraryID:     issued.LibraryID,
			RequestID:     requestID,
			Status:        joinSessionStatusPending,
			Message:       "join request pending approval",
			Role:          issued.Role,
			OwnerDeviceID: local.DeviceID,
			OwnerRole:     local.Role,
			OwnerPeerID:   localPeerID,
			PeerAddrHint:  payload.PeerAddr,
			ExpiresAt:     issued.ExpiresAt.UTC().Unix(),
		}
		return nil
	})
	return response, err
}

func (s *InviteService) handleInviteJoinStatus(ctx context.Context, libraryID, localPeerID, actualPeerID string, req inviteJoinStatusRequest) (inviteJoinStatusResponse, error) {
	req.LibraryID = strings.TrimSpace(req.LibraryID)
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.PeerID = strings.TrimSpace(req.PeerID)
	if req.LibraryID == "" || req.RequestID == "" || req.DeviceID == "" {
		return inviteJoinStatusResponse{}, fmt.Errorf("library id, request id, and device id are required")
	}
	if req.LibraryID != strings.TrimSpace(libraryID) {
		return inviteJoinStatusResponse{}, fmt.Errorf("invite join status library mismatch")
	}
	if req.PeerID != "" && req.PeerID != strings.TrimSpace(actualPeerID) {
		return inviteJoinStatusResponse{}, fmt.Errorf("invite join status peer mismatch")
	}

	now := time.Now().UTC()
	var row InviteJoinRequest
	if err := s.app.storage.WithContext(ctx).Where("request_id = ?", req.RequestID).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return inviteJoinStatusResponse{}, fmt.Errorf("invite request not found")
		}
		return inviteJoinStatusResponse{}, err
	}
	if strings.TrimSpace(row.LibraryID) != req.LibraryID {
		return inviteJoinStatusResponse{}, fmt.Errorf("invite request belongs to a different library")
	}
	if strings.TrimSpace(row.DeviceID) != req.DeviceID {
		return inviteJoinStatusResponse{}, fmt.Errorf("invite request device mismatch")
	}
	if strings.TrimSpace(row.PeerID) != strings.TrimSpace(actualPeerID) {
		return inviteJoinStatusResponse{}, fmt.Errorf("invite request peer mismatch")
	}
	row, changed := normalizeJoinRequestRecord(row, now)
	if changed {
		if err := s.app.storage.WithContext(ctx).Model(&InviteJoinRequest{}).
			Where("request_id = ?", row.RequestID).
			Updates(map[string]any{
				"status":     row.Status,
				"message":    row.Message,
				"updated_at": row.UpdatedAt,
			}).Error; err != nil {
			return inviteJoinStatusResponse{}, err
		}
	}
	return inviteJoinStatusResponse{
		LibraryID:     row.LibraryID,
		RequestID:     row.RequestID,
		Status:        normalizeJoinSessionStatus(row.Status),
		Message:       firstNonEmpty(strings.TrimSpace(row.Message), "join request pending approval"),
		Role:          firstNonEmpty(strings.TrimSpace(row.ApprovedRole), strings.TrimSpace(row.RequestedRole)),
		OwnerDeviceID: strings.TrimSpace(row.OwnerDeviceID),
		OwnerRole:     strings.TrimSpace(row.OwnerRole),
		OwnerPeerID:   firstNonEmpty(strings.TrimSpace(row.OwnerPeerID), strings.TrimSpace(localPeerID)),
		OwnerFingerprint: func() string {
			if strings.TrimSpace(row.OwnerFingerprint) != "" {
				return strings.TrimSpace(row.OwnerFingerprint)
			}
			if strings.TrimSpace(row.OwnerDeviceID) == "" || strings.TrimSpace(row.OwnerPeerID) == "" {
				return ""
			}
			return fingerprintForDevice(row.OwnerDeviceID, row.OwnerPeerID)
		}(),
		EncryptedMaterial: append([]byte(nil), row.EncryptedMaterial...),
		ExpiresAt:         row.ExpiresAt.UTC().Unix(),
		UpdatedAt:         row.UpdatedAt.UTC().UnixNano(),
	}, nil
}

func (s *InviteService) handleInviteJoinCancel(ctx context.Context, libraryID, actualPeerID string, req inviteJoinCancelRequest) (inviteJoinCancelResponse, error) {
	req.LibraryID = strings.TrimSpace(req.LibraryID)
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.PeerID = strings.TrimSpace(req.PeerID)
	reason := strings.TrimSpace(req.Reason)
	if req.LibraryID == "" || req.RequestID == "" || req.DeviceID == "" {
		return inviteJoinCancelResponse{}, fmt.Errorf("library id, request id, and device id are required")
	}
	if req.LibraryID != strings.TrimSpace(libraryID) {
		return inviteJoinCancelResponse{}, fmt.Errorf("invite join cancel library mismatch")
	}
	if req.PeerID != "" && req.PeerID != strings.TrimSpace(actualPeerID) {
		return inviteJoinCancelResponse{}, fmt.Errorf("invite join cancel peer mismatch")
	}
	if reason == "" {
		reason = "canceled by requester"
	}

	now := time.Now().UTC()
	if err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row InviteJoinRequest
		if err := tx.Where("request_id = ?", req.RequestID).Take(&row).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("invite request not found")
			}
			return err
		}
		if strings.TrimSpace(row.LibraryID) != req.LibraryID {
			return fmt.Errorf("invite request belongs to a different library")
		}
		if strings.TrimSpace(row.DeviceID) != req.DeviceID {
			return fmt.Errorf("invite request device mismatch")
		}
		if strings.TrimSpace(row.PeerID) != strings.TrimSpace(actualPeerID) {
			return fmt.Errorf("invite request peer mismatch")
		}
		row, changed := normalizeJoinRequestRecord(row, now)
		if changed {
			if err := tx.Model(&InviteJoinRequest{}).
				Where("request_id = ?", row.RequestID).
				Updates(map[string]any{
					"status":     row.Status,
					"message":    row.Message,
					"updated_at": row.UpdatedAt,
				}).Error; err != nil {
				return err
			}
		}
		if row.Status == inviteJoinStatusRejected || row.Status == inviteJoinStatusExpired {
			return nil
		}
		return tx.Model(&InviteJoinRequest{}).
			Where("request_id = ?", row.RequestID).
			Updates(map[string]any{
				"status":     inviteJoinStatusRejected,
				"message":    reason,
				"updated_at": now,
			}).Error
	}); err != nil {
		return inviteJoinCancelResponse{}, err
	}
	return inviteJoinCancelResponse{
		Status:    joinSessionStatusRejected,
		Message:   reason,
		UpdatedAt: now.UTC().UnixNano(),
	}, nil
}

func normalizeJoinSessionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case joinSessionStatusApproved:
		return joinSessionStatusApproved
	case joinSessionStatusRejected:
		return joinSessionStatusRejected
	case joinSessionStatusExpired:
		return joinSessionStatusExpired
	case joinSessionStatusCompleted:
		return joinSessionStatusCompleted
	case joinSessionStatusFailed:
		return joinSessionStatusFailed
	case joinSessionStatusPending:
		return joinSessionStatusPending
	default:
		return ""
	}
}

func isTerminalJoinSessionStatus(status string) bool {
	switch normalizeJoinSessionStatus(status) {
	case joinSessionStatusRejected, joinSessionStatusExpired, joinSessionStatusCompleted, joinSessionStatusFailed:
		return true
	default:
		return false
	}
}

func preferredInvitePeerAddr(listenAddrs []string) string {
	best := ""
	bestScore := -1
	for _, addr := range listenAddrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		score := 0
		switch {
		case strings.Contains(addr, "/ip4/0.0.0.0/") || strings.Contains(addr, "/ip6/::/"):
			score = 0
		case strings.Contains(addr, "/ip4/127.0.0.1/") || strings.Contains(addr, "/ip6/::1/"):
			score = 1
		default:
			score = 2
		}
		if score > bestScore {
			best = addr
			bestScore = score
		}
	}
	return best
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
	phase := JobPhaseRunning
	progress := 0.5
	switch strings.TrimSpace(session.Status) {
	case joinSessionStatusPending:
		phase = JobPhaseRunning
		progress = 0.25
	case joinSessionStatusApproved:
		phase = JobPhaseRunning
		progress = 0.75
	case joinSessionStatusCompleted:
		phase = JobPhaseCompleted
		progress = 1
	case joinSessionStatusRejected, joinSessionStatusExpired, joinSessionStatusFailed:
		phase = JobPhaseFailed
		progress = 1
	default:
		phase = JobPhaseRunning
		progress = 0.5
	}
	if existing, ok := s.app.jobs.Get(session.SessionID); ok &&
		existing.Kind == jobKindJoinSession &&
		existing.Phase == phase &&
		strings.TrimSpace(existing.Message) == message &&
		existing.Progress == progress {
		return
	}
	switch phase {
	case JobPhaseCompleted:
		job.Complete(progress, message)
	case JobPhaseFailed:
		job.Fail(progress, message, nil)
	default:
		job.Running(progress, message)
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
	err := a.storage.WithContext(ctx).Where("device_id = ?", deviceID).Take(&device).Error
	if err == nil {
		if strings.TrimSpace(device.PeerID) != "" && (expectedPeerID == "" || strings.TrimSpace(device.PeerID) == expectedPeerID) {
			return strings.TrimSpace(device.PeerID), nil
		}
		peerID := firstNonEmpty(expectedPeerID, pseudoPeerID(deviceID))
		if err := a.storage.WithContext(ctx).Model(&Device{}).
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
	if err := a.storage.WithContext(ctx).Create(&Device{
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
