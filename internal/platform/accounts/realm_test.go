package accounts_test

import (
	"maps"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
)

func TestHTXRealmBinding(t *testing.T) {
	base := map[string]string{
		"AG_OIDC_ISSUER": "https://auth.example.vn/realms/agrigreen-htx", "AG_KC_REALM": "agrigreen-htx",
		"AG_OIDC_AUDIENCE": "agrigreen-htx-api", "AG_KC_CLIENT_ID": "agrigreen-htx-accounts",
		"AG_KC_LOGIN_CLIENT_ID": "agrigreen-htx-login", "AG_KC_ACTIONS_CLIENT_ID": "agrigreen-htx-web",
		"AG_OIDC_JWKS_URL": "http://keycloak:8080/realms/agrigreen-htx/protocol/openid-connect/certs",
	}
	if err := accounts.ValidateHTXRealmBinding(func(k string) string { return base[k] }); err != nil {
		t.Fatal(err)
	}
	for key, wrong := range map[string]string{
		"AG_OIDC_ISSUER": "https://auth.example.vn/realms/agrigreen", "AG_KC_REALM": "agrigreen",
		"AG_OIDC_AUDIENCE": "agrigreen-api", "AG_KC_CLIENT_ID": "agrigreen-accounts",
		"AG_KC_LOGIN_CLIENT_ID": "kn-web", "AG_KC_ACTIONS_CLIENT_ID": "agrigreen-web-ops",
		"AG_OIDC_JWKS_URL": "http://keycloak:8080/realms/agrigreen/protocol/openid-connect/certs",
	} {
		t.Run(key, func(t *testing.T) {
			env := maps.Clone(base)
			env[key] = wrong
			if err := accounts.ValidateHTXRealmBinding(func(k string) string { return env[k] }); err == nil {
				t.Fatal("mixed realm accepted")
			}
		})
	}
	for _, issuer := range []string{"https://auth.example.vn/realms/agrigreen-htx?realm=kn", "https://auth.example.vn/realms/agrigreen-htx#kn", "ftp://auth.example.vn/realms/agrigreen-htx"} {
		env := maps.Clone(base)
		env["AG_OIDC_ISSUER"] = issuer
		if err := accounts.ValidateHTXRealmBinding(func(k string) string { return env[k] }); err == nil {
			t.Fatal("invalid issuer accepted")
		}
	}
	legacy := map[string]string{"AG_OIDC_ISSUER": "https://auth.example.vn/realms/agrigreen", "AG_KC_REALM": "agrigreen"}
	if err := accounts.ValidateHTXRealmBinding(func(k string) string { return legacy[k] }); err != nil {
		t.Fatal(err)
	}
}
