package desktopcore

import (
	"ben/registryauth"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestInviteIssueListRevokeFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "invite-issue"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	code, err := app.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleGuest, Uses: 2})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}
	if !strings.HasPrefix(code.InviteCode, "ben-invite-v2.") {
		t.Fatalf("invite code = %q", code.InviteCode)
	}
	payload, err := decodeInviteCode(code.InviteCode)
	if err != nil {
		t.Fatalf("decode invite code: %v", err)
	}
	if payload.InviteAuth == nil {
		t.Fatal("expected invite registry auth")
	}
	if err := registryauth.VerifyInviteAttestation(*payload.InviteAuth, time.Now().UTC()); err != nil {
		t.Fatalf("verify invite registry auth: %v", err)
	}

	active, err := app.ListIssuedInvites(ctx, issuedInviteStatusActive)
	if err != nil {
		t.Fatalf("list active invites: %v", err)
	}
	if len(active) != 1 || active[0].InviteCode != code.InviteCode {
		t.Fatalf("active invites = %+v", active)
	}

	if err := app.RevokeIssuedInvite(ctx, active[0].InviteID, "manual revoke"); err != nil {
		t.Fatalf("revoke issued invite: %v", err)
	}

	revoked, err := app.ListIssuedInvites(ctx, issuedInviteStatusRevoked)
	if err != nil {
		t.Fatalf("list revoked invites: %v", err)
	}
	if len(revoked) != 1 || revoked[0].RevokeReason != "manual revoke" {
		t.Fatalf("revoked invites = %+v", revoked)
	}
}

func TestJoinApprovalFinalizeFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "invite-join")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	code, err := owner.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	joinerDevice, err := joiner.ensureCurrentDevice(ctx)
	if err != nil {
		t.Fatalf("joiner current device: %v", err)
	}
	session, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: code.InviteCode,
		DeviceName: "Joiner",
	})
	if err != nil {
		t.Fatalf("start join from invite: %v", err)
	}
	if !session.Pending || session.RequestID == "" {
		t.Fatalf("join session = %+v", session)
	}

	request := waitForJoinRequestStatus(t, ctx, owner, session.RequestID, inviteJoinStatusPending)
	if request.DeviceID != joinerDevice.DeviceID {
		t.Fatalf("join request device id = %q, want %q", request.DeviceID, joinerDevice.DeviceID)
	}

	job, ok, err := joiner.GetJob(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get pending join job: %v", err)
	}
	if !ok || job.Kind != jobKindJoinSession || job.Phase != JobPhaseRunning {
		t.Fatalf("pending join job = %+v ok=%v", job, ok)
	}

	if err := owner.ApproveJoinRequest(ctx, session.RequestID, roleGuest); err != nil {
		t.Fatalf("approve join request: %v", err)
	}

	session = waitForJoinSessionStatus(t, ctx, joiner, session.SessionID, joinSessionStatusApproved)
	if session.Role != roleGuest {
		t.Fatalf("approved session role = %q, want %q", session.Role, roleGuest)
	}
	job, ok, err = joiner.GetJob(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get approved join job: %v", err)
	}
	if !ok || job.Phase != JobPhaseRunning || !strings.Contains(job.Message, "approved") {
		t.Fatalf("approved join job = %+v ok=%v", job, ok)
	}

	result, err := joiner.FinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("finalize join session: %v", err)
	}
	if result.LibraryID != library.LibraryID || result.DeviceID != joinerDevice.DeviceID || result.Role != roleGuest {
		t.Fatalf("join result = %+v", result)
	}

	job, ok, err = joiner.GetJob(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get completed join job: %v", err)
	}
	if !ok || job.Phase != JobPhaseCompleted {
		t.Fatalf("completed join job = %+v ok=%v", job, ok)
	}

	var membership Membership
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, joinerDevice.DeviceID).
		Take(&membership).Error; err != nil {
		t.Fatalf("load joined membership: %v", err)
	}
	if membership.Role != roleGuest {
		t.Fatalf("membership role = %q, want %q", membership.Role, roleGuest)
	}
}

