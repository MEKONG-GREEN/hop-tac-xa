package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

var memberNow = time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)

func memberBase() port.MemberNoticeBase {
	return port.MemberNoticeBase{SourceID: uuid.Must(uuid.NewV7()), CooperativeRef: uuid.Must(uuid.NewV7()),
		Recipients: []uuid.UUID{uuid.Must(uuid.NewV7())}}
}

func TestAH22_AC2_MemberTemplates(t *testing.T) {
	cal, season, task := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	cases := []struct {
		name                              string
		tpl                               func() (Template, error)
		kind, title, body, link, subjectS string
	}{
		{"NTF.1", func() (Template, error) {
			return CalendarPublishedTemplate(port.CalendarPublishedNotice{MemberNoticeBase: memberBase(), CalendarRef: cal,
				CropName: "Lúa", SeasonName: "Đông Xuân", Year: 2026, SowFrom: "2026-11-05", SowTo: "2026-11-20"}, memberNow)
		}, port.KindSeasonCalendarPublished, "Lịch thời vụ mới: Lúa, vụ Đông Xuân/2026",
			"Khung ngày xuống giống từ 05/11/2026 đến 20/11/2026.", "/cropcycle/calendars/" + cal.String(), cal.String()},
		{"NTF.2 đến hạn", func() (Template, error) {
			return TaskDueTemplate(port.TaskDueNotice{MemberNoticeBase: memberBase(), SeasonRef: season, TaskRef: task,
				HouseholdCode: "NH001", TaskName: "Bón phân", DueDate: "2026-10-02"}, memberNow)
		}, port.KindSeasonTaskDue, "Việc đến hạn: Bón phân", "Nông hộ NH001, hạn 02/10/2026.",
			"/cropcycle/seasons/" + season.String() + "/tasks/" + task.String(), task.String()},
		{"NTF.2 trễ hạn", func() (Template, error) {
			return TaskDueTemplate(port.TaskDueNotice{MemberNoticeBase: memberBase(), SeasonRef: season, TaskRef: task,
				HouseholdCode: "NH001", TaskName: "Bón phân", DueDate: "2026-10-02", Overdue: true}, memberNow)
		}, port.KindSeasonTaskDue, "Việc trễ hạn: Bón phân", "Nông hộ NH001, hạn 02/10/2026.",
			"/cropcycle/seasons/" + season.String() + "/tasks/" + task.String(), task.String()},
		{"NTF.3", func() (Template, error) {
			return LogMissingTemplate(port.LogMissingNotice{MemberNoticeBase: memberBase(), SeasonRef: season,
				HouseholdCode: "NH001", Days: 4}, memberNow)
		}, port.KindSeasonLogMissing, "Vụ đã 4 ngày chưa có nhật ký", "Nông hộ NH001 — mở vụ để ghi nhật ký.",
			"/cropcycle/seasons/" + season.String(), season.String()},
		{"NTF.4", func() (Template, error) {
			return ProcessChangedTemplate(port.ProcessChangedNotice{MemberNoticeBase: memberBase(), SeasonRef: season,
				HouseholdCode: "NH001", VarietyName: "ST25", ChangedTasks: 3}, memberNow)
		}, port.KindSeasonProcessChanged, "Quy trình của vụ đã thay đổi: 3 việc thay đổi", "Nông hộ NH001, vụ đổi sang giống ST25.",
			"/cropcycle/seasons/" + season.String(), season.String()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl, err := c.tpl()
			if err != nil {
				t.Fatal(err)
			}
			if tpl.Kind != c.kind || tpl.Title != c.title || tpl.Body != c.body || tpl.DeepLink != c.link {
				t.Fatalf("template = %+v", tpl)
			}
			if tpl.SubjectRef == nil || tpl.SubjectRef.String() != c.subjectS {
				t.Fatalf("subject_ref = %v, muốn %s", tpl.SubjectRef, c.subjectS)
			}
			if !tpl.CreatedAt.Equal(memberNow) {
				t.Fatalf("created_at = %v", tpl.CreatedAt)
			}
			n := tpl.For(uuid.Must(uuid.NewV7()))
			if !n.Normalize() {
				t.Fatal("Normalize() = false")
			}
		})
	}
}

