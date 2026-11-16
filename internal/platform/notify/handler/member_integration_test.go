package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/channel"
	notifyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// AC1: trung tâm thông báo — mốc thời gian, trạng thái đọc, lọc chưa đọc, đọc một, đọc tất cả.
func TestAH22_AC1_TrungTamThongBao(t *testing.T) {
	f := newFixture(t, nil)
	coop := ids.New()
	a := f.newMember(coop)
	ctx := context.Background()
	n := f.mod.MemberNotifier()
	base := func() notifyport.MemberNoticeBase {
		return notifyport.MemberNoticeBase{SourceID: ids.New(), CooperativeRef: coop, Recipients: []uuid.UUID{a.actor}}
	}
	season := ids.New()

	// Hôm qua: NTF.1 và NTF.2; hôm nay: NTF.3 và NTF.4.
	f.clk.Advance(-24 * time.Hour)
	if err := n.CalendarPublished(ctx, f.tenant, notifyport.CalendarPublishedNotice{MemberNoticeBase: base(), CalendarRef: ids.New(),
		CropName: "Lúa", SeasonName: "Đông Xuân", Year: 2026, SowFrom: "2026-11-05", SowTo: "2026-11-20"}); err != nil {
		t.Fatal(err)
	}
	if err := n.TaskDue(ctx, f.tenant, notifyport.TaskDueNotice{MemberNoticeBase: base(), SeasonRef: season, TaskRef: ids.New(),
		HouseholdCode: "NH001", TaskName: "Bón phân", DueDate: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(24 * time.Hour)
	if err := n.LogMissing(ctx, f.tenant, notifyport.LogMissingNotice{MemberNoticeBase: base(), SeasonRef: season, HouseholdCode: "NH001", Days: 4}); err != nil {
		t.Fatal(err)
	}
	if err := n.ProcessChanged(ctx, f.tenant, notifyport.ProcessChangedNotice{MemberNoticeBase: base(), SeasonRef: season,
		HouseholdCode: "NH001", VarietyName: "ST25", ChangedTasks: 3}); err != nil {
		t.Fatal(err)
	}

	items := a.list("")
	if len(items) != 4 {
		t.Fatalf("muốn 4 thông báo, có %d", len(items))
	}
	created := make([]string, 0, len(items))
	for _, it := range items {
		if str(it, "created_at") == "" {
			t.Fatalf("thiếu created_at: %v", it)
		}
		if it["read_at"] != nil {
			t.Fatalf("thông báo mới phải chưa đọc: %v", it)
		}
		created = append(created, str(it, "created_at"))
	}
	if !sort.SliceIsSorted(created, func(i, j int) bool { return created[i] > created[j] }) {
		t.Fatalf("muốn mới nhất trước, có %v", created)
	}
	if a.unread() != 4 || len(a.list("?unread=true")) != 4 {
		t.Fatal("muốn 4 chưa đọc")
	}

	id := str(items[0], "id")
	if r := a.do(http.MethodPost, "/v1/notifications/"+id+"/read", nil); r.status != http.StatusOK || r.body["read_at"] == nil {
		t.Fatalf("đánh dấu đọc: %d %v", r.status, r.body)
	}
	if a.unread() != 3 || len(a.list("?unread=true")) != 3 {
		t.Fatal("đọc một thông báo phải giảm số chưa đọc còn 3")
	}

	if r := a.do(http.MethodPost, "/v1/notifications/read-all", nil); r.status != http.StatusOK || r.num("marked") != 3 {
		t.Fatalf("read-all: %d %v", r.status, r.body)
	}
	if a.unread() != 0 || len(a.list("?unread=true")) != 0 {
		t.Fatal("sau Đọc tất cả số chưa đọc phải là 0")
	}
	for _, it := range a.list("") {
		if it["read_at"] == nil {
			t.Fatalf("còn thông báo chưa đọc: %v", it)
		}
	}
}

// AC2: đích đến của từng loại.
func TestAH22_AC2_DichDen(t *testing.T) {
	f := newFixture(t, nil)
	coop := ids.New()
	a := f.newMember(coop)
	ctx := context.Background()
	n := f.mod.MemberNotifier()
	base := func() notifyport.MemberNoticeBase {
		return notifyport.MemberNoticeBase{SourceID: ids.New(), CooperativeRef: coop, Recipients: []uuid.UUID{a.actor}}
	}
	cal, season, task := ids.New(), ids.New(), ids.New()
	steps := []error{
		n.CalendarPublished(ctx, f.tenant, notifyport.CalendarPublishedNotice{MemberNoticeBase: base(), CalendarRef: cal,
			CropName: "Lúa", SeasonName: "Đông Xuân", Year: 2026, SowFrom: "2026-11-05", SowTo: "2026-11-20"}),
		n.TaskDue(ctx, f.tenant, notifyport.TaskDueNotice{MemberNoticeBase: base(), SeasonRef: season, TaskRef: task,
			HouseholdCode: "NH001", TaskName: "Bón phân", DueDate: "2026-10-01", Overdue: true}),
		n.LogMissing(ctx, f.tenant, notifyport.LogMissingNotice{MemberNoticeBase: base(), SeasonRef: season, HouseholdCode: "NH001", Days: 4}),
		n.ProcessChanged(ctx, f.tenant, notifyport.ProcessChangedNotice{MemberNoticeBase: base(), SeasonRef: season,
			HouseholdCode: "NH001", VarietyName: "ST25", ChangedTasks: 3}),
	}
	for _, err := range steps {
		if err != nil {
			t.Fatal(err)
		}
	}
	type want struct{ link, subject, title string }
	wants := map[string]want{
		notifyport.KindSeasonCalendarPublished: {"/cropcycle/calendars/" + cal.String(), cal.String(), "Lịch thời vụ mới: Lúa, vụ Đông Xuân/2026"},
		notifyport.KindSeasonTaskDue:           {"/cropcycle/seasons/" + season.String() + "/tasks/" + task.String(), task.String(), "Việc trễ hạn: Bón phân"},
		notifyport.KindSeasonLogMissing:        {"/cropcycle/seasons/" + season.String(), season.String(), "Vụ đã 4 ngày chưa có nhật ký"},
		notifyport.KindSeasonProcessChanged:    {"/cropcycle/seasons/" + season.String(), season.String(), "Quy trình của vụ đã thay đổi: 3 việc thay đổi"},
	}
	items := a.list("")
	if len(items) != len(wants) {
		t.Fatalf("muốn %d thông báo, có %d", len(wants), len(items))
	}
	for _, it := range items {
		w, ok := wants[str(it, "kind")]
		if !ok {
			t.Fatalf("kind lạ: %v", it)
		}
		if str(it, "deep_link") != w.link || str(it, "subject_ref") != w.subject || str(it, "title") != w.title {
			t.Errorf("%s: deep_link=%q subject_ref=%q title=%q, muốn %+v", str(it, "kind"), str(it, "deep_link"), str(it, "subject_ref"), str(it, "title"), w)
		}
	}
}

// AC3: kênh đẩy chưa cấu hình — thông báo vẫn có trong trung tâm, bản gửi ghi dropped kèm lý do, không bao giờ sent.
func TestAH22_AC3_KenhDayChuaCauHinh(t *testing.T) {
	f := newFixture(t, channel.DisabledPusher{Reason: channel.ReasonFCMNotConfigured})
	coop := ids.New()
	a := f.newMember(coop)
	a.registerToken()
	if _, err := f.taskDue(coop, a.actor); err != nil {
		t.Fatal(err)
	}
	items := a.list("")
	if len(items) != 1 {
		t.Fatalf("thông báo phải có trong trung tâm dù push tắt, có %d", len(items))
	}
	id := str(items[0], "id")
	env := f.pushEnv(id)
	if _, err := f.runPush(env); err != nil {
		t.Fatal(err)
	}
	ds := f.deliveries(id)
	if len(ds) != 1 || ds[0].status != "dropped" || ds[0].lastError != "channel_disabled:fcm_not_configured" || ds[0].sent {
		t.Fatalf("bản gửi = %+v, muốn 1 bản dropped channel_disabled:fcm_not_configured, chưa gửi", ds)
	}
	// Giao lặp cùng phong bì: vẫn một bản gửi, không có sent.
	if processed, err := f.runPush(env); err != nil || processed {
		t.Fatalf("giao lặp: processed=%v err=%v", processed, err)
	}
	if ds = f.deliveries(id); len(ds) != 1 || ds[0].status == "sent" {
		t.Fatalf("sau giao lặp: %+v", ds)
	}
	if len(a.list("")) != 1 {
		t.Fatal("thông báo vẫn phải có trong trung tâm")
	}
}

// AC3: thiết bị không nhận được push — token hỏng, lỗi tạm thời, không có token: thông báo vẫn có, không ghi sent khi chưa gửi.
func TestAH22_AC3_ThietBiKhongNhan(t *testing.T) {
	t.Run("token hỏng → dropped token_invalid", func(t *testing.T) {
		p := &fakePusher{err: channel.ErrTokenInvalid}
		f := newFixture(t, p)
		coop := ids.New()
		a := f.newMember(coop)
		a.registerToken()
		if _, err := f.taskDue(coop, a.actor); err != nil {
			t.Fatal(err)
		}
		id := str(a.list("")[0], "id")
		if _, err := f.runPush(f.pushEnv(id)); err != nil {
			t.Fatal(err)
		}
		ds := f.deliveries(id)
		if len(ds) != 1 || ds[0].status != "dropped" || ds[0].lastError != "token_invalid" || ds[0].sent {
			t.Fatalf("bản gửi = %+v", ds)
		}
		if p.sends() != 1 || len(a.list("")) != 1 {
			t.Fatalf("sends=%d, thông báo=%d", p.sends(), len(a.list("")))
		}
	})
	t.Run("lỗi tạm thời → failed, hàng đợi giao lại", func(t *testing.T) {
		p := &fakePusher{err: errors.New("fcm 503")}
		f := newFixture(t, p)
		coop := ids.New()
		a := f.newMember(coop)
		a.registerToken()
		if _, err := f.taskDue(coop, a.actor); err != nil {
			t.Fatal(err)
		}
		id := str(a.list("")[0], "id")
		if _, err := f.runPush(f.pushEnv(id)); !errors.Is(err, app.ErrPushRetry) {
			t.Fatalf("muốn ErrPushRetry, có %v", err)
		}
		ds := f.deliveries(id)
		if len(ds) != 1 || ds[0].status != "failed" || ds[0].sent {
			t.Fatalf("bản gửi = %+v", ds)
		}
		if p.sends() != 1 || len(a.list("")) != 1 {
			t.Fatalf("sends=%d, thông báo=%d", p.sends(), len(a.list("")))
		}
	})
	t.Run("không có token → không gửi, thông báo vẫn có", func(t *testing.T) {
		p := &fakePusher{}
		f := newFixture(t, p)
		coop := ids.New()
		a := f.newMember(coop)
		if _, err := f.taskDue(coop, a.actor); err != nil {
			t.Fatal(err)
		}
		items := a.list("")
		if len(items) != 1 {
			t.Fatalf("thông báo phải có trong trung tâm, có %d", len(items))
		}
		if ds := f.deliveries(str(items[0], "id")); len(ds) != 0 {
			t.Fatalf("không có token mà vẫn xếp lượt gửi: %+v", ds)
		}
		const requestedSQL = `SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`
		if n := f.count(requestedSQL, f.tenant, notifyport.EventPushRequested); n != 0 {
			t.Fatalf("không có token mà phát pf.notify.push.requested %d lần", n)
		}
	})
}

// AC4: chỉ Thành viên HTX của đúng HTX nhận; người nhận sai → từ chối cả lần gọi, không tạo gì; gọi lại không tạo trùng.
func TestAH22_AC4_NguoiNhan(t *testing.T) {
	f := newFixture(t, nil)
	coop, otherCoop := ids.New(), ids.New()
	a, b := f.newMember(coop), f.newMember(coop)
	c := f.newMember(otherCoop)
	admin := f.newAdmin()

	for name, recipients := range map[string][]uuid.UUID{
		"có quản trị nền tảng":       {a.actor, admin.actor},
		"có Thành viên HTX khác":     {a.actor, c.actor},
		"có tài khoản không tồn tại": {a.actor, ids.New()},
	} {
		_, err := f.taskDue(coop, recipients...)
		if p := problem.From(err); p.Code != "notify.recipient_not_member" {
			t.Fatalf("%s: lỗi = %v", name, err)
		}
	}
	if n := len(a.list("")); n != 0 {
		t.Fatalf("bị từ chối mà A vẫn có %d thông báo", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_notify.notification WHERE tenant_id = $1`, f.tenant); n != 0 {
		t.Fatalf("bị từ chối mà có %d thông báo trong bảng", n)
	}

	// A và B có token đẩy để lần gọi hợp lệ xếp lượt gửi thật.
	a.registerToken()
	b.registerToken()
	notice, err := f.taskDue(coop, a.actor, b.actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.list("")) != 1 || len(b.list("")) != 1 {
		t.Fatal("mỗi Thành viên phải có đúng 1 thông báo")
	}
	if len(admin.list("")) != 0 || len(c.list("")) != 0 {
		t.Fatal("quản trị và Thành viên HTX khác không được nhận")
	}

	// Gọi lại cùng SourceID: không tạo trùng, không xếp thêm lượt gửi.
	const deliveriesSQL = `SELECT count(*) FROM pf_notify.push_delivery WHERE tenant_id = $1`
	const requestedSQL = `SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`
	before := f.count(deliveriesSQL, f.tenant)
	if before != 2 {
		t.Fatalf("lần gọi đầu phải xếp 2 lượt gửi (A, B), có %d", before)
	}
	requestedBefore := f.count(requestedSQL, f.tenant, notifyport.EventPushRequested)
	if requestedBefore == 0 {
		t.Fatal("lần gọi đầu phải phát pf.notify.push.requested")
	}
	if err := f.mod.MemberNotifier().TaskDue(context.Background(), f.tenant, notice); err != nil {
		t.Fatal(err)
	}
	if len(a.list("")) != 1 || len(b.list("")) != 1 {
		t.Fatal("gọi lại cùng SourceID không được tạo bản thứ hai")
	}
	if after := f.count(deliveriesSQL, f.tenant); after != before {
		t.Fatalf("lượt gửi tăng từ %d lên %d khi gọi lại", before, after)
	}
	if after := f.count(requestedSQL, f.tenant, notifyport.EventPushRequested); after != requestedBefore {
		t.Fatalf("pf.notify.push.requested tăng từ %d lên %d khi gọi lại", requestedBefore, after)
	}
}

// AC5: mỗi tài khoản chỉ đọc và đánh dấu thông báo của chính mình.
func TestAH22_AC5_QuyenServer(t *testing.T) {
	f := newFixture(t, nil)
	coop := ids.New()
	a, b := f.newMember(coop), f.newMember(coop)
	if _, err := f.taskDue(coop, a.actor); err != nil {
		t.Fatal(err)
	}
	aid := str(a.list("")[0], "id")

	r := b.do(http.MethodPost, "/v1/notifications/"+aid+"/read", nil)
	if r.status != http.StatusNotFound || r.str("code") != "notify.notification_not_found" {
		t.Fatalf("B đọc thông báo của A: %d %v", r.status, r.body)
	}
	if a.unread() != 1 || a.list("")[0]["read_at"] != nil {
		t.Fatal("thông báo của A phải vẫn chưa đọc")
	}
	if len(b.list("")) != 0 {
		t.Fatal("B không được thấy thông báo của A")
	}
	if r := b.do(http.MethodPost, "/v1/notifications/read-all", nil); r.status != http.StatusOK || r.num("marked") != 0 {
		t.Fatalf("read-all của B: %d %v", r.status, r.body)
	}
	if a.unread() != 1 {
		t.Fatal("Đọc tất cả của B không được đổi số chưa đọc của A")
	}
}
