package desktopcore

import (
	"ben/registryauth"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/peer"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	inviteJoinStatusPending   = "pending"
	inviteJoinStatusApproved  = "approved"
	inviteJoinStatusRejected  = "rejected"
	inviteJoinStatusExpired   = "expired"
	inviteJoinStatusCompleted = "completed"
	inviteJoinStatusFailed    = "failed"

	defaultInviteExpiry     = 24 * time.Hour
	activeJoinRequestTTL    = 15 * time.Minute
	terminalJoinResultTTL   = 2 * time.Minute
	inviteCodeCurrentPrefix = "ben-invite-v4"
)

type InviteService struct {
	app *App

	mu       sync.Mutex
	requests map[string]*activeInviteJoinRequest
	attempts map[string]*activeJoinAttempt
}

type inviteCodePayload struct {
	Version             int                             `json:"version,omitempty"`
	TokenID             string                          `json:"tokenId"`
	LibraryID           string                          `json:"libraryId"`
	OwnerPeerID         string                          `json:"ownerPeerId"`
	RegistryURL         string                          `json:"registryUrl"`
	RelayBootstrapAddrs []string                        `json:"relayBootstrapAddrs,omitempty"`
	InviteAuth          *registryauth.InviteAttestation `json:"inviteAuth,omitempty"`
	Role                string                          `json:"role"`
	Reusable            bool                            `json:"reusable,omitempty"`
	ExpiresAt           int64                           `json:"expiresAt,omitempty"`
}

type joinMaterial struct {
	LibraryName        string                 `json:"libraryName"`
	RootPublicKey      string                 `json:"rootPublicKey"`
	LibraryKey         string                 `json:"libraryKey"`
	AdmissionAuthority *joinAuthorityMaterial `json:"admissionAuthority,omitempty"`
	RecoveryToken      string                 `json:"recoveryToken"`
	MembershipCert     membershipCertEnvelope `json:"membershipCert"`
}

type activeInviteJoinRequest struct {
	RequestID         string
	LibraryID         string
	TokenID           string
	Reusable          bool
	DeviceID          string
	DeviceName        string
	PeerID            string
	DeviceFingerprint string
	Role              string
	JoinPubKey        []byte
	Status            string
	Message           string
	OwnerDeviceID     string
	OwnerRole         string
	OwnerPeerID       string
	OwnerFingerprint  string
	EncryptedMaterial []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ExpiresAt         time.Time
	ResultExpiresAt   time.Time
}

type activeJoinAttempt struct {
	AttemptID           string
	RequestID           string
	LibraryID           string
	RegistryURL         string
	RelayBootstrapAddrs []string
	OwnerPeerID         string
	OwnerAddrs          []string
	DeviceID            string
	DeviceName          string
	LocalPeerID         string
	DeviceFingerprint   string
	Role                string
	Status              string
	Message             string
	OwnerDeviceID       string
	OwnerRole           string
	OwnerFingerprint    string
	JoinPublicKey       *[32]byte
	JoinPrivateKey      *[32]byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (s *InviteService) ensureRuntimeStateLocked() {
	if s.requests == nil {
		s.requests = make(map[string]*activeInviteJoinRequest)
	}
	if s.attempts == nil {
		s.attempts = make(map[string]*activeJoinAttempt)
	}
}

func (s *InviteService) clearLibraryRuntimeState(libraryID string) {
	libraryID = strings.TrimSpace(libraryID)
	if s == nil || libraryID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, req := range s.requests {
		if req.LibraryID == libraryID {
			delete(s.requests, id)
		}
	}
	for id, attempt := range s.attempts {
		if attempt.LibraryID == libraryID {
			delete(s.attempts, id)
		}
	}
}

func (s *InviteService) cleanupRuntimeStateLocked(now time.Time) {
	for id, req := range s.requests {
		if req.Status == inviteJoinStatusPending && !req.ExpiresAt.IsZero() && !req.ExpiresAt.After(now) {
			delete(s.requests, id)
			continue
		}
		if req.Status != inviteJoinStatusPending && !req.ResultExpiresAt.IsZero() && !req.ResultExpiresAt.After(now) {
			delete(s.requests, id)
		}
	}
	for id, attempt := range s.attempts {
		if attempt.Status == inviteJoinStatusCompleted || attempt.Status == inviteJoinStatusRejected || attempt.Status == inviteJoinStatusExpired || attempt.Status == inviteJoinStatusFailed {
			if now.Sub(attempt.UpdatedAt) > terminalJoinResultTTL {
				delete(s.attempts, id)
			}
		}
	}
}

func (s *InviteService) CreateInvite(ctx context.Context, req apitypes.InviteCreateRequest) (apitypes.InviteRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.InviteRecord{}, err
	}
	if !canManageLibrary(local.Role) {
		return apitypes.InviteRecord{}, fmt.Errorf("invite creation requires owner or admin role")
	}

	role := normalizeRole(req.Role)
	now := time.Now().UTC()
	expiresAt := time.Time{}
	if !req.Reusable {
		expiresAt = now.Add(defaultInviteExpiry)
	}

	relayCfg, err := s.app.relayConfigForLibrary(ctx, local.LibraryID)
	if err != nil {
		return apitypes.InviteRecord{}, err
	}
	if strings.TrimSpace(relayCfg.RegistryURL) == "" {
		return apitypes.InviteRecord{}, fmt.Errorf("invite creation requires a relay registry url")
	}

	tokenID := uuid.NewString()
	peerID, err := s.ensureInviteOwnerPeer(ctx, local.LibraryID)
	if err != nil {
		return apitypes.InviteRecord{}, err
	}
	inviteAuth, err := s.buildInviteRegistryAuth(ctx, local.LibraryID, tokenID, peerID, expiresAt)
	if err != nil {
		return apitypes.InviteRecord{}, err
	}
	encodedAuth, err := json.Marshal(inviteAuth)
	if err != nil {
		return apitypes.InviteRecord{}, fmt.Errorf("encode invite auth: %w", err)
	}

	if err := s.cleanupExpiredInvites(ctx, local.LibraryID, now); err != nil {
		return apitypes.InviteRecord{}, err
	}
	row := IssuedInvite{
		InviteID:                tokenID,
		LibraryID:               local.LibraryID,
		TokenID:                 tokenID,
		RegistryURL:             relayCfg.RegistryURL,
		RelayBootstrapAddrsJSON: encodeStringListJSON(relayCfg.RelayBootstrapAddrs),
		OwnerPeerID:             peerID,
		InviteAuthJSON:          string(encodedAuth),
		Role:                    role,
		Reusable:                req.Reusable,
		ExpiresAt:               expiresAt,
		CreatedAt:               now,
	}
	if err := s.app.storage.WithContext(ctx).Create(&row).Error; err != nil {
		return apitypes.InviteRecord{}, err
	}
	record, err := s.toInviteRecord(row)
	if err != nil {
		return apitypes.InviteRecord{}, err
	}
	return record, nil
}

