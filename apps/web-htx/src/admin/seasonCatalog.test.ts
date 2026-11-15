import { describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { validateForm } from "@/components/form";
import { fieldErrorsFromProblem, monthPairError, monthsLabel, SEASON_RULES, seasonBody, usageLabel } from "./seasonCatalog";

const problem = (code: string, errors: { field: string; reason: string }[] = [], status = 422) => new ApiProblem({ type: "", title: "", status, code, errors });
const form = (over: Partial<Parameters<typeof seasonBody>[0]> = {}) => ({ name: "Vụ A", description: "", startMonth: "", endMonth: "", ...over });

describe("danh mục mùa vụ: hàm thuần (AH-7)", () => {
  it("monthPairError: đủ hai tháng hoặc trống cả hai thì hợp lệ; thiếu một tháng báo ở ô còn thiếu", () => {
    expect(monthPairError({ startMonth: "11", endMonth: "3" })).toEqual({});
    expect(monthPairError({ startMonth: "", endMonth: "" })).toEqual({});
    expect(monthPairError({ startMonth: "11", endMonth: "" }).endMonth).toMatch(/Nhập cả tháng bắt đầu và tháng kết thúc/);
    expect(monthPairError({ startMonth: "", endMonth: "3" }).startMonth).toMatch(/Nhập cả tháng bắt đầu và tháng kết thúc/);
  });

  it("luật form: tên bắt buộc, tối đa 200; mô tả tối đa 500", () => {
    expect(validateForm(form({ name: " " }), SEASON_RULES).errors.name).toBeTruthy();
    expect(validateForm(form({ name: "x".repeat(201) }), SEASON_RULES).errors.name).toBeTruthy();
    expect(validateForm(form({ description: "x".repeat(501) }), SEASON_RULES).errors.description).toBeTruthy();
    expect(validateForm(form({ description: "x".repeat(500) }), SEASON_RULES).valid).toBe(true);
  });

  it("seasonBody: tên đã trim; không gửi mô tả rỗng; chỉ gửi tháng khi đã chọn cả hai", () => {
    expect(seasonBody(form({ name: "  Vụ A " }))).toEqual({ name: "Vụ A", attrs: {} });
    expect(seasonBody(form({ description: "  mô tả " })).attrs).toEqual({ description: "mô tả" });
    expect(seasonBody(form({ startMonth: "11", endMonth: "3" })).attrs).toEqual({ start_month: "11", end_month: "3" });
    expect(seasonBody(form({ startMonth: "11" })).attrs).toEqual({});
  });

  it("AC2: 409 trùng tên báo ở ô Tên; 422 theo trường báo đúng ô", () => {
    expect(fieldErrorsFromProblem(problem("masterdata.catalog_item_name_taken", [{ field: "name", reason: "Tên đã tồn tại" }], 409))).toEqual({ name: "Tên đã tồn tại" });
    expect(fieldErrorsFromProblem(problem("masterdata.catalog_item_name_taken", [], 409)).name).toBe("Tên đã tồn tại");
    const e = fieldErrorsFromProblem(problem("validation_failed", [{ field: "attrs.end_month", reason: "phải là số nguyên từ 1 đến 12" }, { field: "attrs.description", reason: "quá dài" }]));
    expect(e.endMonth).toBe("phải là số nguyên từ 1 đến 12");
    expect(e.description).toBe("quá dài");
    expect(e.name).toBeUndefined();
  });

  it("usageLabel: có số thì 'N vụ' (không có số lịch); vắng thì dấu gạch", () => {
    expect(usageLabel(3)).toBe("3 vụ");
    expect(usageLabel(0)).toBe("0 vụ");
    expect(usageLabel(undefined)).toBe("—");
    expect(usageLabel(2)).not.toMatch(/lịch/);
  });

  it("monthsLabel: hiện thời gian dự kiến khi có đủ hai tháng", () => {
    expect(monthsLabel({ start_month: "11", end_month: "3" })).toBe("Tháng 11 – Tháng 3");
    expect(monthsLabel({})).toBe("");
  });
});
