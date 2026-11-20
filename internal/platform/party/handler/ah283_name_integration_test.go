package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Tạo đơn lẻ, sao chép/nhập và đổi tên phải dùng cùng một quy tắc, kể cả DB
// LC_CTYPE=C. HTX ngừng hoạt động vẫn giữ tên; tenant khác không giữ tên hộ nhau.
func TestAH283CooperativeNames(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	body := map[string]any{"name": "  htx   LÚA XANH  ", "admin_unit_code": "30002"}
	if _, err := root.provisionCooperative(body, key()); err == nil || problem.From(err).Code != "party.cooperative_name_taken" {
		t.Fatalf("tạo đơn lẻ trùng tên tiếng Việt: %v", err)
	}
	path := "/v1/orgs/" + tr.c2.String() + "/rename"
	if r := root.adminDo(http.MethodPost, path, map[string]any{"name": "HTX LÚA XANH"}); r.status != http.StatusConflict || r.body["code"] != "party.cooperative_name_taken" {
		t.Fatalf("đổi tên trùng: %d %v", r.status, r.body)
	}
	verifyChain(t, f, tr.c2, 1)
	if r := root.adminDo(http.MethodPost, path, map[string]any{"name": "htx cần thơ xanh"}); r.status != http.StatusOK {
		t.Fatalf("đổi hoa/thường tên của chính HTX: %d %v", r.status, r.body)
	}
	if r := root.adminDo(http.MethodPost, "/v1/cooperatives/"+tr.c1.String()+"/deactivate", map[string]any{"reason": "Ngừng tham gia theo quyết định của HTX"}); r.status != http.StatusOK {
		t.Fatalf("ngừng HTX: %d %v", r.status, r.body)
	}
	if _, err := root.provisionCooperative(body, key()); err == nil || problem.From(err).Code != "party.cooperative_name_taken" {
		t.Fatalf("HTX ngừng hoạt động vẫn giữ tên: %v", err)
	}
	other := f.as(ids.New(), uuid.Nil, port.RoleRegistryAdmin, authz.RolePlatformAdmin)
	if _, err := other.provisionCooperative(body, key()); err != nil {
		t.Fatalf("tenant khác được dùng tên độc lập: %v", err)
	}
}

func TestAH283ConcurrentCooperativeName(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	const requests = 6
	start := make(chan struct{})
	results := make(chan error, requests)
	for i := range requests {
		name := "HTX Đồng Thuận"
		if i%2 == 0 {
			name = "htx  ĐỒNG THUẬN"
		}
		go func() {
			<-start
			_, err := root.provisionCooperative(map[string]any{"name": name, "admin_unit_code": "30001"}, key())
			results <- err
		}()
	}
	close(start)
	created, rejected := 0, 0
	for range requests {
		err := <-results
		switch {
		case err == nil:
			created++
		case problem.From(err).Code == "party.cooperative_name_taken":
			rejected++
		default:
			t.Fatalf("tạo đồng thời: %v", err)
		}
	}
	if created != 1 || rejected != requests-1 {
		t.Fatalf("cần đúng một HTX, nhận created=%d rejected=%d", created, rejected)
	}
}
