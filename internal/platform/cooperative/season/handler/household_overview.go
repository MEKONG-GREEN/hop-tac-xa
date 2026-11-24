package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// householdSeasonOverview: AH-140 — danh sách vụ nông hộ của HTX kèm tiến độ (chỉ đọc), lọc trạng thái, người mở vụ.
func (h Handler) householdSeasonOverview(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	var openedBy uuid.UUID
	if v := c.Query("opened_by"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("opened_by", "mã Thành viên không hợp lệ"))
			return
		}
		openedBy = u
	}
	page, err := h.Svc.HouseholdSeasonOverview(c.Request.Context(), coop, c.Query("status"), openedBy, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(page.Items))
	for _, it := range page.Items {
		s := it.Season
		item := gin.H{"season_ref": s.SeasonRef, "household_ref": s.HouseholdRef, "crop_code": s.CropCode, "season_code": s.SeasonCode,
			"season_year": s.SeasonYear, "variety_code": s.VarietyCode, "area_ha": s.AreaHa.String(), "status": s.Status,
			"opened_by": s.OpenedBy, "opened_at": ts(s.OpenedAt), "progress": nil}
		if it.OpenedByLabel != "" {
			item["opened_by_label"] = it.OpenedByLabel
		}
		if p := it.Progress; p != nil {
			item["progress"] = gin.H{"percent": p.Percent, "done": p.Done, "total": p.Total, "skipped": p.Skipped, "due": p.Due, "overdue": p.Overdue}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(page.Next)})
}
