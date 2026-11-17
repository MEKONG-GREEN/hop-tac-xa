// AH-301 (FN-01/EX-06): lớp bọc fetch dùng chung nhận 403 accounts.account_locked của token đang dùng.
import { describe, expect, it, vi } from "vitest";
import { ACCOUNT_LOCKED_CODE, withAccountLockWatch } from "./accountLock";

const TOKEN = "tok-hien-tai";

function req(token: string | null = TOKEN): Request {
  const headers = new Headers();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  return new Request("https://api.vi-du.vn/v1/me", { headers });
}

function reply(status: number, body: unknown): Response {
  const text = typeof body === "string" ? body : JSON.stringify(body);
  return new Response(text, { status, headers: { "Content-Type": "application/problem+json" } });
}

function watch(res: Response, current: string | null = TOKEN) {
  const onLocked = vi.fn();
  const fetchFn = withAccountLockWatch(async () => res, () => current, onLocked);
  return { fetchFn, onLocked };
}

describe("withAccountLockWatch", () => {
  it("[AH-301 FN-01/AC-N2] 403 accounts.account_locked của token đang dùng → onLocked một lần, thân phản hồi vẫn đọc được", async () => {
    const body = { code: ACCOUNT_LOCKED_CODE, detail: "Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa." };
    const { fetchFn, onLocked } = watch(reply(403, body));
    const res = await fetchFn(req());
    expect(onLocked).toHaveBeenCalledTimes(1);
    expect(res.status).toBe(403);
    expect(await res.json()).toEqual(body);
  });

  it.each([
    ["403 mã khác", reply(403, { code: "accounts.data_consent_required" }), TOKEN, TOKEN],
    ["thân không phải JSON", reply(403, "<html>"), TOKEN, TOKEN],
    ["409 có mã khóa", reply(409, { code: ACCOUNT_LOCKED_CODE }), TOKEN, TOKEN],
    ["phản hồi của token cũ về muộn", reply(403, { code: ACCOUNT_LOCKED_CODE }), "tok-cu", TOKEN],
    ["không còn token hiện tại", reply(403, { code: ACCOUNT_LOCKED_CODE }), TOKEN, null],
    ["yêu cầu không mang token", reply(403, { code: ACCOUNT_LOCKED_CODE }), null, TOKEN],
  ])("không gọi onLocked: %s", async (_name, res, sent, current) => {
    const { fetchFn, onLocked } = watch(res, current);
    const out = await fetchFn(req(sent));
    expect(onLocked).not.toHaveBeenCalled();
    expect(out).toBe(res);
  });
});
