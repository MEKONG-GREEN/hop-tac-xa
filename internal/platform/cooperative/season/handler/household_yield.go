package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// encodeYieldCursor: cursor mờ "YYYY-MM-DD|season_ref" (base64url, không đệm).
func encodeYieldCursor(c *store.YieldCursor) *string {
	if c == nil {
		return nil
	}
	s := base64.RawURLEncoding.EncodeToString([]byte(c.Day.Format(time.DateOnly) + "|" + c.SeasonRef.String()))
	return &s
}

func decodeYieldCursor(raw string) (*store.YieldCursor, bool) {
	if raw == "" {
		return nil, true
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, false
	}
	day, ref, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil, false
	}
	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, false
	}
	u, err := uuid.Parse(ref)
	if err != nil {
		return nil, false
	}
	return &store.YieldCursor{Day: d, SeasonRef: u}, true
}

func tonnesPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(2)
	return &s
}

func pctPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(1)
	return &s
}

// householdYields: AH-153 — sản lượng thực tế so với dự kiến của vụ nông hộ trong HTX (chỉ đọc, KHMV-FN-06 R).
func (h Handler) householdYields(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	q := app.HouseholdYieldQuery{CooperativeRef: coop, State: c.Query("state"), SeasonCode: strings.TrimSpace(c.Query("season_code"))}
	if v := c.Query("opened_by"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("opened_by", "mã Thành viên không hợp lệ"))
			return
		}
		q.OpenedBy = u
	}
	if v := c.Query("season_year"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("season_year", "năm không hợp lệ"))
			return
		}
		q.SeasonYear = n
	}
	q.HarvestRecordedOnly = c.Query("harvest_recorded_only") == "true"
	after, ok := decodeYieldCursor(c.Query("cursor"))
	if !ok {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return
	}
	q.After = after
	q.Limit, _ = strconv.Atoi(c.Query("limit"))
	page, err := h.Svc.HouseholdYields(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(page.Items))
	for _, it := range page.Items {
		s := it.Season
		var expectedDate *string
		if !s.ExpectedHarvestDate.IsZero() {
			v := domain.FormatDay(s.ExpectedHarvestDate)
			expectedDate = &v
		}
		item := gin.H{"season_ref": s.SeasonRef, "household_ref": s.HouseholdRef, "crop_code": s.CropCode, "season_code": s.SeasonCode,
			"season_year": s.SeasonYear, "variety_code": s.VarietyCode, "area_ha": s.AreaHa.String(), "status": s.Status,
			"opened_by": s.OpenedBy, "expected_harvest_date": expectedDate, "overdue": it.Overdue, "harvest_recorded": it.HarvestRecorded, "current_event_id": s.CurrentEventID,
			"expected_t": tonnesPtr(it.ExpectedT), "expected_straw_t": nil, "expected_source": nil,
			"actual_t": tonnesPtr(it.ActualT), "harvest_date": nil, "yield_t_per_ha": nil, "zero_confirmed": false,
			"diff_t": tonnesPtr(it.DiffT), "diff_pct": pctPtr(it.DiffPct)}
		if it.OpenedByLabel != "" {
			item["opened_by_label"] = it.OpenedByLabel
		}
		if e := it.Estimate; e != nil {
			item["expected_straw_t"] = tonnesPtr(e.Straw())
			item["expected_source"] = "computed"
			if e.EnteredRice != nil {
				item["expected_source"] = "entered"
			}
		}
		if hv := it.Harvest; hv != nil {
			item["harvest_date"] = domain.FormatDay(hv.HarvestDate)
			item["yield_t_per_ha"] = hv.YieldTPerHa.StringFixed(2)
			item["zero_confirmed"] = hv.AnomalyKind == domain.AnomalyZero
		}
		items = append(items, item)
	}
	t := page.Totals
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeYieldCursor(page.Next), "totals": gin.H{
		"pending": t.Pending, "declared": t.Declared, "declared_without_estimate": t.DeclaredWithoutEstimate,
		"pending_expected_t": t.PendingExpectedT.StringFixed(2), "expected_t": t.ExpectedT.StringFixed(2), "actual_t": t.ActualT.StringFixed(2),
		"diff_t": t.DiffT.StringFixed(2), "diff_pct": pctPtr(t.DiffPct)}})
}
