"use client";

import { useRef, useState, type FormEvent } from "react";
import { listAllCooperatives } from "@/api/adapters/cooperatives";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { AppGet, Cooperative } from "@/api/types";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";

type Profile = AppGet<"/v1/households/{ref}/profile">;
type EditorProps = { coop: Pick<Cooperative, "ref" | "name">; initial?: Profile; onClose: () => void; onSaved: () => void };

function EditorForm({ coop, initial, onClose, onSaved }: EditorProps) {
  const { api, principal } = useAuth();
  const admin = hasRole(principal, ROLES.platformAdmin);
  const [target, setTarget] = useState(initial?.household.cooperative_ref ?? (admin ? "" : coop.ref));
  const [name, setName] = useState(initial?.identity.full_name ?? "");
  const [phone, setPhone] = useState(initial?.identity.phone ?? "");
  const [nationalID, setNationalID] = useState(initial?.identity.national_id ?? "");
  const [address, setAddress] = useState(initial?.identity.address ?? "");
  const [area, setArea] = useState(initial?.household.cultivated_area_ha ?? "");
  const [livestock, setLivestock] = useState(initial?.household.livestock_area_ha ?? "");
  const [active, setActive] = useState(initial?.household.active ?? true);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const pending = useRef(false);
  const key = useRef(newIdempotencyKey());
  const coops = useAsync(() => admin && !initial ? listAllCooperatives(api) : Promise.resolve([]), [api, admin, initial]);
  const changed = () => { key.current = newIdempotencyKey(); setValidation(null); };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (pending.current) return;
    if (!target) { setValidation("Bắt buộc nhập HTX."); return; }
    if (!/^[0-9]{12}$/.test(nationalID.trim())) { setValidation("CCCD gồm đúng 12 chữ số"); return; }
    if (!/^0[0-9]{9}$/.test(phone.trim())) { setValidation("SĐT gồm 10 chữ số, bắt đầu bằng 0"); return; }
    // Decimal strings go to the server unchanged; the server verifies exact
    // precision, positivity and the current active-season allocation.
    const cultivated=area.trim().replace(",","."), animals=livestock.trim().replace(",",".");
    if (!/^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,4})?$/.test(cultivated) || !/[1-9]/.test(cultivated)) { setValidation("Diện tích phải lớn hơn 0, tối đa 4 chữ số thập phân"); return; }
    if (animals && !/^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,4})?$/.test(animals)) { setValidation("Diện tích không được âm, tối đa 4 chữ số thập phân"); return; }
    pending.current = true; setBusy(true); setProblem(null); setValidation(null);
    const body = { cooperative_ref: target, commune_code: initial?.household.commune_code,
      cultivated_area_ha: cultivated, livestock_area_ha: animals || null, active,
      expected_revision: initial?.revision ?? 0,
      identity: { full_name: name.trim(), phone: phone.trim(), national_id: nationalID.trim(), address: address.trim() } };
    try {
      if (initial) await unwrap(api.POST("/v1/households/{ref}/profile/revisions", { params: { path: { ref: initial.household.household_ref }, header: { "Idempotency-Key": key.current } }, body }));
      else await unwrap(api.POST("/v1/household-profiles", { params: { header: { "Idempotency-Key": key.current } }, body }));
      onSaved(); onClose();
    } catch (error) {
      const p = toApiProblem(error); setProblem(p);
      if (p.status >= 400 && p.status < 500) key.current = newIdempotencyKey();
      else { setUncertain(true); setValidation("Chưa hoàn tất thao tác. Giữ nguyên thông tin và thử lại."); }
    } finally { pending.current = false; setBusy(false); }
  };
  return <form noValidate onSubmit={(event) => void submit(event)}>
    <fieldset disabled={busy || uncertain} className="stack" onChange={changed}>
      {initial ? <p>Mã hộ: <code>{initial.code}</code> · HTX: {coop.name}</p> : admin ? <div className="field"><label htmlFor="native-hh-coop">HTX *</label>
        <select id="native-hh-coop" className="input" value={target} onChange={(e) => setTarget(e.target.value)} required>
          <option value="">Chọn HTX</option>{!coops.loading && !coops.error ? coops.data?.map((c) => <option key={c.ref} value={c.ref}>{c.name}</option>) : null}
        </select>{coops.loading ? <Loading /> : null}{coops.error ? <ProblemAlert problem={coops.error} onRetry={coops.reload} /> : null}</div> : <p>HTX: {coop.name}</p>}
      <div className="field"><label htmlFor="native-hh-name">Họ và tên chủ hộ *</label><input id="native-hh-name" className="input" value={name} onChange={(e) => setName(e.target.value)} required maxLength={200} autoComplete="off" /></div>
      <div className="field"><label htmlFor="native-hh-national">CCCD *</label><input id="native-hh-national" className="input" value={nationalID} onChange={(e) => setNationalID(e.target.value)} inputMode="numeric" required maxLength={12} autoComplete="off" /></div>
      <div className="field"><label htmlFor="native-hh-phone">Số điện thoại *</label><input id="native-hh-phone" className="input" value={phone} onChange={(e) => setPhone(e.target.value)} inputMode="tel" required maxLength={10} autoComplete="off" /></div>
      <div className="field"><label htmlFor="native-hh-address">Ấp / khu phố, địa chỉ</label><input id="native-hh-address" className="input" value={address} onChange={(e) => setAddress(e.target.value)} maxLength={500} autoComplete="off" /></div>
      <div className="field"><label htmlFor="native-hh-area">Diện tích trồng trọt (ha) *</label><input id="native-hh-area" className="input" inputMode="decimal" value={area} onChange={(e) => setArea(e.target.value)} required /></div>
      <div className="field"><label htmlFor="native-hh-livestock">Diện tích chăn nuôi (ha)</label><input id="native-hh-livestock" className="input" inputMode="decimal" value={livestock} onChange={(e) => setLivestock(e.target.value)} /></div>
      {initial ? <div className="field"><label htmlFor="native-hh-state">Trạng thái</label><select id="native-hh-state" className="input" value={active?"active":"inactive"} onChange={(e)=>setActive(e.target.value==="active")}><option value="active">Đang tham gia</option><option value="inactive">Ngừng tham gia</option></select></div> : null}
    </fieldset>
      {validation ? <p role="alert">{validation}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      <button className="btn btn-p" type="submit" disabled={busy || (admin && !initial && (coops.loading || !!coops.error))}>{busy ? "Đang lưu…" : "Lưu hộ"}</button>
  </form>;
}

