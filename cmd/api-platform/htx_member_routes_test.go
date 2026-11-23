package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// memberGuardDetail là thông điệp của assembly.memberWriteGuard — để phân biệt 403 của lớp lắp ráp với 403 của dịch vụ.
const memberGuardDetail = "Thành viên HTX chỉ được xem (FN-05/BR-03)"

var routeParam = regexp.MustCompile(`[:*][A-Za-z_][A-Za-z0-9_]*`)

// FN-05/BR-03, TECH-01 trên router THẬT (Application API và Admin API): tài khoản chỉ là Thành viên HTX bị từ chối
// (403 forbidden) ở MỌI route ghi ngoài htxroles.MemberWriteRoutes, kể cả khi gọi thẳng API; route trong danh sách
// (thao tác cá nhân: đánh dấu đã đọc thông báo, đăng ký push token) gọi được; Đại diện HTX và tài khoản kiêm hai vai
// (BR-02) không bị lớp này chặn.
func TestFN05_BR03_HTXMemberWriteRoutesDenied(t *testing.T) {
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
	tid, org := ids.New(), ids.New()
	token := func(roles ...string) string {
		return iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, OrgRef: org, MFA: true, Roles: roles})
	}
	member := token(htxroles.Member)
	allow := htxroles.MemberWriteRoutes()

	for _, surface := range []struct {
		name   string
		router *gin.Engine
	}{{"Application API", NewRouter(d)}, {"Admin API", NewAdminRouter(d)}} {
		srv := httptest.NewServer(surface.router)
		t.Cleanup(srv.Close)
		var checked []string
		for _, rt := range surface.router.Routes() {
			if !isWrite(rt.Method) || !strings.HasPrefix(rt.Path, "/v1/") || strings.HasPrefix(rt.Path, "/v1/public/") {
				continue // /v1/public: không đăng nhập (đăng nhập, quên mật khẩu — FN-01, FN-10)
			}
			route := rt.Method + " " + rt.Path
			if slices.Contains(allow, route) {
				continue
			}
			checked = append(checked, route)
			path := routeParam.ReplaceAllStringFunc(rt.Path, func(string) string { return ids.New().String() })
			status, code, _ := callJSON(t, srv.URL, rt.Method, path, member)
			if status != http.StatusForbidden || code != "forbidden" {
				t.Errorf("%s %s: Thành viên HTX nhận %d %q, cần 403 forbidden", surface.name, route, status, code)
			}
		}
		t.Logf("%s: %d route ghi bị chặn với Thành viên HTX", surface.name, len(checked))
		if len(checked) == 0 {
			t.Fatalf("%s: không thấy route ghi nào — kiểm lại cách đăng ký", surface.name)
		}
		if surface.name == "Application API" && !slices.Contains(checked, "POST /v1/households") {
			t.Fatalf("Application API: thiếu POST /v1/households trong bảng duyệt: %d route", len(checked))
		}

		if surface.name != "Application API" {
			continue
		}
		// Route trong member_write_routes phải có trên router thật (không sai mẫu đường dẫn) và tới được dịch vụ.
		registered := map[string]bool{}
		for _, rt := range surface.router.Routes() {
			registered[rt.Method+" "+rt.Path] = true
		}
		for _, route := range allow {
			if !registered[route] {
				t.Errorf("member_write_routes %q không có trên Application API", route)
			}
		}
		// Thao tác cá nhân của pf/notify (không phải ghi dữ liệu nghiệp vụ): Thành viên HTX gọi được; kết quả do
		// dịch vụ quyết định, không phải lớp chặn ghi.
		for _, c := range []struct {
			method, path string
			status       int
			code         string
		}{
			{http.MethodPost, "/v1/notifications/read-all", http.StatusOK, ""},
			{http.MethodPost, "/v1/notifications/" + ids.New().String() + "/read", http.StatusNotFound, "notify.notification_not_found"},
			{http.MethodPost, "/v1/devices/" + ids.New().String() + "/push-token", http.StatusUnprocessableEntity, "validation_failed"},
		} {
			status, code, detail := callJSON(t, srv.URL, c.method, c.path, member)
			if detail == memberGuardDetail || status != c.status || code != c.code {
				t.Errorf("Thành viên HTX %s %s = %d %q %q, cần %d %q", c.method, c.path, status, code, detail, c.status, c.code)
			}
		}
		// AH-39 (ma trận KHMV): Thành viên HTX mở vụ, ghi nhật ký, khai sản lượng, đồng ý thu thập dữ liệu — lớp lắp
		// ráp cho qua, dịch vụ quyết định (thân {} → lỗi dữ liệu hoặc lỗi của dịch vụ, không phải 403 của lớp chặn).
		for _, path := range []string{"/v1/cropcycle/seasons", "/v1/cropcycle/farm-logs", "/v1/cropcycle/harvests/bulk", "/v1/me/data-consent"} {
			if status, code, detail := callJSON(t, srv.URL, http.MethodPost, path, member); detail == memberGuardDetail || code == "forbidden" {
				t.Errorf("Thành viên HTX POST %s bị lớp chặn ghi từ chối: %d %q %q", path, status, code, detail)
			}
		}
		// AH-312: mở vụ, xem trước lịch việc, tích việc, ghi việc của vụ theo hộ (KHMV-FN-03/04/05). Thân đủ để qua
		// bước đọc request, nên lỗi (nếu có) là của dịch vụ:
		//   - Thành viên HTX, hộ (không tồn tại) của HTX mình → lớp chặn cho qua, dịch vụ báo không thấy hộ (AC1);
		//   - Thành viên HTX, HTX khác → 404 như trước (AC2);
		//   - Đại diện HTX → dịch vụ vẫn chỉ cho xem (AC2).
		other, hh, season := ids.New(), ids.New().String(), ids.New().String()
		manager := token(partyport.RoleCoopManager)
		for _, c := range []struct{ path, body string }{
			{"/households/" + hh + "/seasons", `{"anchor_date":"2026-11-01"}`},
			{"/households/" + hh + "/season-plan-preview", `{"anchor_date":"2026-11-01","variety_code":"OM18","area_ha":"1","sop_version_id":"` + ids.New().String() + `"}`},
			{"/households/" + hh + "/seasons/" + season + "/task-completions", `{"step_seq":1,"expected_season_event_id":"` + ids.New().String() + `"}`},
			{"/households/" + hh + "/seasons/" + season + "/task-records", `{"step_seq":1,"expected_season_event_id":"` + ids.New().String() + `","state":"done","done_on":"2026-11-01"}`},
		} {
			own := "/v1/cooperatives/" + org.String() + c.path
			if status, code, detail := callBody(t, srv.URL, own, c.body, member); detail == memberGuardDetail || code == "forbidden" || status < 400 {
				t.Errorf("AC1: Thành viên HTX POST %s = %d %q %q, cần tới dịch vụ (lỗi của dịch vụ, không phải lớp chặn)", own, status, code, detail)
			}
			foreign := "/v1/cooperatives/" + other.String() + c.path
			if status, code, _ := callBody(t, srv.URL, foreign, c.body, member); status != http.StatusNotFound || code != "cropcycle.household_not_found" {
				t.Errorf("AC2: Thành viên HTX POST %s (HTX khác) = %d %q, cần 404 cropcycle.household_not_found", foreign, status, code)
			}
			if status, code, detail := callBody(t, srv.URL, own, c.body, manager); status != http.StatusForbidden || code != "cropcycle.read_only" || detail == memberGuardDetail {
				t.Errorf("AC2: Đại diện HTX POST %s = %d %q %q, cần 403 cropcycle.read_only", own, status, code, detail)
			}
		}
		// Đối chứng: lớp lắp ráp không chặn Đại diện HTX và tài khoản kiêm hai vai (BR-02).
		for _, roles := range [][]string{{partyport.RoleCoopManager}, {partyport.RoleCoopManager, htxroles.Member}} {
			if status, _, detail := callJSON(t, srv.URL, http.MethodPost, "/v1/households/copy", token(roles...)); status == http.StatusForbidden && detail == memberGuardDetail {
				t.Errorf("%v bị chặn bởi lớp chặn ghi của Thành viên HTX", roles)
			}
		}
		// AH-39: tài khoản kiêm hai vai dùng quyền Đại diện HTX (chỉ xem vụ) — dịch vụ từ chối, không phải lớp chặn.
		// (Route không qua cổng đồng ý thu thập dữ liệu, để thấy đúng lỗi của dịch vụ.)
		dual := token(partyport.RoleCoopManager, htxroles.Member)
		if status, code, detail := callJSON(t, srv.URL, http.MethodPost, "/v1/cropcycle/seasons/bulk-open", dual); status != http.StatusForbidden ||
			code != "cropcycle.read_only" || detail == memberGuardDetail {
			t.Errorf("kiêm hai vai POST /v1/cropcycle/seasons/bulk-open = %d %q %q, cần 403 cropcycle.read_only", status, code, detail)
		}
	}
}

func isWrite(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

// callJSON gọi route với thân {} và Idempotency-Key; trả status, code, detail của problem.
func callJSON(t *testing.T, base, method, path, token string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body.Code, body.Detail
}

// callBody gọi POST path với thân body và Idempotency-Key; trả status, code, detail của problem.
func callBody(t *testing.T, base, path, body, token string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Code, out.Detail
}
