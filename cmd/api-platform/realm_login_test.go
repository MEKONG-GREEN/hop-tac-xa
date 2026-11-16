package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// FN-01 (ADR-0060): realm chỉ có MỘT client đăng nhập bằng mật khẩu (agrigreen-htx-login, direct grant có nhánh OTP).
// Không còn client hay luồng chỉ kiểm mật khẩu: mỗi lần kiểm mật khẩu đúng sẽ xóa bộ đếm lần sai của Keycloak, nên một
// đường kiểm không OTP cho phép đoán OTP không giới hạn (nới xác thực hai yếu tố).
func TestRealmHTXLoginClients(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	type execution struct {
		Authenticator string `json:"authenticator"`
		FlowAlias     string `json:"flowAlias"`
	}
	var realm struct {
		DirectGrantFlow string `json:"directGrantFlow"`
		Flows           []struct {
			Alias      string      `json:"alias"`
			Executions []execution `json:"authenticationExecutions"`
		} `json:"authenticationFlows"`
		Clients []struct {
			ClientID        string            `json:"clientId"`
			Description     string            `json:"description"`
			Enabled         bool              `json:"enabled"`
			PublicClient    bool              `json:"publicClient"`
			StandardFlow    bool              `json:"standardFlowEnabled"`
			ImplicitFlow    bool              `json:"implicitFlowEnabled"`
			DirectGrants    bool              `json:"directAccessGrantsEnabled"`
			ServiceAccounts bool              `json:"serviceAccountsEnabled"`
			Secret          string            `json:"secret"`
			DefaultScopes   []string          `json:"defaultClientScopes"`
			Attributes      map[string]string `json:"attributes"`
			FlowOverrides   map[string]string `json:"authenticationFlowBindingOverrides"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}

	// Luồng direct grant của realm có nhánh OTP đã cấu hình.
	var hasOTP bool
	for _, f := range realm.Flows {
		if strings.Contains(f.Alias, "password check") {
			t.Errorf("realm không còn luồng chỉ kiểm mật khẩu: %q", f.Alias)
		}
		if f.Alias == "agrigreen direct grant otp" {
			hasOTP = slices.ContainsFunc(f.Executions, func(e execution) bool { return e.Authenticator == "direct-grant-validate-otp" })
		}
	}
	if realm.DirectGrantFlow != "agrigreen direct grant" || !hasOTP {
		t.Fatalf("directGrantFlow=%q, nhánh OTP=%v", realm.DirectGrantFlow, hasOTP)
	}

	// Direct grant chỉ bật ở client có trước ticket này (agrigreen-seed, chỉ dev) và client đăng nhập mới.
	allowedDirect := []string{"agrigreen-seed", "agrigreen-htx-login"}
	var found bool
	for _, c := range realm.Clients {
		if strings.Contains(c.ClientID, "login-check") || len(c.FlowOverrides) != 0 && strings.HasPrefix(c.ClientID, "agrigreen-htx") {
			t.Errorf("client %q không được tồn tại hoặc ghi đè luồng", c.ClientID)
		}
		if c.DirectGrants && !slices.Contains(allowedDirect, c.ClientID) {
			t.Errorf("client %q bật direct grant ngoài danh sách cho phép %v", c.ClientID, allowedDirect)
		}
		if c.ClientID != "agrigreen-htx-login" {
			continue
		}
		found = true
		if c.Secret != "${AG_KC_LOGIN_CLIENT_SECRET}" || c.PublicClient || !c.Enabled || !c.DirectGrants || c.StandardFlow || c.ImplicitFlow || c.ServiceAccounts {
			t.Errorf("%s: phải là client bí mật chỉ direct grant, bí mật từ biến môi trường: %+v", c.ClientID, c)
		}
		if len(c.FlowOverrides) != 0 {
			t.Errorf("%s dùng directGrantFlow của realm (có OTP), không ghi đè: %v", c.ClientID, c.FlowOverrides)
		}
		if !slices.Equal(c.DefaultScopes, []string{"basic", "roles", "web-origins", "profile", "agrigreen-claims"}) {
			t.Errorf("%s scope: %v", c.ClientID, c.DefaultScopes)
		}
		if c.Attributes["client.session.idle.timeout"] != "14400" || c.Attributes["client.session.max.lifespan"] != "36000" {
			t.Errorf("%s phiên idle/max: %v", c.ClientID, c.Attributes)
		}
		if len(c.Description) > 255 {
			t.Errorf("%s: mô tả %d ký tự > 255", c.ClientID, len(c.Description))
		}
	}
	if !found {
		t.Fatal("thiếu client agrigreen-htx-login")
	}
}
