package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestHTXRealmIsolation(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/compose/keycloak-htx/agrigreen-htx-realm.json")
	if err != nil {
		t.Fatal(err)
	}
	var realm realmFile
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}
	if realm.PasswordPolicy != "length(6) and notUsername and passwordHistory(5)" {
		t.Fatal("FN-07 password policy")
	}
	if realm.RegistrationAllowed || realm.LoginWithEmail || !realm.BruteForceProtected {
		t.Fatal("native realm login guards")
	}
	var roleNames []string
	for _, role := range realm.Roles.Realm {
		roleNames = append(roleNames, role.Name)
		if !slices.Contains([]string{"platform_admin", "coop_manager", "htx_member", "login_totp_required"}, role.Name) {
			t.Fatalf("foreign role %s", role.Name)
		}
	}
	for _, want := range []string{"platform_admin", "coop_manager", "htx_member"} {
		if !slices.Contains(roleNames, want) {
			t.Fatalf("missing %s", want)
		}
	}
	var clients []string
	for _, client := range realm.Clients {
		clients = append(clients, client.ClientID)
		if strings.HasPrefix(client.ClientID, "kn-") {
			t.Fatal("KN client")
		}
	}
	for _, want := range []string{"agrigreen-htx-api", "agrigreen-htx-accounts", "agrigreen-htx-web", "agrigreen-htx-app", "agrigreen-htx-login"} {
		if !slices.Contains(clients, want) {
			t.Fatalf("missing client %s", want)
		}
	}
	if len(clients) != 5 {
		t.Fatal("foreign clients")
	}
	if !strings.Contains(string(raw), `"included.client.audience": "agrigreen-htx-api"`) || strings.Contains(string(raw), `"included.client.audience": "agrigreen-api"`) {
		t.Fatal("mixed audience")
	}
}