export function HouseholdEditorButton({ coop, initial, onSaved }: { coop: Pick<Cooperative, "ref" | "name">; initial?: Profile; onSaved: () => void }) {
  const { principal } = useAuth();
  const [open, setOpen] = useState(false);
  if (!matrixAllows(principal, "FN-04", initial ? "U" : "C") || (!hasRole(principal, ROLES.platformAdmin) && principal?.orgRef !== coop.ref)) return null;
  const identity = JSON.stringify([principal?.subject, principal?.tenantId, principal?.orgRef, principal?.roles, principal?.scopeUnits]);
  return <span key={identity}><button className="btn btn-p btn-sm" type="button" onClick={() => setOpen(true)}>{initial ? "Sửa hộ" : "Thêm hộ"}</button>
    {open ? <Dialog title={initial ? "Sửa hộ xã viên" : "Thêm hộ xã viên"} onClose={() => setOpen(false)}><EditorForm coop={coop} initial={initial} onClose={() => setOpen(false)} onSaved={onSaved} /></Dialog> : null}
  </span>;
}

export function HouseholdDeleteButton({ initial, onSaved }: { initial: Profile; onSaved: () => void }) {
  const { api, principal } = useAuth();
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const pending = useRef(false);
  const key = useRef(newIdempotencyKey());
  if (!matrixAllows(principal, "FN-04", "D") || (!hasRole(principal, ROLES.platformAdmin) && principal?.orgRef !== initial.household.cooperative_ref)) return null;
  const remove = async () => {
    if (pending.current) return; pending.current = true; setBusy(true); setProblem(null);
    try { await unwrap(api.POST("/v1/households/{ref}/profile/deletions", { params: { path: { ref: initial.household.household_ref }, header: { "Idempotency-Key": key.current } }, body: { expected_revision: initial.revision } })); onSaved(); }
    catch (error) { setProblem(toApiProblem(error)); }
    finally { pending.current = false; setBusy(false); }
  };
  return <><button type="button" className="btn btn-o btn-sm" onClick={() => setConfirm(true)}>Xóa hộ</button>
    {confirm ? <Dialog title="Xác nhận xóa hộ" onClose={() => { if (!busy) setConfirm(false); }} actions={<button type="button" className="btn btn-p" disabled={busy} onClick={() => void remove()}>{busy ? "Đang xóa…" : "Xác nhận xóa"}</button>}>
      <p>Chỉ xóa hộ chưa có vụ hoặc phiếu thu gom. Hộ đã có sản xuất nên chuyển sang “Ngừng tham gia” trong màn sửa.</p>{problem ? <ProblemAlert problem={problem} /> : null}
    </Dialog> : null}</>;
}
