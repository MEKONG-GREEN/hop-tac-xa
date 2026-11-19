"use client";

import { useState, type FormEvent } from "react";
import { householdsPage } from "@/api/adapters/cooperatives";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages, usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { HouseholdListItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { toApiProblem, useAsync, useRequestKey } from "@/lib/useAsync";
import { HouseholdSeasonsDialog } from "./HouseholdSeasonsDialog";
import { useCatalogNames } from "./common";
import { todayVN } from "./rules";
import { CoopPicker, useHtxScope } from "./scope";
import { calendarDraftError, calendarRefPattern, type CalendarDraft, type SeasonCalendar } from "./seasonCalendarModel";

function CalendarForm({ coop, current, onClose, onSaved }: { coop: string; current?: SeasonCalendar; onClose: () => void; onSaved: (c: SeasonCalendar) => void }) {
  const { api } = useAuth();
  const [draft, setDraft] = useState<CalendarDraft>(() => ({ crop_code: current?.crop_code ?? "", season_code: current?.season_code ?? "", season_year: current?.season_year ?? Number(todayVN().slice(0, 4)), sow_from: current?.sow_from ?? "", sow_to: current?.sow_to ?? "", recipients: current?.recipients ?? [] }));
  const [key, setKey] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const metadata = useAsync(async () => {
    const load = (catalog: "crop" | "season") => collectPages((cursor) => unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog }, query: { cursor, limit: 200 } } })));
    const [crops, seasons] = await Promise.all([load("crop"), load("season")]);
    return { crops: crops.filter((c) => !c.attrs?.parent_code && c.active !== false), seasons: seasons.filter((c) => c.active !== false) };
  }, [api]);
  const recipients = usePagedList(async (cursor) => {
    const page = await unwrap(api.GET("/v1/cooperatives/{ref}/season-calendar-recipients", { params: { path: { ref: coop }, query: { offset: cursor ? Number(cursor) : 0, limit: 50 } } }));
    return { items: page.items, next_cursor: page.next_offset ? String(page.next_offset) : null };
  }, [api, coop]);
  function edit(patch: Partial<CalendarDraft>) { setDraft((d) => ({ ...d, ...patch })); setKey(newIdempotencyKey()); setError(null); setValidation(null); }
  async function submit(e: FormEvent) {
    e.preventDefault(); if (busy) return;
    const invalid = calendarDraftError(draft); if (invalid) { setValidation(invalid); return; }
    setBusy(true); setError(null);
    try {
      const result = current
        ? await unwrap(api.POST("/v1/cooperatives/{ref}/season-calendars/{calendar_ref}/revisions", { params: { path: { ref: coop, calendar_ref: current.calendar_ref }, header: { "Idempotency-Key": key } }, body: { ...draft, expected_event_id: current.current_event_id } }))
        : await unwrap(api.POST("/v1/cooperatives/{ref}/season-calendars", { params: { path: { ref: coop }, header: { "Idempotency-Key": key } }, body: draft }));
      onSaved(result);
    } catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  }
  const selected = new Set(draft.recipients);
  const known = new Set(recipients.items.map((r) => r.actor_ref));
  return <Dialog title={current ? "Sửa lịch thời vụ" : "Lập và phát lịch thời vụ"} onClose={() => { if (!busy) onClose(); }}>
    <form onSubmit={submit}>
      <fieldset disabled={busy} className="stack">
        <label className="field">Loại cây<select className="select" required value={draft.crop_code} onChange={(e) => edit({ crop_code: e.target.value })}><option value="">Chọn loại cây</option>{metadata.data?.crops.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></label>
        <label className="field">Mùa vụ<select className="select" required value={draft.season_code} onChange={(e) => edit({ season_code: e.target.value })}><option value="">Chọn mùa vụ</option>{metadata.data?.seasons.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></label>
        <label className="field">Năm<input className="input" type="number" required min={2000} max={2100} value={draft.season_year} onChange={(e) => edit({ season_year: Number(e.target.value) })} /></label>
        <label className="field">Xuống giống từ<input className="input" type="date" required value={draft.sow_from} onChange={(e) => edit({ sow_from: e.target.value })} /></label>
        <label className="field">Đến ngày<input className="input" type="date" required value={draft.sow_to} onChange={(e) => edit({ sow_to: e.target.value })} /></label>
        <fieldset><legend>Thành viên nhận lịch ({selected.size}/500)</legend>
          {recipients.items.map((r) => <label className="row" key={r.actor_ref}><input type="checkbox" checked={selected.has(r.actor_ref)} disabled={!selected.has(r.actor_ref) && selected.size >= 500} onChange={(e) => edit({ recipients: e.target.checked ? [...draft.recipients, r.actor_ref] : draft.recipients.filter((id) => id !== r.actor_ref) })} />{r.label || `Chưa có tên · ${r.actor_ref.slice(0, 8)}`}</label>)}
          {draft.recipients.filter((id) => !known.has(id)).map((id) => <label className="row" key={id}><input type="checkbox" checked onChange={() => edit({ recipients: draft.recipients.filter((ref) => ref !== id) })} />Đã chọn · {id.slice(0, 8)} (chưa có trong trang danh bạ)</label>)}
          <LoadMore hasMore={recipients.hasMore} loading={recipients.loading} onClick={recipients.loadMore} />
          {!recipients.loading && !recipients.items.length && !recipients.hasMore ? <p>Không có Thành viên đang hoạt động.</p> : null}
        </fieldset>
        <p className="small">Khung ngày chỉ hướng dẫn, không giới hạn ngày mở vụ. Phát hoặc sửa lịch sẽ xếp thông báo tới Thành viên được chọn.</p>
        <button className="btn btn-p" type="submit" disabled={metadata.loading || !!metadata.error || !!recipients.error}>{busy ? "Đang lưu…" : current ? "Lưu và phát lại" : "Phát lịch"}</button>
      </fieldset>
      {validation ? <p role="alert">{validation}</p> : null}
      {error ? <ProblemAlert problem={error} /> : null}
      {metadata.error ? <ProblemAlert problem={metadata.error} onRetry={metadata.reload} /> : null}
      {recipients.error ? <ProblemAlert problem={recipients.error} onRetry={recipients.reload} /> : null}
    </form>
  </Dialog>;
}

function CalendarHouseholdPicker({ calendar, onClose }: { calendar: SeasonCalendar; onClose: () => void }) {
  const { api } = useAuth();
  const [selected, setSelected] = useState<HouseholdListItem | null>(null);
  const list = usePagedList((cursor) => householdsPage(api, calendar.cooperative_ref, cursor), [api, calendar.cooperative_ref]);
  if (selected) return <HouseholdSeasonsDialog coopRef={calendar.cooperative_ref} household={selected} calendarPrefill={calendar} onClose={onClose} onSaved={list.reload} />;
  return <Dialog title="Chọn nông hộ để mở vụ theo lịch" onClose={onClose}>
    <p>Ngày đầu khung được điền sẵn. Bạn có thể đổi ngày, chọn giống và SOP khi mở vụ.</p>
    {list.loading ? <Loading /> : null}
    {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
    <ul>{list.items.map((h) => <li key={h.household_ref}><button type="button" className="btn btn-o" disabled={!h.can_open_season} onClick={() => setSelected(h)}>{h.label || `Nông hộ ${h.household_ref.slice(0, 8)}`}</button>{!h.can_open_season ? <span> · Không mở được vụ mới</span> : null}</li>)}</ul>
    <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
  </Dialog>;
}

function CalendarDetail({ calendarRef, refresh, coopName, onEdit, onCancel }: { calendarRef: string; refresh: number; coopName: (ref: string) => string; onEdit: (v: SeasonCalendar) => void; onCancel: (v: SeasonCalendar) => void }) {
  const { api, principal } = useAuth();
  const crops=useCatalogNames("crop"),seasons=useCatalogNames("season");
  const [opening, setOpening] = useState(false);
  const detail = useAsync(() => unwrap(api.GET("/v1/cropcycle/calendars/{calendar_ref}", { params: { path: { calendar_ref: calendarRef } } })), [api, calendarRef, refresh]);
  if (detail.loading) return <Loading />;
  if (detail.error) return <ProblemAlert problem={detail.error} onRetry={detail.reload} />;
  const c = detail.data;
  if (!c) return null;
  return <section aria-label="Chi tiết lịch thời vụ" className="card stack">
    <h2>{crops.get(c.crop_code)??c.crop_code} · {seasons.get(c.season_code)??c.season_code}/{c.season_year}</h2>
    <p>HTX: {coopName(c.cooperative_ref)}</p>
    <p>Từ {formatDate(c.sow_from)} đến {formatDate(c.sow_to)} · {c.status === "cancelled" ? "Đã hủy" : "Đã phát"}</p>
    <p>Khung ngày chỉ hướng dẫn; mở vụ ngoài khung vẫn được. Hủy lịch không đổi vụ đã mở.</p>
    {c.status === "published" && c.can_manage ? <div className="row">{matrixAllows(principal,"KHMV-FN-03-LICH","U")?<button className="btn btn-o" onClick={() => onEdit(c)}>Sửa lịch</button>:null}{matrixAllows(principal,"KHMV-FN-03-LICH","D")?<button className="btn btn-o" onClick={() => onCancel(c)}>Hủy lịch</button>:null}</div> : null}
    {c.status === "published" && !c.can_manage && matrixAllows(principal, "KHMV-FN-03", "C") ? <button className="btn btn-p" onClick={() => setOpening(true)}>Mở vụ theo lịch</button> : null}
    {opening && c.status === "published" ? <CalendarHouseholdPicker calendar={c} onClose={() => setOpening(false)} /> : null}
  </section>;
}

function CancelCalendar({ calendar, onClose, onSaved }: { calendar: SeasonCalendar; onClose: () => void; onSaved: () => void }) {
  const { api } = useAuth(); const [key] = useState(newIdempotencyKey); const [busy, setBusy] = useState(false); const [error, setError] = useState<ApiProblem | null>(null);
  async function cancel() {
    if (busy) return; setBusy(true); setError(null);
    try { await unwrap(api.POST("/v1/cooperatives/{ref}/season-calendars/{calendar_ref}/cancel", { params: { path: { ref: calendar.cooperative_ref, calendar_ref: calendar.calendar_ref }, header: { "Idempotency-Key": key } }, body: { expected_event_id: calendar.current_event_id } })); onSaved(); }
    catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  }
  return <Dialog title="Hủy lịch thời vụ?" onClose={() => { if (!busy) onClose(); }}><p>Lịch sẽ ẩn khỏi danh sách Thành viên. Không gửi thông báo hủy và không thay đổi vụ đã mở.</p><button className="btn btn-p" disabled={busy} onClick={() => void cancel()}>{busy ? "Đang hủy…" : "Xác nhận hủy lịch"}</button>{error ? <ProblemAlert problem={error} /> : null}</Dialog>;
}

function CalendarWorkspace({ initialRef = "" }: { initialRef?: string }) {
  const { api, principal } = useAuth(); const scope = useHtxScope();
  const crops=useCatalogNames("crop"),seasons=useCatalogNames("season");
  const canRead = matrixAllows(principal, "KHMV-FN-03-LICH", "R");
  const canCreate = matrixAllows(principal, "KHMV-FN-03-LICH", "C");
  const [selected, setSelected] = useState(calendarRefPattern.test(initialRef) ? initialRef : "");
  const [refresh, setRefresh] = useState(0); const [editing, setEditing] = useState<SeasonCalendar | "new" | null>(null); const [cancelling, setCancelling] = useState<SeasonCalendar | null>(null);
  const list = usePagedList((after) => !canRead || !scope.coopRef ? Promise.resolve({ items: [] as SeasonCalendar[], next_cursor: null }) : unwrap(api.GET("/v1/cooperatives/{ref}/season-calendars", { params: { path: { ref: scope.coopRef }, query: { after, limit: 50 } } })), [api, canRead, scope.coopRef, refresh]);
  function saved(c?: SeasonCalendar) { setEditing(null); setCancelling(null); if (c) setSelected(c.calendar_ref); setRefresh((v) => v + 1); }
  if (!canRead) return <p role="alert">Bạn không có quyền xem lịch thời vụ.</p>;
  return <>
    <PageHead title="Lịch thời vụ" subtitle="Khung xuống giống chung của HTX" actions={<button className="btn btn-o" onClick={()=>setRefresh((v)=>v+1)}>Tải lại</button>} />
    <CoopPicker scope={{ ...scope, setCoopRef: (ref) => { setSelected(""); setEditing(null); setCancelling(null); scope.setCoopRef(ref); } }} allowAll={false} />
    {canCreate && scope.coopRef ? <button className="btn btn-p" onClick={() => setEditing("new")}>Lập lịch</button> : null}
    {!scope.coopRef ? <p>Chọn HTX để xem lịch.</p> : null}
    {initialRef && !calendarRefPattern.test(initialRef) ? <p role="alert">Liên kết lịch không hợp lệ.</p> : null}
    {list.loading ? <Loading /> : list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : scope.coopRef && !list.items.length ? <p>Chưa có lịch thời vụ.</p> : null}
    <ul>{list.items.map((c) => <li key={c.calendar_ref}><button className="btn btn-o" onClick={() => setSelected(c.calendar_ref)}>{crops.get(c.crop_code)??c.crop_code} · {seasons.get(c.season_code)??c.season_code}/{c.season_year} · {formatDate(c.sow_from)}–{formatDate(c.sow_to)}{c.status === "cancelled" ? " · Đã hủy" : ""}</button></li>)}</ul>
    <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    {selected ? <CalendarDetail key={selected} calendarRef={selected} refresh={refresh} coopName={(ref)=>scope.coopName(ref)} onEdit={setEditing} onCancel={setCancelling} /> : null}
    {editing ? <CalendarForm key={editing === "new" ? scope.coopRef : editing.current_event_id} coop={editing === "new" ? scope.coopRef : editing.cooperative_ref} current={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} onSaved={saved} /> : null}
    {cancelling ? <CancelCalendar calendar={cancelling} onClose={() => setCancelling(null)} onSaved={() => saved()} /> : null}
  </>;
}

export function SeasonCalendars({initialRef=""}:{initialRef?:string}){
  const {principal}=useAuth();
  const session=useRequestKey([principal]);
  return <CalendarWorkspace key={session.value} initialRef={initialRef}/>;
}
