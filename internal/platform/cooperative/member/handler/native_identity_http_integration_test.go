package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port/farmerid"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
)

func TestAH285NativeHouseholdIdentityThroughVaultAndOwnerHTTP(t *testing.T) {
	f := newFixture(t)
	tree := f.seedTree(f.root())
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), db.Module("pf/cooperative"))
	scopes, err := client.NewHouseholdScopeHTTP(f.app.URL, f.app.Client())
	if err != nil {
		t.Fatal(err)
	}
	vault := clienttest.NewPersistentHTTPWithHouseholdScopes(t, f.env.SizedPool(t, migrations.Vault, "vault_app", 1), httpx.Authenticate(f.iss.Verifier()), scopes)
	f.svc.Vault = farmerid.Households(vault)
	// Existing registrar flow seeds the encrypted identity. Native readers below
	// carry no Vault roles, and owner proof never invokes the identity adapter.
	seed := f.as(f.tenant, tree.c1, htxroles.Representative, client.RoleRegistrar)
	body := farmer(tree.c1, "0907285021")
	body["identity"].(map[string]any)["national_id"] = "079123456789"
	created := seed.appDo(http.MethodPost, "/v1/households", body)
	if created.status != http.StatusCreated {
		t.Fatalf("seed household: %d %v", created.status, created.body)
	}
	ref := uuid.MustParse(str(created.body, "household", "household_ref"))
	reader, ok := client.AsHouseholdIdentityReader(vault)
	if !ok {
		t.Fatal("missing narrow identity adapter")
	}
	for _, tc := range []struct {
		name                  string
		who                   caller
		phone, national, code string
	}{
		{"Member", f.as(f.tenant, tree.c1, htxroles.Member), "0907***021", "079******789", ""},
		{"Representative", f.as(f.tenant, tree.c1, htxroles.Representative), "0907285021", "079123456789", ""},
		{"Admin", f.as(f.tenant, uuid.Nil, htxroles.Admin), "0907285021", "079123456789", ""},
		{"Foreign cooperative", f.as(f.tenant, tree.c2, htxroles.Representative), "", "", client.CodeNotFound},
		{"Foreign tenant", f.as(ids.New(), uuid.Nil, htxroles.Admin), "", "", client.CodeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := tc.who.appDo(http.MethodGet, "/v1/households/"+ref.String()+"/profile", nil)
			if profile.hdr.Get("Cache-Control") != "no-store" {
				t.Fatal("PF identity response was cached")
			}
			if tc.code == "" {
				if profile.status != http.StatusOK || str(profile.body, "identity", "phone") != tc.phone || str(profile.body, "identity", "national_id") != tc.national {
					t.Fatalf("PF profile boundary: status=%d", profile.status)
				}
			} else if profile.status != http.StatusNotFound {
				t.Fatalf("foreign PF profile: status=%d", profile.status)
			}
			ctx, cancel := context.WithTimeout(authz.WithRawToken(context.Background(), tc.who.token), 10*time.Second)
			defer cancel()
			out, err := reader.ReadHouseholdIdentity(ctx, client.PurposeContact, ref)
			if tc.code != "" {
				if err == nil || problem.From(err).Code != tc.code || out.Phone != "" || out.NationalID != "" {
					t.Fatalf("owner boundary: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if out.Phone != tc.phone || out.NationalID != tc.national {
				t.Fatal("identity was not masked at Vault")
			}
			if _, err := vault.Resolve(ctx, client.PurposeContact, ref); err == nil || problem.From(err).Code != "forbidden" {
				t.Fatalf("generic Resolve expanded: %v", err)
			}
		})
	}
}
