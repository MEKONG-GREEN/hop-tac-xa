package app

// AH-191 (NTF.2, NTF-05, NTF-BR-03/04, NTF-EX-02, KH-21): lúc 07:00 giờ Việt Nam nhắc Thành viên HTX việc vừa chuyển
// "Đến hạn" và việc vừa chuyển "Trễ" của vụ nông hộ đang canh tác.
//
//   - Mỗi việc tối đa hai nhắc (một due, một overdue): khóa chính pf_coop.task_reminder chặn trùng khi chạy lại.
//   - Gửi qua hai pha: ghi lượt nhắc pending trong giao dịch, nhận nguyên tử (UPDATE … WHERE status='pending'
//     RETURNING), rồi gọi pf/notify NGOÀI giao dịch với SourceID cố định — dừng giữa chừng thì lượt sau gửi lại cùng
//     SourceID và pf/notify không tạo bản trùng.
//   - Kiểm lại lúc gửi: việc đã Xong/Bỏ qua hoặc vụ không còn canh tác thì lượt nhắc thành dropped kèm lý do.
//   - Việc "Không ngày" không nhắc; việc cửa sổ Mở chỉ nhắc Đến hạn. Mỗi vụ một thông báo cho mỗi loại nhắc (KH-21).
//   - Chỉ nhắc chuyển trạng thái trong ReminderCatchUpDays ngày gần nhất (bù khi worker lỡ một lượt), không dồn nhắc
//     các việc đã trễ từ lâu.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

const (
	// ReminderCatchUpDays: lượt chạy vẫn nhắc việc chuyển trạng thái trong số ngày này (worker lỡ lượt 07:00).
	ReminderCatchUpDays = 3
	// ReminderClaimTimeout: lượt nhắc đã nhận quá lâu mà chưa đóng được coi là của tiến trình đã dừng.
	ReminderClaimTimeout = 30 * time.Minute
	reminderPage         = 200
	memberPage           = 200

	ReminderDue     = "due"
	ReminderOverdue = "overdue"
)

// TenantLister liệt kê tenant cần chạy job.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// ReminderResult là số liệu của một lượt nhắc.
type ReminderResult struct {
	Tenants, Seasons, Notices, Sent, Dropped, Failed int
	// AH-192: nhắc vụ quá 3 ngày chưa có nhật ký.
	LogNotices, LogSent, LogDropped int
}

// TaskRemindersReady kiểm phụ thuộc khi khởi động worker (danh bạ thành viên và cổng thông báo).
func (s *Service) TaskRemindersReady() error {
	if s.CalendarMembers == nil || s.TaskNotifier == nil {
		return errors.New("task reminders: configure accounts directory and notify before starting the job")
	}
	return nil
}

// reminderCandidates lập lượt nhắc cho các việc vừa chuyển Đến hạn hoặc Trễ (trong ReminderCatchUpDays ngày).
func reminderCandidates(cur store.Season, progress domain.Checklist, today time.Time) []store.TaskReminder {
	since := today.AddDate(0, 0, -ReminderCatchUpDays)
	notice := map[string]uuid.UUID{}
	var out []store.TaskReminder
	add := func(t domain.TaskProgress, kind string, due time.Time) {
		if notice[kind] == uuid.Nil {
			notice[kind] = ids.New()
		}
		out = append(out, store.TaskReminder{SeasonRef: cur.SeasonRef, CooperativeRef: cur.CooperativeRef, HouseholdRef: cur.HouseholdRef,
			NoticeID: notice[kind], StepSeq: t.Seq, Kind: kind, TaskName: t.Name, DueDate: due, BusinessDate: today})
	}
	for _, st := range progress.Stages {
		for _, t := range st.Tasks {
			if t.DoneOn != nil || t.Undated || t.WindowKind == domain.WindowNone || t.FromDate == nil {
				continue
			}
			from, err := domain.ParseDay(*t.FromDate)
			if err != nil {
				continue
			}
			switch t.CompletionStatus {
			case domain.ScheduleDue:
				if !from.Before(since) {
					due := from
					if t.ToDate != nil {
						if to, err := domain.ParseDay(*t.ToDate); err == nil {
							due = to
						}
					}
					add(t, ReminderDue, due)
				}
			case domain.ScheduleOverdue:
				if t.WindowKind == domain.WindowOpen || t.ToDate == nil {
					continue // cửa sổ Mở chỉ nhắc Đến hạn
				}
				to, err := domain.ParseDay(*t.ToDate)
				if err == nil && !to.AddDate(0, 0, 1).Before(since) {
					add(t, ReminderOverdue, to)
				}
			}
		}
	}
	return out
}

