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
// (403 forbidden) ở MỌI route ghi ngoài htxroles.MemberWriteRoutes, kể cả khi gọi thẳng API; Đại diện HTX và tài khoản
// kiêm hai vai (BR-02) không bị lớp này chặn.
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
		// Đối chứng: lớp lắp ráp không chặn Đại diện HTX và tài khoản kiêm hai vai (BR-02).
		for _, roles := range [][]string{{partyport.RoleCoopManager}, {partyport.RoleCoopManager, htxroles.Member}} {
			if status, _, detail := callJSON(t, srv.URL, http.MethodPost, "/v1/households/copy", token(roles...)); status == http.StatusForbidden && detail == memberGuardDetail {
				t.Errorf("%v bị chặn bởi lớp chặn ghi của Thành viên HTX", roles)
			}
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
