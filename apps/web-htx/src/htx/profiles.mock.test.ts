import { describe, expect, it } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ADMIN_UNITS, COOPERATIVES, htxCoop } from "@/api/mock/data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";

const app = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
const admin = (persona: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });
const input = () => ({ name: `HTX kiểm thử ${newIdempotencyKey()}`, commune_code: ADMIN_UNITS.find((unit) => unit.level === "commune" && unit.active !== false)!.code, head_office: "Trụ sở thử nghiệm", representative_name: "", phone: "0912345678" });

describe("AH-283: mock chạy cùng hợp đồng hồ sơ HTX", () => {
  it("tạo không bắt đại diện, receipt không PII; phát lại không tạo trùng", async () => {
    const body = input(), key = newIdempotencyKey();
    const options = { body, params: { header: { "Idempotency-Key": key } } };
    const first = await unwrap(admin("setter").POST("/v1/cooperative-profiles", options));
    try {
      expect(Object.keys(first).sort()).toEqual(["code", "ref", "revision"]);
      expect(await unwrap(admin("setter").POST("/v1/cooperative-profiles", options))).toEqual(first);
      const profile = await unwrap(app("setter").GET("/v1/cooperatives/{ref}/profile", { params: { path: { ref: first.ref } } }));
      expect(profile).toMatchObject({ name: body.name, phone: body.phone, representative_name: "", can_edit: true, machine_count: null });
      expect(profile.household_areas).toMatchObject({ households: 0, cultivated_ha: "0", livestock_ha: "0" });
      await expect(unwrap(admin("setter").POST("/v1/cooperative-profiles", { body: { ...body, name: `  ${body.name.toUpperCase()}  ` }, params: { header: { "Idempotency-Key": newIdempotencyKey() } } }))).rejects.toMatchObject({ status: 409, code: "party.cooperative_name_taken" });
      const update = { ...body, name: `${body.name} sửa`, expected_revision: first.revision };
      const saved = await unwrap(app("setter").POST("/v1/cooperatives/{ref}/profile/revisions", { params: { path: { ref: first.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: update }));
      expect(saved).toMatchObject({ ref: first.ref, code: first.code, revision: first.revision + 1 });
      await expect(unwrap(app("setter").POST("/v1/cooperatives/{ref}/profile/revisions", { params: { path: { ref: first.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: update }))).rejects.toMatchObject({ status: 409, code: "party.cooperative_profile_changed" });
      await expect(unwrap(app("htx").GET("/v1/cooperatives/{ref}/profile", { params: { path: { ref: first.ref } } }))).rejects.toMatchObject({ status: 404 });
    } finally {
      const index = COOPERATIVES.findIndex((coop) => coop.api.ref === first.ref);
      if (index >= 0) COOPERATIVES.splice(index, 1);
    }
  });

  it.each(["htx", "htx-member", "admin-no-mfa"])("%s không được thêm HTX", async (persona) => {
    await expect(unwrap(admin(persona).POST("/v1/cooperative-profiles", { body: input(), params: { header: { "Idempotency-Key": newIdempotencyKey() } } }))).rejects.toMatchObject({ status: 403 });
  });

  it("Member không được sửa kể cả HTX của mình", async () => {
    await expect(unwrap(app("htx-member").POST("/v1/cooperatives/{ref}/profile/revisions", { params: { path: { ref: htxCoop().api.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { ...input(), expected_revision: 1 } }))).rejects.toMatchObject({ status: 403 });
  });
});
