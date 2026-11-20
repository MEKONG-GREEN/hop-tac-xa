"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { checkHTXDeletion, deleteHTX } from "./profiles";

function DeleteConfirmation({ cooperativeRef, onClose }: { cooperativeRef: string; onClose: () => void }) {
  const { adminApi } = useAuth();
  const router = useRouter();
  const check = useAsync(() => checkHTXDeletion(adminApi, cooperativeRef), [adminApi, cooperativeRef]);
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const sending = useRef(false);
  const mounted = useRef(true);
  const pending = useRef<{ revision: number; key: string } | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  async function remove() {
    if (sending.current || !check.data?.can_delete || check.loading || check.error) return;
    sending.current = true; setBusy(true); setProblem(null);
    pending.current ??= { revision: check.data.revision, key: newIdempotencyKey() };
    try {
      await deleteHTX(adminApi, cooperativeRef, pending.current.revision, pending.current.key);
      if (mounted.current) router.replace("/htx");
    } catch (cause) {
      if (!mounted.current) return;
      const error = toApiProblem(cause);
      setProblem(error);
      const unknown = error.status === 0 || error.status >= 500 || error.code === "idempotency_in_progress";
      setUncertain(unknown);
      if (!unknown) { pending.current = null; check.reload(); }
    } finally {
      sending.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  const data = check.data;
  return <Dialog title="Xóa HTX" onClose={() => { if (!busy && !uncertain) onClose(); }} actions={
    <button type="button" className="btn btn-danger" disabled={busy || check.loading || Boolean(check.error) || !data?.can_delete} onClick={() => void remove()}>
      {busy ? "Đang xóa…" : uncertain ? "Thử xóa lại" : "Xác nhận xóa HTX"}
    </button>
  }>
    {check.loading ? <Loading /> : check.error ? <ProblemAlert problem={check.error} onRetry={check.reload} /> : data ? <>
      <p><strong>{data.name}</strong> ({data.code})</p>
      <p>HTX còn {data.households} hộ, {data.accounts} tài khoản.</p>
      {data.pending_accounts > 0 ? <p>Còn {data.pending_accounts} thao tác tài khoản chưa hoàn tất.</p> : null}
      {data.has_sharing ? <p>HTX có yêu cầu hoặc lịch sử chia sẻ dữ liệu.</p> : null}
      {data.has_business ? <p>HTX có dữ liệu nghiệp vụ liên kết.</p> : null}
      {data.can_delete ? <p>Hồ sơ HTX sẽ được xóa khỏi danh sách. Chỉ xác nhận nếu đây là HTX tạo nhầm chưa phát sinh dữ liệu.</p> : <p>Chỉ xóa HTX chưa phát sinh dữ liệu.</p>}
    </> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
    {uncertain ? <p role="status">Thao tác chưa hoàn tất. Bấm “Thử xóa lại” để tiếp tục.</p> : null}
  </Dialog>;
}

export function HTXDeleteButton({ cooperativeRef }: { cooperativeRef: string }) {
  const { principal } = useAuth();
  const [open, setOpen] = useState(false);
  if (!hasRole(principal, ROLES.platformAdmin)) return null;
  const identity = `${principal?.subject}:${principal?.tenantId}:${principal?.roles.join("|")}`;
  return <>
    <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(true)}>Xóa HTX</button>
    {open ? <DeleteConfirmation key={`${identity}:${cooperativeRef}`} cooperativeRef={cooperativeRef} onClose={() => setOpen(false)} /> : null}
  </>;
}
