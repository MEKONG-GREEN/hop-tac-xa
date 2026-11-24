package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/lunar"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Trạng thái số liệu của một HTX.
const (
	StatusOK         = "ok"
	StatusNoLocation = "no_location" // HTX chưa có tọa độ (FN-TT-01/EX-02)
	StatusNoData     = "no_data"     // chưa có số liệu lần nào (FN-TT-01/EX-03)
)

// Câu thông báo nguyên văn BRD.
const (
	MsgNoLocation = "Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật."
	MsgNoData     = "Chưa có số liệu thời tiết"
)

// Nhãn số liệu ngày đã qua (FN-TT-02/BR-04).
const (
	LabelObserved      = "Thực đo"
	LabelFinalForecast = "Dự báo cuối ngày"
)

// Source là nguồn và độ mới của số liệu.
type Source struct {
	Provider  string
	Label     string
	UpdatedAt *time.Time // lần lấy của bản dự báo đang hiển thị
	Stale     bool       // lần lấy gần nhất lỗi hoặc số liệu cũ hơn hai chu kỳ (EX-01: hiện "Cập nhật lúc …")
}

// Today là thẻ Thời tiết hôm nay (FN-TT-01).
type Today struct {
	CooperativeRef uuid.UUID
	Day            string
	Lunar          lunar.Date
	Status         string
	Current        *domain.Reading
	Summary        *domain.DaySummary
	Source         Source
}

// CalendarDay là một ngày của lịch tháng (FN-TT-02).
type CalendarDay struct {
	Day     string
	Lunar   lunar.Date
	Kind    string // forecast | observed | final_forecast | "" (không có số liệu)
	Label   string
	Summary *domain.DaySummary
	Periods []domain.Period
	Hourly  bool // có số liệu theo giờ; false → chi tiết chỉ một khối cả ngày (EX-01)
}

// Calendar là lịch tháng.
type Calendar struct {
	CooperativeRef uuid.UUID
	Month          string // YYYY-MM
	Status         string
	ForecastDays   int
	Days           []CalendarDay
	Source         Source
}

// scope trả HTX được xem: Đại diện HTX, Thành viên HTX → HTX của mình; Admin → HTX chọn ở bộ lọc (mọi HTX — AC-06).
func (s *Service) scope(ctx context.Context, want uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	p, err := authz.Require(ctx, htxroles.Admin, htxroles.Representative, htxroles.Member)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if tid, err := tenant.FromContext(ctx); err != nil || tid != p.TenantID {
		return uuid.Nil, uuid.Nil, problem.Unauthenticated
	}
	if p.Has(htxroles.Admin) {
		if want == uuid.Nil {
			return uuid.Nil, uuid.Nil, problem.Validation.WithField("cooperative_ref", "Chọn HTX")
		}
		return p.TenantID, want, nil
	}
	if p.OrgRef == uuid.Nil {
		return uuid.Nil, uuid.Nil, problem.NodeScopeRequired
	}
	if want != uuid.Nil && want != p.OrgRef {
		return uuid.Nil, uuid.Nil, problem.Forbidden
	}
	return p.TenantID, p.OrgRef, nil
}

// site trả tọa độ trụ sở của HTX đang hoạt động; HTX không có trong tenant → NotFound; chưa có tọa độ → nil.
func (s *Service) site(ctx context.Context, coop uuid.UUID) (*domain.Point, error) {
	if s.Coops == nil {
		return nil, ErrDependency
	}
	sites, err := s.Coops.Active(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range sites {
		if c.Ref == coop {
			if c.Lat == nil || c.Lon == nil {
				return nil, nil
			}
			p := domain.Point{Lat: *c.Lat, Lon: *c.Lon}
			return &p, nil
		}
	}
	return nil, problem.NotFound
}

// snapshot đọc trong một giao dịch: cấu hình, trạng thái lấy, điểm gần trụ sở, bản dự báo và số liệu ngày [from, to].
type snapshot struct {
	cfg      *store.ConfigRow
	state    domain.FetchState
	loc      *store.Location
	forecast *store.ForecastRow
	days     []store.DailyRow
}

func (s *Service) read(ctx context.Context, tid uuid.UUID, at domain.Point, from, to string) (snapshot, error) {
	var sn snapshot
	err := tenant.WithTenantTxOptions(ctx, s.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		if sn.cfg, e = store.LatestConfig(ctx, tx); e != nil {
			return e
		}
		if sn.state, e = store.LoadFetchState(ctx, tx, false); e != nil {
			return e
		}
		locs, e := store.Locations(ctx, tx)
		if e != nil {
			return e
		}
		pts := make([]domain.Point, len(locs))
		for i, l := range locs {
			pts[i] = l.Point
		}
		i := domain.Nearest(at, pts)
		if i < 0 {
			return nil
		}
		sn.loc = &locs[i]
		if sn.forecast, e = store.LatestForecast(ctx, tx, sn.loc.ID); e != nil {
			return e
		}
		if from != "" {
			sn.days, e = store.DailyRange(ctx, tx, sn.loc.ID, from, to)
		}
		return e
	})
	return sn, err
}

