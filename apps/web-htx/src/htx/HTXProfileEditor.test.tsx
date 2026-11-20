import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HTXProfileEditor } from "./HTXProfileEditor";
import { HTXProfileOverview } from "./HTXProfileOverview";
import type { HTXProfile } from "./profiles";

const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));
const REF = "01927b3e-0ff1-7000-8000-000000002830";
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
const profile: HTXProfile = { ref: REF, kind: "cooperative", code: "HTX000283", name: "HTX thử nghiệm", active: true, admin_unit_code: "X-OLD", parent_ref: null, representative_ref: null,
  province_code: "P-OLD", province_name: "Tỉnh đang lưu", commune_name: "Xã đang lưu", status_reason: null, tax_code: null, area_ha: "999", established_on: null,
  location: null, location_provisional: true, confirmation_status: "unconfirmed", confirmation_label: "Chưa xác nhận", member_households: 1, digitized_plots: 0, address: "Trụ sở đã lưu",
  revision: 42, representative_name: "", phone: "0907248360", contact_masked: false, contact_configured: true, can_edit: true, machine_count: null, machine_groups: null,
  household_areas: { cultivated_ha: "1.5", livestock_ha: "0.5", households: 1, missing_cultivated: 0, missing_livestock: 0 } };

function setup(persona = "setter", options: { edit?: boolean; failure?: boolean; overview?: boolean; machines?: boolean; data?: HTXProfile } = {}) {
  const person = MOCK_PERSONAS.find((person) => person.id === persona)!;
  const principal = buildPrincipal({ sub: person.me.actor_ref, name: person.displayName, amr: person.amr, realm_access: { roles: person.me.roles } }, person.me);
  const writes: { path: string; key: string | null; body: Record<string, unknown> }[] = [], gets: string[] = [];
  const data = options.data ?? profile;
  const fetch = async (request: Request) => {
    const url = new URL(request.url);
    if (request.method === "POST") {
      writes.push({ path: url.pathname, key: request.headers.get("Idempotency-Key"), body: await request.json() as Record<string, unknown> });
      if (options.failure && writes.length === 1) return json({ type: "", status: 503, title: "Thử lại", code: "party.dependency_unavailable" }, 503);
      return json({ ref: data.ref, code: data.code, revision: data.revision + 1 }, options.edit ? 200 : 201);
    }
    gets.push(url.pathname);
    if (url.pathname.endsWith("/profile")) return json(data);
    if (url.pathname === "/v1/masterdata/admin-units") return json({ items: options.edit ? [] : url.searchParams.has("parent")
      ? [{ code: "X-NEW", name: "Xã kiểm thử", level: "commune", parent_code: "P-NEW" }]
      : [{ code: "P-NEW", name: "Tỉnh kiểm thử", level: "province", parent_code: null }], next_cursor: null });
    throw new Error(`Unexpected GET ${url.pathname}`);
  };
  const auth = { principal, status: "authenticated", api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch }),
    adminApi: createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => "t", fetch }) } as unknown as AuthContextValue;
  render(<AuthContextForTests value={auth}>{options.overview ? <HTXProfileOverview cooperativeRef={data.ref} machinesOnly={options.machines} /> : <HTXProfileEditor cooperativeRef={options.edit ? data.ref : undefined} />}</AuthContextForTests>);
  return { writes, gets };
}

