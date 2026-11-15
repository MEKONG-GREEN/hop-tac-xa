import { describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { validateForm } from "@/components/form";
import { CROP_RULES, fieldErrorsFromProblem, isCropType, nameKey, VARIETY_RULES } from "./cropCatalog";

const problem = (code: string, errors: { field: string; reason: string }[] = [], status = 422) => new ApiProblem({ type: "", title: "", status, code, errors });

describe("loại cây và giống: hàm thuần (AH-2)", () => {
  it("nameKey khớp luật so trùng của máy chủ", () => {
    expect(nameKey("om 5451")).toBe(nameKey("OM  5451"));
    expect(nameKey(" Rau  MÀU ")).toBe(nameKey("rau màu"));
    expect(nameKey("OM5451")).not.toBe(nameKey("OM 5451"));
  });

  it("isCropType: mục crop không có nhóm cha mới là loại cây", () => {
    expect(isCropType({ catalog: "crop", attrs: { is_rice: "true" } })).toBe(true);
    expect(isCropType({ catalog: "crop", attrs: { parent_code: "RAU_MAU" } })).toBe(false);
    expect(isCropType({ catalog: "rice_variety", attrs: {} })).toBe(false);
  });

  it("AC3: thời gian sinh trưởng ≤ 0, thập phân hoặc rỗng bị chặn ngay trên form; số nguyên dương hợp lệ", () => {
    const base = { crop: "LUA", name: "Giống A", growthDays: "" };
    for (const bad of ["0", "-5", "1.5", "abc", ""]) {
      const r = validateForm({ ...base, growthDays: bad }, VARIETY_RULES);
      expect(r.valid, bad).toBe(false);
      expect(r.errors.growthDays, bad).toBeTruthy();
    }
    expect(validateForm({ ...base, growthDays: "400" }, VARIETY_RULES).valid).toBe(true);
    expect(validateForm({ ...base, name: " " }, VARIETY_RULES).errors.name).toBeTruthy();
    expect(validateForm({ ...base, crop: "", growthDays: "95" }, VARIETY_RULES).errors.crop).toBeTruthy();
    expect(validateForm({ name: "", isRice: "false" }, CROP_RULES).errors.name).toBeTruthy();
  });

  it("fieldErrorsFromProblem ánh xạ trường máy chủ sang ô của form", () => {
    const p = problem("validation_failed", [
      { field: "attrs.growth_days", reason: "Thời gian sinh trưởng phải là số nguyên lớn hơn 0 (ngày)" },
      { field: "attrs.crop_code", reason: "Loại cây không tồn tại hoặc đang ẩn" },
    ]);
    expect(fieldErrorsFromProblem(p, "variety")).toEqual({
      growthDays: "Thời gian sinh trưởng phải là số nguyên lớn hơn 0 (ngày)",
      crop: "Loại cây không tồn tại hoặc đang ẩn",
    });
    expect(fieldErrorsFromProblem(problem("validation_failed", [{ field: "attrs.is_rice", reason: "bắt buộc" }]), "crop")).toEqual({ isRice: "bắt buộc" });
  });

  it("AC2: tên trùng đặt lỗi vào ô Tên, giống kèm 'trong loại cây này'", () => {
    const taken = problem("masterdata.catalog_item_name_taken", [{ field: "name", reason: "Tên đã tồn tại" }], 409);
    expect(fieldErrorsFromProblem(taken, "variety").name).toBe("Tên đã tồn tại trong loại cây này");
    expect(fieldErrorsFromProblem(taken, "crop").name).toBe("Tên đã tồn tại");
    expect(fieldErrorsFromProblem(problem("network_error", [], 0), "variety")).toEqual({});
  });
});
