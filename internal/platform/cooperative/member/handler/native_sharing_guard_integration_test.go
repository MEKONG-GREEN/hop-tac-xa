package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// rawPost is goroutine-safe: it never calls testing.T.
func rawPost(c caller, path, key string, body any) (int, map[string]any, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.f.app.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = r.Body.Close() }()
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	return r.StatusCode, m, nil
}

// FN-09: one user operation per key, stale revision and concurrent decisions
// never produce a second active request or a server error.
func TestNativeSharingReplayStaleRevisionAndRaces(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	a := f.as(f.tenant, tr.c1, htxroles.Representative)
	b := f.as(f.tenant, tr.c2, htxroles.Representative)

	k := "share-" + ids.New().String()
	first := nativePost(a, "/v1/htx-sharing", k, map[string]any{"owner_cooperative_ref": tr.c2})
	again := nativePost(a, "/v1/htx-sharing", k, map[string]any{"owner_cooperative_ref": tr.c2})
	if first.status != 201 || again.status != 201 || str(first.body, "ref") != str(again.body, "ref") {
		t.Fatalf("replay must return the same request: %+v / %+v", first, again)
	}
	if reused := nativePost(a, "/v1/htx-sharing", k, map[string]any{"owner_cooperative_ref": tr.c1}); reused.status < 400 || reused.status >= 500 {
		t.Fatalf("key reused with another body: %+v", reused)
	}
	if list := a.appDo(http.MethodGet, "/v1/htx-sharing", nil); len(items(list)) != 1 {
		t.Fatalf("replay created one row only: %+v", list)
	}
	ref := str(first.body, "ref")
	path := "/v1/htx-sharing/" + ref + "/transitions"

	// A approve and a withdrawal race on revision 1: exactly one wins.
	type result struct {
		status int
		err    error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, op := range []struct {
		c      caller
		action string
	}{{b, "approve"}, {a, "revoke"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, _, err := rawPost(op.c, path, "race-"+ids.New().String(), map[string]any{"action": op.action, "expected_revision": 1})
			results <- result{s, err}
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		switch r.status {
		case 200:
			won++
		case 409:
		default:
			t.Fatalf("race status %d", r.status)
		}
	}
	if won != 1 {
		t.Fatalf("exactly one decision on revision 1, got %d", won)
	}
	if stale := b.appDo(http.MethodPost, path, map[string]any{"action": "reject", "expected_revision": 1}); stale.status != 409 {
		t.Fatalf("stale revision: %+v", stale)
	}

	// Concurrent requests for the same direction: one active request at most.
	statuses := make(chan result, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, _, err := rawPost(b, "/v1/htx-sharing", "pair-"+ids.New().String(), map[string]any{"owner_cooperative_ref": tr.c1})
			statuses <- result{s, err}
		}()
	}
	wg.Wait()
	close(statuses)
	created := 0
	for r := range statuses {
		if r.err != nil {
			t.Fatal(r.err)
		}
		switch r.status {
		case 201:
			created++
		case 409:
		default:
			t.Fatalf("concurrent request status %d", r.status)
		}
	}
	if created != 1 {
		t.Fatalf("one active request per direction, got %d", created)
	}
}

// The RLS shared-read branch only opens in a physically READ ONLY transaction
// with the local opt-in, for a live approved grant; it never opens writes.
func TestNativeSharedReadRLSNeverOpensWrites(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	a := f.as(f.tenant, tr.c1, htxroles.Representative)
	b := f.as(f.tenant, tr.c2, htxroles.Representative)
	seed := b.appDo(http.MethodPost, "/v1/households", farmer(tr.c2, "0907278011"))
	if seed.status != http.StatusCreated {
		t.Fatalf("seed: %+v", seed)
	}
	household := str(seed.body, "household", "household_ref")
	ctxA := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: a.actor, TenantID: f.tenant, OrgRef: tr.c1, Roles: []string{htxroles.Representative}})
	visible := func(mode pgx.TxAccessMode, optIn bool) int {
		t.Helper()
		var n int
		err := tenant.WithTenantTxOptions(ctxA, f.db, f.tenant, pgx.TxOptions{AccessMode: mode}, func(tx pgx.Tx) error {
			if optIn {
				if _, err := tx.Exec(ctxA, `SELECT pf_coop.native_shared_read_begin()`); err != nil {
					return err
				}
			}
			return tx.QueryRow(ctxA, `SELECT count(*) FROM pf_coop.household WHERE household_ref=$1`, household).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	writeChanges := func(mode pgx.TxAccessMode, stmt string) int64 {
		t.Helper()
		var changed int64
		_ = tenant.WithTenantTxOptions(ctxA, f.db, f.tenant, pgx.TxOptions{AccessMode: mode}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctxA, `SELECT pf_coop.native_shared_read_begin()`); err != nil {
				return err
			}
			tag, err := tx.Exec(ctxA, stmt, household)
			if err == nil {
				changed = tag.RowsAffected()
			}
			return err
		})
		return changed
	}
	if visible(pgx.ReadOnly, true) != 0 {
		t.Fatal("no grant: shared row hidden")
	}
	r := a.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": tr.c2})
	path := "/v1/htx-sharing/" + str(r.body, "ref") + "/transitions"
	if visible(pgx.ReadOnly, true) != 0 {
		t.Fatal("pending grant: shared row hidden")
	}
	if got := b.appDo(http.MethodPost, path, map[string]any{"action": "approve", "expected_revision": 1}); got.status != 200 {
		t.Fatalf("approve: %+v", got)
	}
	if visible(pgx.ReadOnly, true) != 1 {
		t.Fatal("approved grant: READ ONLY opt-in sees the shared row")
	}
	if visible(pgx.ReadOnly, false) != 0 || visible(pgx.ReadWrite, true) != 0 {
		t.Fatal("shared branch requires both opt-in and READ ONLY")
	}
	// A pinned source list outside READ ONLY opens neither reads nor writes.
	var forged int
	var forgedChanged int64
	_ = tenant.WithTenantTxOptions(ctxA, f.db, f.tenant, pgx.TxOptions{AccessMode: pgx.ReadWrite}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctxA, `SELECT set_config('app.native_shared_sources',$1,true)`, "{"+tr.c2.String()+"}"); err != nil {
			return err
		}
		if err := tx.QueryRow(ctxA, `SELECT count(*) FROM pf_coop.household WHERE household_ref=$1`, household).Scan(&forged); err != nil {
			return err
		}
		tag, err := tx.Exec(ctxA, `UPDATE pf_coop.household SET revision=revision WHERE household_ref=$1`, household)
		forgedChanged = tag.RowsAffected()
		return err
	})
	if forged != 0 || forgedChanged != 0 {
		t.Fatalf("read-write transaction with pinned sources saw %d, changed %d", forged, forgedChanged)
	}
	for _, stmt := range []string{
		`UPDATE pf_coop.household SET revision=revision WHERE household_ref=$1`,
		`DELETE FROM pf_coop.household WHERE household_ref=$1`,
		`UPDATE pf_coop.household_membership SET until_date=current_date WHERE household_ref=$1`,
	} {
		for _, mode := range []pgx.TxAccessMode{pgx.ReadWrite, pgx.ReadOnly} {
			if n := writeChanges(mode, stmt); n != 0 {
				t.Fatalf("%s (%s) changed %d shared rows", stmt, mode, n)
			}
		}
	}
	if visible(pgx.ReadOnly, true) != 1 {
		t.Fatal("shared row unchanged after rejected writes")
	}
	// Another node never benefits from A's grant.
	other := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: f.tenant, OrgRef: uuid.New(), Roles: []string{htxroles.Representative}})
	var n int
	_ = tenant.WithTenantTxOptions(other, f.db, f.tenant, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		_, _ = tx.Exec(other, `SELECT pf_coop.native_shared_read_begin()`)
		return tx.QueryRow(other, `SELECT count(*) FROM pf_coop.household WHERE household_ref=$1`, household).Scan(&n)
	})
	if n != 0 {
		t.Fatal("grant is bound to the requesting cooperative")
	}
	if got := a.appDo(http.MethodPost, path, map[string]any{"action": "revoke", "expected_revision": 2}); got.status != 200 {
		t.Fatalf("revoke: %+v", got)
	}
	if visible(pgx.ReadOnly, true) != 0 {
		t.Fatal("revoked grant: shared row hidden at the next transaction")
	}
}

