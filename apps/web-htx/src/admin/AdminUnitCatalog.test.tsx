import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { AdminUnitCatalog } from "./AdminUnitCatalog";
import type { ManagedUnit } from "./adminUnits";

const province: ManagedUnit = { code: "P-TEST", name: "Tỉnh thử", level: "province", parent_code: "", valid_from: "2026-11-19", active: true, selectable: true, revision: 1, commune_count: 1, cooperative_count: null };
const commune: ManagedUnit = { ...province, code: "X-TEST", name: "Xã thử", level: "commune", parent_code: province.code, revision: 42, commune_count: 0 };

function setup(persona = "setter", failOnce = false) {
  const p = MOCK_PERSONAS.find((p) => p.id === persona)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const writes: { path: string; key: string | null; body: Record<string, unknown> }[] = [];
  const gets: string[] = [];
  let items = [{ ...province }, { ...commune }];
  const fetch = async (req: Request) => {
    const url = new URL(req.url);
    if (req.method === "POST") {
      const body = await req.json() as Record<string, unknown>;
      writes.push({ path: url.pathname, key: req.headers.get("Idempotency-Key"), body });
      if (failOnce && writes.length === 1) throw new TypeError("offline");
      const current = items.find((u) => url.pathname.endsWith(`/${u.code}/revisions`));
      const result: ManagedUnit = current ? { ...current, name: String(body.name), active: Boolean(body.active), revision: current.revision + 1 } : { ...province, code: "P-NEW", name: String(body.name), level: body.level as ManagedUnit["level"], parent_code: String(body.parent_code), active: Boolean(body.active) };
      if (current) items = items.map((u) => u.code === current.code ? result : u); else items.push(result);
      return Response.json(result, { status: current ? 200 : 201 });
    }
    gets.push(url.pathname);
    const parent = url.searchParams.get("parent") ?? "";
    return Response.json({ items: items.filter((u) => u.parent_code === parent), next_cursor: null });
  };
  render(<AuthContextForTests.Provider value={{ principal, status: "authenticated", adminApi: createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => "test", fetch }), api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch }) } as AuthContextValue}><AdminUnitCatalog /></AuthContextForTests.Provider>);
  return { writes, gets };
}

describe("AH-271: danh mục đơn vị hành chính", () => {
  it.each(["htx", "htx-member"])("%s không có quản trị và không đọc Admin API", (persona) => {
    const { gets, writes } = setup(persona);
    expect(screen.getByRole("alert")).toHaveTextContent("Chỉ Admin");
    expect(gets).toEqual([]); expect(writes).toEqual([]);
  });
  it("mã tự sinh, giữ dữ liệu và key khi mất mạng", async () => {
    const { writes } = setup("setter", true);
    fireEvent.click(await screen.findByRole("button", { name: "Thêm tỉnh / thành phố" }));
    expect(screen.queryByLabelText("Mã")).toBeNull();
    fireEvent.change(screen.getByLabelText("Tên"), { target: { value: "Tỉnh mới" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu" })).not.toBeDisabled());
    expect(screen.getByLabelText("Tên")).toHaveValue("Tỉnh mới");
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(2));
    expect(writes[0]!.key).toBeTruthy(); expect(writes[1]!.key).toBe(writes[0]!.key);
    expect(writes[1]!.body).toEqual({ name: "Tỉnh mới", active: true, level: "province", parent_code: "" });
    await screen.findByRole("button", { name: "Tỉnh mới" });
  });
  it("chọn tỉnh ra xã, không giả số HTX là 0, ngừng dùng gửi phiên bản và không xóa", async () => {
    const { writes } = setup();
    fireEvent.click(await screen.findByRole("button", { name: "Tỉnh thử" }));
    await screen.findByRole("button", { name: "Xã thử" });
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Xóa" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Sửa" }));
    expect(screen.getByText("Mã: X-TEST")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("checkbox", { name: "Đang sử dụng" }));
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.path).toBe("/v1/masterdata/admin-unit-profiles/X-TEST/revisions");
    expect(writes[0]!.body).toEqual({ name: "Xã thử", active: false, expected_revision: 42 });
    await screen.findByRole("tab", { name: /Ngừng sử dụng/ });
  });
  it("thêm xã được gắn đúng tỉnh đang chọn", async () => {
    const { writes } = setup();
    fireEvent.click(await screen.findByRole("button", { name: "Tỉnh thử" }));
    fireEvent.click(await screen.findByRole("button", { name: "Thêm xã / phường" }));
    fireEvent.change(screen.getByLabelText("Tên"), { target: { value: "Phường mới" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body).toEqual({ name: "Phường mới", active: true, level: "commune", parent_code: province.code });
  });
});
