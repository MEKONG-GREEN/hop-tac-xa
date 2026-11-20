// GIẢ LẬP xuất Excel danh mục dùng chung Vật tư/Máy móc (VTKM-Q-07, AH-274) theo hợp đồng
// GET /v1/masterdata/shared-catalogs/{catalog}/export: Admin, Đại diện HTX, Thành viên HTX; chỉ mục đang dùng.
// Mock trả CSV UTF-8 (BOM) cùng cột với tệp .xlsx thật; tên tệp ghi rõ "gia-lap".
import { CATALOGS } from "./data";
import { hasRole, problem, type Route } from "./http";

const SHARED = ["material", "machine_type"] as const;
const LABELS: Record<string, string> = { group: "Nhóm", unit: "Đơn vị tính", farm_stage: "Giai đoạn" };
const cell = (v: string) => `"${v.replaceAll('"', '""')}"`;

export const sharedCatalogRoutes: Route[] = [{
  method: "GET",
  pattern: /^\/v1\/masterdata\/shared-catalogs\/([\w-]+)\/export$/,
  handler: ({ params, persona }) => {
    if (!hasRole(persona, "platform_admin", "coop_manager", "htx_member")) return problem(403, "forbidden", "Forbidden");
    const catalog = SHARED.find((c) => c === params[0]);
    if (!catalog) return problem(404, "masterdata.catalog_not_found", "Không có danh mục này");
    const items = CATALOGS[catalog].filter((it) => it.active);
    const attrs = [...new Set(items.flatMap((it) => Object.keys(it.attrs)))].sort();
    const head = ["Mã", "Tên", ...attrs.map((k) => LABELS[k] ?? k)].map(cell).join(",");
    const lines = items.map((it) => [it.code, it.name, ...attrs.map((k) => it.attrs[k] ?? "")].map(cell).join(","));
    return new Response(`﻿${head}\r\n${lines.join("\r\n")}\r\n`, {
      status: 200,
      headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": `attachment; filename="danh-muc-${catalog}-gia-lap.csv"`, "Cache-Control": "no-store" },
    });
  },
}];
