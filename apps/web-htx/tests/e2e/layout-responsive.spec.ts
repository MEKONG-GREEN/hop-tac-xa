import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

// AC-N2 (AH-292, NFR#7): khung trang và mẫu danh sách UX-01 ở 360 / 768 / 1280 px — không cuộn ngang cả trang,
// không cắt chữ, mọi nút bấm được. Màn đã áp mẫu: Danh mục mùa vụ.
const WIDTHS = [360, 768, 1280];
const PATH = "/du-lieu-nen/danh-muc-mua-vu";

const pageOverflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

/** Phần tử chữ ngoài khung cuộn của bảng: hộp nằm trong khung nhìn, không bị cắt bởi overflow/ellipsis. */
async function clippedTexts(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const sel = "h1, [role=tab], .btn, .fchip, .fsearch, .fcount, .freset, .pager, .crumb b, .mock-badge";
    const bad: string[] = [];
    for (const el of Array.from(document.querySelectorAll<HTMLElement>(sel))) {
      if (el.closest(".tbl-wrap") || el.getClientRects().length === 0) continue;
      const r = el.getBoundingClientRect();
      const name = `${el.tagName.toLowerCase()}.${el.className} "${(el.textContent ?? "").trim().slice(0, 30)}"`;
      if (r.left < -1 || r.right > window.innerWidth + 1) bad.push(`${name} ngoài khung nhìn`);
      const cs = getComputedStyle(el);
      if ((cs.overflow !== "visible" || cs.textOverflow === "ellipsis") && el.scrollWidth > el.clientWidth + 1) bad.push(`${name} bị cắt`);
    }
    return bad;
  });
}

for (const width of WIDTHS) {
  test.describe(`bề rộng ${width}px`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await loginAs(page, "setter", PATH);
      await expect(page.getByRole("heading", { level: 1, name: "Danh mục mùa vụ" })).toBeVisible();
      await expect(page.getByRole("tablist")).toBeVisible();
      await expect(page.getByRole("table")).toBeVisible();
    });

    test("không cuộn ngang cả trang, không cắt chữ", async ({ page }) => {
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      expect(await clippedTexts(page)).toEqual([]);
    });

    test("mọi nút và liên kết ở thanh trên và nội dung bấm được", async ({ page }) => {
      const targets = page.locator("header.top button, header.top a, main button, main a");
      const n = await targets.count();
      for (let i = 0; i < n; i++) {
        const t = targets.nth(i);
        if ((await t.isVisible()) && (await t.isEnabled())) await t.click({ trial: true });
      }
    });

    if (width < 960) {
      test("ngăn kéo menu mở được, vẫn không cuộn ngang", async ({ page }) => {
        await page.getByRole("button", { name: "Mở menu" }).click();
        await expect(page.getByRole("complementary", { name: "Điều hướng chính" })).toBeInViewport();
        expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      });
    }
  });
}