// RunTaskReminders chạy một lượt nhắc cho mọi tenant; lỗi một tenant không chặn tenant khác.
func (s *Service) RunTaskReminders(ctx context.Context, tenants TenantLister) (ReminderResult, error) {
	var res ReminderResult
	if err := s.TaskRemindersReady(); err != nil {
		return res, ErrDependency.WithCause(err)
	}
	list, err := tenants(ctx)
	if err != nil {
		return res, err
	}
	var errs []error
	for _, tid := range list {
		res.Tenants++
		if err := s.remindTenant(ctx, tid, &res); err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return res, errors.Join(errs...)
}

func (s *Service) remindTenant(ctx context.Context, tid uuid.UUID, res *ReminderResult) error {
	today, now := s.today(), s.now()
	after := uuid.Nil
	for {
		var page []store.Season
		err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			var err error
			if page, err = store.OpenHouseholdSeasons(ctx, tx, after, reminderPage); err != nil {
				return err
			}
			var rows []store.TaskReminder
			for _, cur := range page {
				list, err := s.householdChecklistTx(ctx, tx, cur, true)
				if err != nil {
					s.logWarn("bỏ qua vụ khi lập nhắc việc", "season_ref", cur.SeasonRef, "error", err)
					continue
				}
				rows = append(rows, reminderCandidates(cur, list.Progress, today)...)
			}
			if err := store.InsertTaskReminders(ctx, tx, rows); err != nil {
				return err
			}
			// AH-192: vụ quá LogMissingDays ngày chưa có nhật ký (tính từ bản ghi gần nhất hoặc ngày mở vụ).
			refs := make([]uuid.UUID, 0, len(page))
			for _, cur := range page {
				refs = append(refs, cur.SeasonRef)
			}
			last, err := store.LastLogDays(ctx, tx, refs)
			if err != nil {
				return err
			}
			var missing []store.LogMissingReminder
			for _, cur := range page {
				if r, ok := logMissingCandidate(cur, last[cur.SeasonRef], today); ok {
					missing = append(missing, r)
				}
			}
			return store.InsertLogMissingReminders(ctx, tx, missing)
		})
		if err != nil {
			return err
		}
		res.Seasons += len(page)
		if len(page) < reminderPage {
			break
		}
		after = page[len(page)-1].SeasonRef
	}
	var claimed []store.TaskReminder
	if err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		claimed, err = store.ClaimTaskReminders(ctx, tx, now, now.Add(-ReminderClaimTimeout))
		return err
	}); err != nil {
		return err
	}
	groups := map[uuid.UUID][]store.TaskReminder{}
	var order []uuid.UUID
	for _, r := range claimed {
		if _, ok := groups[r.NoticeID]; !ok {
			order = append(order, r.NoticeID)
		}
		groups[r.NoticeID] = append(groups[r.NoticeID], r)
	}
	members := map[uuid.UUID][]uuid.UUID{}
	var errs []error
	for _, id := range order {
		res.Notices++
		if err := s.sendReminder(ctx, tid, id, groups[id], members, res); err != nil {
			res.Failed++
			errs = append(errs, err)
		}
	}
	var missing []store.LogMissingReminder
	if err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		missing, err = store.ClaimLogMissingReminders(ctx, tx, now, now.Add(-ReminderClaimTimeout))
		return err
	}); err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, r := range missing {
		res.LogNotices++
		if err := s.sendLogMissing(ctx, tid, r, members, res); err != nil {
			res.Failed++
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// LogMissingDays (AH-192, NTF-04): vụ "quá 3 ngày" chưa có nhật ký thì nhắc nhẹ một lần cho mỗi mốc.
const LogMissingDays = 3

// logMissingCandidate lập lượt nhắc khi vụ đang canh tác đã quá LogMissingDays ngày từ bản ghi gần nhất (hoặc ngày mở
// vụ nếu chưa có). Mốc là ngày đó: có bản ghi mới thì mốc đổi, lần vượt ngưỡng sau là một lượt mới.
func logMissingCandidate(cur store.Season, lastLog, today time.Time) (store.LogMissingReminder, bool) {
	since := businessDay(cur.OpenedAt)
	if !lastLog.IsZero() {
		if d := domain.Day(lastLog.Year(), lastLog.Month(), lastLog.Day()); d.After(since) {
			since = d
		}
	}
	days := int(today.Sub(since).Hours() / 24)
	if days <= LogMissingDays {
		return store.LogMissingReminder{}, false
	}
	return store.LogMissingReminder{SeasonRef: cur.SeasonRef, CooperativeRef: cur.CooperativeRef, HouseholdRef: cur.HouseholdRef,
		NoticeID: ids.New(), SinceDate: since, BusinessDate: today, Days: days}, true
}

// sendLogMissing kiểm lại lúc gửi (vụ còn canh tác, chưa có bản ghi mới) rồi gửi NTF.3 cho Thành viên HTX.
func (s *Service) sendLogMissing(ctx context.Context, tid uuid.UUID, r store.LogMissingReminder, members map[uuid.UUID][]uuid.UUID, res *ReminderResult) error {
	stale, code := false, ""
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		cur, err := store.SeasonByRef(ctx, tx, r.SeasonRef, false)
		if err != nil {
			return err
		}
		if cur == nil || cur.EntryKind != "household" || cur.Status != port.SeasonOpen {
			stale = true
			return nil
		}
		last, err := store.LastLogDays(ctx, tx, []uuid.UUID{r.SeasonRef})
		if err != nil {
			return err
		}
		if c, ok := logMissingCandidate(*cur, last[r.SeasonRef], s.today()); !ok || !c.SinceDate.Equal(r.SinceDate) {
			stale = true
			return nil
		}
		code, err = store.HouseholdNativeCode(ctx, tx, cur.HouseholdRef)
		return err
	})
	if err != nil {
		return err
	}
	finish := func(status, reason string) error {
		return s.inTx(ctx, tid, func(tx pgx.Tx) error {
			return store.FinishLogMissingReminder(ctx, tx, r.NoticeID, status, reason, s.now())
		})
	}
	if stale {
		res.LogDropped++
		return finish("dropped", "logged_or_season_closed")
	}
	recipients, ok := members[r.CooperativeRef]
	if !ok {
		if recipients, err = s.coopMembers(ctx, tid, r.CooperativeRef); err != nil {
			return err
		}
		members[r.CooperativeRef] = recipients
	}
	if len(recipients) == 0 {
		res.LogDropped++
		return finish("dropped", "no_active_member")
	}
	if code == "" {
		code = "H-" + r.HouseholdRef.String()[:8]
	}
	if err := s.TaskNotifier.LogMissing(ctx, tid, port.LogMissingV1{SourceID: r.NoticeID, CooperativeRef: r.CooperativeRef, SeasonRef: r.SeasonRef,
		HouseholdCode: code, Days: r.Days, Recipients: recipients}); err != nil {
		return err // giữ pending; lượt sau gửi lại cùng SourceID
	}
	res.LogSent++
	return finish("sent", "")
}

