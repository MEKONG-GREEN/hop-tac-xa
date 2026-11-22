import { render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { COOPERATIVES } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetNativeSharingMock, seedNativeSharing } from "@/api/mock/native-sharing";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { buildPrincipal } from "@/auth/principal";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { CoopSharingTab, sharingTabAudience } from "./CoopSharingTab";

beforeEach(resetNativeSharingMock);
const persona = (id: string) => MOCK_PERSONAS.find((p) => p.id === id)!;
const principalOf = (id: string) => {
  const p = persona(id);
  return buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
};
const own = () => persona("htx").me.org!.ref;
const other = () => COOPERATIVES.find((c) => c.api.active && c.api.ref !== own())!.api.ref;

function setup(id: string, coopRef: string, manage: boolean) {
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(id), fetch });
  render(<AuthContextForTests.Provider value={{ api, principal: principalOf(id), status: "authenticated" } as AuthContextValue}><CoopSharingTab coopRef={coopRef} manage={manage} /></AuthContextForTests.Provider>);
}

it("[QTPQ-Q-55, AC-N1] tab audience: Representative own HTX manages, Admin views any HTX, Member and shared HTX have none", () => {
  expect(sharingTabAudience(principalOf("htx"), own())).toBe("manage");
  expect(sharingTabAudience(principalOf("htx"), other())).toBeNull();
  expect(sharingTabAudience(principalOf("htx-member"), own())).toBeNull();
  expect(sharingTabAudience(principalOf("setter"), other())).toBe("view");
});

it("[FN-03, QTPQ-Q-55] Representative sees own relations and the link to Liên kết & chia sẻ dữ liệu", async () => {
  seedNativeSharing(own(), other(), "approved");
  setup("htx", own(), true);
  const table = await screen.findByRole("table", { name: "Liên kết chia sẻ dữ liệu" });
  expect(within(table).getByText("Duyệt")).toBeInTheDocument();
  expect(within(table).getByText("HTX này")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Liên kết & chia sẻ dữ liệu" })).toHaveAttribute("href", "/htx/chia-se");
});

it("[AC-N2] Admin sees another HTX's relations read-only, without actions", async () => {
  seedNativeSharing(own(), other(), "requested");
  setup("setter", other(), false);
  const table = await screen.findByRole("table", { name: "Liên kết chia sẻ dữ liệu" });
  expect(within(table).getByText("Gửi yêu cầu")).toBeInTheDocument();
  expect(screen.getByText(/Chỉ xem/)).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Liên kết & chia sẻ dữ liệu" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

it("shows the empty state when the HTX has no relations", async () => {
  setup("htx", own(), true);
  expect(await screen.findByText("Chưa có liên kết chia sẻ dữ liệu.")).toBeInTheDocument();
});
