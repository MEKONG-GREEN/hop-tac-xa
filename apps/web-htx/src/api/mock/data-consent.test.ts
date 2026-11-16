// AH-23 / REQ-011 / KH-24: backend giả lập của Đồng ý thu thập dữ liệu — tài khoản HTX mới phải đồng ý, đồng ý xong thì
// không hỏi lại, phiên bản sai bị từ chối, Admin không bị hỏi.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, unwrap, type ApiClient } from "../client";
import { newIdempotencyKey } from "../idempotency";
import { MOCK_DATA_CONSENT, resetMockDataConsent } from "./data-consent";
import { mockFetchNoDelay } from "./handler";
import { mockToken } from "./personas";

const clientFor = (persona: string): ApiClient =>
  createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });

const accept = (api: ApiClient, version: string) =>
  api.POST("/v1/me/data-consent", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { version } });

describe("mock Đồng ý thu thập dữ liệu", () => {
  beforeEach(() => resetMockDataConsent());

  it("AC1: tài khoản HTX mới → required kèm nội dung và phiên bản", async () => {
    const r = await unwrap(clientFor("htx").GET("/v1/me/data-consent"));
    expect(r.required).toBe(true);
    expect(r.version).toBe(MOCK_DATA_CONSENT.version);
    expect(r.text).toBe(MOCK_DATA_CONSENT.text);
    expect(r.consented_at).toBeNull();
  });

  it("AC2/AC-N1: đồng ý → 201 kèm bằng chứng; GET sau đó không required; đồng ý lại → 200 bản cũ", async () => {
    const api = clientFor("htx");
    const first = await accept(api, MOCK_DATA_CONSENT.version);
    expect(first.response.status).toBe(201);
    expect(first.data?.version).toBe("tam-1");
    expect(first.data?.consented_at).toBeTruthy();
    const after = await unwrap(api.GET("/v1/me/data-consent"));
    expect(after.required).toBe(false);
    expect(after.consented_at).toBe(first.data?.consented_at);
    const again = await accept(api, MOCK_DATA_CONSENT.version);
    expect(again.response.status).toBe(200);
    expect(again.data?.id).toBe(first.data?.id);
  });

  it("phiên bản không còn hiện hành → 409 stale; version sai dạng → 422", async () => {
    const api = clientFor("htx");
    const stale = await accept(api, "tam-0");
    expect(stale.response.status).toBe(409);
    expect((stale.error as { code?: string }).code).toBe("accounts.data_consent_version_stale");
    expect((await accept(api, "sai dạng!")).response.status).toBe(422);
  });

  it("persona htx mặc định đã đồng ý tam-1 (luồng thử hiện có không đổi) cho tới khi reset", async () => {
    vi.resetModules();
    const fresh = await import("./data-consent");
    const handlerMod = await import("./handler");
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("htx"), fetch: handlerMod.mockFetchNoDelay });
    expect(await unwrap(api.GET("/v1/me/data-consent"))).toMatchObject({ required: false, version: "tam-1" });
    fresh.resetMockDataConsent();
    expect((await unwrap(api.GET("/v1/me/data-consent"))).required).toBe(true);
  });

  it("AC-N4: Admin không bị hỏi và không ghi được", async () => {
    const api = clientFor("setter");
    const r = await unwrap(api.GET("/v1/me/data-consent"));
    expect(r).toMatchObject({ required: false, version: null, text: null });
    expect((await accept(api, MOCK_DATA_CONSENT.version)).response.status).toBe(403);
  });
});
