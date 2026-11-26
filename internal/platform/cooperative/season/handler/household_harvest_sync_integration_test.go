package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	seasonapp "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// AH-190 AC1–AC3: khai báo sản lượng ngoại tuyến đẩy lên qua hàng đợi đồng bộ — kiểm lại như online (quyền, phạm vi,
// số, ngày, xác nhận 0), không trùng khi gửi lại, vụ đổi trạng thái trong lúc offline bị từ chối, giờ thiết bị và giờ
// nhận tách riêng, hai thiết bị cùng khai một vụ chỉ một bản được ghi.
func TestAH190_OfflineHouseholdHarvestSync(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH190 OM5451", "OM5451")
	f.householdLand("3", true)
	c := f.memberA()
	open := func(area, anchor string) (string, uuid.UUID, string) {
		t.Helper()
		r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": area, "anchor_date": anchor}))
		expect(t, r, http.StatusCreated, "mở vụ "+anchor)
		ref := str(r.body, "season", "season_ref")
		return f.householdPath() + "/" + ref, uuid.MustParse(ref), str(r.body, "season", "current_event_id")
	}
	pathA, refA, _ := open("0.50", "2026-06-01")
	pathB, refB, eventB := open("0.50", "2026-06-10")
	pathC, refC, _ := open("0.50", "2026-06-15")
	_, refD, _ := open("0.50", "2026-06-20")

	member := authz.Principal{TenantID: c.tid, ActorRef: c.actor, OrgRef: c.org, Roles: []string{"htx_member"}}
	sink := seasonapp.SyncSink{Svc: f.mod.svc}
	device := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	record := func(season uuid.UUID, mass, day string, zero bool, extra map[string]any) syncport.Record {
		t.Helper()
		payload := map[string]any{"recorded_by": member.ActorRef, "tenant_id": member.TenantID, "cooperative_ref": f.coopA, "household_ref": f.h1,
			"season_ref": season, "mass_t": mass, "harvest_date": day, "zero_confirmed": zero, "gps": map[string]any{"source": "denied"}}
		for k, v := range extra {
			payload[k] = v
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return syncport.Record{ID: ids.New(), IdemKey: key(), Entity: seasonapp.EntityHouseholdHarvest, Op: syncport.OpCreate, EventTime: device, DeviceID: ids.New(), Payload: raw}
	}
	accept := func(p authz.Principal, rec syncport.Record) syncport.Result {
		return sink.Accept(authz.WithPrincipal(context.Background(), p), authz.Scope{}, rec)
	}

	// AC1: bản khai hợp lệ được ghi; gửi lại cùng khóa trả duplicate.
	recA := record(refA, "3.25", "2026-09-20", false, nil)
	if r := accept(member, recA); r.Status != syncport.StatusAccepted {
		t.Fatalf("bản khai hợp lệ: %+v", r)
	}
	if r := accept(member, recA); r.Status != syncport.StatusDuplicate {
		t.Fatalf("gửi lại: %+v", r)
	}
	g := c.get(pathA)
	if str(g.body, "status") != "completed" || str(g.body, "harvest", "mass_t") != "3.25" || str(g.body, "harvest", "harvest_id") != recA.ID.String() {
		t.Fatalf("vụ A sau đồng bộ: %v", g.body)
	}
	declared, _ := time.Parse(time.RFC3339Nano, str(g.body, "harvest", "declared_at"))
	if !declared.Equal(device) {
		t.Fatalf("giờ thiết bị: %v", g.body["harvest"])
	}
	if task := checklistTask(c.get(pathA+"/checklist").body, 5); task["done_on"] != "2026-09-20" {
		t.Fatalf("việc Thu hoạch tự Xong: %v", task)
	}
	if n := len(list(c.get(pathA+"/harvest/history").body, "items")); n != 1 {
		t.Fatalf("ghi trùng: %d", n)
	}

	// AC2: vụ đã khai báo (bản khác) hoặc đã dừng trong lúc offline → không ghi, kèm lý do.
	if r := accept(member, record(refA, "4", "2026-09-21", false, nil)); r.Status != syncport.StatusRejected || r.Code != "cropcycle.season_state" {
		t.Fatalf("vụ đã khai báo: %+v", r)
	}
	expect(t, c.post(pathB+"/stop", map[string]any{"expected_season_event_id": eventB, "reason": "Ruộng bị ngập nặng, dừng vụ"}), http.StatusOK, "dừng B")
	if r := accept(member, record(refB, "2", "2026-09-20", false, nil)); r.Status != syncport.StatusRejected || r.Code != "cropcycle.season_state" {
		t.Fatalf("vụ đã dừng: %+v", r)
	}

	// AC3: kiểm lại như online.
	for name, tc := range map[string]struct {
		p    authz.Principal
		rec  syncport.Record
		code string
	}{
		"số âm":                {member, record(refC, "-1", "2026-09-20", false, nil), "validation_failed"},
		"ngày sau hôm nay":     {member, record(refC, "2", "2099-01-01", false, nil), "validation_failed"},
		"bằng 0 chưa xác nhận": {member, record(refC, "0", "2026-09-20", false, nil), "cropcycle.harvest_zero_confirmation_required"},
		"trường lạ":            {member, record(refC, "2", "2026-09-20", false, map[string]any{"role": "admin"}), "validation_failed"},
		"tài khoản khác":       {member, record(refC, "2", "2026-09-20", false, map[string]any{"recorded_by": ids.New()}), "forbidden"},
		"Đại diện HTX":         {authz.Principal{TenantID: member.TenantID, ActorRef: member.ActorRef, OrgRef: member.OrgRef, Roles: []string{"coop_manager"}}, record(refC, "2", "2026-09-20", false, nil), "cropcycle.read_only"},
	} {
		if r := accept(tc.p, tc.rec); r.Status != syncport.StatusRejected || r.Code != tc.code {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	// Thành viên HTX khác: vụ ngoài phạm vi bị từ chối.
	other := f.memberB()
	otherP := authz.Principal{TenantID: other.tid, ActorRef: other.actor, OrgRef: other.org, Roles: []string{"htx_member"}}
	recOther := record(refC, "2", "2026-09-20", false, map[string]any{"recorded_by": otherP.ActorRef, "tenant_id": otherP.TenantID})
	if r := accept(otherP, recOther); r.Status != syncport.StatusRejected {
		t.Fatalf("ngoài phạm vi: %+v", r)
	}
	if r := accept(member, record(refC, "0", "2026-09-20", true, nil)); r.Status != syncport.StatusAccepted {
		t.Fatalf("bằng 0 đã xác nhận: %+v", r)
	}
	if g := c.get(pathC); str(g.body, "harvest", "mass_t") != "0.00" || g.body["harvest"].(map[string]any)["zero_confirmed"] != true {
		t.Fatalf("vụ C: %v", g.body)
	}

	// API online không nhận kênh sync từ client.
	evD := str(c.get(f.householdPath()+"/"+refD.String()).body, "current_event_id")
	body := harvestBody(evD, "2", "2026-09-20")
	body["channel"] = "sync"
	expect(t, c.post(f.householdPath()+"/"+refD.String()+"/harvest", body), http.StatusUnprocessableEntity, "kênh sync qua API online")

	// Hai thiết bị cùng khai một vụ: chỉ một bản được ghi.
	results := make([]syncport.Result, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		rec := record(refD, "2", "2026-09-20", false, nil)
		go func(i int) {
			defer wg.Done()
			results[i] = accept(member, rec)
		}(i)
	}
	wg.Wait()
	accepted := 0
	for _, r := range results {
		switch {
		case r.Status == syncport.StatusAccepted:
			accepted++
		case r.Status == syncport.StatusRejected && r.Code == "cropcycle.season_state":
		default:
			t.Fatalf("đồng thời: %+v", results)
		}
	}
	if accepted != 1 {
		t.Fatalf("đồng thời: %+v", results)
	}
}
