package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

func TestAH285HouseholdScopeForwardsOriginalTokenAndValidatesProof(t *testing.T) {
	ref, cooperative := ids.New(), ids.New()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer synthetic-scope-token" || r.Header.Get("Cache-Control") != "no-store" {
			t.Error("owner verification changed method/token/cache policy")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/cooperatives/") {
			_ = json.NewEncoder(w).Encode(client.HouseholdIdentityScope{Ref: cooperative, CooperativeRef: cooperative, Active: true, ContactAccess: "full", CanWrite: true})
			return
		}
		_ = json.NewEncoder(w).Encode(client.HouseholdIdentityScope{Ref: ref, CooperativeRef: cooperative, Active: true, ContactAccess: "masked"})
	}))
	defer server.Close()
	verifier, err := client.NewHouseholdScopeHTTP(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.HouseholdIdentityScope(context.Background(), ref); !errors.Is(err, client.ErrNoToken) {
		t.Fatalf("missing token: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("missing token reached owner")
	}
	ctx := authz.WithRawToken(context.Background(), "synthetic-scope-token")
	if scope, err := verifier.HouseholdIdentityScope(ctx, ref); err != nil || scope.ContactAccess != "masked" || scope.CanWrite {
		t.Fatalf("read proof: %+v %v", scope, err)
	}
	if scope, err := verifier.HouseholdRegistrationScope(ctx, cooperative); err != nil || !scope.CanWrite {
		t.Fatalf("registration proof: %+v %v", scope, err)
	}
}

func TestAH285HouseholdScopeRefusesRedirectAndMalformedOrUnavailableOwner(t *testing.T) {
	ref := ids.New()
	var escaped atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Add(1) }))
	defer other.Close()
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		redirect bool
		code     string
	}{
		{"redirect", http.StatusTemporaryRedirect, "", true, "vault.household_scope_unavailable"},
		{"unavailable", http.StatusServiceUnavailable, "private upstream diagnostics", false, "vault.household_scope_unavailable"},
		{"foreign", http.StatusNotFound, "", false, "vault.not_found"},
		{"wrong ref", http.StatusOK, `{"ref":"` + ids.New().String() + `","contact_access":"full"}`, false, "vault.household_scope_unavailable"},
		{"masked write", http.StatusOK, `{"ref":"` + ref.String() + `","contact_access":"masked","can_write":true}`, false, "vault.household_scope_unavailable"},
		{"unexpected identity payload", http.StatusOK, `{"ref":"` + ref.String() + `","contact_access":"full","phone":"must-not-be-returned"}`, false, "vault.household_scope_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.redirect {
					w.Header().Set("Location", other.URL)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			verifier, err := client.NewHouseholdScopeHTTP(server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = verifier.HouseholdIdentityScope(authz.WithRawToken(context.Background(), "synthetic-scope-token"), ref)
			if problem.From(err).Code != tc.code {
				t.Fatalf("scope failure: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "private upstream") || strings.Contains(err.Error(), "must-not-be-returned")) {
				t.Fatal("owner diagnostics or identity echoed")
			}
		})
	}
	if escaped.Load() != 0 {
		t.Fatal("redirect leaked request/token to another endpoint")
	}
}
