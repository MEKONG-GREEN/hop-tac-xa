package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// FN-01 (ADR-0060): hai client bí mật của màn đăng nhập riêng. Đọc dạng triển khai: client kiểm mật khẩu phải gắn flow
// KHÔNG có OTP và token không có aud/vai (API không nhận), client chính dùng direct grant có nhánh OTP.
func TestRealmHTXLoginClients(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	type execution struct {
		Authenticator       string `json:"authenticator"`
		AuthenticatorConfig string `json:"authenticatorConfig"`
		FlowAlias           string `json:"flowAlias"`
		Requirement         string `json:"requirement"`
	}
	var realm struct {
		DirectGrantFlow string `json:"directGrantFlow"`
		Flows           []struct {
			ID         string      `json:"id"`
			Alias      string      `json:"alias"`
			TopLevel   bool        `json:"topLevel"`
			Provider   string      `json:"providerId"`
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
			FullScope       *bool             `json:"fullScopeAllowed"`
			Secret          string            `json:"secret"`
			DefaultScopes   []string          `json:"defaultClientScopes"`
			OptionalScopes  []string          `json:"optionalClientScopes"`
			Attributes      map[string]string `json:"attributes"`
			FlowOverrides   map[string]string `json:"authenticationFlowBindingOverrides"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}

	var checkFlowID string
	for _, f := range realm.Flows {
		if f.Alias != "agrigreen htx password check" {
			continue
		}
		checkFlowID = f.ID
		if !f.TopLevel || f.Provider != "basic-flow" || f.ID == "" || len(f.Executions) != 2 {
			t.Fatalf("flow kiểm mật khẩu: %+v", f)
		}
		for _, e := range f.Executions {
			if e.Requirement != "REQUIRED" || e.FlowAlias != "" || (e.Authenticator != "direct-grant-validate-username" && e.Authenticator != "direct-grant-validate-password") {
				t.Errorf("flow kiểm mật khẩu chỉ gồm tên đăng nhập và mật khẩu (không OTP): %+v", e)
			}
		}
	}
	if checkFlowID == "" {
		t.Fatal("thiếu flow 'agrigreen htx password check'")
	}
	// Flow direct grant của realm (client chính) phải có nhánh OTP đã cấu hình.
	var mainHasOTP bool
	for _, f := range realm.Flows {
		if f.Alias == "agrigreen direct grant otp" {
			mainHasOTP = slices.ContainsFunc(f.Executions, func(e execution) bool { return e.Authenticator == "direct-grant-validate-otp" })
		}
	}
	if realm.DirectGrantFlow != "agrigreen direct grant" || !mainHasOTP {
		t.Fatalf("directGrantFlow=%q, nhánh OTP=%v", realm.DirectGrantFlow, mainHasOTP)
	}

	seen := map[string]bool{}
	for _, c := range realm.Clients {
		switch c.ClientID {
		case "agrigreen-htx-login":
			seen[c.ClientID] = true
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
		case "agrigreen-htx-login-check":
			seen[c.ClientID] = true
			if c.Secret != "${AG_KC_LOGIN_CHECK_CLIENT_SECRET}" || c.PublicClient || !c.Enabled || !c.DirectGrants || c.StandardFlow || c.ImplicitFlow || c.ServiceAccounts {
				t.Errorf("%s: phải là client bí mật chỉ direct grant, bí mật từ biến môi trường: %+v", c.ClientID, c)
			}
			if c.FlowOverrides["direct_grant"] != checkFlowID {
				t.Errorf("%s phải gắn flow kiểm mật khẩu (không OTP): %v", c.ClientID, c.FlowOverrides)
			}
			if !slices.Equal(c.DefaultScopes, []string{"basic"}) || len(c.OptionalScopes) != 0 || c.FullScope == nil || *c.FullScope {
				t.Errorf("%s: chỉ scope basic, không scope tùy chọn, fullScopeAllowed=false để token không có aud/vai/tenant: %+v", c.ClientID, c)
			}
		}
		if (c.ClientID == "agrigreen-htx-login" || c.ClientID == "agrigreen-htx-login-check") && len(c.Description) > 255 {
			t.Errorf("%s: mô tả %d ký tự > 255", c.ClientID, len(c.Description))
		}
	}
	if len(seen) != 2 {
		t.Fatalf("thiếu client đăng nhập: %v", seen)
	}
}