func (s *InviteService) ListActiveInvites(ctx context.Context) ([]apitypes.InviteRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := s.cleanupExpiredInvites(ctx, local.LibraryID, now); err != nil {
		return nil, err
	}
	var rows []IssuedInvite
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ?", local.LibraryID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]apitypes.InviteRecord, 0, len(rows))
	for _, row := range rows {
		record, err := s.toInviteRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *InviteService) DeleteInvite(ctx context.Context, inviteID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("invite deletion requires owner or admin role")
	}
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return fmt.Errorf("invite id is required")
	}
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND invite_id = ?", local.LibraryID, inviteID).
		Delete(&IssuedInvite{}).Error; err != nil {
		return err
	}
	s.clearInviteRuntimeRequests(local.LibraryID, inviteID)
	return nil
}

func (s *InviteService) StartJoinFromInvite(ctx context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinAttempt, error) {
	payload, err := decodeInviteCode(req.InviteCode)
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}
	if strings.TrimSpace(payload.RegistryURL) == "" || payload.InviteAuth == nil {
		return apitypes.JoinAttempt{}, fmt.Errorf("invite is missing relay metadata")
	}

	current, err := s.app.ensureCurrentDevice(ctx)
	if err != nil {
		return apitypes.JoinAttempt{}, fmt.Errorf("ensure current device: %w", err)
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		deviceID = current.DeviceID
	}
	deviceName := chooseDeviceName("", req.DeviceName, firstNonEmpty(current.Name, deviceID))
	peerID, err := s.app.ensureDevicePeerID(ctx, deviceID, deviceName)
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}

	discoverTimeout := req.DiscoverTimeout
	if discoverTimeout <= 0 {
		discoverTimeout = defaultInviteDiscoverTimeout
	}
	discoverCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	ownerAddrs, err := s.resolveInviteOwnerAddrs(discoverCtx, payload)
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}
	joinPublicKey, joinPrivateKey, err := generateInviteJoinKeypair()
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}
	client, err := s.app.openInviteClientTransport(payload.RelayBootstrapAddrs)
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}
	defer func() { _ = client.Close() }()

	var startResp inviteJoinStartResponse
	resolvedPeerID, resolvedPeerAddr, err := client.roundTrip(
		discoverCtx,
		strings.Join(ownerAddrs, "\n"),
		payload.OwnerPeerID,
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
		return apitypes.JoinAttempt{}, fmt.Errorf("contact invite host: %w", err)
	}
	if msg := strings.TrimSpace(startResp.Error); msg != "" {
		return apitypes.JoinAttempt{}, fmt.Errorf("%s", msg)
	}
	if strings.TrimSpace(startResp.RequestID) == "" {
		return apitypes.JoinAttempt{}, fmt.Errorf("invite host response missing request id")
	}

	now := time.Now().UTC()
	attempt := &activeJoinAttempt{
		AttemptID:           uuid.NewString(),
		RequestID:           strings.TrimSpace(startResp.RequestID),
		LibraryID:           payload.LibraryID,
		RegistryURL:         payload.RegistryURL,
		RelayBootstrapAddrs: compactNonEmptyStrings(payload.RelayBootstrapAddrs),
		OwnerPeerID:         firstNonEmpty(strings.TrimSpace(startResp.OwnerPeerID), strings.TrimSpace(resolvedPeerID), payload.OwnerPeerID),
		OwnerAddrs:          compactNonEmptyStrings(append(ownerAddrs, resolvedPeerAddr)),
		DeviceID:            deviceID,
		DeviceName:          deviceName,
		LocalPeerID:         peerID,
		DeviceFingerprint:   fingerprintForDevice(deviceID, peerID),
		Role:                firstNonEmpty(strings.TrimSpace(startResp.Role), payload.Role),
		Status:              normalizeJoinStatus(firstNonEmpty(strings.TrimSpace(startResp.Status), inviteJoinStatusPending)),
		Message:             firstNonEmpty(strings.TrimSpace(startResp.Message), "join request pending approval"),
		OwnerDeviceID:       strings.TrimSpace(startResp.OwnerDeviceID),
		OwnerRole:           strings.TrimSpace(startResp.OwnerRole),
		JoinPublicKey:       joinPublicKey,
		JoinPrivateKey:      joinPrivateKey,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	s.mu.Lock()
	s.ensureRuntimeStateLocked()
	s.cleanupRuntimeStateLocked(now)
	s.attempts[attempt.AttemptID] = attempt
	s.mu.Unlock()
	return toJoinAttemptRecord(attempt), nil
}

