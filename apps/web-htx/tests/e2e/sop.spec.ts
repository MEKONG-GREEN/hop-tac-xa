import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const PATH = "/du-lieu-nen/quy-trinh-sop";

test("AH-8 AC1, AC2, AC4, AC5: soạn nháp, lỗi theo trường, chặn phát hành, phát hành một bước bởi chính người soạn", async ({ page }) => {
  await loginAs(page, "setter", PATH);
  await page.getByRole("button", { name: /Soạn SOP/ }).click();
  await page.getByLabel(/Loại cây/).selectOption("LUA");
  await page.getByLabel(/Tên SOP/).fill("Quy trình thử nghiệm E2E");
  await page.getByLabel(/Tên mốc gốc/).fill("Ngày xuống giống");
  await page.getByLabel("Tên giai đoạn 1").fill("Chuẩn bị");
  // Từ > Đến → báo lỗi đúng ô, chưa gửi.
  await page.getByLabel("Từ (ngày) giai đoạn 1").fill("9");
  await page.getByLabel("Đến (ngày) giai đoạn 1").fill("2");
  await page.getByRole("button", { name: "Lưu nháp" }).click();
  await expect(page.getByLabel("Đến (ngày) giai đoạn 1")).toHaveAttribute("aria-invalid", "true");
  await page.getByLabel("Từ (ngày) giai đoạn 1").fill("-3");
  await page.getByLabel("Đến (ngày) giai đoạn 1").fill("100");
  await page.getByRole("button", { name: "+ Thêm việc vào giai đoạn 1" }).click();
  await page.getByLabel("Nội dung việc 1.1").fill("Thu hoạch lúa");
  await page.getByLabel("Loại việc 1.1").selectOption("thu_hoach");
  await page.getByRole("button", { name: "Lưu nháp" }).click();
  await expect(page.getByRole("status")).toContainText("Đã lưu nháp");
  await expect(page.getByText("Nháp", { exact: true })).toBeVisible();

  // Thu hoạch cuối để "Không ngày" → không phát hành được.
  await page.getByRole("button", { name: "Phát hành" }).click();
  await expect(page.getByText("Chưa phát hành được")).toBeVisible();
  await page.getByLabel("Cửa sổ việc 1.1").selectOption("date_range");
  await page.getByLabel("Từ (ngày) việc 1.1").fill("95");
  await page.getByLabel("Đến (ngày) việc 1.1").fill("100");
  await page.getByRole("button", { name: "Lưu nháp" }).click();
  await expect(page.getByRole("button", { name: "Phát hành" })).toBeEnabled();
  // Người soạn tự phát hành (một bước, không cần người thứ hai).
  await page.getByRole("button", { name: "Phát hành" }).click();
  await expect(page.getByRole("status")).toContainText("Đã phát hành");
  await expect(page.getByText("Đang áp dụng").first()).toBeVisible();
});

test("AH-8 AC6: người phê duyệt tham số chỉ xem SOP, không có nút soạn hoặc phát hành", async ({ page }) => {
  await loginAs(page, "approver", PATH);
  await expect(page.getByRole("button", { name: /Soạn SOP/ })).toHaveCount(0);
  await page.getByRole("button", { name: /Quy trình OM18 vùng ngập/ }).click();
  await expect(page.getByLabel("Nội dung việc 1.1")).toBeDisabled(); // vai xem không sửa
  await expect(page.getByRole("button", { name: "Phát hành" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Lưu nháp" })).toHaveCount(0);
});

test("HTX US-CAT-03: nhân bản giữ nguyên giai đoạn và việc; tên trùng bị chặn", async ({ page }) => {
  await loginAs(page, "setter", PATH);
  await page.getByRole("button", { name: /Quy trình lúa chất lượng cao/ }).click();
  await page.getByRole("button", { name: "Nhân bản", exact: true }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Tên SOP mới").fill("Quy trình lúa thơm ST25");
  await dlg.getByRole("button", { name: "Nhân bản", exact: true }).click();
  await expect(dlg.getByRole("alert")).toContainText("Tên SOP này đã tồn tại, vui lòng chọn tên khác.");
  await dlg.getByLabel("Tên SOP mới").fill("Quy trình CLC cho OM18");
  await dlg.getByRole("button", { name: "Nhân bản", exact: true }).click();
  await expect(page.getByLabel("Nội dung việc 3.1")).toHaveValue("Thu hoạch bằng máy gặt đập liên hợp");
  await expect(page.getByText("Nháp", { exact: true })).toBeVisible();
});

test("gắn giống lúa ↔ quy trình SOP hệ thống đã ban hành", async ({ page }) => {
  await loginAs(page, "setter", PATH);
  const sel = page.getByLabel("Quy trình cho giống OM5451");
  await expect(sel).toHaveValue(/.+/);
  await sel.selectOption({ label: "Quy trình lúa thơm ST25" });
  await expect(page.getByRole("region", { name: "Gắn giống lúa ↔ quy trình SOP" }).getByRole("status")).toContainText("Đã gắn giống OM5451");
});
