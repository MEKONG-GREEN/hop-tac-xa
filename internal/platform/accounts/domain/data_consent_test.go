package domain

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRequiresDataConsent_REQ011(t *testing.T) {
	for _, tc := range []struct {
		roles []string
		want  bool
	}{
		{nil, false},
		{[]string{"coop_manager"}, true},
		{[]string{"account_admin"}, false},
		{[]string{"farmer"}, false},
		{[]string{"kn_officer"}, false},
		// Có cả vai HTX lẫn Admin vẫn phải đồng ý.
		{[]string{"account_admin", "coop_manager"}, true},
		// AH-39: Thành viên HTX cũng phải đồng ý (mở vụ, ghi nhật ký có ảnh/GPS).
		{[]string{"htx_member"}, true},
		{[]string{"htx_member", "coop_manager"}, true},
	} {
		if got := RequiresDataConsent(tc.roles); got != tc.want {
			t.Errorf("RequiresDataConsent(%v) = %v, muốn %v", tc.roles, got, tc.want)
		}
	}
}

func TestValidConsentVersion_REQ011(t *testing.T) {
	for v, want := range map[string]bool{
		"tam-1": true, "v2.0_a": true, strings.Repeat("a", 40): true,
		"": false, strings.Repeat("a", 41): false, "có dấu": false, "a b": false, "a:b": false, "a/b": false,
	} {
		if got := ValidConsentVersion(v); got != want {
			t.Errorf("ValidConsentVersion(%q) = %v, muốn %v", v, got, want)
		}
	}
}

func TestDataConsentStreamKey(t *testing.T) {
	u := uuid.MustParse("0192f0e0-7b5a-7c3e-9d4f-1a2b3c4d5e6f")
	if got := DataConsentStreamKey(u); got != "dataconsent:"+u.String() {
		t.Fatalf("stream key: %s", got)
	}
}

func TestIsDataCollectionRoute_REQ011(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/v1/files", true},
		{"POST", "/v1/sync/push", true},
		{"POST", "/v1/cropcycle/seasons", true},
		{"POST", "/v1/cropcycle/seasons/:ref/harvest", true},
		{"POST", "/v1/cropcycle/farm-logs", true},
		{"POST", "/v1/cropcycle/farm-logs/batch", true},
		{"POST", "/v1/cropcycle/farm-logs/:id/corrections", true},
		{"POST", "/v1/plots", true},
		{"POST", "/v1/plots/:ref/boundaries", true},
		// Đọc không bị chặn; route khác không bị chặn; đường dẫn thật (không phải mẫu) không khớp.
		{"GET", "/v1/files/:ref", false},
		{"GET", "/v1/plots", false},
		{"POST", "/v1/me/data-consent", false},
		{"POST", "/v1/plots/123/boundaries", false},
		{"", "", false},
	} {
		if got := IsDataCollectionRoute(tc.method, tc.path); got != tc.want {
			t.Errorf("IsDataCollectionRoute(%s %s) = %v, muốn %v", tc.method, tc.path, got, tc.want)
		}
	}
}

// Danh sách route chặn phải khớp OpenAPI: đổi tên route mà quên cập nhật danh sách thì test đỏ.
func TestDataCollectionRoutesExistInOpenAPI(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/openapi/application.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pathLine := regexp.MustCompile(`^  (/\S+):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|post|put|patch|delete):`)
	ops := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if m := pathLine.FindStringSubmatch(line); m != nil {
			cur = m[1]
			continue
		}
		if cur != "" && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			cur = ""
		}
		if m := methodLine.FindStringSubmatch(line); m != nil && cur != "" {
			ops[cur] = append(ops[cur], strings.ToUpper(m[1]))
		}
	}
	colon := regexp.MustCompile(`:([A-Za-z_]+)`)
	if len(dataCollectionRoutes) == 0 {
		t.Fatal("danh sách route rỗng")
	}
	for route := range dataCollectionRoutes {
		method, path, _ := strings.Cut(route, " ")
		openapi := colon.ReplaceAllString(path, "{$1}")
		if !slices.Contains(ops[openapi], method) {
			t.Errorf("route %s %s không có trong contracts/openapi/application.yaml", method, openapi)
		}
	}
}
