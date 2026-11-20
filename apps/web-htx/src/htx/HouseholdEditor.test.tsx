import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { htxCoop } from "@/api/mock/data";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import type { AppGet } from "@/api/types";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdEditorButton } from "./HouseholdEditor";

function value(role: string, fetch: typeof globalThis.fetch) {
  const p = MOCK_PERSONAS.find((x) => x.me.roles.includes(role))!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  return { status: "authenticated", principal, api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(p.id), fetch }) } as unknown as AuthContextValue;
}
function form(auth: AuthContextValue, initial?: AppGet<"/v1/households/{ref}/profile">) {
  return <AuthContextForTests.Provider value={auth}><HouseholdEditorButton coop={htxCoop().api} initial={initial} onSaved={() => {}} /></AuthContextForTests.Provider>;
}
function fill() {
  fireEvent.change(screen.getByLabelText("Họ và tên chủ hộ *"), { target: { value: "Chủ hộ thử nghiệm" } });
  fireEvent.change(screen.getByLabelText("CCCD *"), { target: { value: "079123456789" } });
  fireEvent.change(screen.getByLabelText("Số điện thoại *"), { target: { value: "0907285091" } });
  fireEvent.change(screen.getByLabelText("Diện tích trồng trọt (ha) *"), { target: { value: "1.25" } });
}
describe("AH-285: form ghi hộ theo ma trận", () => {
  it("lỗi chưa rõ kết quả khóa dữ liệu và thử lại đúng khóa thao tác", async () => {
    const posts: Request[] = [];
    const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (input, init) => {
      const req = new Request(input, init); posts.push(req);
      if (posts.length === 1) return new Response(JSON.stringify({ code: "dependency_unavailable", title: "Thử lại", status: 503 }), { status: 503, headers: { "Content-Type": "application/problem+json" } });
      return new Response(JSON.stringify({ household_ref: "h", command_ref: "c", revision: 1 }), { status: 201, headers: { "Content-Type": "application/json" } });
    });
    render(form(value("coop_manager", fetch)));
    fireEvent.click(screen.getByRole("button", { name: "Thêm hộ" })); fill();
    fireEvent.click(screen.getByRole("button", { name: "Lưu hộ" }));
    await screen.findByText("Chưa hoàn tất thao tác. Giữ nguyên thông tin và thử lại.");
    expect(screen.getByLabelText("CCCD *")).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Lưu hộ" }));
    await waitFor(() => expect(posts).toHaveLength(2));
    expect(posts[1]!.headers.get("Idempotency-Key")).toBe(posts[0]!.headers.get("Idempotency-Key"));
    expect(await posts[1]!.json()).toEqual(await posts[0]!.json());
  });
  it("Admin phải chọn HTX; một lần bấm đúp chỉ gửi một command", async () => {
    const posts: Request[] = []; let finish!: () => void;
    const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (input, init) => {
      const req = new Request(input, init);
      if (req.method === "GET") return new Response(JSON.stringify({ items: [htxCoop().api], next_cursor: null }), { headers: { "Content-Type": "application/json" } });
      posts.push(req); await new Promise<void>((resolve) => { finish = resolve; });
      return new Response(JSON.stringify({ household_ref: "h", command_ref: "c", revision: 1 }), { status: 201, headers: { "Content-Type": "application/json" } });
    });
    render(form(value("platform_admin", fetch)));
    fireEvent.click(screen.getByRole("button", { name: "Thêm hộ" }));
    await screen.findByRole("option", { name: htxCoop().api.name });
    fill(); fireEvent.click(screen.getByRole("button", { name: "Lưu hộ" }));
    expect(await screen.findByText("Bắt buộc nhập HTX.")).toBeTruthy(); expect(posts).toHaveLength(0);
    fireEvent.change(screen.getByLabelText("HTX *"), { target: { value: htxCoop().api.ref } });
    const button = screen.getByRole("button", { name: "Lưu hộ" }); fireEvent.click(button); fireEvent.click(button);
    await waitFor(() => expect(posts).toHaveLength(1));
    const request = posts[0]!;
    const payload = await request.json(); expect(payload.cooperative_ref).toBe(htxCoop().api.ref); expect(payload.identity.national_id).toBe("079123456789"); expect(request.headers.get("Idempotency-Key")).toBeTruthy();
    await act(async () => { finish(); });
  });
  it("Member không có form; Đại diện không có ô chọn HTX", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>();
    const view = render(form(value("htx_member", fetch)));
    expect(screen.queryByRole("button", { name: "Thêm hộ" })).toBeNull(); expect(fetch).not.toHaveBeenCalled();
    view.rerender(form(value("coop_manager", fetch)));
    fireEvent.click(screen.getByRole("button", { name: "Thêm hộ" }));
    expect(screen.queryByLabelText("HTX *")).toBeNull(); expect(screen.queryByLabelText(/Ngày gia nhập/)).toBeNull();
  });
  it("sửa gửi revision, không có ô sửa mã hoặc đổi HTX", async () => {
    let request: Request | undefined;
    const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (input, init) => { request = new Request(input, init); return new Response(JSON.stringify({ household_ref: "h", command_ref: "c", revision: 4 }), { headers: { "Content-Type": "application/json" } }); });
    const initial: AppGet<"/v1/households/{ref}/profile"> = { code: "H-285", revision: 3, contact_access: "full", household: { household_ref: "h", cooperative_ref: htxCoop().api.ref, commune_code: "30001", cultivated_area_ha: "2", member_since: null, active: true }, identity: { full_name: "Chủ hộ", phone: "0907285091", national_id: "079123456789", address: "Ấp" } };
    render(form(value("platform_admin", fetch), initial)); fireEvent.click(screen.getByRole("button", { name: "Sửa hộ" }));
    expect(screen.getByText("H-285")).toBeTruthy(); expect(screen.queryByLabelText("HTX *")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Lưu hộ" })); await waitFor(() => expect(request).toBeTruthy());
    const body = await request!.json(); expect(body.expected_revision).toBe(3); expect(body.cooperative_ref).toBe(htxCoop().api.ref); expect(body.code).toBeUndefined();
  });
});
