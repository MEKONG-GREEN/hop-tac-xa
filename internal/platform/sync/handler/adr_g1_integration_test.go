package handler_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	syncmod "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// ADR-0006 (mục 19, C-8): gauge ag.sync.conflict.overdue đo được cho MỌI tenant ở worker —
// tìm tenant có xung đột đang mở bằng vai ag_ops_jobs (hai cột, migration pf 000043),
// đếm quá hạn bằng vai sync_app qua RegisterConflictSLAMetrics.
func TestConflictTenantsForSLAGauge_ADR0006(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	results(t, c.post("/v1/sync/push", map[string]any{"device_id": c.device.String(), "records": []any{
		rec(c, 1, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "adr6"}),
	}}))
	pub := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.OpsJobs)
	ts, err := syncmod.ConflictTenants(pub)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ts, f.tenant) {
		t.Fatalf("tenant có xung đột mở phải được liệt kê: %v", ts)
	}
	if _, err := syncmod.RegisterConflictSLAMetrics(f.pool, f.clk, syncmod.ConflictTenants(pub)); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(73 * time.Hour)
	if sla, err := f.mod.ConflictSLA(context.Background(), f.tenant); err != nil || sla.Overdue != 1 {
		t.Fatalf("quá 72 giờ phải đếm quá hạn: %+v %v", sla, err)
	}
}

// ADR-0006: con trỏ pull ký HMAC — production bắt buộc AG_SYNC_CURSOR_KEY; khóa ngắn bị từ chối.
func TestCursorKeyRequiredInProduction_ADR0006(t *testing.T) {
	if err := syncmod.CheckCursorKey(nil, true); !errors.Is(err, syncmod.ErrCursorKey) {
		t.Fatalf("production thiếu khóa: %v", err)
	}
	if err := syncmod.CheckCursorKey([]byte("ngắn"), false); !errors.Is(err, syncmod.ErrCursorKey) {
		t.Fatalf("khóa < 32 byte: %v", err)
	}
	if err := syncmod.CheckCursorKey(nil, false); err != nil {
		t.Fatalf("Local/Dev được sinh khóa ngẫu nhiên: %v", err)
	}
	if err := syncmod.CheckCursorKey([]byte("khoa-con-tro-kiem-thu-0123456789abcdef"), true); err != nil {
		t.Fatal(err)
	}
}

// ADR-0010: Zalo remains online-only. AH-99 separately permits native Web
// journals, with current Member/Admin rights and no field-data pull.
func TestOnlyFlutterPlatformsRegisterForOfflineSync_ADR0010(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	for _, p := range []string{"zalo", "zalo_mini_app"} {
		r := c.post("/v1/devices", map[string]any{"device_id": ids.New().String(), "platform": p, "app_version": "1.0.0"})
		if r.status != http.StatusUnprocessableEntity {
			t.Errorf("nền tảng %q phải bị từ chối: %d %s", p, r.status, r.raw)
		}
	}
	for _, p := range []string{"android", "ios"} {
		if r := c.post("/v1/devices", map[string]any{"device_id": ids.New().String(), "platform": p, "app_version": "1.0.0"}); r.status != http.StatusCreated {
			t.Errorf("app Flutter %s: %d %s", p, r.status, r.raw)
		}
	}
}

func TestAH99WebDeviceIsLimitedToNativeJournals(t *testing.T) {
	f := newFixture(t)
	for _, roles := range [][]string{{"kn_officer"}, {"coop_manager"}, {"htx_member", "coop_manager"}} {
		c := f.officer(roles...)
		r := c.post("/v1/devices", map[string]any{"device_id": ids.New(), "platform": "web", "app_version": "htx-task-1"})
		if r.status != 403 {
			t.Fatalf("web role %v: %d %s", roles, r.status, r.raw)
		}
	}
	c := f.officer("htx_member")
	device := ids.New()
	r := c.post("/v1/devices", map[string]any{"device_id": device, "platform": "web", "app_version": "htx-task-1"})
	if r.status != 201 {
		t.Fatalf("native web registration: %d %s", r.status, r.raw)
	}
	r = c.get("/v1/sync/pull", map[string]string{"X-Device-Id": device.String()})
	if r.status != 403 {
		t.Fatalf("web field pull: %d %s", r.status, r.raw)
	}
}
