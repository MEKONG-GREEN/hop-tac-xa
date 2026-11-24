package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// AH-293 (NFR#5, LEGAL-01): CCCD, SĐT mã hóa khi lưu (AC-N1); tìm hộ theo đủ số CCCD, một phần số không khớp (AC-N2);
// Thành viên HTX nhận SĐT, CCCD đã che (AC-N3); Đại diện HTX xem hộ của HTX mình đầy đủ.
func TestAH293_PIIEncryptedSearchAndMasking(t *testing.T) {
	f := nativeVaultFixture(t)
	tr := f.seedTree(f.root())
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	rep := f.as(f.tenant, tr.c1, htxroles.Representative)
	member := f.as(f.tenant, tr.c1, htxroles.Member)
	const phone, nid = "0913293222", "079293456123"
	created := nativePost(admin, "/v1/household-profiles", "ah293-create", nativeBody(tr.c1, phone, nid, "2"))
	if created.status != 201 {
		t.Fatalf("tạo hộ: %d %v", created.status, created.body)
	}
	href := str(created.body, "household_ref")
	list := "/v1/cooperatives/" + tr.c1.String() + "/households"

	// AC-N2: đủ 12 số CCCD → đúng hộ; 6 số đầu → không khớp theo CCCD; số khác → rỗng.
	r := rep.appDo(http.MethodGet, list+"?national_id="+nid, nil)
	if r.status != 200 || len(items(r)) != 1 || items(r)[0]["household_ref"] != href {
		t.Fatalf("tìm theo đủ CCCD: %d %v", r.status, r.body)
	}
	if r = rep.appDo(http.MethodGet, list+"?national_id="+nid[:6], nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("tìm theo 6 số đầu phải bị từ chối: %d %v", r.status, r.body)
	}
	if r = rep.appDo(http.MethodGet, list+"?national_id=079000000000", nil); r.status != 200 || len(items(r)) != 0 {
		t.Fatalf("CCCD không có: %d %v", r.status, r.body)
	}
	if r = rep.appDo(http.MethodGet, list+"?national_id="+nid+"&phone="+phone, nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("không tìm cùng lúc SĐT và CCCD: %d", r.status)
	}

	// AC-N3: Thành viên HTX — danh sách và hồ sơ hộ chỉ có SĐT, CCCD đã che.
	r = member.appDo(http.MethodGet, list, nil)
	if r.status != 200 || len(items(r)) != 1 || items(r)[0]["masked_phone"] != "0913***222" {
		t.Fatalf("Thành viên xem danh sách: %d %v", r.status, r.body)
	}
	profile := "/v1/households/" + href + "/profile"
	if p := member.appDo(http.MethodGet, profile, nil); p.status != 200 || str(p.body, "identity", "phone") != "0913***222" || str(p.body, "identity", "national_id") != "079******123" {
		t.Fatalf("Thành viên xem hồ sơ: %d %v", p.status, p.body)
	}
	// Đại diện HTX xem hộ của HTX mình thấy đầy đủ.
	if p := rep.appDo(http.MethodGet, profile, nil); p.status != 200 || str(p.body, "identity", "phone") != phone || str(p.body, "identity", "national_id") != nid {
		t.Fatalf("Đại diện xem hồ sơ HTX mình: %d %v", p.status, p.body)
	}

	// AC-N1: trong CSDL không có dạng rõ của SĐT, CCCD.
	vault := f.env.AdminPool(t, migrations.Vault)
	var plain int
	if err := vault.QueryRow(context.Background(), `SELECT count(*) FROM vault.identity
	 WHERE position(convert_to($1,'UTF8') IN coalesce(phone_ct,'')) > 0 OR position(convert_to($2,'UTF8') IN coalesce(national_id_ct,'')) > 0
	    OR position(convert_to('84'||substr($1,2),'UTF8') IN coalesce(phone_ct,'')) > 0`, phone, nid).Scan(&plain); err != nil {
		t.Fatal(err)
	}
	if plain != 0 {
		t.Fatalf("SĐT/CCCD ở dạng rõ trong kho định danh: %d", plain)
	}
	pf := f.env.AdminPool(t, migrations.PF)
	var leaks int
	if err := pf.QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.household h WHERE row_to_json(h)::text LIKE '%'||$1||'%' OR row_to_json(h)::text LIKE '%'||$2||'%'`, nid, phone).Scan(&leaks); err != nil {
		t.Fatal(err)
	}
	if leaks != 0 {
		t.Fatalf("SĐT/CCCD lưu ở pf_coop.household: %d", leaks)
	}
}