func TestStartFinalizeJoinSessionAsync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "invite-join-async")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	code, err := owner.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	session, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: code.InviteCode,
		DeviceName: "Joiner Async",
	})
	if err != nil {
		t.Fatalf("start join from invite: %v", err)
	}
	if err := owner.ApproveJoinRequest(ctx, session.RequestID, roleGuest); err != nil {
		t.Fatalf("approve join request: %v", err)
	}
	waitForJoinSessionStatus(t, ctx, joiner, session.SessionID, joinSessionStatusApproved)

	job, err := joiner.StartFinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("start finalize join session: %v", err)
	}
	if job.JobID != finalizeJoinSessionJobID(session.SessionID) || job.Kind != jobKindFinalizeJoinSession || job.Phase != JobPhaseQueued {
		t.Fatalf("unexpected queued finalize job: %+v", job)
	}

	final := waitForJobPhaseWithin(t, ctx, joiner, finalizeJoinSessionJobID(session.SessionID), JobPhaseCompleted, 20*time.Second)
	if final.Kind != jobKindFinalizeJoinSession || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final finalize job: %+v", final)
	}

	joined := waitForJoinSessionStatus(t, ctx, joiner, session.SessionID, joinSessionStatusCompleted)
	if joined.Role != roleGuest {
		t.Fatalf("finalized join session = %+v", joined)
	}
}

func TestJoinRejectCancelAndInviteUseLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	rejectedJoiner := openCacheTestApp(t, 1024)
	canceledJoiner := openCacheTestApp(t, 1024)
	approvedJoiner := openCacheTestApp(t, 1024)
	exhaustedJoiner := openCacheTestApp(t, 1024)

	if _, err := owner.CreateLibrary(ctx, "invite-limits"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	code, err := owner.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	rejected, err := rejectedJoiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode})
	if err != nil {
		t.Fatalf("start rejected join: %v", err)
	}
	if err := owner.RejectJoinRequest(ctx, rejected.RequestID, "no"); err != nil {
		t.Fatalf("reject join request: %v", err)
	}
	waitForJoinSessionStatus(t, ctx, rejectedJoiner, rejected.SessionID, joinSessionStatusRejected)
	job, ok, err := rejectedJoiner.GetJob(ctx, rejected.SessionID)
	if err != nil {
		t.Fatalf("get rejected join job: %v", err)
	}
	if !ok || job.Phase != JobPhaseFailed {
		t.Fatalf("rejected join job = %+v ok=%v", job, ok)
	}
	if _, err := rejectedJoiner.FinalizeJoinSession(ctx, rejected.SessionID); err == nil || !strings.Contains(err.Error(), joinSessionStatusRejected) {
		t.Fatalf("finalize rejected join err = %v", err)
	}

	canceled, err := canceledJoiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode})
	if err != nil {
		t.Fatalf("start canceled join: %v", err)
	}
	if err := canceledJoiner.CancelJoinSession(ctx, canceled.SessionID); err != nil {
		t.Fatalf("cancel join session: %v", err)
	}
	waitForJoinRequestStatus(t, ctx, owner, canceled.RequestID, inviteJoinStatusRejected)
	job, ok, err = canceledJoiner.GetJob(ctx, canceled.SessionID)
	if err != nil {
		t.Fatalf("get canceled join job: %v", err)
	}
	if !ok || job.Phase != JobPhaseFailed {
		t.Fatalf("canceled join job = %+v ok=%v", job, ok)
	}
	if _, err := canceledJoiner.GetJoinSession(ctx, canceled.SessionID); err != nil {
		t.Fatalf("get canceled join session: %v", err)
	}

	approved, err := approvedJoiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode})
	if err != nil {
		t.Fatalf("start approved join: %v", err)
	}
	exhausted, err := exhaustedJoiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode})
	if err != nil {
		t.Fatalf("start exhausted join: %v", err)
	}
	if err := owner.ApproveJoinRequest(ctx, approved.RequestID, roleMember); err != nil {
		t.Fatalf("approve first limited-use join: %v", err)
	}
	if err := owner.ApproveJoinRequest(ctx, exhausted.RequestID, roleMember); err == nil || !strings.Contains(err.Error(), "no remaining uses") {
		t.Fatalf("approve exhausted invite err = %v", err)
	}
}

