package desktopcore

import (
	"context"
	"strings"
	"testing"

	apitypes "ben/core/api/types"
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
	if !strings.HasPrefix(code.InviteCode, "ben-invite-v1.") {
		t.Fatalf("invite code = %q", code.InviteCode)
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
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "invite-join")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	code, err := app.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	session, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: code.InviteCode,
		DeviceID:   "joiner-device",
		DeviceName: "Joiner",
	})
	if err != nil {
		t.Fatalf("start join from invite: %v", err)
	}
	if !session.Pending || session.RequestID == "" {
		t.Fatalf("join session = %+v", session)
	}
	job, ok, err := app.GetJob(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get pending join job: %v", err)
	}
	if !ok {
		t.Fatalf("expected join job for session %q", session.SessionID)
	}
	if job.Kind != jobKindJoinSession || job.Phase != JobPhaseRunning {
		t.Fatalf("pending join job = %+v", job)
	}

	requests, err := app.ListJoinRequests(ctx, inviteJoinStatusPending)
	if err != nil {
		t.Fatalf("list join requests: %v", err)
	}
	if len(requests) != 1 || requests[0].RequestID != session.RequestID {
		t.Fatalf("join requests = %+v", requests)
	}

	if err := app.ApproveJoinRequest(ctx, session.RequestID, roleGuest); err != nil {
		t.Fatalf("approve join request: %v", err)
	}

	session, err = app.GetJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get join session: %v", err)
	}
	if session.Status != joinSessionStatusApproved || session.Role != roleGuest {
		t.Fatalf("approved session = %+v", session)
	}
	job, ok, err = app.GetJob(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get approved join job: %v", err)
	}
	if !ok {
		t.Fatalf("expected approved join job for session %q", session.SessionID)
	}
	if job.Phase != JobPhaseRunning || !strings.Contains(job.Message, "approved") {
		t.Fatalf("approved join job = %+v", job)
	}

	result, err := app.FinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("finalize join session: %v", err)
	}
	if result.LibraryID != library.LibraryID || result.DeviceID != "joiner-device" || result.Role != roleGuest {
		t.Fatalf("join result = %+v", result)
	}
	job, ok, err = app.GetJob(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get completed join job: %v", err)
	}
	if !ok {
		t.Fatalf("expected completed join job for session %q", session.SessionID)
	}
	if job.Phase != JobPhaseCompleted {
		t.Fatalf("completed join job = %+v", job)
	}

	var membership Membership
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, "joiner-device").
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
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "invite-join-async")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	code, err := app.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	session, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: code.InviteCode,
		DeviceID:   "joiner-async-device",
		DeviceName: "Joiner Async",
	})
	if err != nil {
		t.Fatalf("start join from invite: %v", err)
	}
	if err := app.ApproveJoinRequest(ctx, session.RequestID, roleGuest); err != nil {
		t.Fatalf("approve join request: %v", err)
	}

	job, err := app.StartFinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("start finalize join session: %v", err)
	}
	if job.JobID != finalizeJoinSessionJobID(session.SessionID) || job.Kind != jobKindFinalizeJoinSession || job.Phase != JobPhaseQueued {
		t.Fatalf("unexpected queued finalize job: %+v", job)
	}

	final := waitForJobPhase(t, ctx, app, finalizeJoinSessionJobID(session.SessionID), JobPhaseCompleted)
	if final.Kind != jobKindFinalizeJoinSession || final.LibraryID != library.LibraryID {
		t.Fatalf("unexpected final finalize job: %+v", final)
	}

	joined, err := app.GetJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get finalized join session: %v", err)
	}
	if joined.Status != joinSessionStatusCompleted || joined.Role != roleGuest {
		t.Fatalf("finalized join session = %+v", joined)
	}

	var membership Membership
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, "joiner-async-device").
		Take(&membership).Error; err != nil {
		t.Fatalf("load async joined membership: %v", err)
	}
	if membership.Role != roleGuest {
		t.Fatalf("membership role = %q, want %q", membership.Role, roleGuest)
	}
}

func TestJoinRejectCancelAndInviteUseLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "invite-limits"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	code, err := app.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleMember, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	rejected, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode, DeviceID: "reject-device"})
	if err != nil {
		t.Fatalf("start rejected join: %v", err)
	}
	if err := app.RejectJoinRequest(ctx, rejected.RequestID, "no"); err != nil {
		t.Fatalf("reject join request: %v", err)
	}
	job, ok, err := app.GetJob(ctx, rejected.SessionID)
	if err != nil {
		t.Fatalf("get rejected join job: %v", err)
	}
	if !ok || job.Phase != JobPhaseFailed {
		t.Fatalf("rejected join job = %+v ok=%v", job, ok)
	}
	if _, err := app.FinalizeJoinSession(ctx, rejected.SessionID); err == nil || !strings.Contains(err.Error(), joinSessionStatusRejected) {
		t.Fatalf("finalize rejected join err = %v", err)
	}

	canceled, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode, DeviceID: "cancel-device"})
	if err != nil {
		t.Fatalf("start canceled join: %v", err)
	}
	if err := app.CancelJoinSession(ctx, canceled.SessionID); err != nil {
		t.Fatalf("cancel join session: %v", err)
	}
	job, ok, err = app.GetJob(ctx, canceled.SessionID)
	if err != nil {
		t.Fatalf("get canceled join job: %v", err)
	}
	if !ok || job.Phase != JobPhaseFailed {
		t.Fatalf("canceled join job = %+v ok=%v", job, ok)
	}
	if _, err := app.GetJoinSession(ctx, canceled.SessionID); err != nil {
		t.Fatalf("get canceled join session: %v", err)
	}

	approved, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode, DeviceID: "approved-device"})
	if err != nil {
		t.Fatalf("start approved join: %v", err)
	}
	exhausted, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code.InviteCode, DeviceID: "exhausted-device"})
	if err != nil {
		t.Fatalf("start exhausted join: %v", err)
	}
	if err := app.ApproveJoinRequest(ctx, approved.RequestID, roleMember); err != nil {
		t.Fatalf("approve first limited-use join: %v", err)
	}
	if err := app.ApproveJoinRequest(ctx, exhausted.RequestID, roleMember); err == nil || !strings.Contains(err.Error(), "no remaining uses") {
		t.Fatalf("approve exhausted invite err = %v", err)
	}
}

func TestFinalizeJoinSessionRestoresLibraryMaterialAndOwnerContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "restore-join-material")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}

	code, err := app.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: roleAdmin, Uses: 1})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}
	session, err := app.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: code.InviteCode,
		DeviceID:   "restore-device",
		DeviceName: "Restore Device",
	})
	if err != nil {
		t.Fatalf("start join session: %v", err)
	}
	if err := app.ApproveJoinRequest(ctx, session.RequestID, roleAdmin); err != nil {
		t.Fatalf("approve join request: %v", err)
	}

	if err := app.db.WithContext(ctx).Where("library_id = ?", library.LibraryID).Delete(&AdmissionAuthority{}).Error; err != nil {
		t.Fatalf("delete admission authority: %v", err)
	}
	if err := app.db.WithContext(ctx).Where("library_id = ?", library.LibraryID).Delete(&Library{}).Error; err != nil {
		t.Fatalf("delete library row: %v", err)
	}
	if err := app.db.WithContext(ctx).Where("device_id = ?", local.DeviceID).Delete(&Device{}).Error; err != nil {
		t.Fatalf("delete owner device row: %v", err)
	}
	if err := app.db.WithContext(ctx).Where("library_id = ? AND device_id = ?", library.LibraryID, local.DeviceID).Delete(&Membership{}).Error; err != nil {
		t.Fatalf("delete owner membership row: %v", err)
	}

	result, err := app.FinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("finalize join session: %v", err)
	}
	if result.LibraryID != library.LibraryID || result.Role != roleAdmin {
		t.Fatalf("unexpected join result: %+v", result)
	}

	var restored Library
	if err := app.db.WithContext(ctx).Where("library_id = ?", library.LibraryID).Take(&restored).Error; err != nil {
		t.Fatalf("load restored library: %v", err)
	}
	if restored.Name != "restore-join-material" {
		t.Fatalf("restored library name = %q, want %q", restored.Name, "restore-join-material")
	}
	if strings.TrimSpace(restored.RootPublicKey) == "" || strings.TrimSpace(restored.LibraryKey) == "" {
		t.Fatalf("restored library material = %+v", restored)
	}

	var authority AdmissionAuthority
	if err := app.db.WithContext(ctx).
		Where("library_id = ?", library.LibraryID).
		Order("version DESC").
		Take(&authority).Error; err != nil {
		t.Fatalf("load restored admission authority: %v", err)
	}
	if authority.Version != 1 || strings.TrimSpace(authority.PublicKey) == "" {
		t.Fatalf("restored admission authority = %+v", authority)
	}

	privateKey, err := localSettingValueTx(app.db.WithContext(ctx), admissionAuthorityPrivateKeyLocalSettingKey(library.LibraryID, authority.Version))
	if err != nil {
		t.Fatalf("load admission authority private key: %v", err)
	}
	if strings.TrimSpace(privateKey) == "" {
		t.Fatalf("expected restored admission authority private key")
	}

	var ownerDevice Device
	if err := app.db.WithContext(ctx).Where("device_id = ?", local.DeviceID).Take(&ownerDevice).Error; err != nil {
		t.Fatalf("load restored owner device: %v", err)
	}
	if strings.TrimSpace(ownerDevice.PeerID) == "" {
		t.Fatalf("restored owner device = %+v", ownerDevice)
	}

	var ownerMembership Membership
	if err := app.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, local.DeviceID).
		Take(&ownerMembership).Error; err != nil {
		t.Fatalf("load restored owner membership: %v", err)
	}
	if ownerMembership.Role != roleAdmin {
		t.Fatalf("restored owner role = %q, want %q", ownerMembership.Role, roleAdmin)
	}
}
