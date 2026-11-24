package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// FetchResult là kết quả một lượt job weather_fetch.
type FetchResult struct {
	Tenants   int // tenant đã cấu hình
	Due       int // tenant đến lượt lấy
	Locations int // điểm đã lấy thành công
	Failed    int // tenant có lần lấy lỗi
	Notified  int // sự kiện pf.weather.fetch.failing đã phát
}

// RunFetch chạy một lượt job: với mỗi tenant đã cấu hình và đến lượt (tần suất hiện hành — AC-01, hoặc 3 giờ khi đang
// giãn vì vượt hạn mức — EX-03), lấy dự báo cho mọi điểm của HTX có tọa độ (vị trí cách nhau dưới 1 km dùng chung một
// lần gọi — BR-02), lưu đệm và số liệu ngày (BR-05). Lỗi của một tenant không chặn tenant khác.
func (s *Service) RunFetch(ctx context.Context, tenants TenantLister) (FetchResult, error) {
	var r FetchResult
	tids, err := tenants(ctx)
	if err != nil {
		return r, err
	}
	var errs []error
	for _, tid := range tids {
		r.Tenants++
		if err := s.fetchTenant(tenant.WithID(ctx, tid), tid, &r); err != nil {
			errs = append(errs, err)
		}
	}
	return r, errors.Join(errs...)
}

func (s *Service) fetchTenant(ctx context.Context, tid uuid.UUID, r *FetchResult) error {
	now := s.now()
	var cfg *store.ConfigRow
	var state domain.FetchState
	var locs []store.Location
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		if cfg, e = store.LatestConfig(ctx, tx); e != nil || cfg == nil {
			return e
		}
		if state, e = store.LoadFetchState(ctx, tx, false); e != nil {
			return e
		}
		locs, e = store.Locations(ctx, tx)
		return e
	}); err != nil || cfg == nil {
		return err
	}
	if !state.Due(now, time.Duration(cfg.RefreshMinutes)*time.Minute) {
		return nil
	}
	r.Due++
	targets, err := s.targets(ctx, tid, locs)
	if err != nil {
		return err
	}
	apiKey := ""
	if len(cfg.KeyCipher) > 0 {
		if apiKey, err = open(s.SecretKey, tid, cfg.KeyCipher, cfg.KeyNonce); err != nil {
			return s.recordFailure(ctx, tid, cfg.Provider, now, domain.ErrCodeAuth, r)
		}
	}
	if s.Providers == nil {
		return ErrDependency
	}
	prov, err := s.Providers(cfg.Provider, apiKey)
	if err != nil {
		return s.recordFailure(ctx, tid, cfg.Provider, now, domain.ErrCodeUnavailable, r)
	}
	type got struct {
		loc uuid.UUID
		f   domain.Forecast
	}
	var results []got
	failCode := ""
	for _, loc := range targets {
		f, err := prov.Fetch(ctx, loc.Point, cfg.ForecastDays)
		if err != nil {
			failCode = domain.ErrCodeUnavailable
			var pe domain.ProviderError
			if errors.As(err, &pe) {
				failCode = pe.Code
			}
			if failCode == domain.ErrCodeQuota || failCode == domain.ErrCodeAuth {
				break // các điểm còn lại cũng sẽ lỗi; không gọi thêm
			}
			continue
		}
		results = append(results, got{loc.ID, f})
	}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		for _, g := range results {
			if err := store.SaveForecast(ctx, tx, g.loc, g.f, now); err != nil {
				return err
			}
			if err := saveDays(ctx, tx, g.loc, g.f, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	r.Locations += len(results)
	if failCode != "" {
		return s.recordFailure(ctx, tid, cfg.Provider, now, failCode, r)
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		cur, err := store.LoadFetchState(ctx, tx, true)
		if err != nil {
			return err
		}
		return store.SaveFetchState(ctx, tx, cur.Succeeded(now))
	})
}