func (s *InviteService) GetJoinAttempt(ctx context.Context, attemptID string) (apitypes.JoinAttempt, error) {
	attemptID = strings.TrimSpace(attemptID)
	if attemptID == "" {
		return apitypes.JoinAttempt{}, fmt.Errorf("join attempt id is required")
	}
	attempt, err := s.loadJoinAttempt(attemptID)
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}
	if attempt.Status == inviteJoinStatusPending || attempt.Status == inviteJoinStatusApproved {
		if err := s.refreshJoinAttempt(ctx, attempt); err != nil {
			s.updateJoinAttemptError(attemptID, err)
		}
	}
	attempt, err = s.loadJoinAttempt(attemptID)
	if err != nil {
		return apitypes.JoinAttempt{}, err
	}
	return toJoinAttemptRecord(attempt), nil
}

func (s *InviteService) CancelJoinAttempt(_ context.Context, attemptID string) error {
	attemptID = strings.TrimSpace(attemptID)
	if attemptID == "" {
		return fmt.Errorf("join attempt id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, attemptID)
	return nil
}

func (s *InviteService) ListJoinRequests(ctx context.Context) ([]apitypes.InviteJoinRequestRecord, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRuntimeStateLocked()
	s.cleanupRuntimeStateLocked(now)
	out := []apitypes.InviteJoinRequestRecord{}
	for _, req := range s.requests {
		if req.LibraryID != local.LibraryID || req.Status != inviteJoinStatusPending {
			continue
		}
		out = append(out, toInviteJoinRequestRecord(req))
	}
	return out, nil
}

func (s *InviteService) ApproveJoinRequest(ctx context.Context, requestID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("join approval requires owner or admin role")
	}
	req, err := s.pendingJoinRequest(local.LibraryID, requestID)
	if err != nil {
		return err
	}
	ownerPeerID, err := s.app.ensureDevicePeerID(ctx, local.DeviceID, local.Device)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	var encryptedMaterial []byte
	err = s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		material, err := s.buildJoinMaterialTx(tx, req.LibraryID, req.DeviceID, req.PeerID, req.Role, now)
		if err != nil {
			return err
		}
		encryptedMaterial, err = encryptJoinMaterial(req.JoinPubKey, material)
		if err != nil {
			return err
		}
		if err := upsertDeviceMembershipTx(tx, req.LibraryID, req.DeviceID, req.DeviceName, req.PeerID, req.Role, now); err != nil {
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
		if !req.Reusable {
			result := tx.Where("library_id = ? AND token_id = ?", req.LibraryID, req.TokenID).Delete(&IssuedInvite{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("invite is no longer active")
			}
		} else {
			var row IssuedInvite
			if err := tx.Where("library_id = ? AND token_id = ?", req.LibraryID, req.TokenID).Take(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.requests[strings.TrimSpace(requestID)]; current != nil {
		current.Status = inviteJoinStatusApproved
		current.Message = "join request approved"
		current.OwnerDeviceID = local.DeviceID
		current.OwnerRole = local.Role
		current.OwnerPeerID = ownerPeerID
		current.OwnerFingerprint = fingerprintForDevice(local.DeviceID, ownerPeerID)
		current.EncryptedMaterial = append([]byte(nil), encryptedMaterial...)
		current.UpdatedAt = now
		current.ResultExpiresAt = now.Add(terminalJoinResultTTL)
	}
	if !req.Reusable {
		for id, candidate := range s.requests {
			if id == strings.TrimSpace(requestID) {
				continue
			}
			if candidate.LibraryID == req.LibraryID && candidate.TokenID == req.TokenID && candidate.Status == inviteJoinStatusPending {
				candidate.Status = inviteJoinStatusRejected
				candidate.Message = "invite already used"
				candidate.UpdatedAt = now
				candidate.ResultExpiresAt = now.Add(terminalJoinResultTTL)
			}
		}
	}
	return nil
}

func (s *InviteService) RejectJoinRequest(ctx context.Context, requestID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canManageLibrary(local.Role) {
		return fmt.Errorf("join rejection requires owner or admin role")
	}
	req, err := s.pendingJoinRequest(local.LibraryID, requestID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.requests[req.RequestID]; current != nil {
		current.Status = inviteJoinStatusRejected
		current.Message = "join request rejected"
		current.UpdatedAt = now
		current.ResultExpiresAt = now.Add(terminalJoinResultTTL)
	}
	return nil
}

func (s *InviteService) handleInviteJoinStart(ctx context.Context, libraryID, localPeerID, actualPeerID string, req inviteJoinStartRequest) (inviteJoinStartResponse, error) {
	payload, err := decodeInviteCode(req.InviteCode)
	if err != nil {
		return inviteJoinStartResponse{}, err
	}
	libraryID = strings.TrimSpace(libraryID)
	if payload.LibraryID != libraryID {
		return inviteJoinStartResponse{}, fmt.Errorf("invite library mismatch")
	}
	if payload.OwnerPeerID != "" && payload.OwnerPeerID != strings.TrimSpace(localPeerID) {
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
	if local.LibraryID != libraryID {
		return inviteJoinStartResponse{}, fmt.Errorf("invite host is not serving the requested library")
	}

	now := time.Now().UTC()
	row, err := s.activeInviteByToken(ctx, libraryID, payload.TokenID, now)
	if err != nil {
		return inviteJoinStartResponse{}, err
	}
	if row.InviteID == "" {
		return inviteJoinStartResponse{}, fmt.Errorf("invite not found")
	}

	request := s.upsertActiveJoinRequest(row, req, local, localPeerID, now)
	return inviteJoinStartResponse{
		LibraryID:     request.LibraryID,
		RequestID:     request.RequestID,
		Status:        request.Status,
		Message:       request.Message,
		Role:          request.Role,
		OwnerDeviceID: local.DeviceID,
		OwnerRole:     local.Role,
		OwnerPeerID:   strings.TrimSpace(localPeerID),
		UpdatedAt:     request.UpdatedAt.UnixNano(),
	}, nil
}

func (s *InviteService) handleInviteJoinStatus(_ context.Context, libraryID, localPeerID, actualPeerID string, req inviteJoinStatusRequest) (inviteJoinStatusResponse, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRuntimeStateLocked()
	s.cleanupRuntimeStateLocked(now)
	row := s.requests[req.RequestID]
	if row == nil {
		return inviteJoinStatusResponse{}, fmt.Errorf("join request is no longer active")
	}
	if row.LibraryID != req.LibraryID || row.DeviceID != req.DeviceID || row.PeerID != strings.TrimSpace(actualPeerID) {
		return inviteJoinStatusResponse{}, fmt.Errorf("join request identity mismatch")
	}
	if row.Status == inviteJoinStatusPending && !row.ExpiresAt.After(now) {
		row.Status = inviteJoinStatusExpired
		row.Message = "invite request expired"
		row.UpdatedAt = now
		row.ResultExpiresAt = now.Add(terminalJoinResultTTL)
	}
	return inviteJoinStatusResponse{
		LibraryID:         row.LibraryID,
		RequestID:         row.RequestID,
		Status:            row.Status,
		Message:           row.Message,
		Role:              row.Role,
		OwnerDeviceID:     row.OwnerDeviceID,
		OwnerRole:         row.OwnerRole,
		OwnerPeerID:       firstNonEmpty(row.OwnerPeerID, strings.TrimSpace(localPeerID)),
		OwnerFingerprint:  row.OwnerFingerprint,
		EncryptedMaterial: append([]byte(nil), row.EncryptedMaterial...),
		UpdatedAt:         row.UpdatedAt.UnixNano(),
	}, nil
}

func (s *InviteService) upsertActiveJoinRequest(row IssuedInvite, req inviteJoinStartRequest, local apitypes.LocalContext, ownerPeerID string, now time.Time) *activeInviteJoinRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRuntimeStateLocked()
	s.cleanupRuntimeStateLocked(now)
	for _, existing := range s.requests {
		if existing.LibraryID == row.LibraryID && existing.TokenID == row.TokenID && existing.DeviceID == req.DeviceID && existing.PeerID == req.PeerID && existing.Status == inviteJoinStatusPending {
			existing.DeviceName = req.DeviceName
			existing.JoinPubKey = append([]byte(nil), req.JoinPubKey...)
			existing.UpdatedAt = now
			return existing
		}
	}
	request := &activeInviteJoinRequest{
		RequestID:         uuid.NewString(),
		LibraryID:         row.LibraryID,
		TokenID:           row.TokenID,
		Reusable:          row.Reusable,
		DeviceID:          req.DeviceID,
		DeviceName:        req.DeviceName,
		PeerID:            req.PeerID,
		DeviceFingerprint: fingerprintForDevice(req.DeviceID, req.PeerID),
		Role:              normalizeRole(row.Role),
		JoinPubKey:        append([]byte(nil), req.JoinPubKey...),
		Status:            inviteJoinStatusPending,
		Message:           "join request pending approval",
		OwnerDeviceID:     local.DeviceID,
		OwnerRole:         local.Role,
		OwnerPeerID:       strings.TrimSpace(ownerPeerID),
		OwnerFingerprint:  fingerprintForDevice(local.DeviceID, ownerPeerID),
		CreatedAt:         now,
		UpdatedAt:         now,
		ExpiresAt:         now.Add(activeJoinRequestTTL),
	}
	s.requests[request.RequestID] = request
	return request
}

func (s *InviteService) refreshJoinAttempt(ctx context.Context, attempt *activeJoinAttempt) error {
	client, err := s.app.openInviteClientTransport(attempt.RelayBootstrapAddrs)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	refreshCtx, cancel := context.WithTimeout(ctx, defaultInviteDiscoverTimeout)
	defer cancel()

	ownerAddrs := attempt.OwnerAddrs
	if len(ownerAddrs) == 0 {
		ownerAddrs = relayCircuitAddrsForPeer(attempt.RelayBootstrapAddrs, attempt.OwnerPeerID)
	}
	var resp inviteJoinStatusResponse
	resolvedPeerID, resolvedPeerAddr, err := client.roundTrip(
		refreshCtx,
		strings.Join(ownerAddrs, "\n"),
		attempt.OwnerPeerID,
		desktopInviteJoinStatusProtocolID,
		inviteJoinStatusRequest{
			LibraryID: attempt.LibraryID,
			RequestID: attempt.RequestID,
			DeviceID:  attempt.DeviceID,
			PeerID:    attempt.LocalPeerID,
		},
		&resp,
	)
	if err != nil {
		return err
	}
	if msg := strings.TrimSpace(resp.Error); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	now := time.Now().UTC()
	s.mu.Lock()
	current := s.attempts[attempt.AttemptID]
	if current != nil {
		current.OwnerPeerID = firstNonEmpty(strings.TrimSpace(resp.OwnerPeerID), strings.TrimSpace(resolvedPeerID), current.OwnerPeerID)
		current.OwnerAddrs = compactNonEmptyStrings(append(current.OwnerAddrs, resolvedPeerAddr))
		current.OwnerDeviceID = firstNonEmpty(strings.TrimSpace(resp.OwnerDeviceID), current.OwnerDeviceID)
		current.OwnerRole = firstNonEmpty(strings.TrimSpace(resp.OwnerRole), current.OwnerRole)
		current.OwnerFingerprint = firstNonEmpty(strings.TrimSpace(resp.OwnerFingerprint), current.OwnerFingerprint)
		current.Role = firstNonEmpty(strings.TrimSpace(resp.Role), current.Role)
		current.Status = normalizeJoinStatus(resp.Status)
		current.Message = firstNonEmpty(strings.TrimSpace(resp.Message), current.Message)
		current.UpdatedAt = now
	}
	s.mu.Unlock()

	if normalizeJoinStatus(resp.Status) == inviteJoinStatusApproved {
		return s.finalizeApprovedJoin(ctx, attempt.AttemptID, resp)
	}
	return nil
}

func (s *InviteService) finalizeApprovedJoin(ctx context.Context, attemptID string, resp inviteJoinStatusResponse) error {
	attempt, err := s.loadJoinAttempt(attemptID)
	if err != nil {
		return err
	}
	material, err := decryptJoinMaterial(resp.EncryptedMaterial, attempt.JoinPublicKey, attempt.JoinPrivateKey)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	err = s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := restoreJoinMaterialTx(tx, attempt.LibraryID, material, now); err != nil {
			return err
		}
		if err := tx.Model(&Library{}).
			Where("library_id = ?", attempt.LibraryID).
			Updates(map[string]any{
				"registry_url":               strings.TrimSpace(attempt.RegistryURL),
				"relay_bootstrap_addrs_json": encodeStringListJSON(attempt.RelayBootstrapAddrs),
			}).Error; err != nil {
			return err
		}
		if err := upsertDeviceMembershipTx(tx, attempt.LibraryID, attempt.DeviceID, attempt.DeviceName, attempt.LocalPeerID, attempt.Role, now); err != nil {
			return err
		}
		if material.RecoveryToken != "" {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"token_hash":          hashMembershipRecoveryToken(material.RecoveryToken),
					"issued_by_device_id": strings.TrimSpace(resp.OwnerDeviceID),
					"updated_at":          now,
				}),
			}).Create(&MembershipRecovery{
				LibraryID:        attempt.LibraryID,
				DeviceID:         attempt.DeviceID,
				TokenHash:        hashMembershipRecoveryToken(material.RecoveryToken),
				IssuedByDeviceID: strings.TrimSpace(resp.OwnerDeviceID),
				CreatedAt:        now,
				UpdatedAt:        now,
			}).Error; err != nil {
				return err
			}
			if err := upsertLocalSettingTx(tx, membershipRecoveryLocalSettingKey(attempt.LibraryID, attempt.DeviceID), material.RecoveryToken, now); err != nil {
				return err
			}
		}
		if len(material.MembershipCert.Sig) > 0 {
			if err := saveMembershipCertTx(tx, material.MembershipCert); err != nil {
				return err
			}
		}
		if strings.TrimSpace(resp.OwnerDeviceID) != "" {
			if err := upsertDeviceMembershipTx(tx, attempt.LibraryID, resp.OwnerDeviceID, resp.OwnerDeviceID, firstNonEmpty(resp.OwnerPeerID, attempt.OwnerPeerID), resp.OwnerRole, now); err != nil {
				return err
			}
		}
		current, err := s.app.ensureCurrentDeviceTx(tx)
		if err != nil {
			return err
		}
		if current.DeviceID == attempt.DeviceID {
			if err := tx.Model(&Device{}).
				Where("device_id = ?", current.DeviceID).
				Update("active_library_id", attempt.LibraryID).Error; err != nil {
				return err
			}
		}
		return ensureLikedPlaylistTx(tx, attempt.LibraryID, attempt.DeviceID, now)
	})
	if err != nil {
		s.updateJoinAttemptTerminal(attemptID, inviteJoinStatusFailed, err.Error())
		return err
	}
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		s.updateJoinAttemptTerminal(attemptID, inviteJoinStatusFailed, err.Error())
		return err
	}
	s.updateJoinAttemptTerminal(attemptID, inviteJoinStatusCompleted, "join completed")
	return nil
}

