package desktopcore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"golang.org/x/crypto/nacl/box"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MembershipRefreshRequest struct {
	LibraryID     string `json:"libraryId"`
	DeviceID      string `json:"deviceId"`
	PeerID        string `json:"peerId"`
	RecoveryToken string `json:"recoveryToken"`
	RefreshPubKey []byte `json:"refreshPubKey,omitempty"`
}

type MembershipRefreshResponse struct {
	LibraryID              string                       `json:"libraryId"`
	DeviceID               string                       `json:"deviceId"`
	PeerID                 string                       `json:"peerId"`
	AuthorityChain         []admissionAuthorityEnvelope `json:"authorityChain,omitempty"`
	MembershipCert         *membershipCertEnvelope      `json:"membershipCert,omitempty"`
	EncryptedAdminMaterial []byte                       `json:"encryptedAdminMaterial,omitempty"`
	Error                  string                       `json:"error,omitempty"`
}

type encryptedAdminMaterial struct {
	PrivateKey string `json:"privateKey"`
}

func hashMembershipRecoveryToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func saveLocalAdmissionAuthorityPrivateKeyTx(tx *gorm.DB, libraryID string, version int64, privateKey string, now time.Time) error {
	if strings.TrimSpace(libraryID) == "" || version <= 0 {
		return fmt.Errorf("library id and authority version are required")
	}
	if _, err := decodeEd25519PrivateKey(privateKey); err != nil {
		return fmt.Errorf("decode admission authority private key: %w", err)
	}
	return upsertLocalSettingTx(tx, admissionAuthorityPrivateKeyLocalSettingKey(libraryID, version), privateKey, now.UTC())
}

func rotateAdmissionAuthorityTx(tx *gorm.DB, libraryID string) (AdmissionAuthority, []AdmissionAuthority, string, error) {
	head, chain, signerPrivateKey, err := currentAdmissionAuthoritySigningMaterialTx(tx, libraryID)
	if err != nil {
		return AdmissionAuthority{}, nil, "", err
	}

	nextPublicKey, nextPrivateKey, err := generateSigningKeyPair()
	if err != nil {
		return AdmissionAuthority{}, nil, "", fmt.Errorf("generate rotated admission authority keypair: %w", err)
	}
	signerKey, err := decodeEd25519PrivateKey(signerPrivateKey)
	if err != nil {
		return AdmissionAuthority{}, nil, "", fmt.Errorf("decode current admission authority private key: %w", err)
	}

	now := time.Now().UTC()
	envelope := admissionAuthorityEnvelope{
		Version:      head.Version + 1,
		PublicKey:    nextPublicKey,
		PrevVersion:  head.Version,
		SignedByKind: admissionAuthoritySignedByAuthority,
		CreatedAt:    now.UnixNano(),
	}
	payload, err := admissionAuthoritySigningPayload(strings.TrimSpace(libraryID), envelope)
	if err != nil {
		return AdmissionAuthority{}, nil, "", err
	}
	envelope.Sig = ed25519.Sign(ed25519.PrivateKey(signerKey), payload)

	row := AdmissionAuthority{
		LibraryID:    strings.TrimSpace(libraryID),
		Version:      envelope.Version,
		PublicKey:    envelope.PublicKey,
		PrevVersion:  envelope.PrevVersion,
		SignedByKind: envelope.SignedByKind,
		Sig:          append([]byte(nil), envelope.Sig...),
		CreatedAt:    now,
	}
	if err := tx.Create(&row).Error; err != nil {
		return AdmissionAuthority{}, nil, "", err
	}
	if err := saveLocalAdmissionAuthorityPrivateKeyTx(tx, libraryID, row.Version, nextPrivateKey, now); err != nil {
		return AdmissionAuthority{}, nil, "", err
	}

	chain = append(chain, row)
	sort.Slice(chain, func(i, j int) bool {
		return chain[i].Version < chain[j].Version
	})
	return row, chain, nextPrivateKey, nil
}

func loadManagedMembershipTx(tx *gorm.DB, libraryID, deviceID string) (Membership, error) {
	var membership Membership
	if err := tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Take(&membership).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return Membership{}, fmt.Errorf("device is not a member of library %s", strings.TrimSpace(libraryID))
		}
		return Membership{}, err
	}
	return membership, nil
}

