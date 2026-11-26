"use client";

import { useEffect, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import type { ApiProblem } from "@/api/problem";
import { HouseholdTaskDetail } from "./HouseholdTaskDetail";
import { cachedTaskRead, queuedTasks, flushTasks, enqueueTask, discardTask, taskRejectMessage } from "./taskJournalQueue";
import { snapshotLookup } from "./SeasonPlanEditor";
import { HouseholdTaskJournal } from "./HouseholdTaskJournal";
import { AdhocTaskForm } from "./AdhocTaskForm";
import { TaskBatchForm } from "./TaskBatchForm";

/** AH-114: chu kỳ tự thử lại đồng bộ khi còn nhật ký chờ gửi. */
export const TASK_RETRY_MS = 120_000;

type S = components["schemas"];
const LABEL: Record<string, string> = { upcoming: "Chưa đến hạn", due: "Đến hạn", overdue: "Trễ hạn", waiting_anchor: "Chờ mốc", done: "Đã xong", done_early: "Xong sớm", done_on_time: "Xong đúng hạn", done_late: "Xong trễ", skipped: "Bỏ qua", not_started: "Chưa thực hiện", in_progress: "Đang thực hiện" };

// A bounded, best-effort capture. Cancellation of the confirmation never posts.
export function captureGPS(): Promise<S["CropGPS"]> {
  if (!navigator.geolocation) return Promise.resolve({ source: "no_device" });
  return new Promise((resolve) => {
    let finished = false;
    const timer = setTimeout(() => finish({ source: "no_device" }), 2000);
    const finish = (gps: S["CropGPS"]) => { if (!finished) { finished = true; clearTimeout(timer); resolve(gps); } };
    try { navigator.geolocation.getCurrentPosition((p) => finish({ source: "device", lon: p.coords.longitude.toFixed(7), lat: p.coords.latitude.toFixed(7), accuracy_m: p.coords.accuracy.toFixed(2) }), () => finish({ source: "denied" }), { timeout: 2000, maximumAge: 60000 }); }
    catch { finish({ source: "denied" }); }
  });
}

export function HouseholdChecklist({ coopRef, householdRef, seasonRef, season, onOpenPlan }: { coopRef: string; householdRef: string; seasonRef: string; season?: S["HouseholdSeason"]; onOpenPlan?: () => void }) {
  const { api, principal } = useAuth();
  const canWrite = matrixAllows(principal, "KHMV-FN-05", "C");
  const path = { ref: coopRef, household_ref: householdRef, season_ref: seasonRef };
  const load = useAsync(() => {
    const read = () => unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/checklist", { params: { path } }));
    if (!canWrite || !principal) return read();
    return cachedTaskRead(principal, `checklist:${coopRef}:${householdRef}:${seasonRef}`, read).then(async (value) => {
      try { await cachedTaskRead(principal, `photo-key:${householdRef}`, () => unwrap(api.GET("/v1/households/{ref}/photo-key", { params: { path: { ref: householdRef } } }))); } catch { /* Camera reports a missing key; journals without photos remain usable. */ }
      return value;
    });
  }, [api, principal, canWrite, coopRef, householdRef, seasonRef]);
  const [updated, setUpdated] = useState<S["HouseholdChecklist"] | null>(null);
  const [pending, setPending] = useState<{ seq: number; name: string; event: string; key: string; gps: Promise<S["CropGPS"]> } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  const [detail, setDetail] = useState<S["HouseholdTaskProgress"] | null>(null);
  const [batch, setBatch] = useState<S["HouseholdTaskProgress"] | null>(null);
  const [queueRevision, setQueueRevision] = useState(0);
  const [adhoc, setAdhoc] = useState(false);
  // AH-124: checklist nhóm theo giai đoạn, mặc định đóng.
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const toggleStage = (seq: number) => setExpanded((cur) => { const next = new Set(cur); if (next.has(seq)) next.delete(seq); else next.add(seq); return next; });
  const queue = useAsync(() => canWrite && principal && typeof indexedDB !== "undefined" ? queuedTasks(principal, coopRef, householdRef, seasonRef) : Promise.resolve([]), [principal, canWrite, coopRef, householdRef, seasonRef, queueRevision]);
  const syncQueued = async () => {
    if (!principal || !canWrite || busy) return;
    setBusy(true); setError(null);
    try { const items = await queuedTasks(principal, coopRef, householdRef, seasonRef); await flushTasks(api, principal, items); load.reload(); }
    catch (e) { setError(toApiProblem(e)); }
    finally { setBusy(false); setQueueRevision((r) => r + 1); }
  };
  const syncLatest = useRef(syncQueued);
  useEffect(() => { syncLatest.current = syncQueued; });
  const [online, setOnline] = useState(() => typeof navigator === "undefined" || navigator.onLine !== false);
  useEffect(() => {
    const onOnline = () => { setOnline(true); void syncLatest.current(); };
    const onOffline = () => setOnline(false);
    window.addEventListener("online", onOnline); window.addEventListener("offline", onOffline);
    return () => { window.removeEventListener("online", onOnline); window.removeEventListener("offline", onOffline); };
  }, []);
  // AH-114 AC3: còn bản chờ gửi thì tự thử lại theo chu kỳ; bản lỗi giữ nguyên trên máy.
  const waiting = (queue.data ?? []).some((r) => !r.error);
  useEffect(() => {
    if (!waiting) return;
    const timer = setInterval(() => { if (navigator.onLine !== false) void syncLatest.current(); }, TASK_RETRY_MS);
    return () => clearInterval(timer);
  }, [waiting]);
  const discard = async (id: string) => {
    if (!principal || !window.confirm("Xóa bản ghi chưa đồng bộ? Bản ghi chỉ còn trên máy và không khôi phục được.")) return;
    try { await discardTask(principal, id); } catch (e) { setError(toApiProblem(e)); } finally { setQueueRevision((r) => r + 1); }
  };
  const data = !load.loading && !load.error && load.data ? load.data : updated ?? undefined;
  const refresh = () => { setUpdated(null); load.reload(); };
  const confirm = async () => {
    if (!pending || busy) return;
    setBusy(true); setError(null);
    try {
      const gps = await pending.gps;
      const result = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-completions", {
        params: { path, header: { "Idempotency-Key": pending.key } },
        body: { step_seq: pending.seq, expected_season_event_id: pending.event, gps, channel: "web" },
      }));
      setUpdated(result.checklist); setPending(null); load.reload();
    } catch (e) {
      const problem = toApiProblem(e);
      // AH-114 AC1: mất mạng khi tích nhanh → lưu trên máy "Chờ đồng bộ" như bản ghi việc "Hoàn tất" hôm nay.
      if (problem.status === 0 && principal && season && typeof indexedDB !== "undefined") {
        try {
          const recommended = snapshotLookup(season.sop_snapshot)(pending.seq)?.materials ?? [];
          await enqueueTask(principal, { id: pending.key, coop: coopRef, household: householdRef, season: seasonRef, photos: [], input: {
            expected_season_event_id: pending.event, step_seq: pending.seq, state: "done",
            done_on: new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date()),
            materials: recommended.map((m) => ({ material_code: m.material_code ?? "", name: m.name, unit: m.unit, dose_per_ha: m.dose_per_ha })),
            file_refs: [], note: "", backfill_reason: "", zero_confirmed: false, gps: await pending.gps, channel: "web",
          } });
          setOnline(false); setPending(null); setQueueRevision((r) => r + 1);
          return;
        } catch (queued) { setError(toApiProblem(queued)); return; }
      }
      setError(problem);
      if ((problem.status === 409 && problem.code !== "idempotency_in_progress") || problem.code === "cropcycle.task_season_state" || problem.code === "cropcycle.task_stage_locked") { setPending(null); refresh(); }
    } finally { setBusy(false); }
  };
  return <section aria-label="Checklist công việc">
    <h3>Checklist công việc</h3>
    {!online ? <div role="status" className="alert alert-warn">Đang ngoại tuyến — dữ liệu sẽ đồng bộ khi có mạng</div> : null}
    {load.loading && !updated ? <Loading /> : null}
    {load.error ? <ProblemAlert problem={load.error} onRetry={refresh} /> : null}
    {error ? <ProblemAlert problem={error} /> : null}
    {!queue.loading && queue.data?.length ? <section aria-label="Nhật ký chờ đồng bộ"><p>{queue.data.length} bản ghi lưu trên máy, chưa đánh dấu xong trên máy chủ.</p><ul>{queue.data.map((r) => <li key={r.id}>Việc {r.input.step_seq} · {r.input.done_on} · {r.input.state === "partial" ? "Làm tiếp" : "Hoàn tất"}{r.error ? <> · {taskRejectMessage(r.error)} <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void discard(r.id)}>Xóa</button></> : " · Chờ đồng bộ"}</li>)}</ul><button type="button" disabled={busy} onClick={() => void syncQueued()}>Đồng bộ nhật ký</button></section> : null}
    {queue.error ? <ProblemAlert problem={queue.error} onRetry={queue.reload} /> : null}
    {data ? <>
      <p>Tiến độ: <strong>{data.progress.percent == null ? "—" : `${data.progress.percent}%`}</strong> · {data.progress.done}/{data.progress.total - data.progress.skipped} việc · Đến hạn: <strong>{data.progress.due}</strong> · Trễ: <strong>{data.progress.overdue}</strong></p>
      {onOpenPlan ? <button type="button" className="btn btn-o btn-sm" onClick={onOpenPlan}>Kế hoạch mùa vụ</button> : null}
      {data.progress.stages.map((stage) => <section key={stage.seq} aria-label={stage.name}>
        <h4><button type="button" className="btn btn-sm" aria-expanded={expanded.has(stage.seq)} onClick={() => toggleStage(stage.seq)}>{expanded.has(stage.seq) ? "▾" : "▸"} {stage.name} · {stage.status === "done" ? "Đã xong" : LABEL[stage.status]} · {stage.tasks.length} việc</button></h4>
        {expanded.has(stage.seq) ? <ul>{stage.tasks.map((task) => {
          const record = data.records.find((r) => r.step_seq === task.seq && r.state === "done");
          return <li key={task.seq}>
            {canWrite && task.can_complete ? <label><input type="checkbox" aria-label={`Hoàn thành ${task.name}`} checked={pending?.seq === task.seq} disabled={busy} onChange={() => { setError(null); setPending(pending?.seq === task.seq ? null : { seq: task.seq, name: task.name, event: data.current_event_id, key: newIdempotencyKey(), gps: captureGPS() }); }} /> {task.name}</label> : <span>{task.name}</span>}
            <span className="chip c-info">{LABEL[task.completion_status] ?? task.completion_status}</span>
            {canWrite && task.can_complete && season ? <button type="button" disabled={busy} onClick={() => { setPending(null); setDetail(task); }}>Ghi chi tiết {task.name}</button> : null}
            {canWrite && task.can_complete && season ? <button type="button" disabled={busy} onClick={() => { setPending(null); setBatch(task); }}>Ghi nhiều vụ {task.name}</button> : null}
            {task.undated ? <span> · Chưa đặt ngày</span> : task.from_date ? <span> · {formatDate(task.from_date)}{task.to_date ? ` – ${formatDate(task.to_date)}` : " trở đi"}</span> : null}
            {record ? <span> · Làm xong {formatDate(record.done_on)}{record.gps_missing ? " · Thiếu GPS" : " · Đã ghi nhận vị trí"}{record.inputs.map((i) => ` · ${i.name}: ${i.qty} ${i.unit}`).join("")}</span> : null}
            <ul>{data.records.filter((r) => r.step_seq === task.seq && r.details).map((r) => <li key={r.id}>{formatDate(r.done_on)} · {r.state === "partial" ? "Chưa xong, làm tiếp" : "Đã xong"}{r.details?.backfilled ? " · Ghi bù" : ""}{r.details?.actual_materials.map((m) => ` · ${m.name}: ${m.dose_per_ha} ${m.unit}/ha · tổng ${m.quantity} ${m.unit}`).join("")} · {r.note} · {r.file_refs?.length ?? 0} ảnh</li>)}</ul>
          </li>;
        })}</ul> : null}
      </section>)}
      <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => { setPending(null); refresh(); }}>Tải lại checklist</button>
      {canWrite && (season?.status ?? "open") !== "cancelled" ? adhoc
        ? <AdhocTaskForm path={path} event={data.current_event_id} stages={data.progress.stages} onClose={() => setAdhoc(false)} onSaved={(c) => { setAdhoc(false); setUpdated(c); load.reload(); }} />
        : <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => setAdhoc(true)}>Ghi việc phát sinh</button> : null}
      <HouseholdTaskJournal path={path} checklist={data} writable={canWrite && (season?.status ?? "open") !== "cancelled"} onChanged={(c) => { setUpdated(c); load.reload(); }} />
    </> : null}
    {batch ? <TaskBatchForm key={batch.seq} path={path} task={batch} onClose={() => setBatch(null)} onSaved={() => load.reload()} /> : null}
    {detail && season && data ? <HouseholdTaskDetail key={detail.seq} path={path} event={data.current_event_id} task={detail} season={season} onClose={() => setDetail(null)} onQueued={() => { setDetail(null); setQueueRevision((r) => r + 1); if (navigator.onLine) void syncQueued(); }} /> : null}
    {pending ? <div role="region" aria-label="Xác nhận hoàn thành" className="row" style={{ position: "sticky", bottom: 0, background: "var(--bg)", padding: 12, borderTop: "1px solid var(--line)", zIndex: 2 }}>
      <span>{pending.name} · Ngày làm xong: Hôm nay (giờ Việt Nam)</span>
      <button type="button" className="btn btn-o" disabled={busy} onClick={() => setPending(null)}>Hủy</button>
      <button type="button" className="btn btn-p" disabled={busy} onClick={() => void confirm()}>{busy ? "Đang ghi…" : "Xác nhận"}</button>
    </div> : null}
  </section>;
}
