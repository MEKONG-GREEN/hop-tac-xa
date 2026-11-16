package main

import (
	"net/http"
	"testing"
)

// FN-01 (ADR-0060): route đăng nhập, làm mới và đăng xuất nằm ở /v1/public (không cần token) và KHÔNG đòi Idempotency-Key —
// nhóm /v1/public không có middleware idempotency nên thân chứa mật khẩu không vào kho idempotency. Thiếu cấu hình
// Keycloak thì fail-closed 503 accounts.login_unavailable (không 401/404/400 idempotency_key_required).
func TestLoginRoutesPublicNoIdempotencyKey(t *testing.T) {
	app, _, _ := glueFixture(t)
	body := map[string]any{"identifier": "nguoi.dung", "password": "mat-khau-bat-ky", "refresh_token": "x"}
	for _, p := range []string{"/v1/public/auth/login", "/v1/public/auth/refresh", "/v1/public/auth/logout"} {
		resp, b := send(t, http.MethodPost, app.URL+p, map[string]string{"Content-Type": "application/json"}, body)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Fatalf("%s phải là route công khai: %d %v", p, resp.StatusCode, b)
		}
		if code, _ := b["code"].(string); code == "idempotency_key_required" || resp.StatusCode == http.StatusBadRequest {
			t.Fatalf("%s không được đòi Idempotency-Key: %d %v", p, resp.StatusCode, b)
		}
		if resp.StatusCode != http.StatusServiceUnavailable || b["code"] != "accounts.login_unavailable" {
			t.Fatalf("%s chưa cấu hình đăng nhập phải 503 login_unavailable: %d %v", p, resp.StatusCode, b)
		}
	}
}
