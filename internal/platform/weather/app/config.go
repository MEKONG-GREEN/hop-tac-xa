package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// ConfigView là cấu hình trả cho Admin: khóa chỉ có dạng che (AC-03).
type ConfigView struct {
	Configured bool
	Config     domain.Config
	Providers  []string // nhà cung cấp chọn được
}

// TestResult là kết quả kiểm tra kết nối thành công: số liệu hiện tại ở điểm thử.
type TestResult struct {
	Provider     string
	TemperatureC *float64
	Condition    string
	Days         int
}

// Điểm thử khi tenant chưa có HTX nào có tọa độ: TP. Cần Thơ (đã làm thô).
var fallbackPoint = domain.Point{Lat: decimal.RequireFromString("10.03"), Lon: decimal.RequireFromString("105.78")}

func (s *Service) admin(ctx context.Context) (authz.Principal, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return p, err
	}
	if tid, err := tenant.FromContext(ctx); err != nil || tid != p.TenantID || p.TenantID == uuid.Nil {
		return p, problem.Unauthenticated
	}
	return p, nil
}

func (s *Service) providers() []string {
	if s.AllowFake {
		return []string{domain.ProviderOpenMeteo, domain.ProviderFake}
	}
	return []string{domain.ProviderOpenMeteo}
}

// GetConfig trả cấu hình hiện hành (Admin). Chưa cấu hình → giá trị mặc định, Configured = false.
func (s *Service) GetConfig(ctx context.Context) (ConfigView, error) {
	p, err := s.admin(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	var row *store.ConfigRow
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		row, e = store.LatestConfig(ctx, tx)
		return e
	}); err != nil {
		return ConfigView{}, err
	}
	if row == nil {
		return ConfigView{Config: domain.Config{Provider: domain.ProviderOpenMeteo, RefreshMinutes: domain.DefaultRefreshMinutes,
			ForecastDays: domain.DefaultForecastDays}, Providers: s.providers()}, nil
	}
	return ConfigView{Configured: true, Config: row.Config(), Providers: s.providers()}, nil
}

// candidate là cấu hình định lưu kèm khóa thô (chỉ trong bộ nhớ).
type candidate struct {
	in      domain.ConfigInput
	apiKey  string `pii:"true"`
	current *store.ConfigRow
}

// resolve kiểm dữ liệu và xác định khóa API sẽ dùng: nil = giữ khóa đang có (giải mã), "" = bỏ khóa.
func (s *Service) resolve(ctx context.Context, tid uuid.UUID, in domain.ConfigInput) (candidate, error) {
	in, err := in.Normalize(s.AllowFake)
	if err != nil {
		var fe domain.FieldError
		if errors.As(err, &fe) {
			return candidate{}, problem.Validation.WithField(fe.Field, fe.Msg)
		}
		return candidate{}, err
	}
	c := candidate{in: in}
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		c.current, e = store.LatestConfig(ctx, tx)
		return e
	}); err != nil {
		return candidate{}, err
	}
	switch {
	case in.APIKey != nil:
		c.apiKey = *in.APIKey
	case c.current != nil && len(c.current.KeyCipher) > 0:
		if c.apiKey, err = open(s.SecretKey, tid, c.current.KeyCipher, c.current.KeyNonce); err != nil {
			return candidate{}, ErrSecretMissing.WithDetail("Không mở được khóa API đã lưu; nhập lại khóa API.")
		}
	}
	if c.apiKey != "" && len(s.SecretKey) != 32 {
		return candidate{}, ErrSecretMissing
	}
	return c, nil
}

// testPoint là điểm thử kết nối: trụ sở của HTX đầu tiên có tọa độ, không thì điểm mặc định.
func (s *Service) testPoint(ctx context.Context) domain.Point {
	if s.Coops == nil {
		return fallbackPoint
	}
	sites, err := s.Coops.Active(ctx)
	if err != nil {
		return fallbackPoint
	}
	for _, c := range sites {
		if c.Lat != nil && c.Lon != nil {
			return domain.Snap(domain.Point{Lat: *c.Lat, Lon: *c.Lon})
		}
	}
	return fallbackPoint
}

