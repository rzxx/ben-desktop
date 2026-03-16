import * as InviteFacade from "../../../bindings/ben/desktop/invitefacade";
import { Types } from "./models";

export function createInviteCode(
  req: InstanceType<typeof Types.InviteCodeRequest>,
) {
  return InviteFacade.CreateInviteCode(req);
}

export function listIssuedInvites(status = "") {
  return InviteFacade.ListIssuedInvites(status);
}

export function revokeIssuedInvite(inviteId: string, reason: string) {
  return InviteFacade.RevokeIssuedInvite(inviteId, reason);
}

export function startJoinFromInvite(
  req: InstanceType<typeof Types.JoinFromInviteInput>,
) {
  return InviteFacade.StartJoinFromInvite(req);
}

export function getJoinSession(sessionId: string) {
  return InviteFacade.GetJoinSession(sessionId);
}

export function startFinalizeJoinSession(sessionId: string) {
  return InviteFacade.StartFinalizeJoinSession(sessionId);
}

export function cancelJoinSession(sessionId: string) {
  return InviteFacade.CancelJoinSession(sessionId);
}

export function listJoinRequests(status = "") {
  return InviteFacade.ListJoinRequests(status);
}

export function approveJoinRequest(requestId: string, role: string) {
  return InviteFacade.ApproveJoinRequest(requestId, role);
}

export function rejectJoinRequest(requestId: string, reason: string) {
  return InviteFacade.RejectJoinRequest(requestId, reason);
}
