"use client";

import { useState } from "react";
import type { HtxWorkingRole } from "@/auth/htxWorkingRole";
import { roleLabel } from "@/auth/principal";
import { toApiProblem } from "@/lib/useAsync";
import { ProblemAlert } from "@/components/ProblemAlert";
import type { ApiProblem } from "@/api/problem";

export function HtxRolePicker({ roles, select }: { roles: HtxWorkingRole[]; select: (role: HtxWorkingRole) => Promise<void> }) {
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState<ApiProblem|null>(null);
  const choose=async(role:HtxWorkingRole)=>{setBusy(true);setError(null);try{await select(role);}catch(e){setError(toApiProblem(e));setBusy(false);}};
  return <main className="auth-bg"><section className="card stack" aria-label="Chọn vai trò làm việc"><h1>Chọn vai trò làm việc</h1><p>Các vai cùng thuộc một HTX. Mỗi lần chỉ dùng quyền của vai đang chọn.</p>{error && <ProblemAlert problem={error} />}{roles.map((role)=><button className="btn primary" type="button" disabled={busy} key={role} onClick={()=>void choose(role)}>{roleLabel(role)}</button>)}</section></main>;
}
