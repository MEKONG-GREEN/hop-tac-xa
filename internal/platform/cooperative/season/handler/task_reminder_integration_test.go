package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

type taskSender struct {
	calls []port.TaskDueV1
	fail  bool
}

func (n *taskSender) TaskDue(_ context.Context, _ uuid.UUID, notice port.TaskDueV1) error {
	if n.fail {
		return errors.New("notify unavailable")
	}
	n.calls = append(n.calls, notice)
	return nil
}

// AH-191: nhắc việc đến hạn và trễ — một thông báo mỗi vụ mỗi loại, không trùng khi chạy lại, kiểm lại lúc gửi,
// không ngày/đã xong không nhắc, thiếu người nhận thì dropped, thiếu phụ thuộc thì fail-closed.
func TestAH191_TaskReminders(t *testing.T) {
	f, path, _ := taskFixture(t)
	ctx := context.Background()
	member := f.memberA()
	members := &calendarMembers{tenant: f.tenant, coop: f.coopA, refs: []uuid.UUID{member.actor}}
	sender := &taskSender{}
	svc := f.mod.svc
	tenants := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }

	// Thiếu cổng thông báo → không chạy, không ghi lượt nhắc (fail-closed).
	svc.CalendarMembers = members
	if _, err := svc.RunTaskReminders(ctx, tenants); err == nil {
		t.Fatal("thiếu TaskNotifier phải lỗi phụ thuộc")
	}
	svc.TaskNotifier = sender

	// 29/09 (ngày xuống giống): "Gieo sạ" vừa Đến hạn, "Làm đất" vừa Trễ → hai thông báo của một vụ.
	r, err := svc.RunTaskReminders(ctx, tenants)
	if err != nil || r.Notices != 2 || r.Sent != 2 || len(sender.calls) != 2 {
		t.Fatalf("lượt 1: %+v %v %+v", r, err, sender.calls)
	}
	byKind := map[bool]port.TaskDueV1{}
	for _, c := range sender.calls {
		byKind[c.Overdue] = c
		if len(c.Recipients) != 1 || c.Recipients[0] != member.actor || !strings.HasPrefix(c.HouseholdCode, "H-") || c.CooperativeRef != f.coopA {
			t.Fatalf("thông báo: %+v", c)
		}
	}
	if byKind[false].TaskName != "Gieo sạ" || byKind[false].DueDate != "2026-09-30" || byKind[true].TaskName != "Làm đất" || byKind[true].DueDate != "2026-09-28" {
		t.Fatalf("nội dung nhắc: %+v", byKind)
	}

	// AC4: chạy lại cùng ngày không gửi trùng.
	if r, err = svc.RunTaskReminders(ctx, tenants); err != nil || r.Notices != 0 || len(sender.calls) != 2 {
		t.Fatalf("chạy lại: %+v %v", r, err)
	}

	// 01/10: "Gieo sạ" chuyển Trễ → một nhắc trễ; "Làm đất" đã có nhắc trễ.
	f.clk.Advance(48 * time.Hour)
	if r, err = svc.RunTaskReminders(ctx, tenants); err != nil || r.Sent != 1 || !sender.calls[2].Overdue || sender.calls[2].TaskName != "Gieo sạ" {
		t.Fatalf("nhắc trễ: %+v %v %+v", r, err, sender.calls)
	}

	// 09/10: "Bón thúc" Đến hạn nhưng gửi lỗi → lượt nhắc giữ pending; việc được ghi xong trước lượt sau → dropped.
	f.clk.Advance(8 * 24 * time.Hour)
	sender.fail = true
	if r, err = svc.RunTaskReminders(ctx, tenants); err == nil || r.Failed != 1 {
		t.Fatalf("gửi lỗi phải báo: %+v %v", r, err)
	}
	sender.fail = false
	list := member.get(path + "/checklist")
	expect(t, list, http.StatusOK, "checklist")
	expect(t, member.post(path+"/task-completions", tickBody(3, str(list.body, "current_event_id"))), http.StatusCreated, "ghi xong Bón thúc")
	f.clk.Advance(time.Hour) // quá ReminderClaimTimeout: lượt nhắc của tiến trình trước được nhận lại
	if r, err = svc.RunTaskReminders(ctx, tenants); err != nil || r.Dropped != 1 || r.Sent != 0 || len(sender.calls) != 3 {
		t.Fatalf("việc đã xong không nhắc: %+v %v", r, err)
	}

	// 08/11: "Phun thuốc" Đến hạn nhưng HTX không còn thành viên hoạt động → dropped, không báo "đã gửi".
	f.clk.Advance(30 * 24 * time.Hour)
	members.refs = nil
	if r, err = svc.RunTaskReminders(ctx, tenants); err != nil || r.Dropped != 1 || r.Sent != 0 {
		t.Fatalf("không có người nhận: %+v %v", r, err)
	}

	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var sent, dropped, pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='sent'), count(*) FILTER (WHERE status='dropped'), count(*) FILTER (WHERE status='pending')
	 FROM pf_coop.task_reminder WHERE tenant_id=$1`, f.tenant).Scan(&sent, &dropped, &pending); err != nil {
		t.Fatal(err)
	}
	if sent != 3 || dropped != 2 || pending != 0 {
		t.Fatalf("sổ lượt nhắc: sent=%d dropped=%d pending=%d", sent, dropped, pending)
	}
}
