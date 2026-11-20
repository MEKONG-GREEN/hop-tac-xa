"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

function HouseholdProfileDialog({ householdRef, onClose }: { householdRef: string; onClose: () => void }) {
  const { api } = useAuth();
  const profile = useAsync(
    () => unwrap(api.GET("/v1/households/{ref}/profile", { params: { path: { ref: householdRef } } })),
    [api, householdRef],
  );
  // useAsync retains the previous result while reloading: never render it
  // during a changed identity/request or after a failed fresh authorization.
  const data = !profile.loading && !profile.error ? profile.data : undefined;
  return (
    <Dialog title="Chi tiết hộ xã viên" onClose={onClose}>
      {profile.loading ? <Loading /> : null}
      {profile.error ? <ProblemAlert problem={profile.error} onRetry={profile.reload} /> : null}
      {data ? (
        <>
          {data.contact_access === "masked" ? <p className="small mute">SĐT và CCCD được che theo quyền Thành viên HTX.</p> : null}
          <dl className="stack">
            <div><dt>Họ và tên chủ hộ</dt><dd>{data.identity.full_name || "Chưa khai báo"}</dd></div>
            <div><dt>Số điện thoại</dt><dd>{data.identity.phone || "Chưa khai báo"}</dd></div>
            <div><dt>CCCD</dt><dd>{data.identity.national_id || "Chưa khai báo"}</dd></div>
            <div><dt>Địa chỉ</dt><dd>{data.identity.address || "Chưa khai báo"}</dd></div>
            <div><dt>Diện tích trồng trọt (ha)</dt><dd>{formatDecimal(data.household.cultivated_area_ha, 4)}</dd></div>
            <div><dt>Diện tích chăn nuôi (ha)</dt><dd>{formatDecimal(data.household.livestock_area_ha, 4)}</dd></div>
            <div><dt>Trạng thái</dt><dd>{data.household.active ? "Đang tham gia" : "Ngừng tham gia"}</dd></div>
          </dl>
        </>
      ) : null}
    </Dialog>
  );
}

function ProfileViewer({ householdRef }: { householdRef: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(true)}>Xem chi tiết</button>
      {open ? <HouseholdProfileDialog householdRef={householdRef} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

export function HouseholdProfileButton({ householdRef }: { householdRef: string }) {
  const { principal } = useAuth();
  if (!hasRole(principal, ROLES.platformAdmin, ROLES.coopManager, ROLES.htxMember) || !matrixAllows(principal, "FN-04", "R")) return null;
  const identity = JSON.stringify([principal?.subject, principal?.tenantId, principal?.orgRef, principal?.roles, principal?.scopeAll, principal?.scopeUnits, principal?.mfa]);
  // Discard the dialog and its PII when the authenticated scope changes.
  return <ProfileViewer key={identity} householdRef={householdRef} />;
}
