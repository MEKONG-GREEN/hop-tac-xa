package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/google/uuid"
)

func TestNativeSharingWorkflowAndAuthorization(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	a := f.as(f.tenant, tr.c1, htxroles.Representative)
	b := f.as(f.tenant, tr.c2, htxroles.Representative)
	create := func(c caller, target uuid.UUID) resp {
		return c.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": target})
	}
	change := func(c caller, ref, action string, rev int) resp {
		return c.appDo(http.MethodPost, "/v1/htx-sharing/"+ref+"/transitions", map[string]any{"action": action, "expected_revision": rev})
	}
	want := func(r resp, status int, state string) {
		t.Helper()
		if r.status != status || (state != "" && r.body["state"] != state) {
			t.Fatalf("got %d %v; want %d %s", r.status, r.body, status, state)
		}
	}
	want(create(a, tr.c1), 422, "")
	want(create(a, uuid.Nil), 422, "")
	for _, c := range []caller{f.as(f.tenant, tr.c1, htxroles.Member), f.as(f.tenant, uuid.Nil, htxroles.Admin), f.as(f.tenant, tr.c1, htxroles.Admin, htxroles.Representative)} {
		want(create(c, tr.c2), 403, "")
		want(c.appDo(http.MethodGet, "/v1/htx-sharing", nil), 403, "")
	}
	r := create(a, tr.c2)
	want(r, 201, "requested")
	ref := str(r.body, "ref")
	want(create(a, tr.c2), 409, "")
	for _, c := range []caller{a, b} {
		list := c.appDo(http.MethodGet, "/v1/htx-sharing", nil)
		if list.status != 200 || len(items(list)) != 1 {
			t.Fatalf("both parties see request: %+v", list)
		}
	}
	if candidates := a.appDo(http.MethodGet, "/v1/htx-sharing/candidates", nil); candidates.status != 200 || len(items(candidates)) != 0 {
		t.Fatalf("active pair excluded: %+v", candidates)
	}
	want(change(a, ref, "approve", 1), 409, "")
	want(change(b, ref, "revoke", 1), 409, "")
	outsider := f.as(f.tenant, uuid.New(), htxroles.Representative)
	want(change(outsider, ref, "approve", 1), 404, "")
	want(change(b, ref, "approve", 1), 200, "approved")
	want(change(b, ref, "revoke", 1), 409, "")
	// Reverse direction remains independent while A->B is approved.
	reverse := create(b, tr.c1)
	want(reverse, 201, "requested")
	want(change(a, ref, "revoke", 2), 200, "revoked")
	want(change(b, ref, "approve", 3), 409, "")
	r2 := create(a, tr.c2)
	want(r2, 201, "requested")
	want(change(b, str(r2.body, "ref"), "reject", 1), 200, "rejected")
	r3 := create(a, tr.c2)
	want(r3, 201, "requested")
	want(change(a, str(r3.body, "ref"), "revoke", 1), 200, "revoked")
	list := a.appDo(http.MethodGet, "/v1/htx-sharing", nil)
	if len(items(list)) != 4 {
		t.Fatalf("terminal history retained: %+v", list)
	}
	// No deletion route exists.
	want(a.appDo(http.MethodDelete, "/v1/htx-sharing/"+ref, nil), 404, "")
	foreign := f.as(uuid.New(), tr.c2, htxroles.Representative)
	want(change(foreign, str(reverse.body, "ref"), "approve", 1), 404, "")
}

// Real API -> Vault -> owner callback with one PF connection proves no port is
// called inside the read transaction, and checks masking/revocation end-to-end.
func TestNativeSharingReadsMaskedHouseholdsAndRevokesWithoutLogin(t *testing.T) {
	f := nativeVaultFixture(t)
	tr := f.seedTree(f.root())
	a := f.as(f.tenant, tr.c1, htxroles.Representative)
	b := f.as(f.tenant, tr.c2, htxroles.Representative)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	created := nativePost(admin, "/v1/household-profiles", "shared-household-create", nativeBody(tr.c2, "0913123222", "079123456123", "2"))
	if created.status != 201 {
		t.Fatalf("create: %+v", created)
	}
	href := str(created.body, "household_ref")
	profile := "/v1/households/" + href + "/profile"
	list := "/v1/cooperatives/" + tr.c2.String() + "/households"
	r := a.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": tr.c2})
	if r.status != 201 {
		t.Fatalf("request: %+v", r)
	}
	path := "/v1/htx-sharing/" + str(r.body, "ref") + "/transitions"
	for _, p := range []string{profile, list} {
		if got := a.appDo(http.MethodGet, p, nil); got.status != 404 {
			t.Fatalf("pending cannot read: %+v", got)
		}
	}
	approved := b.appDo(http.MethodPost, path, map[string]any{"action": "approve", "expected_revision": 1})
	if approved.status != 200 {
		t.Fatalf("approve: %+v", approved)
	}
	got := a.appDo(http.MethodGet, profile, nil)
	if got.status != 200 || str(got.body, "identity", "phone") != "0913***222" || str(got.body, "identity", "national_id") != "079******123" || got.body["contact_access"] != "masked" {
		t.Fatalf("shared masked profile: %+v", got)
	}
	proof := a.appDo(http.MethodGet, "/v1/households/"+href+"/identity-scope", nil)
	if proof.status != 200 || proof.body["shared"] != true || proof.body["can_write"] != false {
		t.Fatalf("read-only owner proof: %+v", proof)
	}
	rows := a.appDo(http.MethodGet, list, nil)
	if rows.status != 200 || len(items(rows)) != 1 || items(rows)[0]["masked_phone"] != "0913***222" {
		t.Fatalf("shared masked list: %+v", rows)
	}
	member := f.as(f.tenant, tr.c1, htxroles.Member)
	for _, p := range []string{profile, list} {
		if got := member.appDo(http.MethodGet, p, nil); got.status != 404 {
			t.Fatalf("Member cannot inherit sharing: %+v", got)
		}
	}
	for _, suffix := range []string{"/revisions", "/deletions"} {
		body := nativeBody(tr.c2, "0913123222", "079123456123", "9")
		body["expected_revision"] = 1
		if suffix == "/deletions" {
			body = map[string]any{"expected_revision": 1}
		}
		got := a.appDo(http.MethodPost, profile+suffix, body)
		if got.status < 400 {
			t.Fatalf("shared write accepted: %+v", got)
		}
	}
	revoked := b.appDo(http.MethodPost, path, map[string]any{"action": "revoke", "expected_revision": 2})
	if revoked.status != 200 {
		t.Fatalf("revoke: %+v", revoked)
	}
	for _, p := range []string{profile, list, "/v1/households/" + href + "/identity-scope"} {
		if got := a.appDo(http.MethodGet, p, nil); got.status != 404 {
			t.Fatalf("same token after revoke: %+v", got)
		}
	}
}
