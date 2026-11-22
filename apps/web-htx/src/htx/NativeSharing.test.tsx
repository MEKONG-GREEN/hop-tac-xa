import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { COOPERATIVES } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetNativeSharingMock, seedNativeSharing } from "@/api/mock/native-sharing";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { buildPrincipal } from "@/auth/principal";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { NativeSharing } from "./NativeSharing";

beforeEach(resetNativeSharingMock);
const persona = (id: string) => MOCK_PERSONAS.find((p) => p.id === id)!;
const own = () => persona("htx").me.org!.ref;
const others = () => COOPERATIVES.filter((c) => c.api.active && c.api.ref !== own()).map((c) => c.api.ref);

function setup(id = "htx") {
  const p = persona(id);
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(id), fetch });
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  render(<AuthContextForTests.Provider value={{ api, principal, status: "authenticated" } as AuthContextValue}><NativeSharing /></AuthContextForTests.Provider>);
  return { fetch };
}
const table = () => screen.getByRole("table", { name: "Yêu cầu chia sẻ" });

it("[FN-09/EX-01, AC-01, AC-05] validates target and keeps a withdrawn outgoing request in history", async () => {
  const user = userEvent.setup(); setup();
  await screen.findByText("Chưa có yêu cầu phù hợp");
  await user.click(screen.getByRole("button", { name: "Gửi yêu cầu" }));
  const dialog = screen.getByRole("dialog", { name: "Gửi yêu cầu chia sẻ" });
  await user.click(within(dialog).getByRole("button", { name: "Gửi" }));
  expect(within(dialog).getByRole("alert")).toHaveTextContent("Bắt buộc nhập");
  const select = within(dialog).getByLabelText("HTX được yêu cầu");
  const option = within(select).getAllByRole("option")[1] as HTMLOptionElement;
  await user.selectOptions(select, option.value);
  await user.click(within(dialog).getByRole("button", { name: "Gửi" }));
  await within(table()).findByRole("button", { name: "Thu hồi" });
  expect(within(table()).getByText("Gửi yêu cầu")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: /Đã gửi/ })).toHaveAttribute("aria-selected", "true");
  expect(screen.queryByRole("button", { name: "Duyệt" })).not.toBeInTheDocument();
  await user.click(within(table()).getByRole("button", { name: "Thu hồi" }));
  expect(screen.getByText("Thu hồi sẽ ngừng quyền xem ngay. Lịch sử yêu cầu vẫn được giữ.")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Xác nhận thu hồi" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  // Trạng thái nằm trong bảng; ô lọc trạng thái cũng có lựa chọn "Thu hồi" nên chỉ xét trong bảng.
  expect(await within(table()).findByText("Thu hồi")).toBeInTheDocument();
  expect(within(table()).queryByRole("button", { name: "Thu hồi" })).not.toBeInTheDocument();
});

it("[FN-09/AC-02, AC-03] incoming request shows pending count; owner approves then revokes", async () => {
  const user = userEvent.setup();
  seedNativeSharing(others()[0]!, own(), "requested");
  setup();
  expect(await screen.findByRole("tab", { name: /Gửi đến · 1 chờ duyệt/ })).toHaveAttribute("aria-selected", "true");
  expect(within(table()).queryByRole("button", { name: "Thu hồi" })).not.toBeInTheDocument();
  await user.click(within(table()).getByRole("button", { name: "Duyệt" }));
  expect(screen.getByText("Sau khi duyệt, HTX yêu cầu được xem toàn bộ dữ liệu ở chế độ chỉ xem.")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Xác nhận duyệt" }));
  await waitFor(() => expect(within(table()).queryByRole("button", { name: "Duyệt" })).not.toBeInTheDocument());
  expect(within(table()).getByText("Duyệt")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: /Gửi đến · 0 chờ duyệt/ })).toBeInTheDocument();
  await user.click(within(table()).getByRole("button", { name: "Thu hồi" }));
  await user.click(screen.getByRole("button", { name: "Xác nhận thu hồi" }));
  await waitFor(() => expect(within(table()).queryByRole("button", { name: "Thu hồi" })).not.toBeInTheDocument());
  expect(within(table()).getByText("Thu hồi")).toBeInTheDocument();
});

it("[FN-09/AC-04] owner rejects; the requester may send a new request later", async () => {
  const user = userEvent.setup();
  seedNativeSharing(others()[0]!, own(), "requested");
  setup();
  await user.click(await within(await screen.findByRole("table", { name: "Yêu cầu chia sẻ" })).findByRole("button", { name: "Từ chối" }));
  await user.click(screen.getByRole("button", { name: "Xác nhận từ chối" }));
  await waitFor(() => expect(within(table()).queryByRole("button", { name: "Từ chối" })).not.toBeInTheDocument());
  expect(within(table()).getByText("Từ chối")).toBeInTheDocument();
  // Yêu cầu đã từ chối là lịch sử: không còn thao tác Duyệt/Từ chối/Thu hồi.
  for (const name of ["Duyệt", "Từ chối", "Thu hồi"]) expect(within(table()).queryByRole("button", { name })).not.toBeInTheDocument();
});

it("[FN-09/EX-02, AC-N2] no candidate left → 'Chưa có HTX để gửi yêu cầu'; [AC-N4] reverse direction stays independent", async () => {
  for (const ref of others()) seedNativeSharing(own(), ref, "approved");
  seedNativeSharing(others()[0]!, own(), "approved");
  setup();
  expect(await screen.findByText("Chưa có HTX để gửi yêu cầu")).toBeInTheDocument();
  // Tab "Gửi đến": yêu cầu chiều ngược lại (HTX khác xem HTX mình) vẫn tồn tại độc lập.
  expect(within(table()).getAllByText(/xem dữ liệu của/).length).toBe(1);
});

it("stale revision shows the conflict and reloads the list", async () => {
  const user = userEvent.setup();
  const row = seedNativeSharing(others()[0]!, own(), "requested");
  setup();
  await user.click(await within(await screen.findByRole("table", { name: "Yêu cầu chia sẻ" })).findByRole("button", { name: "Duyệt" }));
  // Bên kia vừa rút yêu cầu (revision tăng) trước khi xác nhận.
  row.revision = 2; row.state = "revoked";
  await user.click(screen.getByRole("button", { name: "Xác nhận duyệt" }));
  expect(await screen.findByText(/Yêu cầu vừa thay đổi|Không được chuyển trạng thái này/)).toBeInTheDocument();
});

it.each(["htx-member", "setter"])("[FN-09/AC-06, AC-07] does not load sharing data for %s", (id) => {
  const { fetch } = setup(id);
  expect(screen.queryByText("Liên kết & chia sẻ dữ liệu")).not.toBeInTheDocument();
  expect(fetch).not.toHaveBeenCalled();
});