func authorizeManagedMembershipMutation(actor Membership, target Membership, actorDeviceID, targetDeviceID string) error {
	if !canManageLibrary(actor.Role) {
		return fmt.Errorf("member management requires admin role")
	}
	if strings.TrimSpace(actorDeviceID) == strings.TrimSpace(targetDeviceID) {
		return fmt.Errorf("member management cannot target the current device")
	}
	if normalizeRole(target.Role) == roleOwner {
		return fmt.Errorf("owner membership cannot be changed")
	}
	return nil
}

func loadMembershipPeerIDTx(tx *gorm.DB, deviceID, fallback string) (string, error) {
	var device Device
	err := tx.Select("peer_id").Where("device_id = ?", strings.TrimSpace(deviceID)).Take(&device).Error
	switch {
	case err == nil && strings.TrimSpace(device.PeerID) != "":
		return strings.TrimSpace(device.PeerID), nil
	case err == nil:
		return strings.TrimSpace(fallback), nil
	case err == gorm.ErrRecordNotFound:
		return strings.TrimSpace(fallback), nil
	default:
		return "", err
	}
}

func revokeMembershipCertTx(tx *gorm.DB, libraryID, deviceID, reason string, deleteCurrent bool) error {
	var current MembershipCert
	err := tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Take(&current).Error
	switch {
	case err == gorm.ErrRecordNotFound:
		return nil
	case err != nil:
		return err
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "revoked"
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&MembershipCertRevocation{
		LibraryID: strings.TrimSpace(libraryID),
		DeviceID:  strings.TrimSpace(deviceID),
		Serial:    current.Serial,
		PeerID:    strings.TrimSpace(current.PeerID),
		Reason:    reason,
		RevokedAt: time.Now().UTC(),
	}).Error; err != nil {
		return err
	}
	if !deleteCurrent {
		return nil
	}
	return tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Delete(&MembershipCert{}).Error
}

func deleteMembershipSecretsTx(tx *gorm.DB, libraryID, deviceID string) error {
	if err := tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Delete(&MembershipRecovery{}).Error; err != nil {
		return err
	}
	return tx.Where("key = ?", membershipRecoveryLocalSettingKey(libraryID, deviceID)).Delete(&LocalSetting{}).Error
}

func refreshMembershipCertWithRecoveryTx(tx *gorm.DB, libraryID, deviceID, peerID, token string, ttl time.Duration) (membershipCertEnvelope, error) {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	token = strings.TrimSpace(token)
	if libraryID == "" || deviceID == "" || peerID == "" || token == "" {
		return membershipCertEnvelope{}, fmt.Errorf("library id, device id, peer id, and recovery token are required")
	}
	if ttl <= 0 {
		ttl = defaultMembershipCertTTL
	}

	var membership Membership
	var recovery MembershipRecovery
	if err := tx.Where("library_id = ? AND device_id = ?", libraryID, deviceID).Take(&recovery).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return membershipCertEnvelope{}, fmt.Errorf("membership recovery credential not found")
		}
		return membershipCertEnvelope{}, err
	}
	if recovery.TokenHash != hashMembershipRecoveryToken(token) {
		return membershipCertEnvelope{}, fmt.Errorf("membership recovery credential rejected")
	}
	if err := tx.Where("library_id = ? AND device_id = ?", libraryID, deviceID).Take(&membership).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return membershipCertEnvelope{}, fmt.Errorf("membership not found")
		}
		return membershipCertEnvelope{}, err
	}
	if err := tx.Model(&Device{}).Where("device_id = ?", deviceID).Update("peer_id", peerID).Error; err != nil {
		return membershipCertEnvelope{}, err
	}
	return issueMembershipCertTx(tx, libraryID, deviceID, peerID, membership.Role, ttl)
}

