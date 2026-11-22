package handler_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// liveSharing stands in for pf/cooperative: the live set of approved sources,
// read on every request (no login snapshot).
type liveSharing struct {
	mu    sync.Mutex
	refs  []uuid.UUID
	calls int
}

func (s *liveSharing) set(refs ...uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs = refs
}

func (s *liveSharing) NativeSharedCooperatives(context.Context) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return slices.Clone(s.refs), nil
}

// FN-06/AC-02, AC-04, AC-05, AC-07 (AH-274) và FN-09/AC-03, AC-08 (AH-278): danh sách và chi tiết HTX của Đại
// diện HTX gồm HTX mình và HTX đã duyệt chia sẻ (kể cả khác tỉnh), mất ngay khi thu hồi với cùng token; HTX được
// chia sẻ chỉ xem; Thành viên HTX và Admin không đổi phạm vi.
func TestFN06_SharedCooperativesFollowLiveGrant(t *testing.T) {
	f := newFixture(t)
	sharing := &liveSharing{}
	f.mod.UseNativeSharing(sharing)
	tr := f.seedFN06(f.root())
	rep := f.as(f.tenant, tr.c1, port.RoleCoopManager)
	listed := func(c caller) []string {
		t.Helper()
		r := c.appDo(http.MethodGet, "/v1/cooperatives", nil)
		if r.status != http.StatusOK {
			t.Fatalf("list: %d %v", r.status, r.body)
		}
		got := refs(r, "ref")
		slices.Sort(got)
		return got
	}
	if got := listed(rep); !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("chưa chia sẻ: %v", got)
	}
	wantOutOfScope(t, rep.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil), "chưa chia sẻ")

	sharing.set(tr.c2) // B (khác tỉnh) duyệt yêu cầu của A
	want := []string{tr.c1.String(), tr.c2.String()}
	slices.Sort(want)
	if got := listed(rep); !slices.Equal(got, want) {
		t.Fatalf("đã duyệt, cùng token: %v, cần %v", got, want)
	}
	if r := rep.appDo(http.MethodGet, "/v1/cooperatives?admin_unit=31001", nil); !slices.Equal(refs(r, "ref"), []string{tr.c2.String()}) {
		t.Fatalf("lọc theo xã của HTX được chia sẻ: %d %v", r.status, r.body)
	}
	if r := rep.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil); r.status != http.StatusOK || str(r.body, "ref") != tr.c2.String() {
		t.Fatalf("chi tiết HTX được chia sẻ: %d %v", r.status, r.body)
	}
	wantOutOfScope(t, rep.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c3.String(), nil), "HTX cùng xã chưa chia sẻ")

	// Chỉ xem: không sửa, không xóa HTX được chia sẻ.
	for _, path := range []string{"/profile/revisions", "/deletions"} {
		if r := rep.appDo(http.MethodPost, "/v1/cooperatives/"+tr.c2.String()+path, map[string]any{"expected_revision": 1, "name": "Đổi tên"}); r.status < 400 {
			t.Fatalf("ghi %s HTX được chia sẻ: %d %v", path, r.status, r.body)
		}
	}

	// Thành viên HTX (kể cả HTX của mình được chia sẻ) và Admin không đổi phạm vi.
	member := f.as(f.tenant, tr.c1, port.RoleHTXMember)
	if got := listed(member); !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("Thành viên HTX: %v", got)
	}
	wantOutOfScope(t, member.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil), "Thành viên HTX")
	admin := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	if got := listed(admin); len(got) != 3 {
		t.Fatalf("Admin: %v", got)
	}

	sharing.set() // thu hồi
	if got := listed(rep); !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("đã thu hồi, cùng token: %v", got)
	}
	wantOutOfScope(t, rep.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil), "đã thu hồi")
}
