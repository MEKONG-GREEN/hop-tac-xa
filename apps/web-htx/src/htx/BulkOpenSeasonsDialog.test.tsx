import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import type { HouseholdListItem } from "@/api/types";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { BulkOpenSeasonsDialog } from "./BulkOpenSeasonsDialog";

const hh = (ref: string, label: string, remaining: string) =>
  ({ household_ref: ref, label, remaining_area_ha: remaining, can_open_season: true, active: true }) as HouseholdListItem;

function setup() {
  const calls: { household: string; key: string | null; body: Record<string, unknown> }[] = [];
  const onDone = vi.fn();
  const p = MOCK_PERSONAS.find((p) => p.id === "htx-member")!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async (input) => {
    const req = input as Request;
    const path = new URL(req.url).pathname;
    if (req.method === "POST") {
      const body = await req.json() as Record<string, unknown>;
      const household = path.split("/")[5]!;
      calls.push({ household, key: req.headers.get("Idempotency-Key"), body });
      // Hộ B: máy chủ chỉ còn 0.30 ha (đã có vụ khác mở trong lúc chọn).
      if (household === "b" && body.area_ha !== "0.30") {
        return Response.json({ type: "", title: "Diện tích vượt", status: 409, code: "cropcycle.household_area_exceeded", remaining_area_ha: "0.30" }, { status: 409 });
      }
      return Response.json({ season: { ...body, season_ref: `s-${household}`, status: "open" } }, { status: 201 });
    }
    if (path.includes("masterdata/catalogs/")) {
      const name = path.split("/").at(-1);
      const items = name === "crop" ? [{ code: "LUA", name: "Lúa", attrs: {} }] : name === "rice_variety" ? [{ code: "OM5451", name: "OM5451", attrs: { crop_code: "LUA" } }] : [{ code: "DX", name: "Đông xuân", attrs: {} }];
      return Response.json({ items, next_cursor: null });
    }
    if (path === "/v1/cropcycle/sops") return Response.json({ items: [{ ref: "sop", name: "Quy trình lúa", crop: "LUA", scope: "system", status: "published", published_version: 1, anchor_name: "Ngày xuống giống" }], next_cursor: null });
    if (path === "/v1/cropcycle/sops/sop") return Response.json({ versions: [{ id: "version", version_no: 1, state: "active" }] });
    return Response.json({ items: [], next_cursor: null });
  } });
  render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
    <BulkOpenSeasonsDialog coopRef="coop" households={[hh("a", "Hộ A", "1.00"), hh("b", "Hộ B", "0.50"), hh("c", "Hộ C", "2.00")]} onClose={() => {}} onDone={onDone} />
  </AuthContextForTests.Provider>);
  return { calls, onDone };
}

async function fillCommon() {
  await screen.findByRole("option", { name: "Lúa" });
  fireEvent.change(screen.getByLabelText("Loại cây"), { target: { value: "LUA" } });
  fireEvent.change(screen.getByLabelText("Giống"), { target: { value: "OM5451" } });
  fireEvent.change(screen.getByLabelText("Mùa vụ"), { target: { value: "DX" } });
  await screen.findByRole("option", { name: /Quy trình lúa/ });
  fireEvent.change(screen.getByLabelText("Ngày xuống giống"), { target: { value: "2026-12-01" } });
}

describe("AH-68 mở vụ đồng loạt trên Web", () => {
  it("AC1–AC3: mỗi hộ một yêu cầu riêng; diện tích mặc định = còn lại; hộ vượt diện tích bị bỏ qua kèm lý do rồi gửi lại được", async () => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: undefined });
    const { calls, onDone } = setup();
    // AC2: diện tích mặc định bằng diện tích còn lại.
    expect(screen.getByLabelText("Diện tích vụ của Hộ A")).toHaveValue("1.00");
    expect(screen.getByLabelText("Diện tích vụ của Hộ C")).toHaveValue("2.00");
    await fillCommon();
    fireEvent.change(screen.getByLabelText("Diện tích vụ của Hộ C"), { target: { value: "1.5" } });
    fireEvent.click(screen.getByRole("button", { name: "Mở vụ đồng loạt (3 hộ)" }));
    await waitFor(() => expect(calls).toHaveLength(3));
    expect(calls.map((c) => c.household)).toEqual(["a", "b", "c"]);
    expect(new Set(calls.map((c) => c.key)).size).toBe(3);
    for (const c of calls) expect(c.body).toMatchObject({ crop_code: "LUA", variety_code: "OM5451", season_code: "DX", anchor_date: "2026-12-01", sop_version_id: "version", channel: "web", gps: { source: "no_device" } });
    expect(calls[2]!.body.area_ha).toBe("1.5");
    // AC3: hộ B bị bỏ qua, có lý do; hộ A, C đã mở.
    expect(await screen.findByText(/Bỏ qua: Vượt diện tích còn lại/)).toBeTruthy();
    expect(screen.getAllByText("Đã mở vụ")).toHaveLength(2);
    expect(screen.getByRole("status")).toHaveTextContent("Đã mở 2/3 vụ. 1 hộ bị bỏ qua");
    expect(onDone).toHaveBeenCalled();
    // Sửa diện tích hộ B → khóa mới, chỉ gửi lại hộ B.
    const firstKey = calls[1]!.key;
    fireEvent.change(screen.getByLabelText("Diện tích vụ của Hộ B"), { target: { value: "0.30" } });
    fireEvent.click(screen.getByRole("button", { name: "Mở vụ đồng loạt (1 hộ)" }));
    await waitFor(() => expect(calls).toHaveLength(4));
    expect(calls[3]!.household).toBe("b");
    expect(calls[3]!.key).not.toBe(firstKey);
    await waitFor(() => expect(screen.getAllByText("Đã mở vụ")).toHaveLength(3));
    expect(screen.queryByRole("button", { name: /Mở vụ đồng loạt/ })).toBeNull();
  });

  it("diện tích sai định dạng bị chặn ở hộ đó, không gửi; các hộ khác vẫn mở", async () => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: undefined });
    const { calls } = setup();
    await fillCommon();
    fireEvent.change(screen.getByLabelText("Diện tích vụ của Hộ A"), { target: { value: "0,5" } });
    fireEvent.click(screen.getByRole("button", { name: "Mở vụ đồng loạt (3 hộ)" }));
    await screen.findByText(/Bỏ qua: Diện tích phải lớn hơn 0/);
    await waitFor(() => expect(calls.map((c) => c.household)).toEqual(["b", "c"]));
  });
});
