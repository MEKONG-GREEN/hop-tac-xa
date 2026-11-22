"use client";

import { useState } from "react";
import { unwrap, type ApiPaths } from "@/api/client";
import { parseProblemResponse, type ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, Loading, PageHead } from "@/components/ui";
import { filenameFromDisposition, saveBlob } from "@/lib/files";
import { toApiProblem, useAsync } from "@/lib/useAsync";

// VTKM-Q-07 (AH-274): Đại diện HTX và Thành viên HTX mở danh sách và xuất Excel chỉ đọc danh mục dùng chung
// Vật tư, Máy móc. Không có thao tác thêm/sửa/xóa; quản trị danh mục vẫn ở màn Admin.
type Item = ApiPaths["/v1/masterdata/catalogs/{catalog}"]["get"]["responses"][200]["content"]["application/json"]["items"][number];
type Shared = "material" | "machine_type";
const CATALOGS: { id: Shared; label: string }[] = [{ id: "material", label: "Vật tư" }, { id: "machine_type", label: "Máy móc" }];
const ATTR_LABELS: Record<string, string> = { group: "Nhóm", unit: "Đơn vị tính", farm_stage: "Giai đoạn" };

export function SharedCatalogs() {
  const { api, principal } = useAuth();
  const allowed = hasRole(principal, ROLES.coopManager, ROLES.htxMember, ROLES.platformAdmin);
  const [catalog, setCatalog] = useState<Shared>("material");
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [exportProblem, setExportProblem] = useState<ApiProblem | null>(null);
  const list = useAsync(async () => {
    if (!allowed) return [] as Item[];
    const out: Item[] = [];
    let cursor: string | undefined;
    for (let i = 0; i < 50; i++) {
      const page = await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog }, query: { limit: 200, cursor } } }));
      out.push(...page.items);
      if (!page.next_cursor) break;
      cursor = page.next_cursor;
    }
    return out;
  }, [api, catalog, allowed]);
  if (!allowed) return null;
  const items = list.data ?? [];
  const attrs = [...new Set(items.flatMap((it) => Object.keys(it.attrs)))].sort();
  const needle = q.trim().toLocaleLowerCase("vi");
  const rows = needle ? items.filter((it) => `${it.code} ${it.name}`.toLocaleLowerCase("vi").includes(needle)) : items;
  const title = CATALOGS.find((c) => c.id === catalog)!.label;

  const exportXlsx = async () => {
    if (busy) return;
    setBusy(true);
    setExportProblem(null);
    try {
      const res = await api.GET("/v1/masterdata/shared-catalogs/{catalog}/export", { params: { path: { catalog } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      saveBlob(res.data, filenameFromDisposition(res.response.headers.get("Content-Disposition"), `danh-muc-${catalog}.xlsx`));
    } catch (e) {
      setExportProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="stack">
      <PageHead title="Danh mục vật tư, máy móc" subtitle="Danh mục dùng chung — chỉ xem và xuất Excel." />
      <div className="card sec">
        <Tabs label="Danh mục" value={catalog} onChange={(v) => { setCatalog(v as Shared); setQ(""); }} tabs={CATALOGS}>
          <div className="row" style={{ marginBottom: 12, alignItems: "flex-end" }}>
            <div className="field" style={{ flex: "1 1 260px" }}>
              <label htmlFor="shared-catalog-q">Tìm theo mã hoặc tên</label>
              <input id="shared-catalog-q" className="input" type="search" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void exportXlsx()}>
              {busy ? "Đang xuất…" : "Xuất Excel"}
            </button>
          </div>
          {exportProblem ? <ProblemAlert problem={exportProblem} /> : null}
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          {list.loading && !list.data ? <Loading /> : null}
          {list.data && rows.length === 0 ? <Empty>Không có mục phù hợp.</Empty> : null}
          {rows.length > 0 ? (
            <div className="tbl-wrap">
              <table className="tbl">
                <caption className="sr-only">Danh mục {title}</caption>
                <thead>
                  <tr>
                    <th scope="col">Mã</th>
                    <th scope="col">Tên</th>
                    {attrs.map((k) => <th key={k} scope="col">{ATTR_LABELS[k] ?? k}</th>)}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((it) => (
                    <tr key={it.code}>
                      <td>{it.code}</td>
                      <td>{it.name}</td>
                      {attrs.map((k) => <td key={k}>{it.attrs[k] ?? "—"}</td>)}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </Tabs>
      </div>
    </div>
  );
}
