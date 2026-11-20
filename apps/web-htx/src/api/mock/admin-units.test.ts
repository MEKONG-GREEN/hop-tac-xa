import { describe, expect, it } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { mockToken } from "./personas";

describe("AH-271: mock catalog contract", () => {
  it("Admin thêm/sửa, Member chỉ dùng ô chọn, xã ngừng dùng vẫn đọc được", async () => {
    const admin = createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken("setter"), fetch: mockAdminFetchNoDelay });
    const memberAdmin = createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken("htx-member"), fetch: mockAdminFetchNoDelay });
    const member = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("htx-member"), fetch: mockFetchNoDelay });
    const create = async (name: string, level: "province" | "commune", parent_code = "") => unwrap(admin.POST("/v1/masterdata/admin-unit-profiles", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { name, level, parent_code } }));
    const suffix = crypto.randomUUID(), province = await create(`Tỉnh ${suffix}`, "province"), commune = await create(`Xã ${suffix}`, "commune", province.code);
    const denied = await memberAdmin.GET("/v1/masterdata/admin-unit-profiles"); expect(denied.response.status).toBe(403);
    const before = await unwrap(member.GET("/v1/masterdata/admin-units", { params: { query: { parent: province.code } } })); expect(before.items.map((u) => u.code)).toContain(commune.code);
    const key = newIdempotencyKey();
    const disable = () => unwrap(admin.POST("/v1/masterdata/admin-unit-profiles/{code}/revisions", { params: { path: { code: commune.code }, header: { "Idempotency-Key": key } }, body: { name: commune.name, active: false, expected_revision: commune.revision } }));
    const changed = await disable(); expect((await disable()).revision).toBe(changed.revision);
    const after = await unwrap(member.GET("/v1/masterdata/admin-units", { params: { query: { parent: province.code } } })); expect(after.items.map((u) => u.code)).not.toContain(commune.code);
    const old = await unwrap(member.GET("/v1/masterdata/admin-units/{code}", { params: { path: { code: commune.code } } })); expect(old.name).toBe(commune.name); expect(old.active).toBe(false);
  });
});
