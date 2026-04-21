package registryauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	AdmissionAuthoritySignedByRoot      = "root"
	AdmissionAuthoritySignedByAuthority = "authority"
)

type PresenceRecord struct {
	LibraryID string    `json:"libraryId"`
	DeviceID  string    `json:"deviceId"`
	PeerID    string    `json:"peerId"`
	Addrs     []string  `json:"addrs"`
	ExpiresAt time.Time `json:"expiresAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AdmissionAuthorityEnvelope struct {
	Version      int64  `json:"version"`
	PublicKey    string `json:"publicKey"`
	PrevVersion  int64  `json:"prevVersion"`
	SignedByKind string `json:"signedByKind"`
	Sig          []byte `json:"sig,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

type MembershipCertEnvelope struct {
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

type TransportPeerAuth struct {
	Cert           MembershipCertEnvelope       `json:"cert"`
	AuthorityChain []AdmissionAuthorityEnvelope `json:"authorityChain,omitempty"`
}

type InviteAttestation struct {
	LibraryID     string `json:"libraryId"`
	TokenID       string `json:"tokenId"`
	OwnerPeerID   string `json:"ownerPeerId"`
	RootPublicKey string `json:"rootPublicKey"`
	ExpiresAt     int64  `json:"expiresAt"`
	Sig           []byte `json:"sig,omitempty"`
}

type PresenceAnnounceRequest struct {
	Record        PresenceRecord    `json:"record"`
	RootPublicKey string            `json:"rootPublicKey"`
	Auth          TransportPeerAuth `json:"auth"`
}

type MemberLookupRequest struct {
	LibraryID     string            `json:"libraryId"`
	PeerID        string            `json:"peerId"`
	RootPublicKey string            `json:"rootPublicKey"`
	Auth          TransportPeerAuth `json:"auth"`
}

type InviteOwnerLookupRequest struct {
	Invite InviteAttestation `json:"invite"`
}

func CompactNonEmptyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func DecodeEd25519PublicKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size")
	}
	return key, nil
}

func DecodeEd25519PrivateKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size")
	}
	return key, nil
}

func AdmissionAuthoritySigningPayload(libraryID string, authority AdmissionAuthorityEnvelope) ([]byte, error) {
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

func MembershipCertSigningPayload(cert MembershipCertEnvelope) ([]byte, error) {
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
		Role:             NormalizeRole(cert.Role),
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

func InviteAttestationSigningPayload(attestation InviteAttestation) ([]byte, error) {
	body := struct {
		LibraryID     string `json:"library_id"`
		TokenID       string `json:"token_id"`
		OwnerPeerID   string `json:"owner_peer_id"`
		RootPublicKey string `json:"root_public_key"`
		ExpiresAt     int64  `json:"expires_at"`
	}{
		LibraryID:     strings.TrimSpace(attestation.LibraryID),
		TokenID:       strings.TrimSpace(attestation.TokenID),
		OwnerPeerID:   strings.TrimSpace(attestation.OwnerPeerID),
		RootPublicKey: strings.TrimSpace(attestation.RootPublicKey),
		ExpiresAt:     attestation.ExpiresAt,
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal invite attestation payload: %w", err)
	}
	return out, nil
}

func SignInviteAttestation(attestation InviteAttestation, encodedPrivateKey string) (InviteAttestation, error) {
	privateKey, err := DecodeEd25519PrivateKey(encodedPrivateKey)
	if err != nil {
		return InviteAttestation{}, err
	}
	derivedPublicKey := base64.StdEncoding.EncodeToString(ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey))
	if strings.TrimSpace(attestation.RootPublicKey) == "" {
		attestation.RootPublicKey = derivedPublicKey
	} else if strings.TrimSpace(attestation.RootPublicKey) != derivedPublicKey {
		return InviteAttestation{}, fmt.Errorf("invite attestation root public key mismatch")
	}
	payload, err := InviteAttestationSigningPayload(attestation)
	if err != nil {
		return InviteAttestation{}, err
	}
	attestation.Sig = ed25519.Sign(ed25519.PrivateKey(privateKey), payload)
	return attestation, nil
}

func VerifyInviteAttestation(attestation InviteAttestation, now time.Time) error {
	attestation.LibraryID = strings.TrimSpace(attestation.LibraryID)
	attestation.TokenID = strings.TrimSpace(attestation.TokenID)
	attestation.OwnerPeerID = strings.TrimSpace(attestation.OwnerPeerID)
	attestation.RootPublicKey = strings.TrimSpace(attestation.RootPublicKey)
	if attestation.LibraryID == "" || attestation.TokenID == "" || attestation.OwnerPeerID == "" || attestation.RootPublicKey == "" {
		return fmt.Errorf("invite attestation is incomplete")
	}
	if attestation.ExpiresAt > 0 && now.UTC().Unix() > attestation.ExpiresAt {
		return fmt.Errorf("invite attestation expired")
	}
	publicKey, err := DecodeEd25519PublicKey(attestation.RootPublicKey)
	if err != nil {
		return fmt.Errorf("decode invite root public key: %w", err)
	}
	payload, err := InviteAttestationSigningPayload(attestation)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, attestation.Sig) {
		return fmt.Errorf("invalid invite attestation signature")
	}
	return nil
}

