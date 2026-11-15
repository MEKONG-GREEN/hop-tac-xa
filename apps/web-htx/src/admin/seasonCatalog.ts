// Danh mục mùa vụ (AH-7, BRD FN-01 luồng phụ Mùa vụ). Dùng lại danh mục season của pf/masterdata: mã do máy chủ sinh,
// thuộc tính description, start_month, end_month (không bắt buộc, chỉ để tham khảo; đã nhập thì nhập cả hai).
// Hàm thuần ở đây; màn hình ở SeasonCatalogScreen.tsx. Máy chủ vẫn là nơi kiểm lại mọi luật.
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import type { FieldRule } from "@/components/form";

/** Mục mùa vụ từ Admin API: kèm số vụ đang dùng (seasons_in_use chỉ có ở Admin API, không có ở Application API). */
export type SeasonItem = CatalogItem & { seasons_in_use?: number };

export interface SeasonForm {
  name: string;
  description: string;
  startMonth: string;
  endMonth: string;
}

export type SeasonErrors = Partial<Record<keyof SeasonForm, string>>;

/** Luật kiểm form mùa vụ: Tên bắt buộc (tối đa 200), Mô tả tối đa 500. */
export const SEASON_RULES: Partial<Record<keyof SeasonForm, FieldRule>> = {
  name: { kind: "text", required: true, maxLength: 200 },
  description: { kind: "text", maxLength: 500 },
};

export const MONTH_OPTIONS = Array.from({ length: 12 }, (_, i) => ({ value: String(i + 1), label: `Tháng ${i + 1}` }));

const NAME_TAKEN = "masterdata.catalog_item_name_taken";

/** Đã nhập thì nhập cả hai tháng: chỉ chọn một tháng thì báo lỗi ở ô còn thiếu. */
export function monthPairError(form: Pick<SeasonForm, "startMonth" | "endMonth">): SeasonErrors {
  const message = "Nhập cả tháng bắt đầu và tháng kết thúc, hoặc bỏ trống cả hai.";
  if (form.startMonth && !form.endMonth) return { endMonth: message };
  if (!form.startMonth && form.endMonth) return { startMonth: message };
  return {};
}

/** Thân gửi máy chủ: tên đã bỏ khoảng trắng hai đầu; thuộc tính nào trống thì không gửi (máy chủ xóa khi sửa). */
export function seasonBody(values: SeasonForm): { name: string; attrs: Record<string, string> } {
  const attrs: Record<string, string> = {};
  const description = values.description.trim();
  if (description) attrs.description = description;
  if (values.startMonth && values.endMonth) {
    attrs.start_month = values.startMonth;
    attrs.end_month = values.endMonth;
  }
  return { name: values.name.trim(), attrs };
}

/**
 * Lỗi theo trường từ phản hồi 422/409 của máy chủ để tô đỏ đúng ô: name, attrs.description, attrs.start_month,
 * attrs.end_month. "Tên đã tồn tại" đặt vào ô Tên.
 */
export function fieldErrorsFromProblem(p: ApiProblem): SeasonErrors {
  const map = fieldErrorMap(p);
  const out: SeasonErrors = {};
  if (map.name) out.name = map.name;
  if (map["attrs.description"]) out.description = map["attrs.description"];
  if (map["attrs.start_month"]) out.startMonth = map["attrs.start_month"];
  if (map["attrs.end_month"]) out.endMonth = map["attrs.end_month"];
  if (p.code === NAME_TAKEN) out.name = "Tên đã tồn tại";
  return out;
}

/** Cột "Đang dùng": số vụ đang dùng mùa vụ; "—" khi máy chủ chưa trả. Chưa hiện số lịch (AH-51 bổ sung). */
export function usageLabel(n?: number): string {
  return n === undefined ? "—" : `${n} vụ`;
}

/** Thời gian dự kiến để tham khảo: "Tháng 11 – Tháng 3"; rỗng khi chưa nhập. */
export function monthsLabel(attrs: CatalogItem["attrs"]): string {
  const { start_month: start, end_month: end } = attrs;
  return start && end ? `Tháng ${start} – Tháng ${end}` : "";
}
