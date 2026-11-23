package weather

import (
	shareddb "github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_weather (ADR-0028). Pool truyền vào là vai đăng nhập của đơn
// vị triển khai (app_platform, app_worker).
var DBModule = shareddb.Module("pf/weather")

func forModule(b tenant.Beginner) tenant.Beginner { return shareddb.ForModule(b, DBModule) }
