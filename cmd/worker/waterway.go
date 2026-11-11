package main

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/waterway"
	wwport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// waterwayBuildConsumer dựng phiên bản mạng đường thủy cho
// pf.waterway.network_build.requested (mục 16, mẫu mục 12). Handler chạy trong
// giao dịch của inbox.Process trên pool r_waterway; tham số dựng đã chốt
// trên job nên worker không cần pf/masterdata.
func waterwayBuildConsumer() consumer {
	return consumer{
		name: wwport.BuildConsumer, cluster: migrations.PF, module: waterway.DBModule,
		events:  []string{wwport.EventBuildRequested},
		handler: waterway.New(nil, clock.System{}, waterway.Deps{}).BuildHandler(),
	}
}
