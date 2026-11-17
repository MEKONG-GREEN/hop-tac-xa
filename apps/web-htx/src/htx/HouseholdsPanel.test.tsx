import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdsPanel } from "./HouseholdsPanel";

function renderAs(personaId: string) {
  const p = MOCK_PERSONAS.find((x) => x.id === personaId);
  if (!p) throw new Error(personaId);
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(personaId), fetch: mockFetchNoDelay });
  const value = { status: "authenticated", principal, api } as unknown as AuthContextValue;
  return render(
    <AuthContextForTests.Provider value={value}>
      <HouseholdsPanel coop={htxCoop().api} />
    </AuthContextForTests.Provider>,
  );
}

describe("HouseholdsPanel: nút theo ma trận vai (FN-05)", () => {
  it("[FN-05/AC-01] Thành viên HTX không có nút thêm/sửa/xóa nông hộ", async () => {
    renderAs("htx-member");
    await waitFor(() => expect(screen.getAllByRole("row").length).toBeGreaterThan(1));
    expect(screen.queryByRole("button", { name: /Sao chép hàng loạt/ })).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Chọn" })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.queryByRole("button", { name: /Gọi nhanh/ })).toBeNull();
  });

  it("đối chứng: Đại diện HTX (coop_manager) có nút sao chép, cột Chọn và nút Gọi", async () => {
    renderAs("htx");
    await waitFor(() => expect(screen.getAllByRole("row").length).toBeGreaterThan(1));
    expect(screen.getByRole("button", { name: /Sao chép hàng loạt/ })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "Chọn" })).toBeTruthy();
    expect(screen.getAllByRole("button", { name: /Gọi nhanh/ }).length).toBeGreaterThan(0);
  });
});
