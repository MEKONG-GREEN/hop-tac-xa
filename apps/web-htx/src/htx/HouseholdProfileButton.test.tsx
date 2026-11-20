import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdProfileButton } from "./HouseholdProfileButton";

const householdRef = "00000000-0000-7000-8000-000000000285";
const profile = {
  household: { household_ref: householdRef, cooperative_ref: "00000000-0000-7000-8000-000000000001", commune_code: "30001", cultivated_area_ha: "1.25", livestock_area_ha: "0.5", member_since: null, active: true },
  identity: { full_name: "Chủ hộ thử nghiệm", phone: "0907***021", national_id: "079******789", address: "Ấp thử nghiệm" },
  contact_access: "masked",
};

function auth(personaId: string, fetch: typeof globalThis.fetch): AuthContextValue {
  const p = MOCK_PERSONAS.find((x) => x.id === personaId)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  return { status: "authenticated", principal, api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(personaId), fetch }) } as unknown as AuthContextValue;
}

function viewer(value: AuthContextValue) {
  return <AuthContextForTests.Provider value={value}><HouseholdProfileButton householdRef={householdRef} /></AuthContextForTests.Provider>;
}

describe("AH-285: chi tiết hộ theo quyền hiện tại", () => {
  it("chỉ tải khi mở, hiển thị bản che từ server và dọn thông tin khi đóng", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response(JSON.stringify(profile), { headers: { "Content-Type": "application/json", "Cache-Control": "no-store" } }));
    render(viewer(auth("htx-member", fetch)));
    expect(fetch).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Xem chi tiết" }));
    await screen.findByText("079******789");
    expect(screen.getByText("0907***021")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Thêm|Sửa|Xóa/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Đóng" }));
    expect(screen.queryByText("079******789")).toBeNull();
  });

  it("không hiển thị dữ liệu của request cũ sau khi đổi người dùng/phạm vi", async () => {
    let finish!: (response: Response) => void;
    const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(() => new Promise<Response>((resolve) => { finish = resolve; }));
    const view = render(viewer(auth("htx", fetch)));
    fireEvent.click(screen.getByRole("button", { name: "Xem chi tiết" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    view.rerender(viewer(auth("htx-member", fetch)));
    await act(async () => { finish(new Response(JSON.stringify({ ...profile, identity: { ...profile.identity, national_id: "079123456789" }, contact_access: "full" }), { headers: { "Content-Type": "application/json" } })); });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText("079123456789")).toBeNull();
  });

  it("owner không sẵn sàng thì báo lỗi, không dựng hồ sơ rỗng hoặc số liệu giả", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response(JSON.stringify({ title: "Chưa xác minh được phạm vi hộ", status: 503, code: "vault.household_scope_unavailable" }), { status: 503, headers: { "Content-Type": "application/problem+json" } }));
    render(viewer(auth("htx-member", fetch)));
    fireEvent.click(screen.getByRole("button", { name: "Xem chi tiết" }));
    await screen.findByRole("alert");
    expect(screen.queryByText("Chủ hộ thử nghiệm")).toBeNull();
    expect(screen.queryByText("Đang tham gia")).toBeNull();
  });
});