func (s *InviteService) pendingJoinRequest(libraryID, requestID string) (*activeInviteJoinRequest, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request id is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRuntimeStateLocked()
	s.cleanupRuntimeStateLocked(now)
	req := s.requests[requestID]
	if req == nil || req.LibraryID != strings.TrimSpace(libraryID) {
		return nil, fmt.Errorf("join request not found")
	}
	if req.Status != inviteJoinStatusPending {
		return nil, fmt.Errorf("join request is %s", req.Status)
	}
	copyReq := *req
	copyReq.JoinPubKey = append([]byte(nil), req.JoinPubKey...)
	return &copyReq, nil
}

func (s *InviteService) loadJoinAttempt(attemptID string) (*activeJoinAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRuntimeStateLocked()
	s.cleanupRuntimeStateLocked(time.Now().UTC())
	attempt := s.attempts[strings.TrimSpace(attemptID)]
	if attempt == nil {
		return nil, fmt.Errorf("join attempt not found")
	}
	copyAttempt := *attempt
	copyAttempt.RelayBootstrapAddrs = append([]string(nil), attempt.RelayBootstrapAddrs...)
	copyAttempt.OwnerAddrs = append([]string(nil), attempt.OwnerAddrs...)
	return &copyAttempt, nil
}

func (s *InviteService) updateJoinAttemptError(attemptID string, err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt := s.attempts[strings.TrimSpace(attemptID)]; attempt != nil && attempt.Status == inviteJoinStatusPending {
		attempt.Message = err.Error()
		attempt.UpdatedAt = time.Now().UTC()
	}
}