func (s *Service) source(sn snapshot, now time.Time) Source {
	src := Source{}
	if sn.cfg != nil {
		src.Provider, src.Label = sn.cfg.Provider, domain.ProviderLabel(sn.cfg.Provider)
	}
	if sn.forecast != nil {
		t := sn.forecast.FetchedAt
		src.UpdatedAt = &t
		src.Provider, src.Label = sn.forecast.Forecast.Provider, domain.ProviderLabel(sn.forecast.Forecast.Provider)
		refresh := time.Duration(domain.DefaultRefreshMinutes) * time.Minute
		if sn.cfg != nil {
			refresh = time.Duration(sn.cfg.RefreshMinutes) * time.Minute
		}
		src.Stale = sn.state.Failures > 0 || now.Sub(t) > 2*sn.state.Interval(now, refresh)
	}
	return src
}

// Today trả thẻ Thời tiết hôm nay (FN-TT-01): ngày dương, ngày âm và số liệu từ bộ đệm máy chủ (BR-01) — không gọi nhà
// cung cấp. Nhà cung cấp lỗi → vẫn trả số liệu gần nhất kèm giờ cập nhật (EX-01, AC-03).
func (s *Service) Today(ctx context.Context, coop uuid.UUID) (Today, error) {
	tid, coop, err := s.scope(ctx, coop)
	if err != nil {
		return Today{}, err
	}
	now := s.now()
	out := Today{CooperativeRef: coop, Day: domain.DayOf(now), Lunar: lunar.Of(now), Status: StatusNoData}
	at, err := s.site(ctx, coop)
	if err != nil {
		return Today{}, err
	}
	if at == nil {
		out.Status = StatusNoLocation
		return out, nil
	}
	sn, err := s.read(ctx, tid, *at, out.Day, out.Day)
	if err != nil {
		return Today{}, err
	}
	out.Source = s.source(sn, now)
	if sn.forecast != nil {
		f := sn.forecast.Forecast
		out.Current = f.Now(now)
		if d, ok := f.DayOn(out.Day); ok {
			out.Summary = &d
		}
		if out.Current != nil || out.Summary != nil {
			out.Status = StatusOK
		}
	}
	if out.Summary == nil && len(sn.days) == 1 {
		out.Summary = &sn.days[0].Summary
		out.Status = StatusOK
	}
	return out, nil
}

// Calendar trả lịch tháng (FN-TT-02): âm lịch mọi ngày (độc lập API — BR-01); ngày từ hôm nay trong phạm vi dự báo
// (FN-TT-03, tối đa 14 ngày — BR-02) hiện dự báo; ngày đã qua hiện số liệu đã lưu kèm nhãn (BR-04); ngày ngoài phạm vi
// dự báo hoặc chưa lưu thì không có số liệu (AC-03, EX-02).
func (s *Service) Calendar(ctx context.Context, coop uuid.UUID, month string) (Calendar, error) {
	first, err := time.ParseInLocation("2006-01", month, domain.VN)
	if err != nil || first.Year() < 2000 || first.Year() > 2100 {
		return Calendar{}, problem.Validation.WithField("month", "Tháng dạng YYYY-MM")
	}
	tid, coop, err := s.scope(ctx, coop)
	if err != nil {
		return Calendar{}, err
	}
	now := s.now()
	last := first.AddDate(0, 1, -1)
	out := Calendar{CooperativeRef: coop, Month: month, Status: StatusNoData}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		out.Days = append(out.Days, CalendarDay{Day: d.Format(time.DateOnly), Lunar: lunar.FromSolar(d.Year(), d.Month(), d.Day())})
	}
	at, err := s.site(ctx, coop)
	if err != nil {
		return Calendar{}, err
	}
	if at == nil {
		out.Status = StatusNoLocation
		return out, nil
	}
	sn, err := s.read(ctx, tid, *at, out.Days[0].Day, out.Days[len(out.Days)-1].Day)
	if err != nil {
		return Calendar{}, err
	}
	out.Source = s.source(sn, now)
	if sn.cfg != nil {
		out.ForecastDays = sn.cfg.ForecastDays
	}
	today := domain.DayOf(now)
	stored := map[string]store.DailyRow{}
	for _, d := range sn.days {
		stored[d.Day] = d
	}
	horizon := ""
	if out.ForecastDays > 0 {
		horizon = domain.DayOf(now.AddDate(0, 0, out.ForecastDays-1))
	}
	for i := range out.Days {
		day := &out.Days[i]
		switch {
		case day.Day >= today && sn.forecast != nil && day.Day <= horizon:
			sum, ok := sn.forecast.Forecast.DayOn(day.Day)
			if !ok {
				continue
			}
			hours := sn.forecast.Forecast.HourlyOn(day.Day)
			day.Kind, day.Summary, day.Hourly, day.Periods = "forecast", &sum, len(hours) > 0, domain.Periods(hours)
		case day.Day < today:
			row, ok := stored[day.Day]
			if !ok {
				continue
			}
			sum := row.Summary
			day.Kind, day.Summary, day.Hourly, day.Periods = row.Kind, &sum, len(row.Hourly) > 0, domain.Periods(row.Hourly)
			day.Label = LabelFinalForecast
			if row.Kind == store.KindObserved {
				day.Label = LabelObserved
			}
		}
		if day.Kind != "" {
			out.Status = StatusOK
		}
	}
	if out.Status == StatusNoData && sn.forecast != nil {
		out.Status = StatusOK // có số liệu nhưng tháng đang xem nằm ngoài phạm vi
	}
	return out, nil
}
