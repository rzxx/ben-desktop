package desktopcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apitypes "ben/desktop/api/types"
	"github.com/libp2p/go-libp2p/core/peer"
	"gorm.io/gorm"
)

func oplogSigningPayload(entry checkpointOplogEntry) ([]byte, error) {
	body := struct {
		OpID                   string `json:"op_id"`
		DeviceID               string `json:"device_id"`
		Seq                    int64  `json:"seq"`
		TSNS                   int64  `json:"ts_ns"`
		EntityType             string `json:"entity_type"`
		EntityID               string `json:"entity_id"`
		OpKind                 string `json:"op_kind"`
		Payload                string `json:"payload"`
		SignerPeerID           string `json:"signer_peer_id"`
		SignerAuthorityVersion int64  `json:"signer_authority_version"`
		SignerCertSerial       int64  `json:"signer_cert_serial"`
		SignerRole             string `json:"signer_role"`
		SignerIssuedAt         int64  `json:"signer_issued_at"`
		SignerExpiresAt        int64  `json:"signer_expires_at"`
		SignerCertSig          []byte `json:"signer_cert_sig"`
	}{
		OpID:                   strings.TrimSpace(entry.OpID),
		DeviceID:               strings.TrimSpace(entry.DeviceID),
		Seq:                    entry.Seq,
		TSNS:                   entry.TSNS,
		EntityType:             strings.TrimSpace(entry.EntityType),
		EntityID:               strings.TrimSpace(entry.EntityID),
		OpKind:                 strings.TrimSpace(entry.OpKind),
		Payload:                strings.TrimSpace(string(entry.PayloadJSON)),
		SignerPeerID:           strings.TrimSpace(entry.SignerPeerID),
		SignerAuthorityVersion: entry.SignerAuthorityVersion,
		SignerCertSerial:       entry.SignerCertSerial,
		SignerRole:             normalizeRole(entry.SignerRole),
		SignerIssuedAt:         entry.SignerIssuedAt,
		SignerExpiresAt:        entry.SignerExpiresAt,
		SignerCertSig:          append([]byte(nil), entry.SignerCertSig...),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal oplog signing payload: %w", err)
	}
	return raw, nil
}

func checkpointEntryFromRow(row OplogEntry) checkpointOplogEntry {
	entry := checkpointOplogEntry{
		OpID:                   strings.TrimSpace(row.OpID),
		DeviceID:               strings.TrimSpace(row.DeviceID),
		Seq:                    row.Seq,
		TSNS:                   row.TSNS,
		EntityType:             strings.TrimSpace(row.EntityType),
		EntityID:               strings.TrimSpace(row.EntityID),
		OpKind:                 strings.TrimSpace(row.OpKind),
		SignerPeerID:           strings.TrimSpace(row.SignerPeerID),
		SignerAuthorityVersion: row.SignerAuthorityVersion,
		SignerCertSerial:       row.SignerCertSerial,
		SignerRole:             normalizeRole(row.SignerRole),
		SignerIssuedAt:         row.SignerIssuedAt,
		SignerExpiresAt:        row.SignerExpiresAt,
		SignerCertSig:          append([]byte(nil), row.SignerCertSig...),
		Sig:                    append([]byte(nil), row.Sig...),
	}
	if payload := strings.TrimSpace(row.PayloadJSON); payload != "" {
		entry.PayloadJSON = json.RawMessage(payload)
	}
	return entry
}