// FN-03, QTPQ-Q-55/AC-N1/AC-N2: tab "Liên kết chia sẻ dữ liệu" — Đại diện HTX trên HTX của mình, Admin trên mọi
// HTX (chỉ đọc), Thành viên HTX không có; Đại diện không xem quan hệ của HTX khác, kể cả HTX đã chia sẻ cho mình.
func TestCooperativeSharingTabAudience(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	a := f.as(f.tenant, tr.c1, htxroles.Representative)
	b := f.as(f.tenant, tr.c2, htxroles.Representative)
	r := a.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": tr.c2})
	if r.status != http.StatusCreated {
		t.Fatalf("request: %+v", r)
	}
	if got := b.appDo(http.MethodPost, "/v1/htx-sharing/"+str(r.body, "ref")+"/transitions", map[string]any{"action": "approve", "expected_revision": 1}); got.status != http.StatusOK {
		t.Fatalf("approve: %+v", got)
	}
	tab := func(c caller, coop uuid.UUID) resp {
		t.Helper()
		got := c.appDo(http.MethodGet, "/v1/cooperatives/"+coop.String()+"/htx-sharing", nil)
		if got.hdr.Get("Cache-Control") != "no-store" {
			t.Fatal("sharing tab must not be cached")
		}
		return got
	}
	one := func(got resp, who string) {
		t.Helper()
		items, _ := got.body["items"].([]any)
		if got.status != http.StatusOK || len(items) != 1 {
			t.Fatalf("%s: %d %v", who, got.status, got.body)
		}
		g, _ := items[0].(map[string]any)
		if g["state"] != "approved" || g["requester_cooperative_ref"] != tr.c1.String() || g["owner_cooperative_ref"] != tr.c2.String() {
			t.Fatalf("%s: %v", who, g)
		}
	}
	one(tab(a, tr.c1), "Đại diện HTX yêu cầu")
	one(tab(b, tr.c2), "Đại diện HTX nhận")
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	one(tab(admin, tr.c1), "Admin trên HTX A")
	one(tab(admin, tr.c2), "Admin trên HTX B")
	if got := tab(a, tr.c2); got.status != http.StatusNotFound {
		t.Fatalf("Đại diện HTX trên HTX được chia sẻ: %d %v", got.status, got.body)
	}
	if got := tab(f.as(f.tenant, tr.c1, htxroles.Member), tr.c1); got.status != http.StatusForbidden {
		t.Fatalf("Thành viên HTX: %d %v", got.status, got.body)
	}
	if got := tab(admin, uuid.New()); got.status != http.StatusNotFound {
		t.Fatalf("Admin, HTX không tồn tại: %d %v", got.status, got.body)
	}
	if got := tab(f.as(ids.New(), uuid.Nil, htxroles.Admin), tr.c1); got.status == http.StatusOK {
		t.Fatalf("Admin tenant khác: %d %v", got.status, got.body)
	}
	// Admin chỉ xem: không gửi, duyệt hay thu hồi.
	if got := admin.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": tr.c1}); got.status != http.StatusForbidden {
		t.Fatalf("Admin gửi yêu cầu: %d %v", got.status, got.body)
	}
}
