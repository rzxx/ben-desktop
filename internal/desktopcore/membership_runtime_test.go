package desktopcore

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestUpdateLibraryMemberRoleRotatesAuthorityAndReissuesCert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "roles")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	now := time.Now().UTC()

	if err := app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, _, err := ensureLibraryJoinMaterialTx(tx, library.LibraryID, now); err != nil {
			return err
		}
		if err := tx.Create(&Device{
			DeviceID:   "device-member",
			Name:       "device-member",
			PeerID:     "peer-member",
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Membership{
			LibraryID:        library.LibraryID,
			DeviceID:         "device-member",
			Role:             roleMember,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		_, err := issueMembershipCertTx(tx, library.LibraryID, "device-member", "peer-member", roleMember, time.Hour)
		return err
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	beforeCert, ok, err := app.loadMembershipCert(ctx, library.LibraryID, "device-member")
	if err != nil || !ok {
		t.Fatalf("load before cert: ok=%v err=%v", ok, err)
	}
	beforeAuthorities, err := app.loadAdmissionAuthorityChain(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("load before authority chain: %v", err)
	}

	if err := app.UpdateLibraryMemberRole(ctx, "device-member", roleAdmin); err != nil {
		t.Fatalf("update member role: %v", err)
	}

	var membership Membership
	if err := app.db.WithContext(ctx).Where("library_id = ? AND device_id = ?", library.LibraryID, "device-member").Take(&membership).Error; err != nil {
		t.Fatalf("load updated membership: %v", err)
	}
	if membership.Role != roleAdmin {
		t.Fatalf("membership role = %q, want %q", membership.Role, roleAdmin)
	}

	afterCert, ok, err := app.loadMembershipCert(ctx, library.LibraryID, "device-member")
	if err != nil || !ok {
		t.Fatalf("load after cert: ok=%v err=%v", ok, err)
	}
	if afterCert.Serial <= beforeCert.Serial {
		t.Fatalf("updated cert serial = %d, want > %d", afterCert.Serial, beforeCert.Serial)
	}
	if afterCert.Role != roleAdmin {
		t.Fatalf("updated cert role = %q, want %q", afterCert.Role, roleAdmin)
	}

	afterAuthorities, err := app.loadAdmissionAuthorityChain(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("load after authority chain: %v", err)
	}
	if len(afterAuthorities) <= len(beforeAuthorities) {
		t.Fatalf("authority chain len = %d, want > %d", len(afterAuthorities), len(beforeAuthorities))
	}

	revoked, err := app.membershipCertRevoked(ctx, library.LibraryID, "device-member", beforeCert.Serial)
	if err != nil {
		t.Fatalf("check revoked serial: %v", err)
	}
	if !revoked {
		t.Fatal("expected previous membership cert serial to be revoked")
	}
	if local.DeviceID == "" {
		t.Fatal("expected local device id")
	}
}

func TestRemoveLibraryMemberRevokesCertClearsRecoveryAndRotatesAuthority(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "remove-member")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	now := time.Now().UTC()

	if err := app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, _, err := ensureLibraryJoinMaterialTx(tx, library.LibraryID, now); err != nil {
			return err
		}
		if err := tx.Create(&Device{
			DeviceID:   "device-admin",
			Name:       "device-admin",
			PeerID:     "peer-admin",
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Membership{
			LibraryID:        library.LibraryID,
			DeviceID:         "device-admin",
			Role:             roleAdmin,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&MembershipRecovery{
			LibraryID:        library.LibraryID,
			DeviceID:         "device-admin",
			TokenHash:        hashMembershipRecoveryToken("recovery-token"),
			IssuedByDeviceID: "issuer",
			CreatedAt:        now,
			UpdatedAt:        now,
		}).Error; err != nil {
			return err
		}
		if err := upsertLocalSettingTx(tx, membershipRecoveryLocalSettingKey(library.LibraryID, "device-admin"), "recovery-token", now); err != nil {
			return err
		}
		_, err := issueMembershipCertTx(tx, library.LibraryID, "device-admin", "peer-admin", roleAdmin, time.Hour)
		return err
	}); err != nil {
		t.Fatalf("seed admin member: %v", err)
	}

	beforeCert, ok, err := app.loadMembershipCert(ctx, library.LibraryID, "device-admin")
	if err != nil || !ok {
		t.Fatalf("load before cert: ok=%v err=%v", ok, err)
	}
	beforeAuthorities, err := app.loadAdmissionAuthorityChain(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("load before authority chain: %v", err)
	}

	if err := app.RemoveLibraryMember(ctx, "device-admin"); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	var remaining int64
	if err := app.db.WithContext(ctx).Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", library.LibraryID, "device-admin").
		Count(&remaining).Error; err != nil {
		t.Fatalf("count remaining membership: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining memberships = %d, want 0", remaining)
	}
	if _, ok, err := app.loadMembershipCert(ctx, library.LibraryID, "device-admin"); err != nil {
		t.Fatalf("load removed cert: %v", err)
	} else if ok {
		t.Fatal("expected removed membership cert to be deleted")
	}
	revoked, err := app.membershipCertRevoked(ctx, library.LibraryID, "device-admin", beforeCert.Serial)
	if err != nil {
		t.Fatalf("check revoked serial: %v", err)
	}
	if !revoked {
		t.Fatal("expected removed cert serial to be revoked")
	}
	if _, ok, err := app.localMembershipRecoverySecret(ctx, library.LibraryID, "device-admin"); err != nil {
		t.Fatalf("load local recovery secret: %v", err)
	} else if ok {
		t.Fatal("expected local recovery secret to be deleted")
	}
	afterAuthorities, err := app.loadAdmissionAuthorityChain(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("load after authority chain: %v", err)
	}
	if len(afterAuthorities) <= len(beforeAuthorities) {
		t.Fatalf("authority chain len = %d, want > %d", len(afterAuthorities), len(beforeAuthorities))
	}
}

func TestIssueMembershipCertTxDoesNotReuseRevokedSerialAfterDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "reissue-after-removal")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	now := time.Now().UTC()

	if err := app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, _, err := ensureLibraryJoinMaterialTx(tx, library.LibraryID, now); err != nil {
			return err
		}
		if err := tx.Create(&Device{
			DeviceID:   "device-member",
			Name:       "device-member",
			PeerID:     "peer-member",
			JoinedAt:   now,
			LastSeenAt: cloneTimePtr(&now),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Membership{
			LibraryID:        library.LibraryID,
			DeviceID:         "device-member",
			Role:             roleMember,
			CapabilitiesJSON: "{}",
			JoinedAt:         now,
		}).Error; err != nil {
			return err
		}
		_, err := issueMembershipCertTx(tx, library.LibraryID, "device-member", "peer-member", roleMember, time.Hour)
		return err
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	beforeCert, ok, err := app.loadMembershipCert(ctx, library.LibraryID, "device-member")
	if err != nil || !ok {
		t.Fatalf("load before cert: ok=%v err=%v", ok, err)
	}

	if err := app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return revokeMembershipCertTx(tx, library.LibraryID, "device-member", "membership removed", true)
	}); err != nil {
		t.Fatalf("revoke existing cert: %v", err)
	}

	revoked, err := app.membershipCertRevoked(ctx, library.LibraryID, "device-member", beforeCert.Serial)
	if err != nil {
		t.Fatalf("check revoked serial: %v", err)
	}
	if !revoked {
		t.Fatal("expected deleted cert serial to be revoked")
	}

	var reissued membershipCertEnvelope
	if err := app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reissued, err = issueMembershipCertTx(tx, library.LibraryID, "device-member", "peer-member-rejoined", roleMember, time.Hour)
		return err
	}); err != nil {
		t.Fatalf("reissue member cert: %v", err)
	}

	if reissued.Serial <= beforeCert.Serial {
		t.Fatalf("reissued cert serial = %d, want > %d", reissued.Serial, beforeCert.Serial)
	}
	if reissued.PeerID != "peer-member-rejoined" {
		t.Fatalf("reissued cert peer id = %q, want %q", reissued.PeerID, "peer-member-rejoined")
	}
	if revoked, err := app.membershipCertRevoked(ctx, library.LibraryID, "device-member", reissued.Serial); err != nil {
		t.Fatalf("check reissued serial: %v", err)
	} else if revoked {
		t.Fatalf("reissued cert serial %d should not be revoked", reissued.Serial)
	}
	afterCert, ok, err := app.loadMembershipCert(ctx, library.LibraryID, "device-member")
	if err != nil || !ok {
		t.Fatalf("load reissued cert: ok=%v err=%v", ok, err)
	}
	if afterCert.Serial != reissued.Serial {
		t.Fatalf("stored cert serial = %d, want %d", afterCert.Serial, reissued.Serial)
	}
}