func TestFinalizeJoinSessionRestoresLibraryMaterialAndOwnerContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	owner := openCacheTestApp(t, 1024)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "restore-join-material")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	ownerLocal, err := owner.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("owner active context: %v", err)
	}
	code, err := owner.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleAdmin, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	session, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: code.InviteCode,
		DeviceName: "Restore Device",
	})
	if err != nil {
		t.Fatalf("start join session: %v", err)
	}
	if err := owner.ApproveJoinRequest(ctx, session.RequestID, roleAdmin); err != nil {
		t.Fatalf("approve join request: %v", err)
	}
	waitForJoinSessionStatus(t, ctx, joiner, session.SessionID, joinSessionStatusApproved)

	result, err := joiner.FinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("finalize join session: %v", err)
	}
	if result.LibraryID != library.LibraryID || result.Role != roleAdmin {
		t.Fatalf("unexpected join result: %+v", result)
	}

	var restored Library
	if err := joiner.db.WithContext(ctx).Where("library_id = ?", library.LibraryID).Take(&restored).Error; err != nil {
		t.Fatalf("load restored library: %v", err)
	}
	if restored.Name != "restore-join-material" {
		t.Fatalf("restored library name = %q, want %q", restored.Name, "restore-join-material")
	}
	if strings.TrimSpace(restored.RootPublicKey) == "" || strings.TrimSpace(restored.LibraryKey) == "" {
		t.Fatalf("restored library material = %+v", restored)
	}

	var authority AdmissionAuthority
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ?", library.LibraryID).
		Order("version DESC").
		Take(&authority).Error; err != nil {
		t.Fatalf("load restored admission authority: %v", err)
	}
	if authority.Version != 1 || strings.TrimSpace(authority.PublicKey) == "" {
		t.Fatalf("restored admission authority = %+v", authority)
	}

	privateKey, err := localSettingValueTx(joiner.db.WithContext(ctx), admissionAuthorityPrivateKeyLocalSettingKey(library.LibraryID, authority.Version))
	if err != nil {
		t.Fatalf("load admission authority private key: %v", err)
	}
	if strings.TrimSpace(privateKey) == "" {
		t.Fatalf("expected restored admission authority private key")
	}

	var ownerDevice Device
	if err := joiner.db.WithContext(ctx).Where("device_id = ?", ownerLocal.DeviceID).Take(&ownerDevice).Error; err != nil {
		t.Fatalf("load restored owner device: %v", err)
	}
	if strings.TrimSpace(ownerDevice.PeerID) == "" {
		t.Fatalf("restored owner device = %+v", ownerDevice)
	}

	var ownerMembership Membership
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, ownerLocal.DeviceID).
		Take(&ownerMembership).Error; err != nil {
		t.Fatalf("load restored owner membership: %v", err)
	}
	if ownerMembership.Role != roleAdmin {
		t.Fatalf("restored owner role = %q, want %q", ownerMembership.Role, roleAdmin)
	}
}

func TestJoinSessionRefreshResumesAfterRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ownerRoot := filepath.Join(t.TempDir(), "owner")
	joinerRoot := filepath.Join(t.TempDir(), "joiner")

	owner := openPlaylistTestAppAtPath(t, ownerRoot)
	t.Cleanup(func() {
		_ = owner.Close()
	})
	joiner := openPlaylistTestAppAtPath(t, joinerRoot)

	library, err := owner.CreateLibrary(ctx, "invite-restart-resume")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	code, err := owner.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	session, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode})
	if err != nil {
		t.Fatalf("start join session: %v", err)
	}
	if err := joiner.Close(); err != nil {
		t.Fatalf("close joiner before restart: %v", err)
	}

	joiner = openPlaylistTestAppAtPath(t, joinerRoot)
	t.Cleanup(func() {
		_ = joiner.Close()
	})

	if err := owner.ApproveJoinRequest(ctx, session.RequestID, roleMember); err != nil {
		t.Fatalf("approve join request: %v", err)
	}
	waitForJoinSessionStatus(t, ctx, joiner, session.SessionID, joinSessionStatusApproved)

	result, err := joiner.FinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("finalize restarted join session: %v", err)
	}
	if result.LibraryID != library.LibraryID || result.Role != roleMember {
		t.Fatalf("unexpected restarted join result: %+v", result)
	}
}

func waitForJoinSessionStatus(t *testing.T, ctx context.Context, app *App, sessionID, want string) apitypes.JoinSession {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		session, err := app.GetJoinSession(ctx, sessionID)
		if err == nil && session.Status == want {
			return session
		}
		time.Sleep(50 * time.Millisecond)
	}

	session, err := app.GetJoinSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get join session after wait: %v", err)
	}
	t.Fatalf("join session %q status = %q, want %q", sessionID, session.Status, want)
	return apitypes.JoinSession{}
}

func waitForJoinRequestStatus(t *testing.T, ctx context.Context, app *App, requestID, want string) apitypes.InviteJoinRequestRecord {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := app.ListJoinRequests(ctx, "")
		if err == nil {
			for _, row := range rows {
				if row.RequestID == requestID && row.Status == want {
					return row
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	rows, err := app.ListJoinRequests(ctx, "")
	if err != nil {
		t.Fatalf("list join requests after wait: %v", err)
	}
	t.Fatalf("request %q status did not reach %q: %+v", requestID, want, rows)
	return apitypes.InviteJoinRequestRecord{}
}
