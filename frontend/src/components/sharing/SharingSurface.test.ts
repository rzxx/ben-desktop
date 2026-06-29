import { describe, expect, test } from "vitest";
import surfaceSource from "./SharingSurface.tsx?raw";
import hookSource from "../../hooks/sharing/useSharingPage.ts?raw";
import inviteApiSource from "../../lib/api/invite.ts?raw";

describe("streamlined sharing surface", () => {
  test("create form sends role and reusable mode only", () => {
    expect(hookSource).toContain("new Types.InviteCreateRequest({");
    expect(hookSource).toContain("Role: inviteRole");
    expect(hookSource).toContain("Reusable: inviteReusable");
    expect(hookSource).not.toContain("MaxUses");
    expect(hookSource).not.toContain("Expires:");
    expect(hookSource).not.toContain("inviteExpiryHours");
  });

  test("request list renders the pending rows surfaced by the hook", () => {
    expect(surfaceSource).toContain("pendingRequests.map");
    expect(surfaceSource).not.toContain("state.requests.map");
    expect(surfaceSource).not.toContain("request.Status");
    expect(surfaceSource).not.toContain("approvalRoles");
  });

  test("join flow has no persisted session or history behavior", () => {
    const sources = [surfaceSource, hookSource, inviteApiSource].join("\n");

    expect(sources).not.toContain("localStorage");
    expect(sources).not.toContain("GetJoinSession");
    expect(sources).not.toContain("StartFinalizeJoinSession");
    expect(sources).not.toContain("CancelJoinSession");
    expect(sources).not.toContain("InviteLink");
    expect(sources).not.toContain("RequestedRole");
    expect(sources).not.toContain("RedemptionCount");
  });
});
