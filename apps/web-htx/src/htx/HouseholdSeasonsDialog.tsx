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
import { householdAreaError, requestGPS, useSeasonMetadata, useSopChoices } from "./seasonOptions";
import { HouseholdChecklist } from "./HouseholdChecklist";
import { SeasonPlanEditor, type PlanChange, type PlanStage } from "./SeasonPlanEditor";
import { RunningSeasonPlan } from "./SeasonPlanCustomizer";
import { StopSeasonForm } from "./StopSeasonForm";
import { SelfMadePlanBuilder, type SelfMadePlan } from "./SelfMadePlanBuilder";
import type { CalendarPrefill } from "./seasonCalendarModel";

export { householdAreaError } from "./seasonOptions";

type S = components["schemas"];
type GPS = S["CropGPS"];

const SEASON_STATUS: Record<string, string> = { open: "Đang canh tác", completed: "Đã thu hoạch", cancelled: "Đã dừng vụ" };

export function HouseholdSeasonsDialog({ coopRef, household, calendarPrefill, onClose, onSaved }: {
  coopRef: string; household: HouseholdListItem; calendarPrefill?: CalendarPrefill; onClose: () => void; onSaved: () => void;
}) {
  const { api, principal } = useAuth();
  const canCreate = hasRole(principal, ROLES.platformAdmin, ROLES.htxMember) && matrixAllows(principal, "KHMV-FN-03", "C");
  // AH-70: dừng vụ — KHMV-FN-03 D; Đại diện không có nút.
  const canStop = hasRole(principal, ROLES.platformAdmin, ROLES.htxMember) && matrixAllows(principal, "KHMV-FN-03", "D");
  const [opening, setOpening] = useState(!!calendarPrefill);
  const [crop, setCrop] = useState(calendarPrefill?.crop_code ?? "");
  const [variety, setVariety] = useState("");
  const [seasonCode, setSeasonCode] = useState(calendarPrefill?.season_code ?? "");
  const [year, setYear] = useState(String(calendarPrefill?.season_year ?? new Date().getFullYear()));
  const [area, setArea] = useState("");
  const [anchor, setAnchor] = useState(calendarPrefill?.sow_from ?? "");
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
  // AH-139: sao chép giống và quy trình (kèm tùy chỉnh) từ một vụ trước của hộ.
  const [copyFrom, setCopyFrom] = useState<S["HouseholdSeason"] | null>(null);
  // AH-113 AC1: tùy chỉnh quy trình trước khi lưu vụ — gom lại, gửi kèm xem trước và mở vụ.
  const [planChanges, setPlanChanges] = useState<PlanChange[]>([]);
  const [customInfo, setCustomInfo] = useState<{ skipped: number[]; steps: Record<number, { window?: S["CropSOPWindow"] | null; materials?: S["CropSOPMaterial"][] | null }> }>({ skipped: [], steps: {} });
  // AH-162: quy trình tự tạo khi loại cây/giống chưa có SOP đang áp dụng.
  const [selfMadeMode, setSelfMadeMode] = useState(false);
  const [selfMade, setSelfMade] = useState<{ plan: SelfMadePlan; anchor: string } | null>(null);
  const resetPlan = () => { setPlanChanges([]); setCustomInfo({ skipped: [], steps: {} }); setSelfMadeMode(false); setSelfMade(null); };
  const path = { ref: coopRef, household_ref: household.household_ref };
  const list = usePagedList((cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path, query: { cursor, limit: 20 } } })), [api, coopRef, household.household_ref, revision]);
  const metadata = useSeasonMetadata(api);
  const choices = useSopChoices(api, metadata.data, coopRef, crop, variety);
  const selected = version || (choices.data?.length === 1 ? choices.data[0]?.id ?? "" : "");
  const planReady = !copyFrom && opening && canCreate && matrixAllows(principal, "KHMV-FN-04", "C") && !choices.loading &&
    !!crop && !!variety && (selfMadeMode ? !!selfMade : !!selected) && !householdAreaError(area) && /^\d{4}-\d{2}-\d{2}$/.test(anchor);
  const preview = useAsync(async () => planReady ? unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/season-plan-preview", {
    params: { path, header: { "Idempotency-Key": newIdempotencyKey() } },
    body: { crop_code: crop, variety_code: variety, ...(selfMade ? { self_made_plan: selfMade.plan } : { sop_version_id: selected }), area_ha: area, anchor_date: anchor, plan_changes: planChanges },
  })) : null, [api, coopRef, household.household_ref, planReady, crop, variety, selected, area, anchor, planChanges, selfMade]);
  const baseSteps = choices.data?.find((v) => v.id === selected)?.steps ?? [];
  const openStages: PlanStage[] = (preview.data?.stages ?? []).map((s) => ({ seq: s.seq, name: s.name, tasks: s.steps.map((t) => ({
    seq: t.seq, stage_seq: t.stage_seq, name: t.name, custom: t.custom, from_date: t.from_date, to_date: t.to_date, undated: t.undated,
    skipped: customInfo.skipped.includes(t.seq),
  })) }));
  // Thử thay đổi bằng một lần xem trước; máy chủ chấp nhận thì mới giữ.
  const changeOpening = async (change: PlanChange) => {
    const next = [...planChanges, change];
    const plan = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/season-plan-preview", {
      params: { path, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: crop, variety_code: variety, ...(selfMade ? { self_made_plan: selfMade.plan } : { sop_version_id: selected }), area_ha: area, anchor_date: anchor, plan_changes: next },
    }));
    setCustomInfo((info) => {
      const steps = { ...info.steps };
      let skipped = info.skipped;
      if (change.kind === "edit" && change.step_seq) steps[change.step_seq] = { window: change.window, materials: change.materials };
      if (change.kind === "add" && change.step) {
        const known = new Set(Object.keys(steps).map(Number));
        const seq = Math.max(0, ...plan.stages.flatMap((s) => s.steps).filter((t) => t.custom && !known.has(t.seq)).map((t) => t.seq));
        if (seq) steps[seq] = { window: change.step.window, materials: change.step.materials };
      }
      if (change.kind === "remove" && change.step_seq && !steps[change.step_seq]) skipped = [...skipped, change.step_seq];
      return { steps, skipped };
    });
    setPlanChanges(next);
    edit();
  };
  const openLookup = (seq: number) => customInfo.steps[seq] ?? baseSteps.find((s) => s.seq === seq);
  const detailPlan = useAsync(async () => detail ? unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/plan", {
    params: { path: { ...path, season_ref: detail.season_ref } },
  })) : null, [api, coopRef, household.household_ref, detail?.season_ref, detail?.current_event_id]);
  const edit = () => { setKey(newIdempotencyKey()); setInputError(null); setProblem(null); };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const invalid = householdAreaError(area);
    if (invalid) { setInputError(invalid); return; }
    if (selfMadeMode && !selfMade) { setInputError("Hoàn tất quy trình tự tạo (giai đoạn, việc, ngày hợp lệ)."); return; }
    if (!selfMadeMode && !selected) { setInputError("Chọn SOP đang áp dụng, hoặc tự tạo quy trình nếu loại cây chưa có SOP."); return; }
    if (typeof navigator !== "undefined" && navigator.onLine === false) { setInputError("Mở vụ cần mạng. Dữ liệu form được giữ lại để thử lại."); return; }
    setBusy(true); setProblem(null); setInputError(null);
    try {
      const point = gps ?? await requestGPS();
      setGPS(point);
      const result = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", {
        params: { path, header: { "Idempotency-Key": key } },
        body: { crop_code: crop, season_code: seasonCode, season_year: Number(year), variety_code: variety, area_ha: area, anchor_date: anchor, ...(selfMade ? { self_made_plan: selfMade.plan, self_made_anchor: selfMade.anchor } : { sop_version_id: selected }), gps: point, channel: "web", plan_changes: planChanges, copy_from_season_ref: copyFrom?.season_ref ?? null },
      }));
      setDetail(result.season); setOpening(false); setRevision((v) => v + 1); onSaved(); resetPlan(); setCopyFrom(null);
      setArea(""); setKey(newIdempotencyKey()); setGPS(null);
      try {
        const households = await collectPages((cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coopRef }, query: { cursor, limit: 200 } } })));
        const fresh = households.find((h) => h.household_ref === household.household_ref);
        setRemaining(fresh?.remaining_area_ha ?? null); setHasCapacity(fresh?.can_open_season ?? false);
      } catch { setRemaining(null); setHasCapacity(false); }
    } catch (error) {
      const p = toApiProblem(error); setProblem(p);
      const current = p.problem.extensions?.remaining_area_ha;
      if (p.code === "cropcycle.household_area_exceeded" && typeof current === "string") setRemaining(current);
    } finally { setBusy(false); }
  };
  return <Dialog title={`Vụ canh tác — ${household.label || "Nông hộ"}`} onClose={() => { if (!busy) onClose(); }}>
    <p>Diện tích còn lại: <strong>{formatDecimal(remaining, 2)} ha</strong>. Mở vụ cần mạng; máy chủ kiểm lại diện tích khi lưu.</p>
    {canCreate && hasCapacity && !opening ? <button className="btn btn-p" onClick={() => setOpening(true)}>Mở vụ mới</button> : null}
    {canCreate && !hasCapacity ? <p role="status">{household.open_season_blocked_reason || "Không còn diện tích hoặc chưa tải lại được số liệu. Đóng và mở lại danh sách hộ để cập nhật."}</p> : null}
    {opening ? <form onSubmit={(e) => void submit(e)} className="stack">
      <fieldset disabled={busy} className="stack" style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        {copyFrom ? <section aria-label="Sao chép từ vụ trước" className="alert alert-info">
          <p>Sao chép từ vụ {copyFrom.season_code} {copyFrom.season_year}: loại cây {copyFrom.crop_code}, giống {copyFrom.variety_code}, quy trình {copyFrom.sop_snapshot.name} bản {copyFrom.sop_snapshot.version_no}{copyFrom.sop_snapshot.custom ? " (kèm tùy chỉnh của vụ)" : ""}. Ngày dự kiến tính theo mốc gốc mới khi lưu.</p>
          <button type="button" className="btn btn-o btn-sm" onClick={() => { setCopyFrom(null); setCrop(""); setVariety(""); setVersion(""); resetPlan(); edit(); }}>Bỏ sao chép</button>
        </section> : <>
        <label className="field">Loại cây<select className="select" required value={crop} onChange={(e) => { setCrop(e.target.value); setVariety(""); setVersion(""); resetPlan(); edit(); }}><option value="">Chọn loại cây</option>{metadata.data?.crops.filter((c) => !c.attrs.parent_code).map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></label>
        <label className="field">Giống<select className="select" required value={variety} onChange={(e) => { setVariety(e.target.value); setVersion(""); resetPlan(); edit(); }}><option value="">Chọn giống</option>{metadata.data?.varieties.filter((v) => v.attrs.crop_code === crop).map((v) => <option key={v.code} value={v.code}>{v.name}</option>)}</select></label>
        </>}
        <label className="field">Mùa vụ<select className="select" required value={seasonCode} onChange={(e) => { setSeasonCode(e.target.value); edit(); }}><option value="">Chọn mùa vụ</option>{metadata.data?.seasons.map((s) => <option key={s.code} value={s.code}>{s.name}</option>)}</select></label>
        <label className="field">Năm<input className="input" required type="number" min={2000} max={2100} value={year} onChange={(e) => { setYear(e.target.value); edit(); }} /></label>
        <label className="field">Diện tích vụ (ha)<input className="input" required inputMode="decimal" value={area} onChange={(e) => { setArea(e.target.value); edit(); }} placeholder="Ví dụ: 0.25" /></label>
        {!copyFrom && !selfMadeMode ? <>
        <label className="field">SOP đang áp dụng<select className="select" required value={selected} onChange={(e) => { setVersion(e.target.value); resetPlan(); edit(); }}><option value="">Chọn SOP</option>{choices.data?.map((v) => <option key={v.id} value={v.id}>{v.label}</option>)}</select></label>
        <button type="button" className="btn btn-o btn-sm" onClick={() => { choices.reload(); setVersion(""); resetPlan(); edit(); }}>Tải lại SOP</button>
        </> : null}
        <label className="field">{copyFrom?.sop_snapshot.anchor_name || choices.data?.find((v) => v.id === selected)?.anchor || "Mốc gốc"}<input className="input" required type="date" value={anchor} onChange={(e) => { setAnchor(e.target.value); edit(); }} /></label>
        {copyFrom ? null : !planReady ? <p>Chọn giống, SOP, diện tích và mốc gốc để xem kế hoạch.</p> : preview.loading ? <Loading label="Đang tính kế hoạch…" /> : preview.error ? <ProblemAlert problem={preview.error} onRetry={preview.reload} /> : preview.data ? <SeasonPlan plan={preview.data} /> : null}
        <p className="small">Bạn tự chọn mốc gốc. Khi lưu, trình duyệt hỏi quyền vị trí; từ chối vẫn mở vụ được và ghi “Thiếu GPS”.</p>
        <button className="btn btn-p" disabled={metadata.loading || choices.loading || (selfMadeMode ? !selfMade : !selected) || (planReady && (preview.loading || !!preview.error))} type="submit">{busy ? "Đang lưu…" : "Lưu vụ"}</button>
      </fieldset>
      {inputError ? <p role="alert">{inputError}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {metadata.error ? <ProblemAlert problem={metadata.error} onRetry={metadata.reload} /> : null}
      {choices.error ? <ProblemAlert problem={choices.error} onRetry={choices.reload} /> : null}
      {!copyFrom && !choices.loading && crop && variety && choices.data?.length === 0 ? <div role="status" className="stack">
        <p>Chưa có SOP đang áp dụng phù hợp. Bạn có thể tự tạo quy trình chỉ cho vụ này, hoặc liên hệ Admin để có SOP chung.</p>
        {!selfMadeMode ? <button type="button" className="btn btn-o btn-sm" onClick={() => { setSelfMadeMode(true); edit(); }}>Tự tạo quy trình</button> : null}
      </div> : null}
      {selfMadeMode && !copyFrom ? <SelfMadePlanBuilder onChange={(plan, anchorName) => { setSelfMade(plan ? { plan, anchor: anchorName } : null); setPlanChanges([]); edit(); }} /> : null}
    </form> : null}
    {opening && planReady && preview.data ? <SeasonPlanEditor stages={openStages} lookup={openLookup} onChange={changeOpening} disabled={busy} /> : null}
    {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
    {list.loading ? <Loading /> : null}
    <ul>{list.items.map((s) => <li key={s.season_ref}><button className="btn btn-o" onClick={() => setDetail(s)}>{s.season_code} {s.season_year} · {s.crop_code} · {formatDecimal(s.area_ha, 2)} ha · {SEASON_STATUS[s.status] ?? s.status}</button>
      {canCreate && hasCapacity && s.status !== "cancelled" ? <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => { setCopyFrom(s); setCrop(s.crop_code); setVariety(s.variety_code); setVersion(s.sop_snapshot.version_id); resetPlan(); setOpening(true); edit(); }}>Sao chép vụ này</button> : null}</li>)}</ul>
    {!list.loading && !list.items.length ? <p>Chưa có vụ theo hộ.</p> : null}
    <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    {detail ? <section aria-label="Chi tiết vụ">
      <h3>{detail.season_code} {detail.season_year} · {detail.sop_snapshot.name}</h3>
      <p>{detail.sop_snapshot.anchor_name}: {detail.anchor_date} · {detail.gps_missing ? "Thiếu GPS" : "Đã ghi nhận vị trí"}</p>
      <p>SOP bản {detail.sop_snapshot.version_no} · Diện tích {formatDecimal(detail.area_ha, 2)} ha · {SEASON_STATUS[detail.status] ?? detail.status}</p>
      {detail.status === "cancelled" && detail.stop_reason ? <p role="status">Đã dừng vụ: {detail.stop_reason}</p> : null}
      {canStop && detail.status === "open" ? <StopSeasonForm key={detail.current_event_id} coopRef={coopRef} season={detail} onStopped={(s) => { setDetail(s); setRevision((v) => v + 1); onSaved(); }} /> : null}
      <ol>{detail.sop_snapshot.steps.map((step, i) => <li key={i}>{step.name}</li>)}</ol>
      <HouseholdChecklist key={`${detail.season_ref}:${detail.current_event_id}`} coopRef={coopRef} householdRef={household.household_ref} seasonRef={detail.season_ref} season={detail} />
      <RunningSeasonPlan coopRef={coopRef} householdRef={household.household_ref} season={detail} onSeason={(s) => { setDetail(s); setRevision((v) => v + 1); }} />
      {detailPlan.loading ? <Loading label="Đang tải kế hoạch…" /> : detailPlan.error ? <ProblemAlert problem={detailPlan.error} onRetry={detailPlan.reload} /> : detailPlan.data ? <SeasonPlan plan={detailPlan.data} /> : null}
    </section> : null}
  </Dialog>;
}
