// Package store là SQL của pf/weather (schema pf_weather, migration pf 20261123041038). Mọi hàm chạy trong giao dịch đã
// đặt tenant (RLS) và vai r_weather, trừ Tenants (vai ag_ops_jobs của job).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

// ConfigRuleVersion ghi vào cột rule_version của sổ cấu hình.
const ConfigRuleVersion = "pf.weather.provider_config/v1"

const configStream = "weather-config"

// ConfigRow là một phiên bản cấu hình kèm khóa đã mã hóa.
type ConfigRow struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Provider       string
	RefreshMinutes int
	ForecastDays   int
	KeyCipher      []byte
	KeyNonce       []byte
	KeyLast4       string
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	Seq            int64
}

// Config trả cấu hình hiển thị (không có bản mã).
func (r ConfigRow) Config() domain.Config {
	return domain.Config{Provider: r.Provider, RefreshMinutes: r.RefreshMinutes, ForecastDays: r.ForecastDays,
		APIKeyLast4: r.KeyLast4, Version: r.Seq, UpdatedAt: r.EventTime}
}

const configCols = `id, tenant_id, provider, refresh_minutes, forecast_days, api_key_cipher, api_key_nonce,
  coalesce(api_key_last4, ''), event_time, actor_ref, idem_key, seq`

func scanConfig(row pgx.Row) (*ConfigRow, error) {
	var r ConfigRow
	err := row.Scan(&r.ID, &r.TenantID, &r.Provider, &r.RefreshMinutes, &r.ForecastDays, &r.KeyCipher, &r.KeyNonce,
		&r.KeyLast4, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// LatestConfig trả phiên bản cấu hình mới nhất của tenant; chưa có → nil.
func LatestConfig(ctx context.Context, tx pgx.Tx) (*ConfigRow, error) {
	return scanConfig(tx.QueryRow(ctx, `SELECT `+configCols+` FROM pf_weather.provider_config
	  WHERE stream_key = $1 ORDER BY seq DESC LIMIT 1`, configStream))
}

// ConfigByIdem tìm phiên bản đã ghi với idem_key (gọi lặp); chưa có → nil.
func ConfigByIdem(ctx context.Context, tx pgx.Tx, key string) (*ConfigRow, error) {
	return scanConfig(tx.QueryRow(ctx, `SELECT `+configCols+` FROM pf_weather.provider_config WHERE idem_key = $1`+
		ledger.NewestByIdem(""), key))
}

// LockConfig khóa advisory theo tenant: hai lần lưu song song không chen nhau.
func LockConfig(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_weather.provider_config:' || shared.current_tenant()::text, 0))`)
	return err
}

// InsertConfig ghi một phiên bản; seq, chuỗi băm do DB tính (shared.fn_chain).
func InsertConfig(ctx context.Context, tx pgx.Tx, r *ConfigRow) error {
	var last4 *string
	if r.KeyLast4 != "" {
		last4 = &r.KeyLast4
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_weather.provider_config
	    (id, tenant_id, stream_key, provider, refresh_minutes, forecast_days, api_key_cipher, api_key_nonce, api_key_last4,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	  RETURNING seq`,
		r.ID, r.TenantID, configStream, r.Provider, r.RefreshMinutes, r.ForecastDays, nilIfEmpty(r.KeyCipher),
		nilIfEmpty(r.KeyNonce), last4, r.EventTime.UTC(), r.ActorRef, r.IdemKey, ConfigRuleVersion).Scan(&r.Seq)
}

func nilIfEmpty(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

// Location là một điểm lấy số liệu.
type Location struct {
	ID    uuid.UUID
	Point domain.Point
}

// Locations liệt kê điểm của tenant.
func Locations(ctx context.Context, tx pgx.Tx) ([]Location, error) {
	rows, err := tx.Query(ctx, `SELECT id, lat, lon FROM pf_weather.location ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Location, error) {
		var l Location
		err := r.Scan(&l.ID, &l.Point.Lat, &l.Point.Lon)
		return l, err
	})
}

// EnsureLocation thêm điểm (đã làm thô) nếu chưa có và trả mã điểm.
func EnsureLocation(ctx context.Context, tx pgx.Tx, id uuid.UUID, p domain.Point) (uuid.UUID, error) {
	var out uuid.UUID
	err := tx.QueryRow(ctx, `WITH ins AS (
	    INSERT INTO pf_weather.location (tenant_id, id, lat, lon) VALUES (shared.current_tenant(), $1, $2, $3)
	    ON CONFLICT (tenant_id, lat, lon) DO NOTHING RETURNING id)
	  SELECT id FROM ins UNION ALL SELECT id FROM pf_weather.location WHERE lat = $2 AND lon = $3 LIMIT 1`,
		id, p.Lat, p.Lon).Scan(&out)
	return out, err
}

// SaveForecast ghi đè bản dự báo gần nhất của điểm.
func SaveForecast(ctx context.Context, tx pgx.Tx, loc uuid.UUID, f domain.Forecast, fetched time.Time) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_weather.forecast (tenant_id, location_id, provider, fetched_at, payload)
	  VALUES (shared.current_tenant(), $1, $2, $3, $4::jsonb)
	  ON CONFLICT (tenant_id, location_id) DO UPDATE SET provider = excluded.provider, fetched_at = excluded.fetched_at,
	    payload = excluded.payload`, loc, f.Provider, fetched.UTC(), string(b)) // chuỗi: []byte bị mã hóa thành bytea qua PgBouncer
	return err
}

// ForecastRow là bản dự báo đã lưu.
type ForecastRow struct {
	Forecast  domain.Forecast
	FetchedAt time.Time
}

// LatestForecast trả bản dự báo gần nhất của điểm; chưa có → nil.
func LatestForecast(ctx context.Context, tx pgx.Tx, loc uuid.UUID) (*ForecastRow, error) {
	var raw []byte
	var r ForecastRow
	err := tx.QueryRow(ctx, `SELECT payload, fetched_at FROM pf_weather.forecast WHERE location_id = $1`, loc).Scan(&raw, &r.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &r.Forecast); err != nil {
		return nil, err
	}
	return &r, nil
}

// DailyRow là số liệu một ngày đã lưu (DATA-TP-06).
type DailyRow struct {
	Day       string // YYYY-MM-DD
	Kind      string // observed | final_forecast
	Provider  string
	FetchedAt time.Time
	Summary   domain.DaySummary
	Hourly    []domain.Reading // nil: không có số liệu theo giờ
}

// Loại số liệu ngày.
const (
	KindObserved      = "observed"
	KindFinalForecast = "final_forecast"
)

// SaveDaily ghi (hoặc ghi đè) số liệu một ngày của điểm. Số liệu thực đo không bị bản dự báo ghi đè.
func SaveDaily(ctx context.Context, tx pgx.Tx, loc uuid.UUID, d DailyRow) error {
	sum, err := json.Marshal(d.Summary)
	if err != nil {
		return err
	}
	var hourly *string
	if d.Hourly != nil {
		b, err := json.Marshal(d.Hourly)
		if err != nil {
			return err
		}
		h := string(b)
		hourly = &h
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_weather.daily (tenant_id, location_id, day, kind, provider, fetched_at, summary, hourly)
	  VALUES (shared.current_tenant(), $1, $2::date, $3, $4, $5, $6::jsonb, $7::jsonb)
	  ON CONFLICT (tenant_id, location_id, day) DO UPDATE SET kind = excluded.kind, provider = excluded.provider,
	    fetched_at = excluded.fetched_at, summary = excluded.summary, hourly = excluded.hourly
	  WHERE pf_weather.daily.kind <> 'observed' OR excluded.kind = 'observed'`,
		loc, d.Day, d.Kind, d.Provider, d.FetchedAt.UTC(), string(sum), hourly)
	return err
}

