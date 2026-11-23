import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdsPanel } from "./HouseholdsPanel";

function renderAs(personaId: string, readOnly = false) {
  const p = MOCK_PERSONAS.find((x) => x.id === personaId);
  if (!p) throw new Error(personaId);
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(personaId), fetch: mockFetchNoDelay });
  const value = { status: "authenticated", principal, api } as unknown as AuthContextValue;
  return render(
    <AuthContextForTests.Provider value={value}>
      <HouseholdsPanel coop={htxCoop().api} readOnly={readOnly} />
    </AuthContextForTests.Provider>,
  );
}

describe("HouseholdsPanel: nút theo ma trận vai (FN-05)", () => {
  it("[AH-50] danh sách diện tích chỉ đọc cho Đại diện HTX, vẫn hiện hộ hết diện tích", async () => {
    renderAs("htx", true);
    await waitFor(() => expect(screen.getAllByRole("row").length).toBeGreaterThan(1));
    for (const name of ["Diện tích đất (ha)", "Đang canh tác (ha)", "Còn lại (ha)"]) {
      expect(screen.getByRole("columnheader", { name })).toBeTruthy();
    }
    expect(screen.queryByRole("button", { name: /Sao chép hàng loạt/ })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getAllByText("Hết diện tích để mở vụ.").length).toBeGreaterThan(0);
  });
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

describe("HouseholdsPanel: mở vụ đồng loạt (AH-68)", () => {
  function renderSeasons(personaId: string) {
    const p = MOCK_PERSONAS.find((x) => x.id === personaId)!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(personaId), fetch: mockFetchNoDelay });
    return render(
      <AuthContextForTests.Provider value={{ status: "authenticated", principal, api } as unknown as AuthContextValue}>
        <HouseholdsPanel coop={htxCoop().api} readOnly showSeasons />
      </AuthContextForTests.Provider>,
    );
  }

  it("Thành viên HTX chọn được hộ còn diện tích; hộ hết diện tích không chọn được; Chọn tất cả bỏ qua hộ đó", async () => {
    renderSeasons("htx-member");
    await waitFor(() => expect(screen.getAllByRole("row").length).toBeGreaterThan(1));
    const button = screen.getByRole("button", { name: "Mở vụ đồng loạt (0 hộ)" });
    expect(button).toBeDisabled();
    const boxes = screen.getAllByRole("checkbox", { name: /để mở vụ$/ });
    expect(boxes.some((b) => (b as HTMLInputElement).disabled)).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn tất cả" }));
    const enabled = boxes.filter((b) => !(b as HTMLInputElement).disabled);
    expect(enabled.length).toBeGreaterThan(0);
    for (const b of enabled) expect(b).toBeChecked();
    for (const b of boxes.filter((b) => (b as HTMLInputElement).disabled)) expect(b).not.toBeChecked();
    expect(screen.getByRole("button", { name: `Mở vụ đồng loạt (${enabled.length} hộ)` })).not.toBeDisabled();
  });

  it("AC5: Đại diện HTX không có chức năng mở vụ đồng loạt", async () => {
    renderSeasons("htx");
    await waitFor(() => expect(screen.getAllByRole("row").length).toBeGreaterThan(1));
    expect(screen.queryByRole("button", { name: /Mở vụ đồng loạt/ })).toBeNull();
    expect(screen.queryByRole("checkbox", { name: "Chọn tất cả" })).toBeNull();
  });
});
