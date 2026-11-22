"use client";

import { useState } from "react";
import type { HtxWorkingRole } from "@/auth/htxWorkingRole";
import { roleLabel } from "@/auth/principal";
import { toApiProblem } from "@/lib/useAsync";
import { ProblemAlert } from "@/components/ProblemAlert";
import type { ApiProblem } from "@/api/problem";

/**
 * FN-01/AC-06, AC-07 (AH-270): tài khoản Đại diện HTX kiêm Thành viên HTX chọn vai trò và HTX làm việc. Mỗi thẻ ghi tên
 * HTX của tài khoản (cả hai vai cùng một HTX, FN-07/BR-02); mỗi phiên chỉ dùng quyền của vai đang chọn (BR-05).
 */
export function HtxRolePicker({ roles, select, htxName }: { roles: HtxWorkingRole[]; select: (role: HtxWorkingRole) => Promise<void>; htxName?: string | null }) {
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
        {error && <ProblemAlert problem={error} />}
        {roles.map((role) => (
          <button className="btn primary" type="button" disabled={busy} key={role} onClick={() => void choose(role)}>
            <b>{roleLabel(role)}</b>
            {htxName ? <span className="small"> · {htxName}</span> : null}
          </button>
        ))}
      </section>
    </main>
  );
}