func VerifyAdmissionAuthorityChain(libraryID string, chain []AdmissionAuthorityEnvelope, rootPublicKey string) (AdmissionAuthorityEnvelope, error) {
	if len(chain) == 0 {
		return AdmissionAuthorityEnvelope{}, fmt.Errorf("admission authority chain is required")
	}
	rootPub, err := DecodeEd25519PublicKey(rootPublicKey)
	if err != nil {
		return AdmissionAuthorityEnvelope{}, fmt.Errorf("decode root public key: %w", err)
	}
	sorted := append([]AdmissionAuthorityEnvelope(nil), chain...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version < sorted[j].Version
	})

	var previous AdmissionAuthorityEnvelope
	for idx, desc := range sorted {
		if desc.Version != int64(idx+1) {
			return AdmissionAuthorityEnvelope{}, fmt.Errorf("admission authority chain is not contiguous")
		}
		payload, err := AdmissionAuthoritySigningPayload(libraryID, desc)
		if err != nil {
			return AdmissionAuthorityEnvelope{}, err
		}
		switch desc.Version {
		case 1:
			if desc.PrevVersion != 0 {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("admission authority v1 prev version mismatch")
			}
			if strings.TrimSpace(desc.SignedByKind) != AdmissionAuthoritySignedByRoot {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("admission authority v1 must be root-signed")
			}
			if !ed25519.Verify(ed25519.PublicKey(rootPub), payload, desc.Sig) {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("invalid admission authority root signature")
			}
		default:
			if desc.PrevVersion != previous.Version {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("admission authority prev version mismatch")
			}
			if strings.TrimSpace(desc.SignedByKind) != AdmissionAuthoritySignedByAuthority {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("admission authority rotation must be authority-signed")
			}
			prevPub, err := DecodeEd25519PublicKey(previous.PublicKey)
			if err != nil {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("decode prior admission authority key: %w", err)
			}
			if !ed25519.Verify(ed25519.PublicKey(prevPub), payload, desc.Sig) {
				return AdmissionAuthorityEnvelope{}, fmt.Errorf("invalid admission authority rotation signature")
			}
		}
		previous = desc
	}
	return previous, nil
}

func VerifyMembershipCert(cert MembershipCertEnvelope, chain []AdmissionAuthorityEnvelope, rootPublicKey string, now time.Time, libraryID, deviceID, actualPeerID string) error {
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
	head, err := VerifyAdmissionAuthorityChain(libraryID, chain, rootPublicKey)
	if err != nil {
		return err
	}
	if head.Version != cert.AuthorityVersion {
		return fmt.Errorf("membership certificate authority version is stale")
	}
	var authority AdmissionAuthorityEnvelope
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
	pub, err := DecodeEd25519PublicKey(authority.PublicKey)
	if err != nil {
		return fmt.Errorf("decode membership authority key: %w", err)
	}
	payload, err := MembershipCertSigningPayload(cert)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, cert.Sig) {
		return fmt.Errorf("invalid membership certificate signature")
	}
	return nil
}

func VerifyHistoricalMembershipCert(cert MembershipCertEnvelope, chain []AdmissionAuthorityEnvelope, rootPublicKey string, _ int64, libraryID, deviceID, actualPeerID string) error {
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
	if _, err := VerifyAdmissionAuthorityChain(libraryID, chain, rootPublicKey); err != nil {
		return err
	}
	var authority AdmissionAuthorityEnvelope
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
	pub, err := DecodeEd25519PublicKey(authority.PublicKey)
	if err != nil {
		return fmt.Errorf("decode membership authority key: %w", err)
	}
	payload, err := MembershipCertSigningPayload(cert)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, cert.Sig) {
		return fmt.Errorf("invalid membership certificate signature")
	}
	return nil
}