func (s *InviteService) updateJoinAttemptTerminal(attemptID, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt := s.attempts[strings.TrimSpace(attemptID)]; attempt != nil {
		attempt.Status = normalizeJoinStatus(status)
		attempt.Message = strings.TrimSpace(message)
		attempt.UpdatedAt = time.Now().UTC()
	}
}

func (s *InviteService) clearInviteRuntimeRequests(libraryID, tokenID string) {
	libraryID = strings.TrimSpace(libraryID)
	tokenID = strings.TrimSpace(tokenID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, req := range s.requests {
		if req.LibraryID == libraryID && req.TokenID == tokenID {
			delete(s.requests, id)
		}
	}
}

func (s *InviteService) activeInviteByToken(ctx context.Context, libraryID, tokenID string, now time.Time) (IssuedInvite, error) {
	if err := s.cleanupExpiredInvites(ctx, libraryID, now); err != nil {
		return IssuedInvite{}, err
	}
	var row IssuedInvite
	err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND token_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(tokenID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return IssuedInvite{}, nil
		}
		return IssuedInvite{}, err
	}
	return row, nil
}

func (s *InviteService) cleanupExpiredInvites(ctx context.Context, libraryID string, now time.Time) error {
	return s.app.storage.WithContext(ctx).
		Where("library_id = ? AND reusable = ? AND expires_at < ?", strings.TrimSpace(libraryID), false, now.UTC()).
		Delete(&IssuedInvite{}).Error
}

