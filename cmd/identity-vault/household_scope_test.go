package main

import "testing"

func TestAH285HouseholdScopeConfigurationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name          string
		env           map[string]string
		disabled, bad bool
	}{
		{"disabled", map[string]string{}, true, false},
		{"configured", map[string]string{"AG_VAULT_HOUSEHOLD_SCOPE_URL": "http://api-platform:8080"}, false, false},
		{"invalid URL", map[string]string{"AG_VAULT_HOUSEHOLD_SCOPE_URL": "file:///private"}, false, true},
		{"credentials in URL", map[string]string{"AG_VAULT_HOUSEHOLD_SCOPE_URL": "https://user:secret@example.test"}, false, true},
		{"partial TLS without URL", map[string]string{"AG_PLATFORM_CLIENT_CERT": "missing.pem"}, false, true},
		{"missing TLS key", map[string]string{"AG_VAULT_HOUSEHOLD_SCOPE_URL": "https://api-platform", "AG_PLATFORM_CLIENT_CERT": "missing.pem"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := householdScopesFromEnv(func(k string) string { return tc.env[k] })
			if (err != nil) != tc.bad || (err == nil && (got == nil) != tc.disabled) {
				t.Fatalf("configuration result: disabled=%v error=%v", got == nil, err)
			}
		})
	}
}