// DailyRange trả số liệu ngày của điểm trong [from, to] (YYYY-MM-DD), theo ngày tăng dần.
func DailyRange(ctx context.Context, tx pgx.Tx, loc uuid.UUID, from, to string) ([]DailyRow, error) {
	rows, err := tx.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), kind, provider, fetched_at, summary, hourly
	  FROM pf_weather.daily WHERE location_id = $1 AND day BETWEEN $2::date AND $3::date ORDER BY day`, loc, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DailyRow, error) {
		var d DailyRow
		var sum, hourly []byte
		if err := r.Scan(&d.Day, &d.Kind, &d.Provider, &d.FetchedAt, &sum, &hourly); err != nil {
			return d, err
		}
		if err := json.Unmarshal(sum, &d.Summary); err != nil {
			return d, err
		}
		if hourly != nil {
			if err := json.Unmarshal(hourly, &d.Hourly); err != nil {
				return d, err
			}
		}
		return d, nil
	})
}

// LoadFetchState đọc và khóa (FOR UPDATE) trạng thái lấy của tenant; chưa có → trạng thái rỗng.
func LoadFetchState(ctx context.Context, tx pgx.Tx, lock bool) (domain.FetchState, error) {
	var s domain.FetchState
	var lastErr *string
	q := `SELECT last_attempt_at, last_success_at, consecutive_failures, last_error, backoff_until, notified FROM pf_weather.fetch_state`
	if lock {
		q += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, q).Scan(&s.LastAttempt, &s.LastSuccess, &s.Failures, &lastErr, &s.BackoffUntil, &s.Notified)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FetchState{}, nil
	}
	if lastErr != nil {
		s.LastError = *lastErr
	}
	return s, err
}

// SaveFetchState ghi trạng thái lấy của tenant.
func SaveFetchState(ctx context.Context, tx pgx.Tx, s domain.FetchState) error {
	var lastErr *string
	if s.LastError != "" {
		lastErr = &s.LastError
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_weather.fetch_state
	    (tenant_id, last_attempt_at, last_success_at, consecutive_failures, last_error, backoff_until, notified)
	  VALUES (shared.current_tenant(), $1, $2, $3, $4, $5, $6)
	  ON CONFLICT (tenant_id) DO UPDATE SET last_attempt_at = excluded.last_attempt_at,
	    last_success_at = excluded.last_success_at, consecutive_failures = excluded.consecutive_failures,
	    last_error = excluded.last_error, backoff_until = excluded.backoff_until, notified = excluded.notified`,
		s.LastAttempt, s.LastSuccess, s.Failures, lastErr, s.BackoffUntil, s.Notified)
	return err
}

// Tenants liệt kê tenant đã cấu hình kết nối thời tiết (vai ag_ops_jobs của job, chỉ cột tenant_id).
func Tenants(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_weather.provider_config`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// LatLon dựng điểm từ hai số thập phân (tiện cho kiểm thử).
func LatLon(lat, lon string) domain.Point {
	return domain.Point{Lat: decimal.RequireFromString(lat), Lon: decimal.RequireFromString(lon)}
}
