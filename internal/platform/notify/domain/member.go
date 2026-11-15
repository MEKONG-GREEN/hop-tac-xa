package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
)

const dateLayout = "2006-01-02"

func invalid(field, reason string) error { return port.ErrMemberNoticeInvalid.WithField(field, reason) }

// ValidateMemberBase kiểm phần chung và trả người nhận đã bỏ trùng.
func ValidateMemberBase(b port.MemberNoticeBase) ([]uuid.UUID, error) {
	if b.SourceID == uuid.Nil || b.SourceID.Version() != 7 {
		return nil, invalid("source_id", "phải là UUIDv7")
	}
	if b.CooperativeRef == uuid.Nil {
		return nil, invalid("cooperative_ref", "bắt buộc")
	}
	rs := DedupRefs(b.Recipients)
	if len(rs) == 0 {
		return nil, invalid("recipients", "cần ít nhất một người nhận")
	}
	if len(rs) > port.MaxMemberRecipients {
		return nil, invalid("recipients", fmt.Sprintf("tối đa %d người nhận", port.MaxMemberRecipients))
	}
	return rs, nil
}

func requireText(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", invalid(field, "bắt buộc")
	}
	return v, nil
}

func requireRef(field string, id uuid.UUID) error {
	if id == uuid.Nil {
		return invalid(field, "bắt buộc")
	}
	return nil
}

func parseDate(field, v string) (time.Time, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(v))
	if err != nil {
		return time.Time{}, invalid(field, "phải có dạng YYYY-MM-DD")
	}
	return t, nil
}

// viDate hiển thị ngày dạng 15/09/2026.
func viDate(t time.Time) string { return t.Format("02/01/2006") }

// CalendarPublishedTemplate: NTF.1 — lịch thời vụ được phát.
func CalendarPublishedTemplate(n port.CalendarPublishedNotice, now time.Time) (Template, error) {
	if _, err := ValidateMemberBase(n.MemberNoticeBase); err != nil {
		return Template{}, err
	}
	if err := requireRef("calendar_ref", n.CalendarRef); err != nil {
		return Template{}, err
	}
	crop, err := requireText("crop_name", n.CropName)
	if err != nil {
		return Template{}, err
	}
	season, err := requireText("season_name", n.SeasonName)
	if err != nil {
		return Template{}, err
	}
	if n.Year < 2000 || n.Year > 2100 {
		return Template{}, invalid("year", "phải từ 2000 đến 2100")
	}
	from, err := parseDate("sow_from", n.SowFrom)
	if err != nil {
		return Template{}, err
	}
	to, err := parseDate("sow_to", n.SowTo)
	if err != nil {
		return Template{}, err
	}
	if from.After(to) {
		return Template{}, invalid("sow_from", "không được sau sow_to")
	}
	id := n.CalendarRef
	return Template{Kind: port.KindSeasonCalendarPublished, Severity: port.SeverityNormal,
		Title:      fmt.Sprintf("Lịch thời vụ mới: %s, vụ %s/%d", crop, season, n.Year),
		Body:       fmt.Sprintf("Khung ngày xuống giống từ %s đến %s.", viDate(from), viDate(to)),
		DeepLink:   "/cropcycle/calendars/" + id.String(),
		SubjectRef: &id, CreatedAt: now}, nil
}

// TaskDueTemplate: NTF.2 — việc đến hạn hoặc trễ hạn.
func TaskDueTemplate(n port.TaskDueNotice, now time.Time) (Template, error) {
	if _, err := ValidateMemberBase(n.MemberNoticeBase); err != nil {
		return Template{}, err
	}
	if err := requireRef("season_ref", n.SeasonRef); err != nil {
		return Template{}, err
	}
	if err := requireRef("task_ref", n.TaskRef); err != nil {
		return Template{}, err
	}
	house, err := requireText("household_code", n.HouseholdCode)
	if err != nil {
		return Template{}, err
	}
	task, err := requireText("task_name", n.TaskName)
	if err != nil {
		return Template{}, err
	}
	due, err := parseDate("due_date", n.DueDate)
	if err != nil {
		return Template{}, err
	}
	label := "Việc đến hạn: "
	if n.Overdue {
		label = "Việc trễ hạn: "
	}
	id := n.TaskRef
	return Template{Kind: port.KindSeasonTaskDue, Severity: port.SeverityNormal,
		Title:      label + task,
		Body:       fmt.Sprintf("Nông hộ %s, hạn %s.", house, viDate(due)),
		DeepLink:   "/cropcycle/seasons/" + n.SeasonRef.String() + "/tasks/" + id.String(),
		SubjectRef: &id, CreatedAt: now}, nil
}

// LogMissingTemplate: NTF.3 — vụ quá số ngày chưa có nhật ký.
func LogMissingTemplate(n port.LogMissingNotice, now time.Time) (Template, error) {
	if _, err := ValidateMemberBase(n.MemberNoticeBase); err != nil {
		return Template{}, err
	}
	if err := requireRef("season_ref", n.SeasonRef); err != nil {
		return Template{}, err
	}
	house, err := requireText("household_code", n.HouseholdCode)
	if err != nil {
		return Template{}, err
	}
	if n.Days < 1 {
		return Template{}, invalid("days", "phải từ 1 trở lên")
	}
	id := n.SeasonRef
	return Template{Kind: port.KindSeasonLogMissing, Severity: port.SeverityNormal,
		Title:      fmt.Sprintf("Vụ đã %d ngày chưa có nhật ký", n.Days),
		Body:       fmt.Sprintf("Nông hộ %s — mở vụ để ghi nhật ký.", house),
		DeepLink:   "/cropcycle/seasons/" + id.String(),
		SubjectRef: &id, CreatedAt: now}, nil
}

// ProcessChangedTemplate: NTF.4 — quy trình của vụ đổi do đổi giống.
func ProcessChangedTemplate(n port.ProcessChangedNotice, now time.Time) (Template, error) {
	if _, err := ValidateMemberBase(n.MemberNoticeBase); err != nil {
		return Template{}, err
	}
	if err := requireRef("season_ref", n.SeasonRef); err != nil {
		return Template{}, err
	}
	house, err := requireText("household_code", n.HouseholdCode)
	if err != nil {
		return Template{}, err
	}
	variety, err := requireText("variety_name", n.VarietyName)
	if err != nil {
		return Template{}, err
	}
	if n.ChangedTasks < 1 {
		return Template{}, invalid("changed_tasks", "phải từ 1 trở lên")
	}
	id := n.SeasonRef
	return Template{Kind: port.KindSeasonProcessChanged, Severity: port.SeverityNormal,
		Title:      fmt.Sprintf("Quy trình của vụ đã thay đổi: %d việc thay đổi", n.ChangedTasks),
		Body:       fmt.Sprintf("Nông hộ %s, vụ đổi sang giống %s.", house, variety),
		DeepLink:   "/cropcycle/seasons/" + id.String(),
		SubjectRef: &id, CreatedAt: now}, nil
}
