package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

func htxProfileBody(name, phone string) map[string]any {
	return map[string]any{"name": name, "commune_code": "30001", "head_office": "Trụ sở HTX, ấp 1", "representative_name": "", "phone": phone}
}

func TestAH283CreateAndReadHTXProfile(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	body := htxProfileBody("HTX Hồ Sơ Mới", "0907248320")
	key := key()
	r := root.call(f.admin.URL, http.MethodPost, "/v1/cooperative-profiles", key, "application/json", []byte(mustJSON(t, body)))
	if r.status != http.StatusCreated || !strings.HasPrefix(str(r.body, "code"), "HTX") {
		t.Fatalf("tạo không cần MST/neo/người đại diện: %d %v", r.status, r.body)
	}
	if _, exists := r.body["phone"]; exists {
		t.Fatal("phản hồi ghi được lưu trong PF không được chứa PII")
	}
	ref := uuid.MustParse(str(r.body, "ref"))
	path := "/v1/cooperatives/" + ref.String() + "/profile"
	profile := root.appDo(http.MethodGet, path, nil)
	if profile.status != http.StatusOK || profile.body["phone"] != "0907248320" || profile.body["representative_name"] != "" || profile.body["can_edit"] != true || profile.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("hồ sơ Admin: %d %v", profile.status, profile.body)
	}
	member := f.as(f.tenant, ref, port.RoleHTXMember)
	if r := member.appDo(http.MethodGet, path, nil); r.status != http.StatusOK || r.body["contact_masked"] != true || r.body["can_edit"] != false || r.body["phone"] == body["phone"] {
		t.Fatalf("thành viên chỉ đọc số đã che: %d %v", r.status, r.body)
	}
	rep := f.as(f.tenant, ref, port.RoleCoopManager)
	if r := rep.appDo(http.MethodGet, path, nil); r.status != http.StatusOK || r.body["phone"] != body["phone"] || r.body["can_edit"] != true {
		t.Fatalf("Đại diện đọc hồ sơ của mình: %d %v", r.status, r.body)
	}
	for _, c := range []caller{rep, member} {
		if r := c.adminDo(http.MethodPost, "/v1/cooperative-profiles", body); r.status != http.StatusForbidden {
			t.Fatalf("vai ngoài Admin không tạo HTX: %d %v", r.status, r.body)
		}
	}
	other := f.as(f.tenant, ids.New(), port.RoleCoopManager, port.RoleHTXMember)
	if r := other.appDo(http.MethodGet, path, nil); r.status != http.StatusNotFound {
		t.Fatalf("vai kiêm vẫn không xem HTX khác: %d %v", r.status, r.body)
	}
	retry := root.call(f.admin.URL, http.MethodPost, "/v1/cooperative-profiles", key, "application/json", []byte(mustJSON(t, body)))
	if str(retry.body, "ref") != ref.String() || retry.hdr.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("retry tạo: %d %v", retry.status, retry.body)
	}
	verifyChain(t, f, ref, 1)
	if strings.Contains(f.pfText(t), "0907248320") {
		t.Fatal("SĐT bị ghi vào sổ/hồ sơ PF")
	}
	duplicate := htxProfileBody("htx  HỒ SƠ MỚI", "0907248321")
	if r := root.adminDo(http.MethodPost, "/v1/cooperative-profiles", duplicate); r.status != http.StatusConflict || r.body["code"] != "party.cooperative_name_taken" {
		t.Fatalf("tạo trùng tên: %d %v", r.status, r.body)
	}
}

func TestAH283HTXProfileValidationAndRecovery(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	for _, phone := range []string{"", "1234567890", "+84907248322", "09072", "09072483xx"} {
		body := htxProfileBody("HTX Kiểm Đầu Vào", phone)
		r := root.adminDo(http.MethodPost, "/v1/cooperative-profiles", body)
		if r.status != http.StatusUnprocessableEntity {
			t.Fatalf("SĐT sai được chấp nhận: %d %v", r.status, r.body)
		}
	}
	body := htxProfileBody("HTX Kiểm Đầu Vào", "0907248322")
	body["code"] = "HTX-TU-NHAP"
	if r := root.adminDo(http.MethodPost, "/v1/cooperative-profiles", body); r.status != http.StatusBadRequest {
		t.Fatalf("không nhận mã HTX client: %d %v", r.status, r.body)
	}
	delete(body, "code")
	f.profiles.failNext(1, nil)
	key := key()
	r := root.call(f.admin.URL, http.MethodPost, "/v1/cooperative-profiles", key, "application/json", []byte(mustJSON(t, body)))
	if r.status != http.StatusServiceUnavailable {
		t.Fatalf("nguồn hồ sơ lỗi phải cho retry: %d %v", r.status, r.body)
	}
	r = root.call(f.admin.URL, http.MethodPost, "/v1/cooperative-profiles", key, "application/json", []byte(mustJSON(t, body)))
	if r.status != http.StatusCreated {
		t.Fatalf("hoàn tất hồ sơ hai bước: %d %v", r.status, r.body)
	}
	verifyChain(t, f, uuid.MustParse(str(r.body, "ref")), 1)
}
