"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { collectPages } from "@/api/pagination";
import { newIdempotencyKey } from "@/api/idempotency";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import type { ApiProblem } from "@/api/problem";
import { TaskCamera } from "./TaskCamera";
import type { TaskPhoto } from "./taskPhoto";
import { cachedTaskRead, enqueueTask } from "./taskJournalQueue";

type S = components["schemas"];
type Material = S["HouseholdTaskDetailInput"]["materials"][number];

export function actualMaterialTotal(dose: string, area: string): string | null {
  const parse = (s: string, digits: number) => {
    if (!new RegExp(`^(0|[1-9][0-9]{0,8})(\\.[0-9]{1,${digits}})?$`).test(s)) return null;
    const [whole, fraction = ""] = s.split(".");
    return BigInt(whole!) * 10n ** BigInt(digits) + BigInt(fraction.padEnd(digits, "0"));
  };
  const d = parse(dose, 4), a = parse(area, 2); if (d == null || a == null) return null;
  const n = d * a; return `${n / 1000000n}.${(n % 1000000n).toString().padStart(6, "0")}`.replace(/\.?0+$/, "");
}

export function HouseholdTaskDetail({ path, event, task, season, onQueued, onClose }: {
  path: { ref: string; household_ref: string; season_ref: string }; event: string; task: S["HouseholdTaskProgress"]; season: S["HouseholdSeason"];
  onQueued: () => void; onClose: () => void;
}) {
  const { api, principal } = useAuth();
  const recommended = season.sop_snapshot.steps.find((s) => s.seq === task.seq)?.materials ?? [];
  const [materials, setMaterials] = useState<Material[]>(() => recommended.map((m) => ({ material_code: m.material_code ?? "", name: m.name, unit: m.unit, dose_per_ha: m.dose_per_ha })));
  const [day, setDay] = useState(() => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date()));
  const [state, setState] = useState<"partial" | "done">("done");
  const [note, setNote] = useState(""); const [reason, setReason] = useState(""); const [zero, setZero] = useState(false);
  const [key, setKey] = useState(newIdempotencyKey);
  const [photos, setPhotos] = useState<{ photo: TaskPhoto; device: string }[]>([]);
  const [busy, setBusy] = useState(false); const [error, setError] = useState<ApiProblem | null>(null); const [message, setMessage] = useState("");
  const catalog = useAsync(() => {
    const read = () => collectPages((cursor) => unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "material" }, query: { cursor, limit: 200 } } })));
    return principal ? cachedTaskRead(principal, "catalog:material", read) : read();
  }, [api, principal]);
  const edit = () => { setKey(newIdempotencyKey()); setError(null); setMessage(""); };
  const changeMaterial = (i: number, patch: Partial<Material>) => { setZero(false); setMaterials((rows) => rows.map((m, n) => n === i ? { ...m, ...patch } : m)); edit(); };
  const needsReason = !task.undated && day < new Date(new Date(`${new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date())}T00:00:00Z`).getTime() - 3 * 86400000).toISOString().slice(0, 10);
  const needsZero = (recommended.length > 0 && materials.length === 0) || materials.some((m) => /^0(?:\.0+)?$/.test(m.dose_per_ha));
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    if (materials.some((m) => actualMaterialTotal(m.dose_per_ha, season.area_ha) == null)) { setMessage("Liều thực tế phải không âm, tối đa 4 chữ số thập phân."); return; }
    if (needsZero && !zero) { setMessage("Xác nhận liều bằng 0 trước khi lưu."); return; }
    setBusy(true); setError(null); setMessage("");
    try {
      if (!principal) throw new Error("Cần đăng nhập để ghi nhật ký.");
      const point = photos[0]?.photo.meta.gps;
      await enqueueTask(principal, { id: key, coop: path.ref, household: path.household_ref, season: path.season_ref, photos: photos.map((p) => p.photo), input: { expected_season_event_id: event, step_seq: task.seq, state, done_on: day, materials, file_refs: photos.map((p) => p.photo.ref), note, backfill_reason: reason, zero_confirmed: zero, gps: point ? { source: "device", lon: point.lon.toFixed(7), lat: point.lat.toFixed(7), accuracy_m: point.accuracy_m?.toFixed(2) } : { source: "no_device" }, channel: "web" } });
      onQueued();
    } catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  };
  return <section aria-label={`Ghi chi tiết ${task.name}`} className="panel stack">
    <h4>Ghi chi tiết — {task.name}</h4>
    <p>Diện tích vụ: {season.area_ha} ha. Liều thực tế được lưu riêng với khuyến cáo SOP.</p>
    <form onSubmit={(e) => void submit(e)}>
      <fieldset disabled={busy} className="stack">
        <label>Ngày làm xong<input type="date" required value={day} onChange={(e) => { setDay(e.target.value); edit(); }} /></label>
        <label>Kết quả<select value={state} onChange={(e) => { setState(e.target.value as "partial" | "done"); edit(); }}><option value="done">Đã xong hoàn toàn</option><option value="partial">Chưa xong, sẽ làm tiếp lần sau</option></select></label>
        {materials.map((m, i) => <div key={i} className="stack">
          <label>Vật tư {i + 1}<select aria-label={`Vật tư ${i + 1}`} value={m.material_code || ""} onChange={(e) => { const c = catalog.data?.find((c) => c.code === e.target.value); changeMaterial(i, { material_code: c?.code ?? "", name: c?.name ?? "", unit: c?.attrs.unit ?? "" }); }}><option value="">Chưa có trong danh mục — nhập tên</option>{m.material_code && !catalog.data?.some((c) => c.code === m.material_code) ? <option value={m.material_code}>{m.name || m.material_code} (SOP)</option> : null}{catalog.data?.filter((c) => c.active || c.code === m.material_code).map((c) => <option key={c.code} value={c.code}>{c.code} · {c.name} · {c.attrs.group} · {c.attrs.unit}</option>)}</select></label>
          {!m.material_code ? <><label>Tên vật tư<input required value={m.name ?? ""} onChange={(e) => changeMaterial(i, { name: e.target.value })} /></label><label>Đơn vị<input required value={m.unit ?? ""} onChange={(e) => changeMaterial(i, { unit: e.target.value })} /></label></> : null}
          <label>Liều thực tế/ha ({m.unit})<input required inputMode="decimal" value={m.dose_per_ha} onChange={(e) => changeMaterial(i, { dose_per_ha: e.target.value })} /></label>
          <span>Tổng thực tế: {actualMaterialTotal(m.dose_per_ha, season.area_ha) ?? "—"} {m.unit}</span>
          <button type="button" onClick={() => { setMaterials((r) => r.filter((_, n) => n !== i)); edit(); }}>Bỏ vật tư {i + 1}</button>
        </div>)}
        <button type="button" disabled={materials.length >= 20 || catalog.loading || !!catalog.error} onClick={() => { setMaterials((r) => [...r, { name: "", unit: "", dose_per_ha: "0" }]); edit(); }}>Thêm vật tư</button>
        {needsZero ? <label><input type="checkbox" checked={zero} onChange={(e) => { setZero(e.target.checked); edit(); }} /> Tôi xác nhận liều bằng 0 hoặc không sử dụng vật tư khuyến cáo</label> : null}
        <label>Ghi chú<textarea maxLength={2000} value={note} onChange={(e) => { setNote(e.target.value); edit(); }} /></label>
        <label>Lý do ghi bù{needsReason ? " (bắt buộc)" : ""}<textarea required={needsReason} maxLength={500} value={reason} onChange={(e) => { setReason(e.target.value); edit(); }} /></label>
        <TaskCamera household={path.household_ref} count={photos.length} disabled={busy} onPhoto={(photo, device) => { setPhotos((r) => [...r, { photo, device }]); edit(); }} />
        {photos.map((p, i) => <button type="button" key={p.photo.ref} onClick={() => { setPhotos((r) => r.filter((v) => v !== p)); edit(); }}>Bỏ ảnh {i + 1}</button>)}
        <p>Nhật ký lưu mã hóa trên trình duyệt, chờ máy chủ xác nhận khi có mạng. Mở vụ vẫn cần mạng.</p>
        <button type="submit">{busy ? "Đang lưu…" : "Lưu chi tiết"}</button><button type="button" onClick={onClose}>Đóng chi tiết</button>
      </fieldset>
      {message ? <p role="alert">{message}</p> : null}{error ? <ProblemAlert problem={error} /> : null}{catalog.error ? <ProblemAlert problem={catalog.error} onRetry={catalog.reload} /> : null}
    </form>
  </section>;
}
