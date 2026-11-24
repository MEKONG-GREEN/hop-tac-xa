"use client";

import { useRef } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { hasRole, ROLES } from "@/auth/principal";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { SeasonPlanEditor, snapshotLookup, type PlanChange, type PlanStage } from "./SeasonPlanEditor";

type S = components["schemas"];

/** Quy trình hiệu lực từ checklist: việc đã xong bị khóa, việc SOP bị bỏ hiện "Bỏ qua". */
export function checklistStages(c: S["HouseholdChecklist"]): PlanStage[] {
  return c.progress.stages.map((s) => ({
    seq: s.seq, name: s.name,
    tasks: s.tasks.map((t) => ({
      seq: t.seq, stage_seq: t.stage_seq, name: t.name, custom: t.custom, from_date: t.from_date, to_date: t.to_date, undated: t.undated,
      locked: t.completion_status.startsWith("done"), skipped: t.completion_status === "skipped",
    })),
  }));
}

/** Lịch sử tùy chỉnh quy trình của vụ (ai, lúc nào, nội dung) — người xem được vụ đều xem được. */
export function SeasonPlanHistory({ coopRef, householdRef, seasonRef, revision }: { coopRef: string; householdRef: string; seasonRef: string; revision: string }) {
  const { api, principal } = useAuth();
  const history = useAsync(() => unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/plan-history", {
    params: { path: { ref: coopRef, household_ref: householdRef, season_ref: seasonRef } },
  })), [api, coopRef, householdRef, seasonRef, revision]);
  return <section aria-label="Lịch sử tùy chỉnh quy trình">
    <h4>Lịch sử tùy chỉnh quy trình</h4>
    {history.loading ? <Loading /> : history.error ? <ProblemAlert problem={history.error} onRetry={history.reload} />
      : !history.data?.items.length ? <p className="small mute">Chưa có thay đổi nào sau khi mở vụ.</p>
        : <ul>{history.data.items.map((h) => <li key={h.event_id}>
          <span className="small mute">{formatDate(h.event_time)} · {h.actor_ref === principal?.subject ? "Bạn" : "Người dùng khác"}</span> — {h.summary}
        </li>)}</ul>}
  </section>;
}

/**
 * AH-113 AC2: tùy chỉnh quy trình của vụ đang canh tác. Mỗi thao tác một Idempotency-Key; thử lại đúng thao tác vừa
 * lỗi thì dùng lại khóa. Đại diện HTX (KHMV-FN-04 chỉ R) chỉ thấy lịch sử.
 */
export function RunningSeasonPlan({ coopRef, householdRef, season, onSeason }: {
  coopRef: string; householdRef: string; season: S["HouseholdSeason"]; onSeason: (s: S["HouseholdSeason"]) => void;
}) {
  const { api, principal } = useAuth();
  const canEdit = season.status === "open" && hasRole(principal, ROLES.platformAdmin, ROLES.htxMember) && matrixAllows(principal, "KHMV-FN-04", "U");
  const path = { ref: coopRef, household_ref: householdRef, season_ref: season.season_ref };
  const checklist = useAsync(() => canEdit ? unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/checklist", { params: { path } })) : Promise.resolve(null),
    [api, canEdit, coopRef, householdRef, season.season_ref, season.current_event_id]);
  const attempt = useRef<{ body: string; key: string } | null>(null);
  const change = async (c: PlanChange) => {
    const body = JSON.stringify([season.current_event_id, c]);
    if (attempt.current?.body !== body) attempt.current = { body, key: newIdempotencyKey() };
    const res = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/plan-changes", {
      params: { path, header: { "Idempotency-Key": attempt.current.key } },
      body: { expected_season_event_id: season.current_event_id, change: c },
    }));
    attempt.current = null;
    onSeason(res.season);
  };
  return <div className="stack">
    {canEdit ? checklist.loading ? <Loading label="Đang tải quy trình…" /> : checklist.error ? <ProblemAlert problem={checklist.error} onRetry={checklist.reload} />
      : checklist.data ? <SeasonPlanEditor stages={checklistStages(checklist.data)} lookup={snapshotLookup(season.sop_snapshot)} onChange={change} /> : null : null}
    <SeasonPlanHistory coopRef={coopRef} householdRef={householdRef} seasonRef={season.season_ref} revision={season.current_event_id} />
  </div>;
}