describe("AH-283 hồ sơ HTX", () => {
  it.each(["htx", "htx-member"])("%s không mở luồng thêm và không gọi API", (persona) => {
    const { gets, writes } = setup(persona);
    expect(screen.getByRole("alert")).toHaveTextContent("không có quyền thêm HTX");
    expect(gets).toEqual([]); expect(writes).toEqual([]);
  });

  it("Đại diện không mở form sửa HTX khác", () => {
    const { gets } = setup("htx", { edit: true });
    expect(screen.getByRole("alert")).toHaveTextContent("không có quyền sửa hồ sơ HTX này");
    expect(gets).toEqual([]);
  });

  it("Admin tạo từ danh mục, người đại diện tùy chọn, không gửi mã/diện tích", async () => {
    const { writes } = setup();
    fireEvent.change(screen.getByLabelText("Tên HTX"), { target: { value: "HTX mới" } });
    await screen.findByRole("option", { name: "Tỉnh kiểm thử" });
    fireEvent.change(screen.getByLabelText("Tỉnh / thành phố"), { target: { value: "P-NEW" } });
    await screen.findByRole("option", { name: "Xã kiểm thử" });
    fireEvent.change(screen.getByLabelText("Xã / phường / đặc khu"), { target: { value: "X-NEW" } });
    fireEvent.change(screen.getByLabelText("Địa chỉ trụ sở"), { target: { value: "Ấp 1" } });
    fireEvent.change(screen.getByLabelText("Số điện thoại"), { target: { value: "0907248361" } });
    expect(screen.queryByLabelText("Mã HTX")).toBeNull();
    expect(screen.queryByLabelText(/Diện tích/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Lưu hồ sơ" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body).toEqual({ name: "HTX mới", commune_code: "X-NEW", head_office: "Ấp 1", representative_name: "", phone: "0907248361" });
    await waitFor(() => expect(navigation.replace).toHaveBeenCalledWith(`/htx/${REF}`));
  });

  it("giữ mã và địa giới đang lưu không còn trong ô chọn; lỗi giữa chừng thử lại cùng body/key", async () => {
    const { writes } = setup("setter", { edit: true, failure: true });
    expect(await screen.findByLabelText("Mã HTX")).toHaveValue(profile.code);
    expect(screen.getByLabelText("Mã HTX")).toHaveAttribute("readonly");
    expect(await screen.findByRole("option", { name: "Xã đang lưu (đang lưu)" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu hồ sơ" })).not.toBeDisabled());
    fireEvent.change(screen.getByLabelText("Tên HTX"), { target: { value: "HTX đã sửa" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu hồ sơ" }));
    await screen.findByRole("button", { name: "Thử lưu lại" });
    expect(screen.getByLabelText("Tên HTX")).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Thử lưu lại" }));
    await waitFor(() => expect(writes).toHaveLength(2));
    expect(writes[1]!.key).toBe(writes[0]!.key);
    expect(writes[1]!.body).toEqual(writes[0]!.body);
    expect(writes[1]!.body).toMatchObject({ name: "HTX đã sửa", expected_revision: 42, commune_code: "X-OLD" });
    expect(writes[1]!.body).not.toHaveProperty("code");
  });

  it("tổng hộ do server trả, không lấy diện tích đăng ký KN thay thế", async () => {
    setup("setter", { overview: true });
    await screen.findByRole("region", { name: "Hồ sơ HTX" });
    expect(screen.getByText("1,5")).toBeInTheDocument();
    expect(screen.getByText("0,5")).toBeInTheDocument();
    expect(screen.queryByText("999")).toBeNull();
  });

  it("chưa có nguồn máy hiện dấu gạch, không hiện số 0 hay năm loại máy giả", async () => {
    setup("setter", { overview: true, machines: true });
    const region = await screen.findByRole("region", { name: "Máy móc hiện có" });
    expect(region).toHaveTextContent("Tổng số máy: —");
    expect(region).toHaveTextContent("Chưa có dữ liệu");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("hồ sơ cũ giữ thông tin tổ chức và yêu cầu bổ sung liên hệ HTX riêng", async () => {
    setup("setter", { edit: true, data: { ...profile, contact_configured: false, phone: "" } });
    expect(await screen.findByText("Hồ sơ cũ chưa có số điện thoại liên hệ HTX riêng. Hãy bổ sung trước khi lưu.")).toBeInTheDocument();
    expect(screen.getByLabelText("Tên HTX")).toHaveValue(profile.name);
    expect(screen.getByLabelText("Số điện thoại")).toHaveValue("");
    expect(screen.getByLabelText("Số điện thoại")).toBeRequired();
  });
});
