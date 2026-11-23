// Package weather là module pf/weather (BRD Thời tiết & Thu gom phụ phẩm — FN-TT-01..03, TECH-TP-02/03, DATA-TP-06;
// ADR-0065): Admin cấu hình kết nối nhà cung cấp thời tiết, worker lấy số liệu theo tọa độ trụ sở HTX và lưu đệm, lưu
// theo ngày; ba vai App HTX xem thẻ Thời tiết hôm nay và Lịch thời tiết kèm âm lịch. App không gọi nhà cung cấp trực
// tiếp (TECH-TP-03).
//
// Module khác chỉ import weather/port (R-02). Tệp này nối các lớp cho cmd/api-platform và cmd/worker.
package weather

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/handler"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/provider/fake"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/provider/openmeteo"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/residency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/schedule"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Biến môi trường (docs/runbooks/thoi-tiet.md).
const (
	EnvSecretKey     = "AG_WEATHER_SECRET_KEY"     //nolint:gosec // G101: tên biến môi trường; giá trị là base64 32 byte, khóa AES-256 mã hóa khóa API nhà cung cấp
	EnvOpenMeteoURL  = "AG_WEATHER_OPEN_METEO_URL" // endpoint Open-Meteo; bắt buộc ở production (guard RB-7 kiểm máy chủ)
	EnvSourceADR     = "AG_WEATHER_SOURCE_ADR"     // ADR của nguồn ngoài lãnh thổ; bắt buộc ở production
	EnvFakeAllowed   = "AG_WEATHER_FAKE"           // "true": cho chọn nhà cung cấp giả (bị bỏ qua ở production)
	FetchTickMinutes = 5                           // job kiểm tenant đến lượt mỗi 5 phút
)

// ErrSourceNotAllowed: nguồn chưa được phép dùng ở môi trường này (thiếu cấu hình RB-7/ADR) — fail-closed.
var ErrSourceNotAllowed = errors.New("weather: nguồn thời tiết chưa được phép ở môi trường này")

// Deps là phụ thuộc của module.
type Deps struct {
	Coops     coopport.Cooperatives // HTX đang hoạt động kèm tọa độ trụ sở (pf/cooperative)
	Providers app.ProviderFactory   // nil → ProvidersFromEnv(os.Getenv)
	SecretKey []byte                // nil → không lưu được khóa API
	AllowFake bool
}

// Module là pf/weather đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool vai đăng nhập của đơn vị (giao dịch chạy dưới r_weather).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	prov := deps.Providers
	if prov == nil {
		prov = ProvidersFromEnv(os.Getenv)
	}
	return &Module{svc: &app.Service{DB: forModule(db), Clock: clk, Coops: deps.Coops, Providers: prov,
		SecretKey: deps.SecretKey, AllowFake: deps.AllowFake}}
}

// DepsFromEnv đọc khóa mã hóa và cờ nhà cung cấp giả. Khóa sai định dạng → lỗi (dừng khởi động thay vì lưu khóa API
// bằng khóa hỏng).
func DepsFromEnv(getenv func(string) string) (Deps, error) {
	d := Deps{AllowFake: !residency.Production(getenv) && getenv(EnvFakeAllowed) == "true"}
	if raw := strings.TrimSpace(getenv(EnvSecretKey)); raw != "" {
		k, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(k) != 32 {
			return d, fmt.Errorf("%s phải là base64 của 32 byte", EnvSecretKey)
		}
		d.SecretKey = k
	}
	return d, nil
}

// ProvidersFromEnv dựng nhà cung cấp. Ở production: Open-Meteo chỉ dùng khi đặt cả endpoint https (guard RB-7 kiểm máy
// chủ thuộc AG_NONPII_ALLOWLIST) và ADR của nguồn; nhà cung cấp giả bị cấm.
func ProvidersFromEnv(getenv func(string) string) app.ProviderFactory {
	prod := residency.Production(getenv)
	base := strings.TrimSpace(getenv(EnvOpenMeteoURL))
	adr := strings.TrimSpace(getenv(EnvSourceADR))
	return func(provider, apiKey string) (app.Provider, error) {
		switch provider {
		case domain.ProviderOpenMeteo:
			if prod {
				u, err := url.Parse(base)
				if base == "" || adr == "" || err != nil || u.Scheme != "https" {
					return nil, ErrSourceNotAllowed
				}
			}
			return openmeteo.Client{BaseURL: base, APIKey: apiKey}, nil
		case domain.ProviderFake:
			if prod {
				return nil, ErrSourceNotAllowed
			}
			return &fake.Provider{}, nil
		}
		return nil, ErrSourceNotAllowed
	}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Tenants liệt kê tenant đã cấu hình (pool ag_ops_jobs của worker).
func Tenants(pool *pgxpool.Pool) app.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) { return store.Tenants(ctx, pool) }
}

// Jobs trả job định kỳ (chỉ chạy ở leader): weather_fetch mỗi 5 phút kiểm tenant đến lượt theo tần suất cấu hình.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobFetch, Spec: schedule.Every{Interval: FetchTickMinutes * time.Minute}, RunOnStart: true, Timeout: 4 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.RunFetch(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("lấy số liệu thời tiết", "event", "job."+port.JobFetch, "tenants", r.Tenants, "due", r.Due,
					"locations", r.Locations, "failed", r.Failed, "notified", r.Notified, "outcome", outcome)
			}
			return err
		},
	}}
}

// RunFetch chạy một lượt job (kiểm thử, công cụ vận hành).
func (m *Module) RunFetch(ctx context.Context, tenants app.TenantLister) (app.FetchResult, error) {
	return m.svc.RunFetch(ctx, tenants)
}
