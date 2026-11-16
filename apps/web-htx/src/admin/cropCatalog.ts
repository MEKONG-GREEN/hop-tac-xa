// Danh mục cây trồng & giống (AH-2, BRD FN-01). Loại cây và giống dùng lại danh mục crop và rice_variety của
// pf/masterdata: loại cây = mục crop KHÔNG có parent_code; giống thuộc một loại cây qua attrs.crop_code.
// Hàm thuần ở đây; màn hình ở CropCatalogScreen.tsx. Máy chủ vẫn là nơi kiểm lại mọi luật.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import type { FieldRule } from "@/components/form";

export type CropCatalogName = "crop" | "rice_variety";
/** Danh mục Admin quản lý ở màn riêng: loại cây, giống (AH-2) và mùa vụ (AH-7). */
export type ManagedCatalogName = CropCatalogName | "season";

/** Loại cây: mục crop không có nhóm cha (mục cũ có parent_code không còn là loại cây, chỉ để xem). */
export function isCropType(it: Pick<CatalogItem, "catalog" | "attrs">): boolean {
  return it.catalog === "crop" && !it.attrs.parent_code;
}

/** Khóa so trùng tên giống máy chủ (domain.NameKey): bỏ khoảng trắng thừa, không phân biệt hoa thường. */
export function nameKey(s: string): string {
  return s.trim().split(/\s+/).filter(Boolean).join(" ").toLowerCase();
}

export interface VarietyForm {
  crop: string;
  name: string;
  growthDays: string;
}

export interface CropForm {
  name: string;
  isRice: string;
}

/** Luật kiểm form giống: Tên bắt buộc, thời gian sinh trưởng là số nguyên > 0, loại cây bắt buộc. */
export const VARIETY_RULES: Partial<Record<keyof VarietyForm, FieldRule>> = {
  crop: { kind: "text", required: true },
  name: { kind: "text", required: true, maxLength: 200 },
  growthDays: { kind: "number", required: true, integer: true, positive: true },
};

export const CROP_RULES: Partial<Record<keyof CropForm, FieldRule>> = {
  name: { kind: "text", required: true, maxLength: 200 },
};

export type VarietyErrors = Partial<Record<keyof VarietyForm, string>>;
export type CropErrors = Partial<Record<keyof CropForm, string>>;

const NAME_TAKEN = "masterdata.catalog_item_name_taken";

/**
 * Lỗi theo trường từ phản hồi 422/409 của máy chủ để tô đỏ đúng ô: name, attrs.growth_days, attrs.crop_code,
 * attrs.is_rice. "Tên đã tồn tại" đặt vào ô Tên (giống: trong loại cây này).
 */
export function fieldErrorsFromProblem(p: ApiProblem, kind: "variety"): VarietyErrors;
export function fieldErrorsFromProblem(p: ApiProblem, kind: "crop"): CropErrors;
export function fieldErrorsFromProblem(p: ApiProblem, kind: "variety" | "crop"): VarietyErrors & CropErrors {
  const map = fieldErrorMap(p);
  const out: VarietyErrors & CropErrors = {};
  if (map.name) out.name = map.name;
  if (map["attrs.growth_days"] && kind === "variety") out.growthDays = map["attrs.growth_days"];
  if (map["attrs.crop_code"] && kind === "variety") out.crop = map["attrs.crop_code"];
  if (map["attrs.is_rice"] && kind === "crop") out.isRice = map["attrs.is_rice"];
  if (p.code === NAME_TAKEN) out.name = kind === "variety" ? "Tên đã tồn tại trong loại cây này" : "Tên đã tồn tại";
  return out;
}

/** Gom mọi trang của một danh mục (cả mục đã ẩn) từ Admin API. */
export function listAll(adminApi: AdminApiClient, catalog: ManagedCatalogName): Promise<CatalogItem[]> {
  return collectPages((cursor) =>
    unwrap(adminApi.GET("/v1/masterdata/catalogs/{catalog}/items", { params: { path: { catalog }, query: { include_inactive: true, cursor, limit: 200 } } })),
  );
}
