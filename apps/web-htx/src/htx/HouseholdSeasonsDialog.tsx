"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { HouseholdListItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { LoadMore, Loading } from "@/components/ui";
import { usePagedList } from "@/api/pagination";
import { formatDecimal } from "@/lib/decimal";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { SeasonPlan } from "./SeasonPlan";
import { HouseholdChecklist } from "./HouseholdChecklist";

type S = components["schemas"];
type GPS = S["CropGPS"];

export function householdAreaError(raw: string): string | null {
  if (!/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(raw) || !/[1-9]/.test(raw)) return "Diện tích phải lớn hơn 0 và có tối đa hai chữ số thập phân.";
  return null;
}

async function requestGPS(): Promise<GPS> {
  if (!navigator.geolocation) return { source: "no_device" };
  return new Promise((resolve) => { try { navigator.geolocation.getCurrentPosition(
    (p) => resolve({ lon: p.coords.longitude.toFixed(7), lat: p.coords.latitude.toFixed(7), accuracy_m: p.coords.accuracy.toFixed(2), source: "device" }),
    () => resolve({ source: "denied" }),
    { enableHighAccuracy: true, timeout: 15000, maximumAge: 0 },
  ); } catch { resolve({ source: "denied" }); } });
}

export function HouseholdSeasonsDialog({ coopRef, household, onClose, onSaved }: {
  coopRef: string; household: HouseholdListItem; onClose: () => void; onSaved: () => void;
}) {
  const { api, principal } = useAuth();
  const canCreate = hasRole(principal, ROLES.platformAdmin, ROLES.htxMember) && matrixAllows(principal, "KHMV-FN-03", "C");
  const [opening, setOpening] = useState(false);
  const [crop, setCrop] = useState("");
  const [variety, setVariety] = useState("");
  const [seasonCode, setSeasonCode] = useState("");
  const [year, setYear] = useState(String(new Date().getFullYear()));
  const [area, setArea] = useState("");
  const [anchor, setAnchor] = useState("");
  const [version, setVersion] = useState("");
  const [key, setKey] = useState(newIdempotencyKey);
  const [gps, setGPS] = useState<GPS | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [inputError, setInputError] = useState<string | null>(null);
  const [remaining, setRemaining] = useState(household.remaining_area_ha);
  const [hasCapacity, setHasCapacity] = useState(household.can_open_season);
  const [detail, setDetail] = useState<S["HouseholdSeason"] | null>(null);
  const [revision, setRevision] = useState(0);
  const path = { ref: coopRef, household_ref: household.household_ref };
  const list = usePagedList((cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path, query: { cursor, limit: 20 } } })), [api, coopRef, household.household_ref, revision]);
  const metadata = useAsync(async () => {
    const catalog = (name: "crop" | "rice_variety" | "season") => collectPages((cursor) => unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: name }, query: { cursor, limit: 200 } } })));
    const [crops, varieties, seasons, templates, bindings] = await Promise.all([
      catalog("crop"), catalog("rice_variety"), catalog("season"),
      collectPages((cursor) => unwrap(api.GET("/v1/cropcycle/sops", { params: { query: { status: "published", cursor, limit: 200 } } }))),
      unwrap(api.GET("/v1/cropcycle/variety-bindings")),
    ]);
    return { crops, varieties, seasons, templates, bindings: bindings.items };
  }, [api]);
  const choices = useAsync(async () => {
    const eligible = (metadata.data?.templates ?? []).filter((t) => t.crop === crop && (!t.variety_code || t.variety_code === variety) && (t.scope === "system" || t.cooperative_ref === coopRef));
    const bound = metadata.data?.bindings.find((b) => b.variety_code === variety)?.template_ref;
    const specific = eligible.filter((t) => t.variety_code === variety || t.ref === bound);
    return Promise.all((specific.length ? specific : eligible).map(async (t) => {
      const d = await unwrap(api.GET("/v1/cropcycle/sops/{ref}", { params: { path: { ref: t.ref } } }));
      const v = d.versions.find((v) => v.state === "active" && v.version_no === t.published_version);
      return v ? { id: v.id, label: `${t.name} · bản ${v.version_no}`, anchor: t.anchor_name } : null;
    })).then((rows) => rows.filter((r) => r !== null));
  }, [api, metadata.data, coopRef, crop, variety]);
  const selected = version || (choices.data?.length === 1 ? choices.data[0]?.id ?? "" : "");
  const planReady = opening && canCreate && matrixAllows(principal, "KHMV-FN-04", "C") && !choices.loading &&
    !!crop && !!variety && !!selected && !householdAreaError(area) && /^\d{4}-\d{2}-\d{2}$/.test(anchor);
  const preview = useAsync(async () => planReady ? unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/season-plan-preview", {
    params: { path, header: { "Idempotency-Key": newIdempotencyKey() } },
    body: { crop_code: crop, variety_code: variety, sop_version_id: selected, area_ha: area, anchor_date: anchor },
  })) : null, [api, coopRef, household.household_ref, planReady, crop, variety, selected, area, anchor]);
  const detailPlan = useAsync(async () => detail ? unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/plan", {
    params: { path: { ...path, season_ref: detail.season_ref } },
  })) : null, [api, coopRef, household.household_ref, detail?.season_ref, detail?.current_event_id]);
  const edit = () => { setKey(newIdempotencyKey()); setInputError(null); setProblem(null); };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const invalid = householdAreaError(area);
    if (invalid) { setInputError(invalid); return; }
    if (!selected) { setInputError("Chọn SOP đang áp dụng. Nếu chưa có, vui lòng liên hệ Admin."); return; }
    if (typeof navigator !== "undefined" && navigator.onLine === false) { setInputError("Mở vụ cần mạng. Dữ liệu form được giữ lại để thử lại."); return; }
    setBusy(true); setProblem(null); setInputError(null);
    try {
      const point = gps ?? await requestGPS();
      setGPS(point);
      const result = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", {
        params: { path, header: { "Idempotency-Key": key } },
        body: { crop_code: crop, season_code: seasonCode, season_year: Number(year), variety_code: variety, area_ha: area, anchor_date: anchor, sop_version_id: selected, gps: point, channel: "web" },
      }));
      setDetail(result.season); setOpening(false); setRevision((v) => v + 1); onSaved();
      setArea(""); setKey(newIdempotencyKey()); setGPS(null);
      try {
        const households = await collectPages((cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coopRef }, query: { cursor, limit: 200 } } })));
        const fresh = households.find((h) => h.household_ref === household.household_ref);
        setRemaining(fresh?.remaining_area_ha ?? null); setHasCapacity(fresh?.can_open_season ?? false);
      } catch { setRemaining(null); setHasCapacity(false); }
    } catch (error) {
      const p = toApiProblem(error); setProblem(p);
      const current = (p.problem as unknown as { remaining_area_ha?: unknown }).remaining_area_ha;
      if (p.code === "cropcycle.household_area_exceeded" && typeof current === "string") setRemaining(current);
    } finally { setBusy(false); }
  };
  return <Dialog title={`Vụ canh tác — ${household.label || "Nông hộ"}`} onClose={() => { if (!busy) onClose(); }}>
    <p>Diện tích còn lại: <strong>{formatDecimal(remaining, 2)} ha</strong>. Mở vụ cần mạng; máy chủ kiểm lại diện tích khi lưu.</p>
    {canCreate && hasCapacity && !opening ? <button className="btn btn-p" onClick={() => setOpening(true)}>Mở vụ mới</button> : null}
    {canCreate && !hasCapacity ? <p role="status">{household.open_season_blocked_reason || "Không còn diện tích hoặc chưa tải lại được số liệu. Đóng và mở lại danh sách hộ để cập nhật."}</p> : null}
    {opening ? <form onSubmit={(e) => void submit(e)} className="stack">
      <fieldset disabled={busy} className="stack" style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <label className="field">Loại cây<select className="select" required value={crop} onChange={(e) => { setCrop(e.target.value); setVariety(""); setVersion(""); edit(); }}><option value="">Chọn loại cây</option>{metadata.data?.crops.filter((c) => !c.attrs.parent_code).map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></label>
        <label className="field">Giống<select className="select" required value={variety} onChange={(e) => { setVariety(e.target.value); setVersion(""); edit(); }}><option value="">Chọn giống</option>{metadata.data?.varieties.filter((v) => v.attrs.crop_code === crop).map((v) => <option key={v.code} value={v.code}>{v.name}</option>)}</select></label>
        <label className="field">Mùa vụ<select className="select" required value={seasonCode} onChange={(e) => { setSeasonCode(e.target.value); edit(); }}><option value="">Chọn mùa vụ</option>{metadata.data?.seasons.map((s) => <option key={s.code} value={s.code}>{s.name}</option>)}</select></label>
        <label className="field">Năm<input className="input" required type="number" min={2000} max={2100} value={year} onChange={(e) => { setYear(e.target.value); edit(); }} /></label>
        <label className="field">Diện tích vụ (ha)<input className="input" required inputMode="decimal" value={area} onChange={(e) => { setArea(e.target.value); edit(); }} placeholder="Ví dụ: 0.25" /></label>
        <label className="field">SOP đang áp dụng<select className="select" required value={selected} onChange={(e) => { setVersion(e.target.value); edit(); }}><option value="">Chọn SOP</option>{choices.data?.map((v) => <option key={v.id} value={v.id}>{v.label}</option>)}</select></label>
        <button type="button" className="btn btn-o btn-sm" onClick={() => { choices.reload(); setVersion(""); edit(); }}>Tải lại SOP</button>
        <label className="field">{choices.data?.find((v) => v.id === selected)?.anchor || "Mốc gốc"}<input className="input" required type="date" value={anchor} onChange={(e) => { setAnchor(e.target.value); edit(); }} /></label>
        {!planReady ? <p>Chọn giống, SOP, diện tích và mốc gốc để xem kế hoạch.</p> : preview.loading ? <Loading label="Đang tính kế hoạch…" /> : preview.error ? <ProblemAlert problem={preview.error} onRetry={preview.reload} /> : preview.data ? <SeasonPlan plan={preview.data} /> : null}
        <p className="small">Bạn tự chọn mốc gốc. Khi lưu, trình duyệt hỏi quyền vị trí; từ chối vẫn mở vụ được và ghi “Thiếu GPS”.</p>
        <button className="btn btn-p" disabled={metadata.loading || choices.loading || !selected || (planReady && (preview.loading || !!preview.error))} type="submit">{busy ? "Đang lưu…" : "Lưu vụ"}</button>
      </fieldset>
      {inputError ? <p role="alert">{inputError}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {metadata.error ? <ProblemAlert problem={metadata.error} onRetry={metadata.reload} /> : null}
      {choices.error ? <ProblemAlert problem={choices.error} onRetry={choices.reload} /> : null}
      {!choices.loading && crop && variety && choices.data?.length === 0 ? <p role="status">Chưa có SOP đang áp dụng phù hợp. Vui lòng liên hệ Admin.</p> : null}
    </form> : null}
    {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
    {list.loading ? <Loading /> : null}
    <ul>{list.items.map((s) => <li key={s.season_ref}><button className="btn btn-o" onClick={() => setDetail(s)}>{s.season_code} {s.season_year} · {s.crop_code} · {formatDecimal(s.area_ha, 2)} ha · {s.status === "open" ? "Đang canh tác" : s.status}</button></li>)}</ul>
    {!list.loading && !list.items.length ? <p>Chưa có vụ theo hộ.</p> : null}
    <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    {detail ? <section aria-label="Chi tiết vụ">
      <h3>{detail.season_code} {detail.season_year} · {detail.sop_snapshot.name}</h3>
      <p>{detail.sop_snapshot.anchor_name}: {detail.anchor_date} · {detail.gps_missing ? "Thiếu GPS" : "Đã ghi nhận vị trí"}</p>
      <p>SOP bản {detail.sop_snapshot.version_no} · Diện tích {formatDecimal(detail.area_ha, 2)} ha</p>
      <ol>{detail.sop_snapshot.steps.map((step, i) => <li key={i}>{step.name}</li>)}</ol>
      <HouseholdChecklist key={detail.season_ref} coopRef={coopRef} householdRef={household.household_ref} seasonRef={detail.season_ref} />
      {detailPlan.loading ? <Loading label="Đang tải kế hoạch…" /> : detailPlan.error ? <ProblemAlert problem={detailPlan.error} onRetry={detailPlan.reload} /> : detailPlan.data ? <SeasonPlan plan={detailPlan.data} /> : null}
    </section> : null}
  </Dialog>;
}
