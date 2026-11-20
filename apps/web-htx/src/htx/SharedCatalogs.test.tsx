import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { buildPrincipal } from "@/auth/principal";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { SharedCatalogs } from "./SharedCatalogs";

beforeEach(() => {
  vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: vi.fn(() => "blob:x"), revokeObjectURL: vi.fn() }));
});
afterEach(() => vi.unstubAllGlobals());

function setup(id: string) {
  const p = MOCK_PERSONAS.find((x) => x.id === id)!;
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(id), fetch });
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  render(<AuthContextForTests.Provider value={{ api, principal, status: "authenticated" } as AuthContextValue}><SharedCatalogs /></AuthContextForTests.Provider>);
  return { fetch };
}

it.each(["htx", "htx-member"])("[VTKM-Q-07] %s xem danh mục Vật tư/Máy móc chỉ đọc và xuất Excel", async (id) => {
  const user = userEvent.setup();
  const { fetch } = setup(id);
  const table = await screen.findByRole("table", { name: "Danh mục Vật tư" });
  expect(within(table).getByText("Phân urê")).toBeInTheDocument();
  for (const name of ["Thêm", "Sửa", "Xóa", "Ẩn"]) expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Xuất Excel" }));
  const urls = fetch.mock.calls.map(([r]) => (r instanceof Request ? r.url : String(r)));
  expect(urls.some((u) => u.endsWith("/v1/masterdata/shared-catalogs/material/export"))).toBe(true);
  await user.click(screen.getByRole("tab", { name: "Máy móc" }));
  expect(await screen.findByRole("table", { name: "Danh mục Máy móc" })).toBeInTheDocument();
});

it("vai ngoài App HTX không tải danh mục", () => {
  const { fetch } = setup("xa");
  expect(screen.queryByText("Danh mục vật tư, máy móc")).not.toBeInTheDocument();
  expect(fetch).not.toHaveBeenCalled();
});
