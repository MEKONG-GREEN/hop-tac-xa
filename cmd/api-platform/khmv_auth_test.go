package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// khmvSurface: route của phân hệ Kế hoạch mùa vụ (vụ, kế hoạch, nhật ký, sản lượng, SOP, bảng điều hành) cùng nông
// hộ, thửa và thông báo mà phân hệ dùng.
func khmvSurface(path string) bool {
	return strings.HasPrefix(path, "/v1/cropcycle/") || path == "/v1/cooperatives/:ref/households" ||
		path == "/v1/cooperatives/:ref/plots" || strings.HasPrefix(path, "/v1/notifications")
}

// AH-39 AC5 (NFR-05): không có truy cập công khai — trên router THẬT (Application API và Admin API), mọi route của
// phân hệ gọi không kèm Authorization đều nhận 401 và phản hồi không mang dữ liệu.
func TestKHMV_AC5_NoAnonymousAccess(t *testing.T) {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	mods, pools, err := buildModules(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{
		Log: telemetry.NewLogger("test", io.Discard), Verifier: iss.Verifier(),
		Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Modules: mods, Pools: pools,
	}
	for _, surface := range []struct {
		name   string
		router *gin.Engine
	}{{"Application API", NewRouter(d)}, {"Admin API", NewAdminRouter(d)}} {
		srv := httptest.NewServer(surface.router)
		t.Cleanup(srv.Close)
		checked := 0
		for _, rt := range surface.router.Routes() {
			if !khmvSurface(rt.Path) {
				continue
			}
			checked++
			path := routeParam.ReplaceAllStringFunc(rt.Path, func(string) string { return ids.New().String() })
			req, _ := http.NewRequest(rt.Method, srv.URL+path, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s %s chưa đăng nhập: HTTP %d, cần 401", surface.name, rt.Method, rt.Path, resp.StatusCode)
			}
			for _, k := range []string{"items", "season", "log", "harvest", "features", "totals", "plots"} {
				if _, ok := body[k]; ok {
					t.Errorf("%s %s %s chưa đăng nhập: phản hồi mang %q", surface.name, rt.Method, rt.Path, k)
				}
			}
		}
		t.Logf("%s: %d route của phân hệ đòi đăng nhập", surface.name, checked)
		if surface.name == "Application API" && checked == 0 {
			t.Fatalf("%s: không thấy route nào của phân hệ — kiểm lại cách đăng ký", surface.name)
		}
	}
}