func (a *App) localMembershipRecoverySecret(ctx context.Context, libraryID, deviceID string) (string, bool, error) {
	key := membershipRecoveryLocalSettingKey(libraryID, deviceID)
	if strings.TrimSpace(key) == "" {
		return "", false, nil
	}
	var setting LocalSetting
	if err := a.storage.WithContext(ctx).Where("key = ?", key).Take(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	value := strings.TrimSpace(setting.Value)
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func (a *App) requestMembershipRefresh(ctx context.Context, local apitypes.LocalContext, peerID string) (transportPeerAuth, error) {
	recoveryToken, ok, err := a.localMembershipRecoverySecret(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return transportPeerAuth{}, fmt.Errorf("load membership recovery secret: %w", err)
	}
	if !ok || strings.TrimSpace(recoveryToken) == "" {
		return transportPeerAuth{}, fmt.Errorf("membership recovery credential missing")
	}

	transport := a.activeSyncTransport()
	if transport == nil {
		return transportPeerAuth{}, fmt.Errorf("peer transport is not configured")
	}
	peers, err := transport.ListPeers(ctx, local)
	if err != nil {
		return transportPeerAuth{}, fmt.Errorf("list peers for membership refresh: %w", err)
	}
	if len(peers) == 0 {
		return transportPeerAuth{}, fmt.Errorf("membership refresh peer unavailable")
	}

	refreshPubKey, refreshPrivKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return transportPeerAuth{}, fmt.Errorf("generate membership refresh keypair: %w", err)
	}
	rootPublicKey, err := a.libraryRootPublicKey(ctx, local.LibraryID)
	if err != nil {
		return transportPeerAuth{}, err
	}

	seen := make(map[string]struct{}, len(peers))
	var firstErr error
	for _, candidate := range peers {
		key := strings.TrimSpace(candidate.PeerID())
		if key == "" {
			key = strings.TrimSpace(candidate.Address())
		}
		if key == "" {
			key = strings.TrimSpace(candidate.DeviceID())
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}

		resp, err := candidate.RefreshMembership(ctx, MembershipRefreshRequest{
			LibraryID:     local.LibraryID,
			DeviceID:      local.DeviceID,
			PeerID:        peerID,
			RecoveryToken: recoveryToken,
			RefreshPubKey: refreshPubKey[:],
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if msg := strings.TrimSpace(resp.Error); msg != "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s", msg)
			}
			continue
		}
		if resp.MembershipCert == nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("membership refresh response missing certificate")
			}
			continue
		}
		if err := verifyMembershipCert(*resp.MembershipCert, resp.AuthorityChain, rootPublicKey, time.Now().UTC(), local.LibraryID, local.DeviceID, peerID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("verify refreshed membership certificate: %w", err)
			}
			continue
		}

		if err := a.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if len(resp.AuthorityChain) > 0 {
				if err := saveAdmissionAuthorityChainTx(tx, local.LibraryID, resp.AuthorityChain); err != nil {
					return fmt.Errorf("store refreshed admission authority chain: %w", err)
				}
			}
			if len(resp.EncryptedAdminMaterial) > 0 {
				if len(resp.AuthorityChain) == 0 {
					return fmt.Errorf("membership refresh admin material missing authority chain")
				}
				privateKey, err := decryptAdmissionMaterial(resp.EncryptedAdminMaterial, refreshPubKey, refreshPrivKey)
				if err != nil {
					return fmt.Errorf("decrypt refreshed admission authority material: %w", err)
				}
				head := resp.AuthorityChain[len(resp.AuthorityChain)-1]
				if err := saveLocalAdmissionAuthorityPrivateKeyTx(tx, local.LibraryID, head.Version, privateKey, time.Now().UTC()); err != nil {
					return fmt.Errorf("store refreshed admission authority private key: %w", err)
				}
			}
			if err := saveMembershipCertTx(tx, *resp.MembershipCert); err != nil {
				return fmt.Errorf("store refreshed membership certificate: %w", err)
			}
			return nil
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		return transportPeerAuth{
			Cert:           *resp.MembershipCert,
			AuthorityChain: append([]admissionAuthorityEnvelope(nil), resp.AuthorityChain...),
		}, nil
	}

	if firstErr == nil {
		firstErr = fmt.Errorf("membership refresh peer unavailable")
	}
	return transportPeerAuth{}, firstErr
}

