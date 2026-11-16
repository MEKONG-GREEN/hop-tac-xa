package app

import (
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
)

// AH-23 / KH-24 / REQ-011: nội dung tạm "tam-1" nằm ở tham số masterdata (đổi không phải sửa mã).
const dataConsentTamText = "Ứng dụng HTX thu thập ảnh hiện trường có gắn tọa độ GPS và dữ liệu sản xuất (vụ, nhật ký canh tác, sản lượng) của các nông hộ do HTX của bạn quản lý, để lập hồ sơ sản xuất và phục vụ báo cáo Đề án 1 triệu ha lúa. Dữ liệu được lưu và xử lý theo Nghị định 13/2023/NĐ-CP. Bấm Đồng ý để tiếp tục sử dụng ứng dụng."

func TestDataConsentSeed_KH24_TemporaryText(t *testing.T) {
	seeds, err := (&Service{ParamRegistry: NewParamRegistry()}).parameterSeeds()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]port.ParamSeed{}
	for _, s := range seeds {
		got[s.Key] = s
	}
	for key, want := range map[string]string{
		port.ParamAccountsDataConsentVersion: "tam-1",
		port.DataConsentTextKey("tam-1"):     dataConsentTamText,
	} {
		s, ok := got[key]
		if !ok || s.Text == nil || *s.Text != want {
			t.Errorf("seed %s = %+v, muốn %q", key, s, want)
		}
		if s.EffectiveFrom != "2026-01-01" || s.SourceDoc == "" {
			t.Errorf("seed %s thiếu hiệu lực/văn bản gốc: %+v", key, s)
		}
		// Seed phải qua bộ kiểm đề xuất tham số (độ dài, kiểu).
		svc := &Service{ParamRegistry: NewParamRegistry()}
		at, err := domain.ParseInstant(s.EffectiveFrom)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.validateParameterProposal(domain.ParamVersion{Key: s.Key, Text: s.Text, Unit: s.Unit, EffectiveFrom: at, SourceDoc: s.SourceDoc}); err != nil {
			t.Errorf("seed %s không hợp lệ: %v", key, err)
		}
	}
}

func TestDataConsentSeed_KH24_SpecsAreText(t *testing.T) {
	svc := &Service{ParamRegistry: NewParamRegistry()}
	byKey := map[string]port.ParamSpec{}
	for _, sp := range svc.parameterSpecs() {
		byKey[sp.Key] = sp
	}
	ver, ok := byKey[port.ParamAccountsDataConsentVersion]
	if !ok || ver.Kind != domain.ParamKindText {
		t.Errorf("version: %+v", ver)
	}
	txt, ok := byKey[port.ParamAccountsDataConsentTextPrefix]
	if !ok || txt.Kind != domain.ParamKindText || !txt.Dimensional {
		t.Errorf("text: %+v", txt)
	}
	if got := port.DataConsentTextKey("tam-1"); got != "accounts.data_consent.text:tam-1" {
		t.Errorf("khóa nội dung: %s", got)
	}
	// Khóa có chiều là mã phiên bản phải qua bộ kiểm khóa.
	if err := domain.ValidateParamKey(port.DataConsentTextKey("tam-1")); err != nil {
		t.Errorf("khóa nội dung không hợp lệ: %v", err)
	}
}
