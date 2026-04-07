package desktopcore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	admissionAuthoritySignedByAuthority = "authority"
	defaultMembershipCertTTL            = 30 * 24 * time.Hour
)

type admissionAuthorityEnvelope struct {
	Version      int64  `json:"version"`
	PublicKey    string `json:"publicKey"`
	PrevVersion  int64  `json:"prevVersion"`
	SignedByKind string `json:"signedByKind"`
	Sig          []byte `json:"sig,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

type transportPeerAuth struct {
	Cert           membershipCertEnvelope       `json:"cert"`
	AuthorityChain []admissionAuthorityEnvelope `json:"authorityChain,omitempty"`
}

func generateSigningKeyPair() (string, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate signing keypair: %w", err)
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

func decodeEd25519PublicKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size")
	}
	return key, nil
}

func decodeEd25519PrivateKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size")
	}
	return key, nil
}

func admissionAuthoritySigningPayload(libraryID string, authority admissionAuthorityEnvelope) ([]byte, error) {
	body := struct {
		LibraryID    string `json:"library_id"`
		Version      int64  `json:"version"`
		PublicKey    string `json:"public_key"`
		PrevVersion  int64  `json:"prev_version"`
		SignedByKind string `json:"signed_by_kind"`
		CreatedAtNS  int64  `json:"created_at_ns"`
	}{
		LibraryID:    strings.TrimSpace(libraryID),
		Version:      authority.Version,
		PublicKey:    strings.TrimSpace(authority.PublicKey),
		PrevVersion:  authority.PrevVersion,
		SignedByKind: strings.TrimSpace(authority.SignedByKind),
		CreatedAtNS:  authority.CreatedAt,
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal admission authority payload: %w", err)
	}
	return out, nil
}

func membershipCertSigningPayload(cert membershipCertEnvelope) ([]byte, error) {
	body := struct {
		LibraryID        string `json:"library_id"`
		DeviceID         string `json:"device_id"`
		PeerID           string `json:"peer_id"`
		Role             string `json:"role"`
		AuthorityVersion int64  `json:"authority_version"`
		Serial           int64  `json:"serial"`
		IssuedAt         int64  `json:"issued_at"`
		ExpiresAt        int64  `json:"expires_at"`
	}{
		LibraryID:        strings.TrimSpace(cert.LibraryID),
		DeviceID:         strings.TrimSpace(cert.DeviceID),
		PeerID:           strings.TrimSpace(cert.PeerID),
		Role:             normalizeRole(cert.Role),
		AuthorityVersion: cert.AuthorityVersion,
		Serial:           cert.Serial,
		IssuedAt:         cert.IssuedAt,
		ExpiresAt:        cert.ExpiresAt,
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal membership certificate payload: %w", err)
	}
	return out, nil
}

func verifyAdmissionAuthorityChain(libraryID string, chain []admissionAuthorityEnvelope, rootPublicKey string) (admissionAuthorityEnvelope, error) {
	if len(chain) == 0 {
		return admissionAuthorityEnvelope{}, fmt.Errorf("admission authority chain is required")
	}
	rootPub, err := decodeEd25519PublicKey(rootPublicKey)
	if err != nil {
		return admissionAuthorityEnvelope{}, fmt.Errorf("decode root public key: %w", err)
	}
	sorted := append([]admissionAuthorityEnvelope(nil), chain...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version < sorted[j].Version
	})

	var previous admissionAuthorityEnvelope
	for idx, desc := range sorted {
		if desc.Version != int64(idx+1) {
			return admissionAuthorityEnvelope{}, fmt.Errorf("admission authority chain is not contiguous")
		}
		pub, err := decodeEd25519PublicKey(desc.PublicKey)
		if err != nil {
			return admissionAuthorityEnvelope{}, fmt.Errorf("decode admission authority key: %w", err)
		}
		payload, err := admissionAuthoritySigningPayload(libraryID, desc)
		if err != nil {
			return admissionAuthorityEnvelope{}, err
		}
		switch desc.Version {
		case 1:
			if desc.PrevVersion != 0 {
				return admissionAuthorityEnvelope{}, fmt.Errorf("admission authority v1 prev version mismatch")
			}
			if strings.TrimSpace(desc.SignedByKind) != admissionAuthoritySignedByRoot {
				return admissionAuthorityEnvelope{}, fmt.Errorf("admission authority v1 must be root-signed")
			}
			if !ed25519.Verify(ed25519.PublicKey(rootPub), payload, desc.Sig) {
				return admissionAuthorityEnvelope{}, fmt.Errorf("invalid admission authority root signature")
			}
		default:
			if desc.PrevVersion != previous.Version {
				return admissionAuthorityEnvelope{}, fmt.Errorf("admission authority prev version mismatch")
			}
			if strings.TrimSpace(desc.SignedByKind) != admissionAuthoritySignedByAuthority {
				return admissionAuthorityEnvelope{}, fmt.Errorf("admission authority rotation must be authority-signed")
			}
			prevPub, err := decodeEd25519PublicKey(previous.PublicKey)
			if err != nil {
				return admissionAuthorityEnvelope{}, fmt.Errorf("decode prior admission authority key: %w", err)
			}
			if !ed25519.Verify(ed25519.PublicKey(prevPub), payload, desc.Sig) {
				return admissionAuthorityEnvelope{}, fmt.Errorf("invalid admission authority rotation signature")
			}
		}
		_ = pub
		previous = desc
	}
	return previous, nil
}

func verifyMembershipCert(cert membershipCertEnvelope, chain []admissionAuthorityEnvelope, rootPublicKey string, now time.Time, libraryID, deviceID, actualPeerID string) error {
	if strings.TrimSpace(cert.LibraryID) != strings.TrimSpace(libraryID) {
		return fmt.Errorf("membership certificate library mismatch")
	}
	if strings.TrimSpace(cert.DeviceID) != strings.TrimSpace(deviceID) {
		return fmt.Errorf("membership certificate device mismatch")
	}
	if strings.TrimSpace(cert.PeerID) == "" || strings.TrimSpace(cert.PeerID) != strings.TrimSpace(actualPeerID) {
		return fmt.Errorf("membership certificate peer mismatch")
	}
	if cert.Serial <= 0 {
		return fmt.Errorf("membership certificate serial is invalid")
	}
	if cert.AuthorityVersion <= 0 {
		return fmt.Errorf("membership certificate authority version is invalid")
	}
	if cert.ExpiresAt > 0 && now.UTC().UnixNano() > cert.ExpiresAt {
		return fmt.Errorf("membership certificate expired")
	}
	head, err := verifyAdmissionAuthorityChain(libraryID, chain, rootPublicKey)
	if err != nil {
		return err
	}
	if head.Version != cert.AuthorityVersion {
		return fmt.Errorf("membership certificate authority version is stale")
	}
	var authority admissionAuthorityEnvelope
	found := false
	for _, candidate := range chain {
		if candidate.Version == cert.AuthorityVersion {
			authority = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("membership certificate authority not found")
	}
	pub, err := decodeEd25519PublicKey(authority.PublicKey)
	if err != nil {
		return fmt.Errorf("decode membership authority key: %w", err)
	}
	payload, err := membershipCertSigningPayload(cert)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, cert.Sig) {
		return fmt.Errorf("invalid membership certificate signature")
	}
	return nil
}

func verifyHistoricalMembershipCert(cert membershipCertEnvelope, chain []admissionAuthorityEnvelope, rootPublicKey string, _ int64, libraryID, deviceID, actualPeerID string) error {
	if strings.TrimSpace(cert.LibraryID) != strings.TrimSpace(libraryID) {
		return fmt.Errorf("membership certificate library mismatch")
	}
	if strings.TrimSpace(cert.DeviceID) != strings.TrimSpace(deviceID) {
		return fmt.Errorf("membership certificate device mismatch")
	}
	if strings.TrimSpace(cert.PeerID) == "" || strings.TrimSpace(cert.PeerID) != strings.TrimSpace(actualPeerID) {
		return fmt.Errorf("membership certificate peer mismatch")
	}
	if cert.Serial <= 0 || cert.AuthorityVersion <= 0 {
		return fmt.Errorf("membership certificate is invalid")
	}
	if _, err := verifyAdmissionAuthorityChain(libraryID, chain, rootPublicKey); err != nil {
		return err
	}
	var authority admissionAuthorityEnvelope
	found := false
	for _, candidate := range chain {
		if candidate.Version == cert.AuthorityVersion {
			authority = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("membership certificate authority not found")
	}
	pub, err := decodeEd25519PublicKey(authority.PublicKey)
	if err != nil {
		return fmt.Errorf("decode membership authority key: %w", err)
	}
	payload, err := membershipCertSigningPayload(cert)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, cert.Sig) {
		return fmt.Errorf("invalid membership certificate signature")
	}
	return nil
}

func admissionAuthorityEnvelopeFromRow(row AdmissionAuthority) admissionAuthorityEnvelope {
	return admissionAuthorityEnvelope{
		Version:      row.Version,
		PublicKey:    strings.TrimSpace(row.PublicKey),
		PrevVersion:  row.PrevVersion,
		SignedByKind: strings.TrimSpace(row.SignedByKind),
		Sig:          append([]byte(nil), row.Sig...),
		CreatedAt:    row.CreatedAt.UTC().UnixNano(),
	}
}

func admissionAuthorityChainFromRows(rows []AdmissionAuthority) []admissionAuthorityEnvelope {
	out := make([]admissionAuthorityEnvelope, 0, len(rows))
	for _, row := range rows {
		out = append(out, admissionAuthorityEnvelopeFromRow(row))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Version < out[j].Version
	})
	return out
}

func membershipCertEnvelopeFromRow(row MembershipCert) membershipCertEnvelope {
	return membershipCertEnvelope{
		LibraryID:        strings.TrimSpace(row.LibraryID),
		DeviceID:         strings.TrimSpace(row.DeviceID),
		PeerID:           strings.TrimSpace(row.PeerID),
		Role:             normalizeRole(row.Role),
		AuthorityVersion: row.AuthorityVersion,
		Serial:           row.Serial,
		IssuedAt:         row.IssuedAt,
		ExpiresAt:        row.ExpiresAt,
		Sig:              append([]byte(nil), row.Sig...),
	}
}

func saveMembershipCertTx(tx *gorm.DB, cert membershipCertEnvelope) error {
	if strings.TrimSpace(cert.LibraryID) == "" || strings.TrimSpace(cert.DeviceID) == "" || strings.TrimSpace(cert.PeerID) == "" {
		return fmt.Errorf("library id, device id, and peer id are required")
	}
	if len(cert.Sig) == 0 {
		return fmt.Errorf("membership certificate signature is required")
	}
	authorityVersion := cert.AuthorityVersion
	if authorityVersion <= 0 {
		authorityVersion = 1
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"peer_id", "role", "authority_version", "serial", "issued_at", "expires_at", "sig"}),
	}).Create(&MembershipCert{
		LibraryID:        strings.TrimSpace(cert.LibraryID),
		DeviceID:         strings.TrimSpace(cert.DeviceID),
		PeerID:           strings.TrimSpace(cert.PeerID),
		Role:             normalizeRole(cert.Role),
		AuthorityVersion: authorityVersion,
		Serial:           cert.Serial,
		IssuedAt:         cert.IssuedAt,
		ExpiresAt:        cert.ExpiresAt,
		Sig:              append([]byte(nil), cert.Sig...),
	}).Error
}

func saveAdmissionAuthorityChainTx(tx *gorm.DB, libraryID string, chain []admissionAuthorityEnvelope) error {
	libraryID = strings.TrimSpace(libraryID)
	for _, authority := range chain {
		createdAt := time.Now().UTC()
		if authority.CreatedAt > 0 {
			createdAt = time.Unix(0, authority.CreatedAt).UTC()
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "library_id"}, {Name: "version"}},
			DoUpdates: clause.AssignmentColumns([]string{"public_key", "prev_version", "signed_by_kind", "sig", "created_at"}),
		}).Create(&AdmissionAuthority{
			LibraryID:    libraryID,
			Version:      authority.Version,
			PublicKey:    strings.TrimSpace(authority.PublicKey),
			PrevVersion:  authority.PrevVersion,
			SignedByKind: strings.TrimSpace(authority.SignedByKind),
			Sig:          append([]byte(nil), authority.Sig...),
			CreatedAt:    createdAt,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func currentAdmissionAuthoritySigningMaterialTx(tx *gorm.DB, libraryID string) (AdmissionAuthority, []AdmissionAuthority, string, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return AdmissionAuthority{}, nil, "", fmt.Errorf("library id is required")
	}
	var rows []AdmissionAuthority
	if err := tx.Where("library_id = ?", libraryID).Order("version ASC").Find(&rows).Error; err != nil {
		return AdmissionAuthority{}, nil, "", err
	}
	if len(rows) == 0 {
		return AdmissionAuthority{}, nil, "", fmt.Errorf("admission authority chain is missing")
	}
	head := rows[len(rows)-1]
	privateKey, err := localSettingValueTx(tx, admissionAuthorityPrivateKeyLocalSettingKey(libraryID, head.Version))
	if err != nil {
		return AdmissionAuthority{}, nil, "", err
	}
	if strings.TrimSpace(privateKey) == "" {
		return AdmissionAuthority{}, nil, "", fmt.Errorf("current admission authority private key missing")
	}
	return head, rows, privateKey, nil
}

func issueMembershipCertTx(tx *gorm.DB, libraryID, deviceID, peerID, role string, ttl time.Duration) (membershipCertEnvelope, error) {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	role = normalizeRole(role)
	if libraryID == "" || deviceID == "" || peerID == "" {
		return membershipCertEnvelope{}, fmt.Errorf("library id, device id, and peer id are required")
	}
	if ttl <= 0 {
		ttl = defaultMembershipCertTTL
	}

	head, _, privateKey, err := currentAdmissionAuthoritySigningMaterialTx(tx, libraryID)
	if err != nil {
		return membershipCertEnvelope{}, err
	}
	priv, err := decodeEd25519PrivateKey(privateKey)
	if err != nil {
		return membershipCertEnvelope{}, fmt.Errorf("decode admission authority private key: %w", err)
	}

	now := time.Now().UTC()
	serial := int64(1)
	var existing MembershipCert
	err = tx.Where("library_id = ? AND device_id = ?", libraryID, deviceID).Take(&existing).Error
	switch {
	case err == nil:
		if existing.Serial >= serial {
			serial = existing.Serial + 1
		}
		if existing.Serial > 0 && strings.TrimSpace(existing.PeerID) != "" {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&MembershipCertRevocation{
				LibraryID: libraryID,
				DeviceID:  deviceID,
				Serial:    existing.Serial,
				PeerID:    strings.TrimSpace(existing.PeerID),
				Reason:    "superseded",
				RevokedAt: now.UTC(),
			}).Error; err != nil {
				return membershipCertEnvelope{}, err
			}
		}
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return membershipCertEnvelope{}, err
	}

	cert := membershipCertEnvelope{
		LibraryID:        libraryID,
		DeviceID:         deviceID,
		PeerID:           peerID,
		Role:             role,
		AuthorityVersion: head.Version,
		Serial:           serial,
		IssuedAt:         now.UnixNano(),
		ExpiresAt:        now.Add(ttl).UnixNano(),
	}
	payload, err := membershipCertSigningPayload(cert)
	if err != nil {
		return membershipCertEnvelope{}, err
	}
	cert.Sig = ed25519.Sign(ed25519.PrivateKey(priv), payload)
	if err := saveMembershipCertTx(tx, cert); err != nil {
		return membershipCertEnvelope{}, err
	}
	return cert, nil
}

func (a *IdentityMembershipService) loadMembershipCert(ctx context.Context, libraryID, deviceID string) (MembershipCert, bool, error) {
	var row MembershipCert
	err := a.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return MembershipCert{}, false, nil
		}
		return MembershipCert{}, false, err
	}
	return row, true, nil
}

func (a *IdentityMembershipService) loadAdmissionAuthorityChain(ctx context.Context, libraryID string) ([]AdmissionAuthority, error) {
	var rows []AdmissionAuthority
	if err := a.storage.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("version ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (a *IdentityMembershipService) membershipCertRevoked(ctx context.Context, libraryID, deviceID string, serial int64) (bool, error) {
	if serial <= 0 {
		return false, nil
	}
	var count int64
	if err := a.storage.WithContext(ctx).Model(&MembershipCertRevocation{}).
		Where("library_id = ? AND device_id = ? AND serial = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), serial).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *IdentityMembershipService) transportIdentityPeerID() (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is nil")
	}
	priv, err := loadOrCreateTransportIdentityKey(a.cfg.IdentityKeyPath)
	if err != nil {
		return "", err
	}
	peerID, err := peer.IDFromPublicKey(priv.GetPublic())
	if err != nil {
		return "", fmt.Errorf("derive transport peer id: %w", err)
	}
	return peerID.String(), nil
}

func (a *IdentityMembershipService) ensureLocalTransportMembershipAuth(ctx context.Context, local apitypes.LocalContext, transportPeerID string) (transportPeerAuth, error) {
	if a == nil {
		return transportPeerAuth{}, fmt.Errorf("app is nil")
	}
	local.LibraryID = strings.TrimSpace(local.LibraryID)
	local.DeviceID = strings.TrimSpace(local.DeviceID)
	transportPeerID = strings.TrimSpace(transportPeerID)
	if local.LibraryID == "" || local.DeviceID == "" || transportPeerID == "" {
		return transportPeerAuth{}, fmt.Errorf("library id, device id, and peer id are required")
	}

	var cert membershipCertEnvelope
	rows, err := a.loadAdmissionAuthorityChain(ctx, local.LibraryID)
	if err != nil {
		return transportPeerAuth{}, fmt.Errorf("load admission authority chain: %w", err)
	}
	chain := admissionAuthorityChainFromRows(rows)
	var headVersion int64
	if len(chain) > 0 {
		headVersion = chain[len(chain)-1].Version
	}
	row, ok, err := a.loadMembershipCert(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return transportPeerAuth{}, fmt.Errorf("load membership certificate: %w", err)
	}
	nowNS := time.Now().UTC().UnixNano()
	switch {
	case ok:
		cert = membershipCertEnvelopeFromRow(row)
	case !ok:
		refreshed, refreshErr := transportPeerAuth{}, fmt.Errorf("membership certificate missing")
		if canManageLibrary(local.Role) {
			refreshErr = a.storage.Transaction(ctx, func(tx *gorm.DB) error {
				if _, _, _, err := ensureLibraryJoinMaterialTx(tx, local.LibraryID, time.Now().UTC()); err != nil {
					return err
				}
				issued, err := issueMembershipCertTx(tx, local.LibraryID, local.DeviceID, transportPeerID, local.Role, defaultMembershipCertTTL)
				if err != nil {
					return err
				}
				cert = issued
				return nil
			})
		}
		if refreshErr != nil {
			refreshed, refreshErr = a.requestMembershipRefresh(ctx, local, transportPeerID)
			if refreshErr != nil {
				return transportPeerAuth{}, fmt.Errorf("refresh local membership certificate: %w", refreshErr)
			}
			cert = refreshed.Cert
		}
		rows, err = a.loadAdmissionAuthorityChain(ctx, local.LibraryID)
		if err != nil {
			return transportPeerAuth{}, fmt.Errorf("reload admission authority chain: %w", err)
		}
		chain = admissionAuthorityChainFromRows(rows)
		if len(chain) > 0 {
			headVersion = chain[len(chain)-1].Version
		}
	}

	needsReissue := cert.Serial <= 0 || cert.ExpiresAt <= nowNS || cert.PeerID != transportPeerID || cert.AuthorityVersion < headVersion || normalizeRole(cert.Role) != normalizeRole(local.Role)
	if needsReissue {
		refreshErr := fmt.Errorf("membership certificate requires refresh")
		if canManageLibrary(local.Role) {
			refreshErr = a.storage.Transaction(ctx, func(tx *gorm.DB) error {
				if _, _, _, err := ensureLibraryJoinMaterialTx(tx, local.LibraryID, time.Now().UTC()); err != nil {
					return err
				}
				issued, err := issueMembershipCertTx(tx, local.LibraryID, local.DeviceID, transportPeerID, local.Role, defaultMembershipCertTTL)
				if err != nil {
					return err
				}
				cert = issued
				return nil
			})
		}
		if refreshErr != nil {
			refreshed, err := a.requestMembershipRefresh(ctx, local, transportPeerID)
			if err != nil {
				return transportPeerAuth{}, fmt.Errorf("refresh local membership certificate: %w", err)
			}
			cert = refreshed.Cert
		}
		rows, err = a.loadAdmissionAuthorityChain(ctx, local.LibraryID)
		if err != nil {
			return transportPeerAuth{}, fmt.Errorf("reload admission authority chain: %w", err)
		}
		chain = admissionAuthorityChainFromRows(rows)
	}

	return transportPeerAuth{
		Cert:           cert,
		AuthorityChain: append([]admissionAuthorityEnvelope(nil), chain...),
	}, nil
}

func (a *IdentityMembershipService) verifyTransportPeerAuth(ctx context.Context, libraryID, claimedDeviceID, claimedPeerID, actualPeerID string, auth transportPeerAuth) (membershipCertEnvelope, error) {
	if a == nil {
		return membershipCertEnvelope{}, fmt.Errorf("app is nil")
	}
	libraryID = strings.TrimSpace(libraryID)
	claimedDeviceID = strings.TrimSpace(claimedDeviceID)
	claimedPeerID = strings.TrimSpace(claimedPeerID)
	actualPeerID = strings.TrimSpace(actualPeerID)
	if libraryID == "" || claimedDeviceID == "" || actualPeerID == "" {
		return membershipCertEnvelope{}, fmt.Errorf("library id, device id, and actual peer id are required")
	}
	if claimedPeerID != "" && claimedPeerID != actualPeerID {
		return membershipCertEnvelope{}, fmt.Errorf("peer id claim mismatch")
	}
	if !a.isLibraryMember(ctx, libraryID, claimedDeviceID) {
		return membershipCertEnvelope{}, fmt.Errorf("device not allowed")
	}

	rootPublicKey, err := a.libraryRootPublicKey(ctx, libraryID)
	if err != nil {
		return membershipCertEnvelope{}, err
	}
	if err := verifyMembershipCert(auth.Cert, auth.AuthorityChain, rootPublicKey, time.Now().UTC(), libraryID, claimedDeviceID, actualPeerID); err != nil {
		return membershipCertEnvelope{}, err
	}

	localRows, err := a.loadAdmissionAuthorityChain(ctx, libraryID)
	if err != nil {
		return membershipCertEnvelope{}, fmt.Errorf("load admission authority head: %w", err)
	}
	if len(localRows) > 0 {
		head := localRows[len(localRows)-1]
		if auth.Cert.AuthorityVersion < head.Version {
			return membershipCertEnvelope{}, fmt.Errorf("membership certificate authority version is stale")
		}
	}
	if current, ok, err := a.loadMembershipCert(ctx, libraryID, claimedDeviceID); err != nil {
		return membershipCertEnvelope{}, fmt.Errorf("load membership certificate state: %w", err)
	} else if ok && auth.Cert.Serial < current.Serial {
		return membershipCertEnvelope{}, fmt.Errorf("membership certificate serial is stale")
	}
	revoked, err := a.membershipCertRevoked(ctx, libraryID, claimedDeviceID, auth.Cert.Serial)
	if err != nil {
		return membershipCertEnvelope{}, fmt.Errorf("check membership certificate revocation: %w", err)
	}
	if revoked {
		return membershipCertEnvelope{}, fmt.Errorf("membership certificate is revoked")
	}

	if err := a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if len(auth.AuthorityChain) > 0 {
			if err := saveAdmissionAuthorityChainTx(tx, libraryID, auth.AuthorityChain); err != nil {
				return err
			}
		}
		if err := saveMembershipCertTx(tx, auth.Cert); err != nil {
			return err
		}
		now := time.Now().UTC()
		deviceName := chooseDeviceName("", claimedDeviceID, claimedDeviceID)
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":         deviceName,
				"peer_id":      actualPeerID,
				"last_seen_at": &now,
			}),
		}).Create(&Device{
			DeviceID:   claimedDeviceID,
			Name:       deviceName,
			PeerID:     actualPeerID,
			JoinedAt:   now,
			LastSeenAt: &now,
		}).Error
	}); err != nil {
		return membershipCertEnvelope{}, fmt.Errorf("persist authenticated peer state: %w", err)
	}
	return auth.Cert, nil
}
