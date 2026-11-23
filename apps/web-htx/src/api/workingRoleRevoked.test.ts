// AH-222 (FN-TK-01/EX-03, AC-06): lớp bọc fetch nhận 409 accounts.working_role_revoked của vai đang chọn.
import { describe, expect, it, vi } from "vitest";
import { WORKING_ROLE_REVOKED_CODE, withWorkingRoleRevokedWatch } from "./workingRoleRevoked";

function req(role: string | null): Request {
  const headers = new Headers();
  if (role) headers.set("X-HTX-Role", role);
  return new Request("https://api.vi-du.vn/v1/htx", { headers });
}

function reply(status: number, body: unknown): Response {
  return new Response(typeof body === "string" ? body : JSON.stringify(body), { status, headers: { "Content-Type": "application/problem+json" } });
}

describe("withWorkingRoleRevokedWatch", () => {
  it("409 của vai đang chọn → onRevoked, thân phản hồi vẫn đọc được", async () => {
    const onRevoked = vi.fn();
    const body = { code: WORKING_ROLE_REVOKED_CODE, detail: "Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc." };
    const res = await withWorkingRoleRevokedWatch(async () => reply(409, body), () => "htx_member", onRevoked)(req("htx_member"));
    expect(onRevoked).toHaveBeenCalledTimes(1);
    expect(await res.json()).toEqual(body);
  });

  it.each([
    ["409 mã khác", reply(409, { code: "accounts.working_role_required" }), "htx_member", "htx_member"],
    ["403 cùng mã", reply(403, { code: WORKING_ROLE_REVOKED_CODE }), "htx_member", "htx_member"],
    ["thân không phải JSON", reply(409, "<html>"), "htx_member", "htx_member"],
    ["phản hồi của vai cũ về muộn", reply(409, { code: WORKING_ROLE_REVOKED_CODE }), "htx_member", "coop_manager"],
    ["chưa chọn vai", reply(409, { code: WORKING_ROLE_REVOKED_CODE }), "htx_member", null],
  ])("%s → không gọi onRevoked", async (_name, res, sent, current) => {
    const onRevoked = vi.fn();
    await withWorkingRoleRevokedWatch(async () => res, () => current, onRevoked)(req(sent));
    expect(onRevoked).not.toHaveBeenCalled();
  });
});
