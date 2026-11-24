package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// AH-192: vụ đang canh tác quá 3 ngày chưa có nhật ký → nhắc nhẹ một lần mỗi lần vượt ngưỡng; bản ghi mới tính lại mốc;
// kiểm lại lúc gửi.
func TestAH192_LogMissingReminders(t *testing.T) {
	f, path, _ := taskFixture(t)
	ctx := context.Background()
	member := f.memberA()
	sender := &taskSender{}
	svc := f.mod.svc
	svc.CalendarMembers = &calendarMembers{tenant: f.tenant, coop: f.coopA, refs: []uuid.UUID{member.actor}}
	svc.TaskNotifier = sender
	tenants := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }
	run := func(name string) {
		t.Helper()
		if _, err := svc.RunTaskReminders(ctx, tenants); err != nil && !sender.failLog {
			t.Fatalf("%s: %v", name, err)
		}
	}
	tick := func(step int) {
		t.Helper()
		cl := member.get(path + "/checklist")
		expect(t, cl, http.StatusOK, "checklist")
		expect(t, member.post(path+"/task-completions", tickBody(step, str(cl.body, "current_event_id"))), http.StatusCreated, "ghi nhật ký")
	}

	run("ngày mở vụ")
	if len(sender.missing) != 0 {
		t.Fatalf("chưa quá 3 ngày: %+v", sender.missing)
	}
	// 03/10: 4 ngày từ ngày mở vụ, chưa có nhật ký → một nhắc.
	f.clk.Advance(4 * 24 * time.Hour)
	run("quá 3 ngày")
	if len(sender.missing) != 1 || sender.missing[0].Days != 4 || !strings.HasPrefix(sender.missing[0].HouseholdCode, "H-") ||
		len(sender.missing[0].Recipients) != 1 || sender.missing[0].CooperativeRef != f.coopA {
		t.Fatalf("nhắc thiếu nhật ký: %+v", sender.missing)
	}
	// Chạy lại, sang ngày sau mà mốc không đổi → không nhắc lại.
	run("chạy lại")
	f.clk.Advance(24 * time.Hour)
	run("ngày sau, cùng mốc")
	if len(sender.missing) != 1 {
		t.Fatalf("mỗi lần vượt ngưỡng chỉ nhắc một lần: %+v", sender.missing)
	}
	// 04/10 có bản ghi mới → mốc tính lại; 08/10 quá 3 ngày từ bản ghi → nhắc lần hai.
	tick(2)
	f.clk.Advance(4 * 24 * time.Hour)
	run("vượt ngưỡng lần hai")
	if len(sender.missing) != 2 || sender.missing[1].Days != 4 {
		t.Fatalf("lần hai: %+v", sender.missing)
	}
	// Gửi lỗi → lượt chờ; có bản ghi mới trước lượt sau → kiểm lại lúc gửi thì bỏ.
	tick(3)
	f.clk.Advance(4 * 24 * time.Hour)
	sender.failLog = true
	run("gửi lỗi")
	sender.failLog = false
	tick(4)
	f.clk.Advance(time.Hour)
	r, err := svc.RunTaskReminders(ctx, tenants)
	if err != nil || r.LogDropped != 1 || len(sender.missing) != 2 {
		t.Fatalf("đã ghi nhật ký thì không nhắc: %+v %v", r, err)
	}
	var sent, dropped, pending int
	if err := pgtest.Get(t).AdminPool(t, migrations.PF).QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='sent'), count(*) FILTER (WHERE status='dropped'),
	 count(*) FILTER (WHERE status='pending') FROM pf_coop.log_missing_reminder WHERE tenant_id=$1`, f.tenant).Scan(&sent, &dropped, &pending); err != nil {
		t.Fatal(err)
	}
	if sent != 2 || dropped != 1 || pending != 0 {
		t.Fatalf("sổ lượt nhắc: sent=%d dropped=%d pending=%d", sent, dropped, pending)
	}
}