func (a *App) libraryRootPublicKey(ctx context.Context, libraryID string) (string, error) {
	var library Library
	if err := a.storage.WithContext(ctx).Select("root_public_key").Where("library_id = ?", strings.TrimSpace(libraryID)).Take(&library).Error; err != nil {
		return "", fmt.Errorf("load library root public key: %w", err)
	}
	if strings.TrimSpace(library.RootPublicKey) == "" {
		return "", fmt.Errorf("library root public key missing")
	}
	return strings.TrimSpace(library.RootPublicKey), nil
}

func (a *App) buildMembershipRefreshResponse(ctx context.Context, req MembershipRefreshRequest) (MembershipRefreshResponse, error) {
	req.LibraryID = strings.TrimSpace(req.LibraryID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.PeerID = strings.TrimSpace(req.PeerID)
	req.RecoveryToken = strings.TrimSpace(req.RecoveryToken)
	if req.LibraryID == "" || req.DeviceID == "" || req.PeerID == "" || req.RecoveryToken == "" {
		return MembershipRefreshResponse{}, fmt.Errorf("device id, peer id, and recovery token are required")
	}

	var response MembershipRefreshResponse
	err := a.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cert, err := refreshMembershipCertWithRecoveryTx(tx, req.LibraryID, req.DeviceID, req.PeerID, req.RecoveryToken, defaultMembershipCertTTL)
		if err != nil {
			return err
		}
		authorityRows, err := loadAdmissionAuthorityChainTx(tx, req.LibraryID)
		if err != nil {
			return fmt.Errorf("load admission authority chain: %w", err)
		}

		response = MembershipRefreshResponse{
			LibraryID:      req.LibraryID,
			DeviceID:       cert.DeviceID,
			PeerID:         cert.PeerID,
			AuthorityChain: admissionAuthorityChainFromRows(authorityRows),
			MembershipCert: &cert,
		}
		if canManageLibrary(cert.Role) && len(req.RefreshPubKey) == 32 {
			_, _, privateKey, err := currentAdmissionAuthoritySigningMaterialTx(tx, req.LibraryID)
			if err != nil {
				return fmt.Errorf("load admission authority private key: %w", err)
			}
			encryptedMaterial, err := encryptAdmissionMaterial(req.RefreshPubKey, privateKey)
			if err != nil {
				return err
			}
			response.EncryptedAdminMaterial = encryptedMaterial
		}
		return nil
	})
	if err != nil {
		return MembershipRefreshResponse{}, err
	}
	return response, nil
}

func loadAdmissionAuthorityChainTx(tx *gorm.DB, libraryID string) ([]AdmissionAuthority, error) {
	var rows []AdmissionAuthority
	if err := tx.Where("library_id = ?", strings.TrimSpace(libraryID)).Order("version ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func encryptAdmissionMaterial(refreshPubKey []byte, privateKey string) ([]byte, error) {
	if len(refreshPubKey) != 32 {
		return nil, fmt.Errorf("refresh public key must be 32 bytes")
	}
	if _, err := decodeEd25519PrivateKey(privateKey); err != nil {
		return nil, fmt.Errorf("decode admission authority private key: %w", err)
	}
	raw, err := json.Marshal(encryptedAdminMaterial{PrivateKey: strings.TrimSpace(privateKey)})
	if err != nil {
		return nil, fmt.Errorf("marshal admission material: %w", err)
	}
	var recipient [32]byte
	copy(recipient[:], refreshPubKey)
	sealed, err := box.SealAnonymous(nil, raw, &recipient, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("encrypt admission material: %w", err)
	}
	return sealed, nil
}

func decryptAdmissionMaterial(ciphertext []byte, refreshPubKey, refreshPrivKey *[32]byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", fmt.Errorf("missing encrypted admission material")
	}
	if refreshPubKey == nil || refreshPrivKey == nil {
		return "", fmt.Errorf("missing refresh keypair")
	}
	opened, ok := box.OpenAnonymous(nil, ciphertext, refreshPubKey, refreshPrivKey)
	if !ok {
		return "", fmt.Errorf("decrypt admission material")
	}
	var material encryptedAdminMaterial
	if err := json.Unmarshal(opened, &material); err != nil {
		return "", fmt.Errorf("decode encrypted admission material: %w", err)
	}
	return strings.TrimSpace(material.PrivateKey), nil
}