func TestAH22_MemberNoticeValidation(t *testing.T) {
	season := uuid.Must(uuid.NewV7())
	logOf := func(mut func(*port.LogMissingNotice)) func() error {
		return func() error {
			n := port.LogMissingNotice{MemberNoticeBase: memberBase(), SeasonRef: season, HouseholdCode: "NH001", Days: 3}
			mut(&n)
			_, err := LogMissingTemplate(n, memberNow)
			return err
		}
	}
	calOf := func(mut func(*port.CalendarPublishedNotice)) func() error {
		return func() error {
			n := port.CalendarPublishedNotice{MemberNoticeBase: memberBase(), CalendarRef: uuid.Must(uuid.NewV7()),
				CropName: "Lúa", SeasonName: "Đông Xuân", Year: 2026, SowFrom: "2026-11-05", SowTo: "2026-11-20"}
			mut(&n)
			_, err := CalendarPublishedTemplate(n, memberNow)
			return err
		}
	}
	many := make([]uuid.UUID, port.MaxMemberRecipients+1)
	for i := range many {
		many[i] = uuid.Must(uuid.NewV7())
	}
	cases := []struct {
		name  string
		run   func() error
		field string
	}{
		{"source nil", logOf(func(n *port.LogMissingNotice) { n.SourceID = uuid.Nil }), "source_id"},
		{"source không phải v7", logOf(func(n *port.LogMissingNotice) { n.SourceID = uuid.New() }), "source_id"},
		{"HTX nil", logOf(func(n *port.LogMissingNotice) { n.CooperativeRef = uuid.Nil }), "cooperative_ref"},
		{"không người nhận", logOf(func(n *port.LogMissingNotice) { n.Recipients = nil }), "recipients"},
		{"toàn Nil", logOf(func(n *port.LogMissingNotice) { n.Recipients = []uuid.UUID{uuid.Nil} }), "recipients"},
		{"quá trần", logOf(func(n *port.LogMissingNotice) { n.Recipients = many }), "recipients"},
		{"days = 0", logOf(func(n *port.LogMissingNotice) { n.Days = 0 }), "days"},
		{"mã nông hộ rỗng", logOf(func(n *port.LogMissingNotice) { n.HouseholdCode = "  " }), "household_code"},
		{"vụ nil", logOf(func(n *port.LogMissingNotice) { n.SeasonRef = uuid.Nil }), "season_ref"},
		{"SowFrom sau SowTo", calOf(func(n *port.CalendarPublishedNotice) { n.SowFrom, n.SowTo = "2026-11-21", "2026-11-20" }), "sow_from"},
		{"ngày sai dạng", calOf(func(n *port.CalendarPublishedNotice) { n.SowTo = "20/11/2026" }), "sow_to"},
		{"năm ngoài khoảng", calOf(func(n *port.CalendarPublishedNotice) { n.Year = 1999 }), "year"},
		{"tên cây rỗng", calOf(func(n *port.CalendarPublishedNotice) { n.CropName = "" }), "crop_name"},
		{"changed_tasks = 0", func() error {
			_, err := ProcessChangedTemplate(port.ProcessChangedNotice{MemberNoticeBase: memberBase(), SeasonRef: season,
				HouseholdCode: "NH001", VarietyName: "ST25"}, memberNow)
			return err
		}, "changed_tasks"},
		{"tên việc rỗng", func() error {
			_, err := TaskDueTemplate(port.TaskDueNotice{MemberNoticeBase: memberBase(), SeasonRef: season, TaskRef: uuid.Must(uuid.NewV7()),
				HouseholdCode: "NH001", DueDate: "2026-10-02"}, memberNow)
			return err
		}, "task_name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.run()
			if err == nil {
				t.Fatal("muốn lỗi")
			}
			p := problem.From(err)
			if p.Code != "notify.member_notice_invalid" || len(p.Errors) == 0 || p.Errors[0].Field != c.field {
				t.Fatalf("lỗi = %+v, muốn trường %s", p, c.field)
			}
		})
	}
}

func TestAH22_MemberHouseholdCodeRedacted(t *testing.T) {
	tpl, err := LogMissingTemplate(port.LogMissingNotice{MemberNoticeBase: memberBase(), SeasonRef: uuid.Must(uuid.NewV7()),
		HouseholdCode: "079123456789", Days: 3}, memberNow)
	if err != nil {
		t.Fatal(err)
	}
	n := tpl.For(uuid.Must(uuid.NewV7()))
	n.Normalize()
	if strings.Contains(n.Body, "079123456789") {
		t.Fatalf("body còn chuỗi giống CCCD: %q", n.Body)
	}
}