// probe gọi nhà cung cấp một lần với cấu hình định lưu (ngoài giao dịch DB).
func (s *Service) probe(ctx context.Context, c candidate) (TestResult, error) {
	if s.Providers == nil {
		return TestResult{}, ErrDependency.WithDetail("Chưa nối nhà cung cấp thời tiết")
	}
	prov, err := s.Providers(c.in.Provider, c.apiKey)
	if err != nil {
		return TestResult{}, ErrConnectionFailed.WithDetail(connectionMessage(domain.ErrCodeUnavailable))
	}
	f, err := prov.Fetch(ctx, s.testPoint(ctx), 1)
	if err != nil {
		code := domain.ErrCodeUnavailable
		var pe domain.ProviderError
		if errors.As(err, &pe) {
			code = pe.Code
		}
		return TestResult{}, ErrConnectionFailed.WithDetail(connectionMessage(code)).With("reason", code)
	}
	r := TestResult{Provider: c.in.Provider, Days: len(f.Daily)}
	if now := f.Now(s.now()); now != nil {
		r.TemperatureC = now.TemperatureC
		if now.WeatherCode != nil {
			r.Condition = domain.Condition(*now.WeatherCode)
		}
	}
	return r, nil
}

// TestConnection kiểm tra kết nối với cấu hình định lưu, không lưu gì (FN-TT-03 B3).
func (s *Service) TestConnection(ctx context.Context, in domain.ConfigInput) (TestResult, error) {
	p, err := s.admin(ctx)
	if err != nil {
		return TestResult{}, err
	}
	c, err := s.resolve(ctx, p.TenantID, in)
	if err != nil {
		return TestResult{}, err
	}
	return s.probe(ctx, c)
}

// SaveConfig lưu một phiên bản cấu hình (Admin). Kiểm tra kết nối trước; lỗi thì không lưu (EX-01, AC-04). Gọi lại cùng
// Idempotency-Key → trả phiên bản đã ghi, không gọi nhà cung cấp lần nữa.
func (s *Service) SaveConfig(ctx context.Context, in domain.ConfigInput, key string) (ConfigView, bool, error) {
	p, err := s.admin(ctx)
	if err != nil {
		return ConfigView{}, false, err
	}
	if key == "" {
		return ConfigView{}, false, problem.IdempotencyRequired
	}
	var prev *store.ConfigRow
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ConfigByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return ConfigView{}, false, err
	}
	if prev != nil {
		if prev.ActorRef != p.ActorRef {
			return ConfigView{}, false, problem.IdempotencyConflict
		}
		return ConfigView{Configured: true, Config: prev.Config(), Providers: s.providers()}, true, nil
	}
	c, err := s.resolve(ctx, p.TenantID, in)
	if err != nil {
		return ConfigView{}, false, err
	}
	if _, err := s.probe(ctx, c); err != nil {
		return ConfigView{}, false, err
	}
	row := store.ConfigRow{ID: ids.New(), TenantID: p.TenantID, Provider: c.in.Provider, RefreshMinutes: c.in.RefreshMinutes,
		ForecastDays: c.in.ForecastDays, EventTime: s.now(), ActorRef: p.ActorRef, IdemKey: key}
	if c.apiKey != "" {
		if row.KeyCipher, row.KeyNonce, err = seal(s.SecretKey, p.TenantID, c.apiKey); err != nil {
			return ConfigView{}, false, err
		}
		row.KeyLast4 = domain.Last4(c.apiKey)
	}
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockConfig(ctx, tx); err != nil {
			return err
		}
		return ledger.Classify(store.InsertConfig(ctx, tx, &row))
	}); err != nil {
		return ConfigView{}, false, err
	}
	return ConfigView{Configured: true, Config: row.Config(), Providers: s.providers()}, false, nil
}
