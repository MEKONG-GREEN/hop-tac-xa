package handler_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/inbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

type fakeSharingReps struct{ byCoop map[uuid.UUID][]uuid.UUID }

func (r fakeSharingReps) Active(_ context.Context, _, coop uuid.UUID) ([]uuid.UUID, error) {
	return r.byCoop[coop], nil
}

type fakeSharingNotifier struct{ calls []port.SharingNoticeV1 }

func (n *fakeSharingNotifier) SharingChanged(_ context.Context, _ uuid.UUID, notice port.SharingNoticeV1) error {
	n.calls = append(n.calls, notice)
	return nil
}

// AH-279 (NTF-01): mỗi lần đổi trạng thái yêu cầu chia sẻ phát sự kiện sau commit; consumer gửi thông báo cho Đại diện
// HTX của đúng bên (AC-N1..N3), không gửi cho người vừa thao tác; giao lại không gửi trùng.
func TestAH279_SharingNoticeRecipients(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	a := f.as(f.tenant, tr.c1, htxroles.Representative)
	b := f.as(f.tenant, tr.c2, htxroles.Representative)
	a2 := f.as(f.tenant, tr.c1, htxroles.Representative)
	reps := fakeSharingReps{byCoop: map[uuid.UUID][]uuid.UUID{tr.c1: {a.actor, a2.actor}, tr.c2: {b.actor}}}
	sender := &fakeSharingNotifier{}
	f.svc.SharingReps, f.svc.SharingNotifier = reps, sender
	pool := f.env.AdminPool(t, migrations.PF)
	seen := map[uuid.UUID]bool{}
	deliver := func(action string) port.SharingNoticeV1 {
		t.Helper()
		rows, err := pool.Query(context.Background(), `SELECT id,event_type,schema_version,tenant_id,occurred_at,producer,payload FROM shared.outbox
		 WHERE tenant_id=$1 AND event_type=$2 AND payload->>'action'=$3 ORDER BY occurred_at`, f.tenant, port.EventNativeSharingChanged, action)
		if err != nil {
			t.Fatal(err)
		}
		var envs []outbox.Envelope
		for rows.Next() {
			var env outbox.Envelope
			if err := rows.Scan(&env.EventID, &env.EventType, &env.SchemaVersion, &env.TenantID, &env.OccurredAt, &env.Producer, &env.Payload); err != nil {
				t.Fatal(err)
			}
			if !seen[env.EventID] {
				envs = append(envs, env)
			}
		}
		rows.Close()
		if len(envs) != 1 {
			t.Fatalf("%s: muốn 1 sự kiện mới, có %d", action, len(envs))
		}
		seen[envs[0].EventID] = true
		before := len(sender.calls)
		for range 2 { // giao lại không gửi trùng
			if _, err := inbox.Process(context.Background(), f.db, port.SharingNoticeConsumer, envs[0], f.svc.HandleSharingNotice); err != nil {
				t.Fatal(err)
			}
		}
		if len(sender.calls) != before+1 {
			t.Fatalf("%s: số lần gửi %d", action, len(sender.calls)-before)
		}
		return sender.calls[len(sender.calls)-1]
	}
	transition := func(c caller, ref, action string, rev int) {
		t.Helper()
		r := c.appDo(http.MethodPost, "/v1/htx-sharing/"+ref+"/transitions", map[string]any{"action": action, "expected_revision": rev})
		if r.status != http.StatusOK {
			t.Fatalf("%s: %d %v", action, r.status, r.body)
		}
	}

	// AC-N1: A gửi → Đại diện HTX B nhận; Đại diện HTX A không nhận.
	r := a.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": tr.c2})
	if r.status != http.StatusCreated {
		t.Fatalf("gửi yêu cầu: %d %v", r.status, r.body)
	}
	ref := str(r.body, "ref")
	n := deliver("request")
	if n.CooperativeRef != tr.c2 || !slices.Equal(n.Recipients, []uuid.UUID{b.actor}) || n.ActorCoopName != "HTX Lúa Xanh" || n.RequestRef.String() != ref {
		t.Fatalf("gửi yêu cầu: %+v", n)
	}
	// AC-N2: B duyệt → mọi Đại diện HTX A nhận.
	transition(b, ref, "approve", 1)
	n = deliver("approve")
	if n.CooperativeRef != tr.c1 || len(n.Recipients) != 2 || n.ActorCoopName != "HTX Cần Thơ Xanh" {
		t.Fatalf("duyệt: %+v", n)
	}
	// AC-N3: A thu hồi → chỉ Đại diện HTX B nhận; người thu hồi (và Đại diện khác của A) không nhận.
	transition(a, ref, "revoke", 2)
	n = deliver("revoke")
	if n.CooperativeRef != tr.c2 || !slices.Equal(n.Recipients, []uuid.UUID{b.actor}) || n.Action != "revoke" {
		t.Fatalf("thu hồi: %+v", n)
	}
	// Từ chối: yêu cầu mới, B từ chối → A nhận.
	r = a.appDo(http.MethodPost, "/v1/htx-sharing", map[string]any{"owner_cooperative_ref": tr.c2})
	deliver("request")
	transition(b, str(r.body, "ref"), "reject", 1)
	if n = deliver("reject"); n.CooperativeRef != tr.c1 || n.Action != "reject" {
		t.Fatalf("từ chối: %+v", n)
	}
	// Thiếu danh bạ/cổng thông báo → consumer không khởi động (fail-closed).
	f.svc.SharingNotifier = nil
	if err := f.svc.SharingNoticeReady(); err == nil {
		t.Fatal("thiếu cổng thông báo phải lỗi")
	}
}