// targets bảo đảm mỗi HTX có tọa độ có một điểm lấy số liệu (dùng chung điểm cách dưới 1 km) và trả các điểm cần lấy.
func (s *Service) targets(ctx context.Context, tid uuid.UUID, locs []store.Location) ([]store.Location, error) {
	if s.Coops == nil {
		return nil, ErrDependency
	}
	sites, err := s.Coops.Active(ctx)
	if err != nil {
		return nil, err
	}
	pts := make([]domain.Point, len(locs))
	for i, l := range locs {
		pts[i] = l.Point
	}
	used := map[uuid.UUID]bool{}
	var out []store.Location
	var fresh []domain.Point
	for _, c := range sites {
		if c.Lat == nil || c.Lon == nil {
			continue
		}
		p := domain.Point{Lat: *c.Lat, Lon: *c.Lon}
		if !p.Valid() {
			continue
		}
		if i := domain.Nearest(p, pts); i >= 0 {
			if i < len(locs) && !used[locs[i].ID] {
				used[locs[i].ID] = true
				out = append(out, locs[i])
			}
			continue
		}
		snapped := domain.Snap(p)
		pts = append(pts, snapped) // vị trí sau trong cùng lượt dùng chung điểm mới này
		fresh = append(fresh, snapped)
	}
	if len(fresh) == 0 {
		return out, nil
	}
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		for _, p := range fresh {
			id, err := store.EnsureLocation(ctx, tx, ids.New(), p)
			if err != nil {
				return err
			}
			if !used[id] {
				used[id] = true
				out = append(out, store.Location{ID: id, Point: p})
			}
		}
		return nil
	})
	return out, err
}

// saveDays ghi số liệu ngày (DATA-TP-06, FN-TT-03/BR-05): ngày đã qua có thực đo (nhà cung cấp có) → 'observed'; hôm
// nay → bản dự báo hiện tại nếu lấy trước 23:30 giờ Việt Nam ('final_forecast'; bản lấy sau mốc này không thay).
func saveDays(ctx context.Context, tx pgx.Tx, loc uuid.UUID, f domain.Forecast, now time.Time) error {
	today := domain.DayOf(now)
	for _, d := range f.Daily {
		row := store.DailyRow{Day: d.Day, Provider: f.Provider, FetchedAt: now, Summary: d}
		if hours := f.HourlyOn(d.Day); len(hours) > 0 {
			row.Hourly = hours
		}
		switch {
		case d.Day < today && f.Observed:
			row.Kind = store.KindObserved
		case d.Day == today && domain.RecordsDay(now):
			row.Kind = store.KindFinalForecast
		default:
			continue
		}
		if err := store.SaveDaily(ctx, tx, loc, row); err != nil {
			return err
		}
	}
	return nil
}

// recordFailure ghi một lần lỗi; chạm ngưỡng thì phát pf.weather.fetch.failing đúng một lần trong cùng giao dịch (EX-02,
// AC-05). App vẫn đọc số liệu gần nhất đã lưu.
func (s *Service) recordFailure(ctx context.Context, tid uuid.UUID, provider string, now time.Time, code string, r *FetchResult) error {
	r.Failed++
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		cur, err := store.LoadFetchState(ctx, tx, true)
		if err != nil {
			return err
		}
		next, notify := cur.Failed(now, code)
		if err := store.SaveFetchState(ctx, tx, next); err != nil {
			return err
		}
		if !notify {
			return nil
		}
		r.Notified++
		p := port.FetchFailingV1{Provider: provider, Failures: next.Failures, LastError: code,
			LastAttempt: now.UTC().Format(time.RFC3339), EffectiveDay: domain.DayOf(now)}
		if next.LastSuccess != nil {
			p.LastSuccess = next.LastSuccess.UTC().Format(time.RFC3339)
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventFetchFailing, SchemaVersion: 1,
			OccurredAt: now, Producer: port.EventProducerName, Payload: p})
	})
}