func (s *InviteService) ensureInviteOwnerPeer(ctx context.Context, libraryID string) (string, error) {
	relayAddrs, err := s.app.relayBootstrapAddrsForLibrary(ctx, libraryID, nil)
	if err != nil {
		return "", err
	}
	relayCfg, err := s.app.relayConfigForLibrary(ctx, libraryID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(relayCfg.RegistryURL) == "" {
		return "", fmt.Errorf("invite creation requires a relay registry url")
	}
	if len(relayAddrs) == 0 {
		return "", fmt.Errorf("invite creation requires relay bootstrap addresses")
	}
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		return "", err
	}
	runtime := s.app.transportService.activeRuntimeForLibrary(libraryID)
	if runtime == nil || runtime.transport == nil {
		return "", fmt.Errorf("invite transport is not running")
	}
	peerID := strings.TrimSpace(runtime.transport.LocalPeerID())
	if peerID == "" {
		return "", fmt.Errorf("invite transport peer id is unavailable")
	}
	if err := s.app.ensureActiveTransportRelayReservation(ctx, defaultInviteDiscoverTimeout); err != nil {
		return "", err
	}
	if reporter, ok := runtime.transport.(*libp2pSyncTransport); ok && len(reporter.relayReservationAddrs()) == 0 {
		return "", fmt.Errorf("invite relay reservation is not active")
	}
	if err := s.app.transportService.announceRuntimePresence(runtime); err != nil {
		return "", err
	}
	return peerID, nil
}

