import * as InviteFacade from "../../../bindings/ben/desktop/invitefacade";
import { Types } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export function createInvite(
  req: InstanceType<typeof Types.InviteCreateRequest>,
) {
  return traceWailsCall(
    "invite",
    "create_invite",
    { role: req.Role, reusable: req.Reusable },
    () => InviteFacade.CreateInvite(req),
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

export function getJoinAttempt(attemptId: string) {
  return traceWailsCall("invite", "get_join_attempt", { attemptId }, () =>
    InviteFacade.GetJoinAttempt(attemptId),
  );
}

export function cancelJoinAttempt(attemptId: string) {
  return traceWailsCall("invite", "cancel_join_attempt", { attemptId }, () =>
    InviteFacade.CancelJoinAttempt(attemptId),
  );
}

export function listJoinRequests() {
  return traceWailsCall("invite", "list_join_requests", undefined, () =>
    InviteFacade.ListJoinRequests(),
  );
}

export function approveJoinRequest(requestId: string) {
  return traceWailsCall("invite", "approve_join_request", { requestId }, () =>
    InviteFacade.ApproveJoinRequest(requestId),
  );
}

export function rejectJoinRequest(requestId: string) {
  return traceWailsCall("invite", "reject_join_request", { requestId }, () =>
    InviteFacade.RejectJoinRequest(requestId),
  );
}
