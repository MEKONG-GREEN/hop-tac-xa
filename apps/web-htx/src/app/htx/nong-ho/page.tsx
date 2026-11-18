"use client";

import { getCooperative } from "@/api/adapters/cooperatives";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { HouseholdsPanel } from "@/htx/HouseholdsPanel";
import { CoopPicker, useHtxScope } from "@/htx/scope";
import { useAsync } from "@/lib/useAsync";

/**
 * Quản lý nông hộ (HTX US-HH-01/02/03): Quản lý HTX xem nông hộ của HTX mình, tìm theo SĐT, gọi nhanh,
 * sao chép hàng loạt. Cán bộ KN/quản trị chọn HTX trong phạm vi.
 */
export default function HouseholdsPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const coop = useAsync(() => (scope.coopRef ? getCooperative(api, scope.coopRef) : Promise.resolve(null)), [api, scope.coopRef]);
  return (
    <>
      <PageHead title="Quản lý nông hộ" subtitle="Diện tích đất, đang canh tác, còn lại và các vụ theo nông hộ trong HTX" />
      <div className="card sec">
        <div className="row" style={{ marginBottom: 12 }}>
          <CoopPicker scope={scope} allowAll={false} id="hh-coop" />
        </div>
        {!scope.coopRef ? <Empty>Chọn HTX để xem nông hộ.</Empty> : null}
        {coop.error ? <ProblemAlert problem={coop.error} onRetry={coop.reload} /> : null}
        {coop.loading && !coop.data ? <Loading /> : null}
        {coop.data ? <HouseholdsPanel key={coop.data.ref} coop={coop.data} readOnly showSeasons /> : null}
      </div>
    </>
  );
}
