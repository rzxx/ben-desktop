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

	result, err := app.FinalizeJoinSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("finalize join session: %v", err)
	}
	if result.LibraryID != library.LibraryID || result.DeviceID != "joiner-device" || result.Role != roleGuest {
		t.Fatalf("join result = %+v", result)
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
