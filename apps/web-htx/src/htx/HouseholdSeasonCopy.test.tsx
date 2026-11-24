import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import type { HouseholdListItem } from "@/api/types";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdSeasonsDialog } from "./HouseholdSeasonsDialog";

const previous = { season_ref: "old", season_code: "DX", season_year: 2026, crop_code: "LUA", variety_code: "OM5451", area_ha: "0.50", status: "open",
  current_event_id: "e", anchor_date: "2026-09-29", gps_missing: true,
  sop_snapshot: { name: "Quy trình lúa", version_no: 2, version_id: "v-old", anchor_name: "Ngày xuống giống", steps: [], stages: [], custom: { added: [{ seq: 6, name: "Xử lý giống" }] } } };

function setup(persona: string) {
  const p = MOCK_PERSONAS.find((x) => x.id === persona)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const posts: Record<string, unknown>[] = [];
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
    const req = input as Request; const path = new URL(req.url).pathname;
    if (req.method === "POST") { const body = await req.json() as Record<string, unknown>; posts.push(body); return Response.json({ season: { ...previous, ...body, season_ref: "new", copied_from: "old" } }, { status: 201 }); }
    if (path.endsWith("/households/hh/seasons")) return Response.json({ items: [previous], next_cursor: null });
    if (path.endsWith("/checklist")) return Response.json({ season_ref: "new", current_event_id: "e", records: [], progress: { as_of_date: "2026-11-24", done: 0, skipped: 0, total: 0, percent: null, stages: [] } });
    if (path.endsWith("/plan")) return Response.json({ as_of_date: "2026-11-24", stages: [] });
    if (path.endsWith("/plan-history")) return Response.json({ items: [] });
    return Response.json({ items: [], next_cursor: null });
  } });
  const household = { household_ref: "hh", label: "Hộ A", remaining_area_ha: "1.50", can_open_season: true } as HouseholdListItem;
  render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
    <HouseholdSeasonsDialog coopRef="coop" household={household} onClose={() => {}} onSaved={() => {}} />
  </AuthContextForTests.Provider>);
  return posts;
}

describe("AH-139 sao chép vụ trước trên Web", () => {
  it("chỉ nhập Vụ/Năm, mốc gốc, diện tích; gửi vụ nguồn kèm giống, quy trình của vụ nguồn", async () => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: undefined });
    const posts = setup("htx-member");
    fireEvent.click(await screen.findByRole("button", { name: "Sao chép vụ này" }));
    expect(screen.getByRole("region", { name: "Sao chép từ vụ trước" })).toHaveTextContent("giống OM5451");
    expect(screen.getByRole("region", { name: "Sao chép từ vụ trước" })).toHaveTextContent("kèm tùy chỉnh của vụ");
    expect(screen.queryByLabelText("Giống")).toBeNull();
    fireEvent.change(screen.getByLabelText("Mùa vụ"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Năm"), { target: { value: "2027" } });
    fireEvent.change(screen.getByLabelText("Diện tích vụ (ha)"), { target: { value: "0.50" } });
    fireEvent.change(screen.getByLabelText("Ngày xuống giống"), { target: { value: "2027-01-10" } });
    const form = screen.getByRole("button", { name: "Lưu vụ" }).closest("form")!;
    // Mùa vụ là danh mục; test chọn trực tiếp giá trị cho form hợp lệ.
    const season = screen.getByLabelText("Mùa vụ") as HTMLSelectElement;
    season.innerHTML = '<option value="HT">Hè thu</option>'; fireEvent.change(season, { target: { value: "HT" } });
    fireEvent.submit(form);
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0]).toMatchObject({ copy_from_season_ref: "old", crop_code: "LUA", variety_code: "OM5451", sop_version_id: "v-old", season_code: "HT", season_year: 2027, area_ha: "0.50", anchor_date: "2027-01-10" });
  });

  it("Đại diện HTX không có nút sao chép", async () => {
    setup("htx");
    await screen.findByText(/DX 2026/);
    expect(screen.queryByRole("button", { name: "Sao chép vụ này" })).toBeNull();
  });
});
