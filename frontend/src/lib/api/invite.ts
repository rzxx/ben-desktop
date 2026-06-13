import * as InviteFacade from "../../../bindings/ben/desktop/invitefacade";
import { Types } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export function createInvite(
  req: InstanceType<typeof Types.InviteCreateRequest>,
) {
  return traceWailsCall("invite", "create_invite", { role: req.Role }, () =>
    InviteFacade.CreateInvite(req),
  );
}

export function listActiveInvites() {
  return traceWailsCall("invite", "list_active_invites", undefined, () =>
    InviteFacade.ListActiveInvites(),
  );
}

export function deleteInvite(inviteId: string) {
  return traceWailsCall("invite", "delete_invite", { inviteId }, () =>
    InviteFacade.DeleteInvite(inviteId),
  );
}

export function startJoinFromInvite(
  req: InstanceType<typeof Types.JoinFromInviteInput>,
) {
  return traceWailsCall("invite", "start_join_from_invite", {}, () =>
    InviteFacade.StartJoinFromInvite(req),
  );
}

export function getJoinSession(sessionId: string) {
  return traceWailsCall("invite", "get_join_session", { sessionId }, () =>
    InviteFacade.GetJoinSession(sessionId),
  );
}

export function startFinalizeJoinSession(sessionId: string) {
  return traceWailsCall(
    "invite",
    "start_finalize_join_session",
    { sessionId },
    () => InviteFacade.StartFinalizeJoinSession(sessionId),
  );
}

export function cancelJoinSession(sessionId: string) {
  return traceWailsCall("invite", "cancel_join_session", { sessionId }, () =>
    InviteFacade.CancelJoinSession(sessionId),
  );
}

export function listJoinRequests(status = "") {
  return traceWailsCall("invite", "list_join_requests", { status }, () =>
    InviteFacade.ListJoinRequests(status),
  );
}

export function approveJoinRequest(requestId: string, role: string) {
  return traceWailsCall(
    "invite",
    "approve_join_request",
    { requestId, role },
    () => InviteFacade.ApproveJoinRequest(requestId, role),
  );
}

export function rejectJoinRequest(requestId: string, reason: string) {
  return traceWailsCall(
    "invite",
    "reject_join_request",
    { requestId, reason },
    () => InviteFacade.RejectJoinRequest(requestId, reason),
  );
}