func (a *App) ensureLocalOplogSignatures(ctx context.Context, local apitypes.LocalContext) error {
	if a == nil {
		return nil
	}
	local, err := a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return err
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return err
	}
	priv, err := loadOrCreateTransportIdentityKey(a.cfg.IdentityKeyPath)
	if err != nil {
		return fmt.Errorf("load transport identity: %w", err)
	}

	return a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var rows []OplogEntry
		if err := tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(local.LibraryID), strings.TrimSpace(local.DeviceID)).
			Order("seq ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			needsSign := len(row.Sig) == 0 || strings.TrimSpace(row.SignerPeerID) != strings.TrimSpace(local.PeerID) || row.SignerCertSerial != auth.Cert.Serial
			if !needsSign {
				continue
			}
			entry := checkpointEntryFromRow(row)
			entry.SignerPeerID = strings.TrimSpace(local.PeerID)
			entry.SignerAuthorityVersion = auth.Cert.AuthorityVersion
			entry.SignerCertSerial = auth.Cert.Serial
			entry.SignerRole = normalizeRole(auth.Cert.Role)
			entry.SignerIssuedAt = auth.Cert.IssuedAt
			entry.SignerExpiresAt = auth.Cert.ExpiresAt
			entry.SignerCertSig = append([]byte(nil), auth.Cert.Sig...)
			payload, err := oplogSigningPayload(entry)
			if err != nil {
				return err
			}
			sig, err := priv.Sign(payload)
			if err != nil {
				return fmt.Errorf("sign oplog entry %s: %w", row.OpID, err)
			}
			if err := tx.Model(&OplogEntry{}).
				Where("library_id = ? AND op_id = ?", row.LibraryID, row.OpID).
				Updates(map[string]any{
					"signer_peer_id":           entry.SignerPeerID,
					"signer_authority_version": entry.SignerAuthorityVersion,
					"signer_cert_serial":       entry.SignerCertSerial,
					"signer_role":              entry.SignerRole,
					"signer_issued_at":         entry.SignerIssuedAt,
					"signer_expires_at":        entry.SignerExpiresAt,
					"signer_cert_sig":          append([]byte(nil), entry.SignerCertSig...),
					"sig":                      append([]byte(nil), sig...),
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func verifyCheckpointOplogEntryTx(tx *gorm.DB, libraryID string, entry checkpointOplogEntry) error {
	if strings.TrimSpace(entry.DeviceID) == "" || strings.TrimSpace(entry.OpID) == "" {
		return fmt.Errorf("invalid oplog identity fields")
	}
	if strings.TrimSpace(entry.SignerPeerID) == "" || entry.SignerCertSerial <= 0 || entry.SignerAuthorityVersion <= 0 || len(entry.SignerCertSig) == 0 {
		return fmt.Errorf("missing oplog signer metadata")
	}
	if len(entry.Sig) == 0 {
		return fmt.Errorf("missing oplog signature")
	}

	var authorityRows []AdmissionAuthority
	if err := tx.Where("library_id = ?", strings.TrimSpace(libraryID)).Order("version ASC").Find(&authorityRows).Error; err != nil {
		return err
	}
	var library Library
	if err := tx.Select("root_public_key").Where("library_id = ?", strings.TrimSpace(libraryID)).Take(&library).Error; err != nil {
		return err
	}
	signerCert := membershipCertEnvelope{
		LibraryID:        strings.TrimSpace(libraryID),
		DeviceID:         strings.TrimSpace(entry.DeviceID),
		PeerID:           strings.TrimSpace(entry.SignerPeerID),
		Role:             normalizeRole(entry.SignerRole),
		AuthorityVersion: entry.SignerAuthorityVersion,
		Serial:           entry.SignerCertSerial,
		IssuedAt:         entry.SignerIssuedAt,
		ExpiresAt:        entry.SignerExpiresAt,
		Sig:              append([]byte(nil), entry.SignerCertSig...),
	}
	if err := verifyHistoricalMembershipCert(signerCert, admissionAuthorityChainFromRows(authorityRows), strings.TrimSpace(library.RootPublicKey), entry.TSNS, libraryID, entry.DeviceID, entry.SignerPeerID); err != nil {
		return err
	}

	pid, err := peer.Decode(strings.TrimSpace(entry.SignerPeerID))
	if err != nil {
		return fmt.Errorf("decode oplog signer peer id: %w", err)
	}
	pub, err := pid.ExtractPublicKey()
	if err != nil {
		return fmt.Errorf("extract oplog signer public key: %w", err)
	}
	payload, err := oplogSigningPayload(entry)
	if err != nil {
		return err
	}
	ok, err := pub.Verify(payload, entry.Sig)
	if err != nil {
		return fmt.Errorf("verify oplog signature: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid oplog signature")
	}
	return nil
}
