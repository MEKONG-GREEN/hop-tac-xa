package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
)

func TestAH285NativeIdentityScopeEnforcesOwnerAndMatrix(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tree := f.seedTree(root)
	other := root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX khác cùng xã AH285", "admin_unit_code": "30001", "parent_ref": tree.t30001.String()})
	first := f.as(f.tenant, tree.c1, htxroles.Representative).appDo(http.MethodPost, "/v1/households", farmer(tree.c1, "0907285001"))
	second := f.as(f.tenant, other, htxroles.Representative).appDo(http.MethodPost, "/v1/households", farmer(other, "0907285003"))
	if first.status != http.StatusCreated || second.status != http.StatusCreated {
		t.Fatalf("seed: %d %d", first.status, second.status)
	}
	h1, h2 := str(first.body, "household", "household_ref"), str(second.body, "household", "household_ref")
	if h1 == "" || h2 == "" {
		t.Fatal("missing household references")
	}
	f.vault.mu.Lock()
	callsBefore := len(f.vault.purposes)
	f.vault.mu.Unlock()
	for _, tc := range []struct {
		name              string
		who               caller
		ref, coop, access string
		status            int
		write             bool
	}{
		{"Admin own tenant", f.as(f.tenant, uuid.Nil, htxroles.Admin), h2, other.String(), "full", http.StatusOK, true},
		{"Representative own", f.as(f.tenant, tree.c1, htxroles.Representative), h1, tree.c1.String(), "full", http.StatusOK, true},
		{"Member own", f.as(f.tenant, tree.c1, htxroles.Member), h1, tree.c1.String(), "masked", http.StatusOK, false},
		{"Mixed native roles", f.as(f.tenant, tree.c1, htxroles.Representative, htxroles.Member), h1, tree.c1.String(), "full", http.StatusOK, true},
		{"Representative other same commune", f.as(f.tenant, tree.c1, htxroles.Representative), h2, "", "", http.StatusNotFound, false},
		{"Member wider legacy role stays native", f.as(f.tenant, tree.c1, htxroles.Member, partyport.RoleRegistryAdmin), h2, "", "", http.StatusNotFound, false},
		{"Representative wider legacy role stays native", f.as(f.tenant, tree.c1, htxroles.Representative, partyport.RoleKNOfficer), h2, "", "", http.StatusNotFound, false},
		{"Foreign tenant Admin", f.as(ids.New(), uuid.Nil, htxroles.Admin), h1, "", "", http.StatusNotFound, false},
		{"Legacy role alone", f.as(f.tenant, uuid.Nil, partyport.RoleRegistryAdmin), h1, "", "", http.StatusForbidden, false},
		{"Missing cooperative", f.as(f.tenant, uuid.Nil, htxroles.Member), h1, "", "", http.StatusForbidden, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.who.appDo(http.MethodGet, "/v1/households/"+tc.ref+"/identity-scope", nil)
			if r.status != tc.status {
				t.Fatalf("scope: %d %v", r.status, r.body)
			}
			if r.hdr.Get("Cache-Control") != "no-store" {
				t.Fatal("scope metadata must not be cached")
			}
			if tc.status == http.StatusOK && (len(r.body) != 6 || r.body["shared"] != false || r.body["ref"] != tc.ref || r.body["cooperative_ref"] != tc.coop || r.body["contact_access"] != tc.access || r.body["can_write"] != tc.write) {
				t.Fatalf("metadata projection: %v", r.body)
			}
		})
	}
	f.vault.mu.Lock()
	callsAfter := len(f.vault.purposes)
	f.vault.mu.Unlock()
	if callsAfter != callsBefore {
		t.Fatal("scope proof must not invoke Vault")
	}
}

func TestAH285NativeRegistrationScopeCannotWidenOrUseMissingProjection(t *testing.T) {
	f := newFixture(t)
	tree := f.seedTree(f.root())
	path := "/v1/cooperatives/" + tree.c1.String() + "/household-identity-scope"
	for _, tc := range []struct {
		who    caller
		status int
	}{
		{f.as(f.tenant, uuid.Nil, htxroles.Admin), http.StatusOK},
		{f.as(f.tenant, tree.c1, htxroles.Representative), http.StatusOK},
		{f.as(f.tenant, tree.c1, htxroles.Member), http.StatusForbidden},
		{f.as(f.tenant, tree.c2, htxroles.Representative, partyport.RoleRegistryAdmin), http.StatusNotFound},
		{f.as(ids.New(), uuid.Nil, htxroles.Admin), http.StatusNotFound},
	} {
		r := tc.who.appDo(http.MethodGet, path, nil)
		if r.status != tc.status {
			t.Fatalf("registration scope: %d %v", r.status, r.body)
		}
		if r.status == http.StatusOK && (len(r.body) != 6 || r.body["shared"] != false || r.body["can_write"] != true || r.body["contact_access"] != "full") {
			t.Fatalf("registration proof: %v", r.body)
		}
	}
	admin := f.env.AdminPool(t, migrations.PF)
	if _, err := admin.Exec(context.Background(), `DELETE FROM k_ref.node_closure WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	r := f.as(f.tenant, tree.c1, htxroles.Representative).appDo(http.MethodGet, path, nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("missing projection authorized registration: %d %v", r.status, r.body)
	}
}
