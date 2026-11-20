package handler_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/plot/app"
	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// fakeSharedReads stands in for the cooperative member check; RLS still needs a real grant row.
type fakeSharedReads struct{ allow atomic.Bool }

func (f *fakeSharedReads) NativeCanReadSharedCooperative(context.Context, uuid.UUID) (bool, error) {
	return f.allow.Load(), nil
}

// FN-06/AH-274: Đại diện HTX A đọc thửa HTX B chỉ khi B đã duyệt chia sẻ; ứng dụng và RLS
// cùng phải cho phép, thu hồi mất quyền ở lần đọc kế tiếp, không mở quyền ghi.
func TestNativeSharedCooperativePlotsReadOnly(t *testing.T) {
	shared := &fakeSharedReads{}
	f := newFixture(t, func(s *app.Service) { s.SharedReads = shared })
	nat := f.national()
	b1, _ := nat.createPlot("31117", f.coopB, rect(600, 0, 100, 100))
	mgr := f.managerA()
	path := "/v1/cooperatives/" + f.coopB.String() + "/plots"

	r := mgr.appDo(http.MethodGet, path, nil)
	if r.status != http.StatusForbidden || code(r) != "geo.out_of_scope" {
		t.Fatalf("không có chia sẻ: %d %s", r.status, r.raw)
	}
	// Kiểm ở ứng dụng sai vẫn không lộ dữ liệu: RLS cần grant "approved" thật.
	shared.allow.Store(true)
	r = mgr.appDo(http.MethodGet, path, nil)
	if r.status != http.StatusOK || len(featureIDs(r)) != 0 {
		t.Fatalf("RLS fail-closed khi chưa có grant: %d %s", r.status, r.raw)
	}
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	grant := ids.New()
	if _, err := admin.Exec(context.Background(), `INSERT INTO pf_coop.native_sharing
	  (tenant_id,ref,requester_cooperative_ref,owner_cooperative_ref,state,revision,requested_at,processed_at,actor_ref)
	  VALUES ($1,$2,$3,$4,'approved',2,$5,$5,$6)`, f.tenant, grant, f.coopA, f.coopB, time.Now(), ids.New()); err != nil {
		t.Fatal(err)
	}
	r = mgr.appDo(http.MethodGet, path, nil)
	if got := featureIDs(r); r.status != http.StatusOK || len(got) != 1 || got[0] != b1.String() {
		t.Fatalf("đã duyệt chia sẻ: %d %s", r.status, r.raw)
	}
	// Không ghi thửa của HTX được chia sẻ.
	r = mgr.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "cooperative_ref": f.coopB.String(), "geometry": rect(0, 300, 50, 50)})
	if r.status < 400 {
		t.Fatalf("ghi thửa HTX được chia sẻ: %d %s", r.status, r.raw)
	}
	// Thành viên HTX A không hưởng chia sẻ (dù kiểm ứng dụng giả cho phép, RLS chỉ mở cho Nút yêu cầu qua opt-in của Đại diện).
	member := f.as(f.tenant, f.coopA, coopport.RoleHTXMember)
	shared.allow.Store(false)
	if r = member.appDo(http.MethodGet, path, nil); r.status != http.StatusForbidden {
		t.Fatalf("Thành viên HTX: %d %s", r.status, r.raw)
	}
	if _, err := admin.Exec(context.Background(), `UPDATE pf_coop.native_sharing SET state='revoked',revision=3 WHERE tenant_id=$1 AND ref=$2`, f.tenant, grant); err != nil {
		t.Fatal(err)
	}
	shared.allow.Store(true)
	if r = mgr.appDo(http.MethodGet, path, nil); r.status != http.StatusOK || len(featureIDs(r)) != 0 {
		t.Fatalf("đã thu hồi: %d %s", r.status, r.raw)
	}
}
