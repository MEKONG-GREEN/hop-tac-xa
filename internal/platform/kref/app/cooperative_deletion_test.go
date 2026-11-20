package app

import (
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/google/uuid"
)

func TestAH283DeletedSnapshotWinsBeforeCreatedDelivery(t *testing.T) {
	f := newFx(t)
	tid, ref := ids.New(), ids.New()
	deleted := orgEv(ref, uuid.Nil, 2)
	deleted["kind"] = "cooperative"
	deleted["active"] = false
	deleted["reason"] = "Xóa HTX tạo nhầm chưa phát sinh dữ liệu"
	f.must(tid, EventOrgDeleted, deleted)
	created := orgEv(ref, uuid.Nil, 1)
	created["kind"] = "cooperative"
	f.must(tid, EventOrgCreated, created)
	snapshot, err := f.mod.Snapshot(f.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Orgs) != 1 || snapshot.Orgs[0].Active || snapshot.Orgs[0].Seq != 2 {
		t.Fatalf("late created resurrected deleted HTX: %+v", snapshot.Orgs)
	}
}