func TestEnsureLocalTransportMembershipAuthRefreshesNonAdminWithRecovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "recovery-refresh")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	_, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)

	token := "refresh-token"
	now := time.Now().UTC()
	if err := owner.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"token_hash", "issued_by_device_id", "updated_at"}),
	}).Create(&MembershipRecovery{
		LibraryID:        library.LibraryID,
		DeviceID:         joinerLocal.DeviceID,
		TokenHash:        hashMembershipRecoveryToken(token),
		IssuedByDeviceID: "issuer",
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error; err != nil {
		t.Fatalf("seed owner recovery credential: %v", err)
	}
	if err := joiner.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ? AND device_id = ?", library.LibraryID, joinerLocal.DeviceID).Delete(&MembershipCert{}).Error; err != nil {
			return err
		}
		return upsertLocalSettingTx(tx, membershipRecoveryLocalSettingKey(library.LibraryID, joinerLocal.DeviceID), token, now)
	}); err != nil {
		t.Fatalf("seed joiner recovery secret: %v", err)
	}

	registry := newMemorySyncRegistry()
	owner.SetSyncTransport(registry.transport("memory://owner", owner))
	joiner.SetSyncTransport(registry.transport("memory://joiner", joiner))

	refreshed, err := joiner.ensureLocalTransportMembershipAuth(ctx, joinerLocal, joinerLocal.PeerID)
	if err != nil {
		t.Fatalf("ensure local transport auth with recovery: %v", err)
	}
	if refreshed.Cert.Serial <= 0 || refreshed.Cert.PeerID != joinerLocal.PeerID {
		t.Fatalf("unexpected refreshed cert: %+v", refreshed.Cert)
	}
	row, ok, err := joiner.loadMembershipCert(ctx, library.LibraryID, joinerLocal.DeviceID)
	if err != nil || !ok {
		t.Fatalf("load refreshed stored cert: ok=%v err=%v", ok, err)
	}
	if row.Serial != refreshed.Cert.Serial {
		t.Fatalf("stored cert serial = %d, want %d", row.Serial, refreshed.Cert.Serial)
	}
}

func TestVerifyRemoteOplogRejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openPlaylistTestApp(t)
	joiner := openPlaylistTestApp(t)

	library, err := owner.CreateLibrary(ctx, "oplog-signatures")
	if err != nil {
		t.Fatalf("create owner library: %v", err)
	}
	_, joinerLocal := seedSharedLibraryForSync(t, owner, joiner, library)
	seedPlaylistRecording(t, owner, library.LibraryID, "rec-1", "One")
	if _, err := owner.CreatePlaylist(ctx, "Queue", ""); err != nil {
		t.Fatalf("create owner playlist: %v", err)
	}

	resp, err := owner.buildSyncResponse(ctx, SyncRequest{
		LibraryID: library.LibraryID,
		DeviceID:  joinerLocal.DeviceID,
		PeerID:    joinerLocal.PeerID,
		Clocks:    map[string]int64{},
		MaxOps:    10,
	})
	if err != nil {
		t.Fatalf("build sync response: %v", err)
	}
	if len(resp.Ops) == 0 {
		t.Fatal("expected signed ops in sync response")
	}
	resp.Ops[0].Sig = []byte("tampered")

	if _, err := joiner.applyRemoteOps(ctx, library.LibraryID, resp.Ops); err == nil || !strings.Contains(strings.ToLower(err.Error()), "oplog") {
		t.Fatalf("expected oplog signature verification error, got %v", err)
	}
}
