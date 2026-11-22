import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { COOPERATIVES, householdRefOf, syntheticPhone } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetNativeSharingMock, seedNativeSharing } from "@/api/mock/native-sharing";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { buildPrincipal } from "@/auth/principal";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { HouseholdProfileButton } from "./HouseholdProfileButton";

// AH-286 (FN-04/AC-04, QTPQ-Q-61 AC-N1, AC-N2): hộ của HTX được chia sẻ chỉ xem, SĐT/CCCD che ở máy chủ.
beforeEach(resetNativeSharingMock);
const persona = MOCK_PERSONAS.find((p) => p.id === "htx")!;
const ownCoop = () => COOPERATIVES.find((c) => c.api.ref === persona.me.org!.ref)!;
const otherCoop = () => COOPERATIVES.find((c) => c.api.active && c.api.ref !== persona.me.org!.ref)!;

function setup(householdRef: string, shared: boolean) {
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("htx"), fetch });
  const principal = buildPrincipal({ sub: persona.me.actor_ref, name: persona.displayName, amr: persona.amr, realm_access: { roles: persona.me.roles } }, persona.me);
  render(<AuthContextForTests.Provider value={{ api, principal, status: "authenticated" } as AuthContextValue}><HouseholdProfileButton householdRef={householdRef} shared={shared} /></AuthContextForTests.Provider>);
}

it("[AC-04, AC-N1] Representative sees a shared HTX household masked and read-only", async () => {
  const coop = otherCoop();
  seedNativeSharing(persona.me.org!.ref, coop.api.ref, "approved");
  const user = userEvent.setup();
  setup(householdRefOf(coop, 0), true);
  await user.click(screen.getByRole("button", { name: "Xem chi tiết" }));
  const dialog = await screen.findByRole("dialog", { name: "Chi tiết hộ xã viên" });
  await within(dialog).findByText("Hộ của HTX được chia sẻ, chỉ xem. SĐT và CCCD được che một phần.");
  const phone = syntheticPhone(coop, 0);
  expect(within(dialog).getByText(`${phone.slice(0, 4)}***${phone.slice(-3)}`)).toBeInTheDocument();
  expect(within(dialog).queryByText(phone)).not.toBeInTheDocument();
  expect(within(dialog).getByText(/^079\*{6}\d{3}$/)).toBeInTheDocument();
  expect(within(dialog).queryByRole("button", { name: "Sửa hộ" })).not.toBeInTheDocument();
  expect(within(dialog).queryByRole("button", { name: "Xóa hộ" })).not.toBeInTheDocument();
});

it("[AC-04] after revocation the shared household is no longer readable", async () => {
  const coop = otherCoop();
  const user = userEvent.setup();
  setup(householdRefOf(coop, 0), true);
  await user.click(screen.getByRole("button", { name: "Xem chi tiết" }));
  const dialog = await screen.findByRole("dialog", { name: "Chi tiết hộ xã viên" });
  expect(await within(dialog).findByRole("alert")).toBeInTheDocument();
  expect(within(dialog).queryByText(syntheticPhone(coop, 0))).not.toBeInTheDocument();
});

it("[AC-N2] Representative sees own HTX household in full", async () => {
  const coop = ownCoop();
  const user = userEvent.setup();
  setup(householdRefOf(coop, 0), false);
  await user.click(screen.getByRole("button", { name: "Xem chi tiết" }));
  const dialog = await screen.findByRole("dialog", { name: "Chi tiết hộ xã viên" });
  expect(await within(dialog).findByText(syntheticPhone(coop, 0))).toBeInTheDocument();
  expect(within(dialog).getByRole("button", { name: "Sửa hộ" })).toBeInTheDocument();
});
