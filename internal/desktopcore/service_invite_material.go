package desktopcore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	admissionAuthoritySignedByRoot = "root"
)

type joinSessionAuthorityMaterial struct {
	Version      int64  `json:"version"`
	PublicKey    string `json:"publicKey"`
	PrivateKey   string `json:"privateKey,omitempty"`
	PrevVersion  int64  `json:"prevVersion"`
	SignedByKind string `json:"signedByKind"`
	Sig          []byte `json:"sig,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

func (s *InviteService) buildJoinSessionMaterialTx(tx *gorm.DB, libraryID, deviceID, peerID, role string, now time.Time) (joinSessionMaterial, error) {
	library, authority, privateKey, err := ensureLibraryJoinMaterialTx(tx, libraryID, now)
	if err != nil {
		return joinSessionMaterial{}, err
	}

	recoveryToken, err := randomToken()
	if err != nil {
		return joinSessionMaterial{}, err
	}

	cert, err := buildMembershipCert(libraryID, deviceID, peerID, role, authority.Version)
	if err != nil {
		return joinSessionMaterial{}, err
	}

	material := joinSessionMaterial{
		LibraryName:    strings.TrimSpace(library.Name),
		RootPublicKey:  strings.TrimSpace(library.RootPublicKey),
		LibraryKey:     strings.TrimSpace(library.LibraryKey),
		RecoveryToken:  recoveryToken,
		MembershipCert: cert,
		AdmissionAuthority: &joinSessionAuthorityMaterial{
			Version:      authority.Version,
			PublicKey:    strings.TrimSpace(authority.PublicKey),
			PrevVersion:  authority.PrevVersion,
			SignedByKind: strings.TrimSpace(authority.SignedByKind),
			Sig:          append([]byte(nil), authority.Sig...),
			CreatedAt:    authority.CreatedAt.UTC().UnixNano(),
		},
	}
	if canManageLibrary(role) {
		material.AdmissionAuthority.PrivateKey = strings.TrimSpace(privateKey)
	}
	return material, nil
}

func ensureLibraryJoinMaterialTx(tx *gorm.DB, libraryID string, now time.Time) (Library, AdmissionAuthority, string, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return Library{}, AdmissionAuthority{}, "", fmt.Errorf("library id is required")
	}

	var library Library
	if err := tx.Where("library_id = ?", libraryID).Take(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return Library{}, AdmissionAuthority{}, "", fmt.Errorf("library %s not found", libraryID)
		}
		return Library{}, AdmissionAuthority{}, "", err
	}

	updates := make(map[string]any)
	if strings.TrimSpace(library.Name) == "" {
		library.Name = defaultLibraryName
		updates["name"] = library.Name
	}
	if strings.TrimSpace(library.RootPublicKey) == "" {
		token, err := randomToken()
		if err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		library.RootPublicKey = token
		updates["root_public_key"] = library.RootPublicKey
	}
	if strings.TrimSpace(library.RootPrivateKey) == "" {
		token, err := randomToken()
		if err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		library.RootPrivateKey = token
		updates["root_private_key"] = library.RootPrivateKey
	}
	if strings.TrimSpace(library.LibraryKey) == "" {
		token, err := randomToken()
		if err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		library.LibraryKey = token
		updates["library_key"] = library.LibraryKey
	}
	if len(updates) > 0 {
		if err := tx.Model(&Library{}).Where("library_id = ?", libraryID).Updates(updates).Error; err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
	}

	var authority AdmissionAuthority
	err := tx.Where("library_id = ?", libraryID).Order("version DESC").Limit(1).Take(&authority).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return Library{}, AdmissionAuthority{}, "", err
		}
		publicKey, err := randomToken()
		if err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		privateKey, err := randomToken()
		if err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		authority = AdmissionAuthority{
			LibraryID:    libraryID,
			Version:      1,
			PublicKey:    publicKey,
			PrevVersion:  0,
			SignedByKind: admissionAuthoritySignedByRoot,
			Sig:          pseudoAuthoritySignature(libraryID, publicKey, 1, 0, admissionAuthoritySignedByRoot, now),
			CreatedAt:    now.UTC(),
		}
		if err := tx.Create(&authority).Error; err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		if err := upsertLocalSettingTx(tx, admissionAuthorityPrivateKeyLocalSettingKey(libraryID, authority.Version), privateKey, now); err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		return library, authority, privateKey, nil
	}

	privateKey, err := localSettingValueTx(tx, admissionAuthorityPrivateKeyLocalSettingKey(libraryID, authority.Version))
	if err != nil {
		return Library{}, AdmissionAuthority{}, "", err
	}
	if strings.TrimSpace(privateKey) == "" {
		privateKey, err = randomToken()
		if err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
		if err := upsertLocalSettingTx(tx, admissionAuthorityPrivateKeyLocalSettingKey(libraryID, authority.Version), privateKey, now); err != nil {
			return Library{}, AdmissionAuthority{}, "", err
		}
	}
	return library, authority, privateKey, nil
}

func restoreJoinSessionMaterialTx(tx *gorm.DB, session JoinSession, material joinSessionMaterial, now time.Time) error {
	libraryID := strings.TrimSpace(session.LibraryID)
	if libraryID == "" {
		return fmt.Errorf("join session library id is required")
	}

	libraryName := strings.TrimSpace(material.LibraryName)
	if libraryName == "" {
		libraryName = defaultLibraryName
	}

	var existing Library
	err := tx.Where("library_id = ?", libraryID).Take(&existing).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := tx.Create(&Library{
			LibraryID:     libraryID,
			Name:          libraryName,
			RootPublicKey: strings.TrimSpace(material.RootPublicKey),
			LibraryKey:    strings.TrimSpace(material.LibraryKey),
			CreatedAt:     now,
		}).Error; err != nil {
			return err
		}
	} else {
		updates := make(map[string]any)
		if strings.TrimSpace(existing.Name) == "" && libraryName != "" {
			updates["name"] = libraryName
		}
		if strings.TrimSpace(existing.RootPublicKey) == "" && strings.TrimSpace(material.RootPublicKey) != "" {
			updates["root_public_key"] = strings.TrimSpace(material.RootPublicKey)
		}
		if strings.TrimSpace(existing.LibraryKey) == "" && strings.TrimSpace(material.LibraryKey) != "" {
			updates["library_key"] = strings.TrimSpace(material.LibraryKey)
		}
		if len(updates) > 0 {
			if err := tx.Model(&Library{}).Where("library_id = ?", libraryID).Updates(updates).Error; err != nil {
				return err
			}
		}
	}

	if material.AdmissionAuthority != nil && material.AdmissionAuthority.Version > 0 {
		authority := material.AdmissionAuthority
		createdAt := now
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
		if strings.TrimSpace(authority.PrivateKey) != "" {
			if err := upsertLocalSettingTx(tx, admissionAuthorityPrivateKeyLocalSettingKey(libraryID, authority.Version), authority.PrivateKey, now); err != nil {
				return err
			}
		}
	}

	return nil
}

func upsertDeviceMembershipTx(tx *gorm.DB, libraryID, deviceID, deviceName, peerID, role string, now time.Time) error {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil
	}
	deviceName = chooseDeviceName("", deviceName, deviceID)
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":         deviceName,
			"peer_id":      strings.TrimSpace(peerID),
			"last_seen_at": &now,
		}),
	}).Create(&Device{
		DeviceID:   deviceID,
		Name:       deviceName,
		PeerID:     strings.TrimSpace(peerID),
		JoinedAt:   now,
		LastSeenAt: &now,
	}).Error; err != nil {
		return err
	}

	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"role": normalizeRole(role),
		}),
	}).Create(&Membership{
		LibraryID:        strings.TrimSpace(libraryID),
		DeviceID:         deviceID,
		Role:             normalizeRole(role),
		CapabilitiesJSON: "{}",
		JoinedAt:         now,
	}).Error
}

func localSettingValueTx(tx *gorm.DB, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	var setting LocalSetting
	if err := tx.Where("key = ?", key).Take(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(setting.Value), nil
}

func admissionAuthorityPrivateKeyLocalSettingKey(libraryID string, version int64) string {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" || version <= 0 {
		return ""
	}
	return fmt.Sprintf("admission_authority_private:%s:%d", libraryID, version)
}

func pseudoAuthoritySignature(libraryID, publicKey string, version, prevVersion int64, signedByKind string, createdAt time.Time) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s|%s|%d|%d|%s|%d",
		strings.TrimSpace(libraryID),
		strings.TrimSpace(publicKey),
		version,
		prevVersion,
		strings.TrimSpace(signedByKind),
		createdAt.UTC().UnixNano(),
	)))
	out := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(out, sum[:])
	return out
}
