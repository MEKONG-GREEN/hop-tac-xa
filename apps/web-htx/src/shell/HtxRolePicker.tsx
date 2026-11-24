"use client";

import { useState } from "react";
import type { HtxWorkingRole } from "@/auth/htxWorkingRole";
import { roleLabel } from "@/auth/principal";
import { toApiProblem } from "@/lib/useAsync";
import { ProblemAlert } from "@/components/ProblemAlert";
import type { ApiProblem } from "@/api/problem";

/**
 * FN-01/AC-06, AC-07 (AH-270): tài khoản Đại diện HTX kiêm Thành viên HTX chọn vai trò và HTX làm việc. Mỗi thẻ ghi tên
 * HTX của tài khoản (cả hai vai cùng một HTX, FN-07/BR-02); mỗi phiên chỉ dùng quyền của vai đang chọn (BR-05). Thẻ của
 * vai dùng lần trước có dấu (UX-TP-09); vai vừa bị ngừng thì hiện câu báo (FN-TK-01/EX-03) — AH-222.
 */
export function HtxRolePicker({ roles, select, htxName, lastUsed, notice }: {
  roles: HtxWorkingRole[]; select: (role: HtxWorkingRole) => Promise<void>; htxName?: string | null;
  /** UX-TP-09 (AH-222): thẻ của vai dùng lần trước ghi "Dùng lần trước". */
  lastUsed?: HtxWorkingRole | null;
  /** FN-TK-01/EX-03 (AH-222): câu báo vai vừa bị ngừng. */
  notice?: string | null;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  const choose = async (role: HtxWorkingRole) => {
    setBusy(true);
    setError(null);
    try {
      await select(role);
    } catch (e) {
      setError(toApiProblem(e));
      setBusy(false);
    }
  };
  return (
    <main className="auth-bg">
      <section className="card stack" aria-label="Chọn vai trò và HTX làm việc">
        <h1>Chọn vai trò và HTX làm việc</h1>
        <p>Các vai cùng thuộc một HTX. Mỗi lần chỉ dùng quyền của vai đang chọn.</p>
        {notice ? <div className="alert alert-warn" role="status">{notice}</div> : null}
        {error && <ProblemAlert problem={error} />}
        {roles.map((role) => (
          <button className="btn primary" type="button" disabled={busy} key={role} onClick={() => void choose(role)}>
            <b>{roleLabel(role)}</b>
            {htxName ? <span className="small"> · {htxName}</span> : null}
            {lastUsed === role ? <span className="small"> · Dùng lần trước</span> : null}
          </button>
        ))}
      </section>
    </main>
  );
}