// sendReminder kiểm lại vụ và việc lúc gửi rồi gửi một thông báo cho cả nhóm (một vụ, một loại nhắc).
func (s *Service) sendReminder(ctx context.Context, tid, notice uuid.UUID, rows []store.TaskReminder, members map[uuid.UUID][]uuid.UUID, res *ReminderResult) error {
	first := rows[0]
	var keep, drop []int
	var code string
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		cur, err := store.SeasonByRef(ctx, tx, first.SeasonRef, false)
		if err != nil {
			return err
		}
		if cur == nil || cur.EntryKind != "household" || cur.Status != port.SeasonOpen {
			for _, r := range rows {
				drop = append(drop, r.StepSeq)
			}
			return nil
		}
		list, err := s.householdChecklistTx(ctx, tx, *cur, true)
		if err != nil {
			return err
		}
		open := map[int]bool{}
		for _, st := range list.Progress.Stages {
			for _, t := range st.Tasks {
				if t.DoneOn == nil && (t.CompletionStatus == domain.ScheduleDue || t.CompletionStatus == domain.ScheduleOverdue) {
					open[t.Seq] = true
				}
			}
		}
		for _, r := range rows {
			if open[r.StepSeq] {
				keep = append(keep, r.StepSeq)
			} else {
				drop = append(drop, r.StepSeq)
			}
		}
		code, err = store.HouseholdNativeCode(ctx, tx, cur.HouseholdRef)
		return err
	})
	if err != nil {
		return err
	}
	finish := func(steps []int, status, reason string) error {
		if len(steps) == 0 {
			return nil
		}
		return s.inTx(ctx, tid, func(tx pgx.Tx) error {
			return store.FinishTaskReminders(ctx, tx, notice, steps, status, reason, s.now())
		})
	}
	if err := finish(drop, "dropped", "task_resolved_or_season_closed"); err != nil {
		return err
	}
	res.Dropped += len(drop)
	if len(keep) == 0 {
		return nil
	}
	recipients, ok := members[first.CooperativeRef]
	if !ok {
		if recipients, err = s.coopMembers(ctx, tid, first.CooperativeRef); err != nil {
			return err
		}
		members[first.CooperativeRef] = recipients
	}
	if len(recipients) == 0 {
		res.Dropped += len(keep)
		return finish(keep, "dropped", "no_active_member")
	}
	kept := make([]store.TaskReminder, 0, len(keep))
	for _, r := range rows {
		for _, k := range keep {
			if r.StepSeq == k {
				kept = append(kept, r)
			}
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		return kept[i].DueDate.Before(kept[j].DueDate) || kept[i].DueDate.Equal(kept[j].DueDate) && kept[i].StepSeq < kept[j].StepSeq
	})
	name := kept[0].TaskName
	if len(kept) > 1 {
		name = fmt.Sprintf("%s và %d việc khác", name, len(kept)-1)
	}
	if code == "" {
		code = "H-" + first.HouseholdRef.String()[:8]
	}
	if err := s.TaskNotifier.TaskDue(ctx, tid, port.TaskDueV1{SourceID: notice, CooperativeRef: first.CooperativeRef, SeasonRef: first.SeasonRef,
		HouseholdCode: code, TaskName: name, DueDate: domain.FormatDay(kept[0].DueDate), Overdue: first.Kind == ReminderOverdue, Recipients: recipients}); err != nil {
		return err // lượt nhắc giữ pending; lượt sau gửi lại cùng SourceID
	}
	res.Sent += len(keep)
	return finish(keep, "sent", "")
}

// coopMembers lấy mọi Thành viên HTX đang hoạt động của HTX (KH-02) qua danh bạ, ngoài giao dịch.
func (s *Service) coopMembers(ctx context.Context, tid, coop uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	offset := 0
	for range 1000 {
		page, err := s.CalendarMembers.Page(ctx, tid, coop, offset, memberPage)
		if err != nil {
			return nil, ErrDependency.WithCause(err)
		}
		out = append(out, page.Refs...)
		if page.NextOffset == 0 || page.NextOffset <= offset {
			break
		}
		offset = page.NextOffset
	}
	return out, nil
}

func (s *Service) logWarn(msg string, args ...any) {
	if s.Log != nil {
		s.Log.Warn(msg, args...)
	}
}
