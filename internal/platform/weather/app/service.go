// Package app là ca sử dụng của pf/weather: cấu hình kết nối (Admin), job lấy số liệu, đọc thời tiết hôm nay và lịch
// tháng (ba vai App HTX). Không gọi nhà cung cấp hay port module khác trong giao dịch DB.
package app

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Provider là nhà cung cấp số liệu thời tiết đã cấu hình (bộ chuyển đổi ở provider/*).
type Provider interface {
	Fetch(ctx context.Context, p domain.Point, days int) (domain.Forecast, error)
}

// ProviderFactory dựng nhà cung cấp theo mã và khóa API (đã giải mã, chỉ trong bộ nhớ).
type ProviderFactory func(provider, apiKey string) (Provider, error)

// TenantLister liệt kê tenant cho job.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// Service gom phụ thuộc của module.
type Service struct {
	DB        tenant.Beginner
	Clock     clock.Clock
	Coops     coopport.Cooperatives // HTX đang hoạt động kèm tọa độ trụ sở (tenant từ context)
	Providers ProviderFactory
	// SecretKey: khóa AES-256 mã hóa khóa API (AG_WEATHER_SECRET_KEY). nil → không lưu được khóa API (fail-closed),
	// vẫn dùng được nhà cung cấp không cần khóa.
	SecretKey []byte `pii:"true"`
	AllowFake bool // nhà cung cấp giả chỉ ngoài production
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}

// Mã lỗi của module (docs/errors.md, mục pf/weather).
var (
	ErrConnectionFailed = problem.New(http.StatusUnprocessableEntity, "weather.connection_failed", "Kiểm tra kết nối thất bại")
	ErrSecretMissing    = problem.New(http.StatusServiceUnavailable, "weather.secret_unavailable", "Máy chủ chưa cấu hình khóa mã hóa khóa API thời tiết")
	ErrDependency       = problem.New(http.StatusServiceUnavailable, "weather.dependency_unavailable", "Chưa nối nguồn dữ liệu HTX")
)

// connectionMessage là câu hiển thị khi kiểm tra kết nối lỗi (FN-TT-03/EX-01).
func connectionMessage(code string) string {
	switch code {
	case domain.ErrCodeAuth:
		return "Khóa API không đúng hoặc đã hết hạn."
	case domain.ErrCodeQuota:
		return "Đã vượt hạn mức gọi của nhà cung cấp."
	case domain.ErrCodeBadResponse:
		return "Nhà cung cấp trả số liệu không đúng định dạng."
	}
	return "Không kết nối được nhà cung cấp thời tiết."
}