func (s *InviteService) buildInviteRegistryAuth(ctx context.Context, libraryID, tokenID, ownerPeerID string, expiresAt time.Time) (registryauth.InviteAttestation, error) {
	var library Library
	if err := s.app.storage.WithContext(ctx).Where("library_id = ?", strings.TrimSpace(libraryID)).Take(&library).Error; err != nil {
		return registryauth.InviteAttestation{}, err
	}
	if strings.TrimSpace(library.RootPrivateKey) == "" {
		return registryauth.InviteAttestation{}, fmt.Errorf("library root private key is required")
	}
	expiry := int64(0)
	if !expiresAt.IsZero() {
		expiry = expiresAt.UTC().Unix()
	}
	invite, err := registryauth.SignInviteAttestation(registryauth.InviteAttestation{
		LibraryID:     strings.TrimSpace(libraryID),
		TokenID:       strings.TrimSpace(tokenID),
		OwnerPeerID:   strings.TrimSpace(ownerPeerID),
		RootPublicKey: strings.TrimSpace(library.RootPublicKey),
		ExpiresAt:     expiry,
	}, library.RootPrivateKey)
	if err != nil {
		return registryauth.InviteAttestation{}, fmt.Errorf("sign invite attestation: %w", err)
	}
	return invite, nil
}

func (s *InviteService) toInviteRecord(row IssuedInvite) (apitypes.InviteRecord, error) {
	code, err := inviteCodeForRow(row)
	if err != nil {
		return apitypes.InviteRecord{}, err
	}
	return apitypes.InviteRecord{
		InviteID:   strings.TrimSpace(row.InviteID),
		LibraryID:  strings.TrimSpace(row.LibraryID),
		InviteCode: code,
		Role:       normalizeRole(row.Role),
		Reusable:   row.Reusable,
		ExpiresAt:  row.ExpiresAt,
		CreatedAt:  row.CreatedAt,
	}, nil
}

func inviteCodeForRow(row IssuedInvite) (string, error) {
	auth, err := inviteAuthFromJSON(row.InviteAuthJSON)
	if err != nil {
		return "", err
	}
	expiresAt := int64(0)
	if !row.ExpiresAt.IsZero() {
		expiresAt = row.ExpiresAt.UTC().Unix()
	}
	return encodeInviteCode(inviteCodePayload{
		Version:             4,
		TokenID:             strings.TrimSpace(row.TokenID),
		LibraryID:           strings.TrimSpace(row.LibraryID),
		OwnerPeerID:         strings.TrimSpace(row.OwnerPeerID),
		RegistryURL:         strings.TrimSpace(row.RegistryURL),
		RelayBootstrapAddrs: decodeStringListJSON(row.RelayBootstrapAddrsJSON),
		InviteAuth:          &auth,
		Role:                normalizeRole(row.Role),
		Reusable:            row.Reusable,
		ExpiresAt:           expiresAt,
	})
}

func inviteAuthFromJSON(value string) (registryauth.InviteAttestation, error) {
	var out registryauth.InviteAttestation
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &out); err != nil {
		return registryauth.InviteAttestation{}, fmt.Errorf("decode invite auth: %w", err)
	}
	return out, nil
}

func (s *InviteService) resolveInviteOwnerAddrs(ctx context.Context, payload inviteCodePayload) ([]string, error) {
	locator := newPeerLocator(payload.RegistryURL)
	if locator != nil && payload.InviteAuth != nil {
		record, ok, err := locator.LookupInviteOwner(ctx, registryauth.InviteOwnerLookupRequest{Invite: *payload.InviteAuth})
		if err != nil {
			return nil, err
		}
		if ok {
			if addrs := filterRelayInviteAddrs(record.Addrs); len(addrs) > 0 {
				return addrs, nil
			}
		}
	}
	return nil, fmt.Errorf("invite host unavailable; keep the invite device online and try again")
}

func filterRelayInviteAddrs(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range compactNonEmptyStrings(addrs) {
		if strings.Contains(addr, "/p2p-circuit") {
			out = append(out, addr)
		}
	}
	return compactNonEmptyStrings(out)
}

func relayCircuitAddrsForPeer(relayAddrs []string, peerID string) []string {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return nil
	}
	out := make([]string, 0, len(relayAddrs))
	for _, addr := range compactNonEmptyStrings(relayAddrs) {
		info, err := peer.AddrInfoFromString(addr)
		if err != nil || info == nil || info.ID == "" {
			continue
		}
		out = append(out, relayReservationBootstrapAddrs(*info, peer.ID(peerID))...)
	}
	return compactNonEmptyStrings(out)
}

func toInviteJoinRequestRecord(row *activeInviteJoinRequest) apitypes.InviteJoinRequestRecord {
	if row == nil {
		return apitypes.InviteJoinRequestRecord{}
	}
	return apitypes.InviteJoinRequestRecord{
		RequestID:         strings.TrimSpace(row.RequestID),
		LibraryID:         strings.TrimSpace(row.LibraryID),
		DeviceID:          strings.TrimSpace(row.DeviceID),
		DeviceName:        strings.TrimSpace(row.DeviceName),
		PeerID:            strings.TrimSpace(row.PeerID),
		DeviceFingerprint: strings.TrimSpace(row.DeviceFingerprint),
		Role:              normalizeRole(row.Role),
		CreatedAt:         row.CreatedAt,
		ExpiresAt:         row.ExpiresAt,
	}
}

func toJoinAttemptRecord(row *activeJoinAttempt) apitypes.JoinAttempt {
	if row == nil {
		return apitypes.JoinAttempt{}
	}
	status := normalizeJoinStatus(row.Status)
	return apitypes.JoinAttempt{
		AttemptID:     strings.TrimSpace(row.AttemptID),
		RequestID:     strings.TrimSpace(row.RequestID),
		Status:        status,
		Message:       strings.TrimSpace(row.Message),
		LibraryID:     strings.TrimSpace(row.LibraryID),
		Role:          normalizeRole(row.Role),
		Pending:       status == inviteJoinStatusPending || status == inviteJoinStatusApproved,
		OwnerDeviceID: strings.TrimSpace(row.OwnerDeviceID),
		OwnerRole:     strings.TrimSpace(row.OwnerRole),
		OwnerPeerID:   strings.TrimSpace(row.OwnerPeerID),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func normalizeJoinStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case inviteJoinStatusApproved:
		return inviteJoinStatusApproved
	case inviteJoinStatusRejected:
		return inviteJoinStatusRejected
	case inviteJoinStatusExpired:
		return inviteJoinStatusExpired
	case inviteJoinStatusCompleted:
		return inviteJoinStatusCompleted
	case inviteJoinStatusFailed:
		return inviteJoinStatusFailed
	default:
		return inviteJoinStatusPending
	}
}

func encodeInviteCode(payload inviteCodePayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode invite code: %w", err)
	}
	return fmt.Sprintf("%s.%s", inviteCodeCurrentPrefix, base64.RawURLEncoding.EncodeToString(body)), nil
}

func decodeInviteCode(code string) (inviteCodePayload, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return inviteCodePayload{}, fmt.Errorf("invite code is required")
	}
	parts := strings.SplitN(code, ".", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) != inviteCodeCurrentPrefix {
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
	payload.Version = 4
	payload.TokenID = strings.TrimSpace(payload.TokenID)
	payload.LibraryID = strings.TrimSpace(payload.LibraryID)
	payload.OwnerPeerID = strings.TrimSpace(payload.OwnerPeerID)
	payload.RegistryURL = strings.TrimSpace(payload.RegistryURL)
	payload.RelayBootstrapAddrs = compactNonEmptyStrings(payload.RelayBootstrapAddrs)
	payload.Role = normalizeRole(payload.Role)
	if payload.TokenID == "" || payload.LibraryID == "" || payload.OwnerPeerID == "" {
		return inviteCodePayload{}, fmt.Errorf("invalid invite code")
	}
	if payload.ExpiresAt > 0 && time.Now().UTC().Unix() > payload.ExpiresAt {
		return inviteCodePayload{}, fmt.Errorf("invite expired")
	}
	if payload.InviteAuth != nil {
		if err := registryauth.VerifyInviteAttestation(*payload.InviteAuth, time.Now().UTC()); err != nil {
			return inviteCodePayload{}, fmt.Errorf("invalid invite registry auth: %w", err)
		}
		if strings.TrimSpace(payload.InviteAuth.LibraryID) != payload.LibraryID ||
			strings.TrimSpace(payload.InviteAuth.TokenID) != payload.TokenID ||
			strings.TrimSpace(payload.InviteAuth.OwnerPeerID) != payload.OwnerPeerID {
			return inviteCodePayload{}, fmt.Errorf("invalid invite registry auth")
		}
	}
	return payload, nil
}

func serviceTagForLibrary(libraryID string) string {
	sum := sha256.Sum256([]byte("service-tag:" + strings.TrimSpace(libraryID)))
	return "ben-" + hex.EncodeToString(sum[:6])
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
