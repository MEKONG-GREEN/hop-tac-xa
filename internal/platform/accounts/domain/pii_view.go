package domain

// FN-TK-02/BR-04 (AH-223): danh sách tài khoản chỉ hiện SĐT đã che; xem đầy đủ ở màn chi tiết được ghi vào sổ chỉ ghi
// thêm pf_accounts.pii_view (ai xem, xem tài khoản nào, trường nào, lúc nào — không chứa SĐT).

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// PIIViewRuleVersion ghi vào cột rule_version của sổ.
const PIIViewRuleVersion = "pf.accounts.pii_view/v1"

// PIIFieldPhone là trường SĐT (CHECK field IN ('phone')).
const PIIFieldPhone = "phone"

// PIIViewStreamKey là stream_key của sổ cho một tài khoản được xem.
func PIIViewStreamKey(user uuid.UUID) string { return "piiview:" + user.String() }

// PIIView là một dòng sổ truy cập.
type PIIView struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserRef    uuid.UUID
	Field      string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// MaskPhoneList che SĐT cho danh sách tài khoản theo mẫu của BRD (09•• ••• 678): giữ 2 chữ số đầu và 3 chữ số cuối.
// Chuỗi không phải SĐT 10 chữ số thì che hết, không lộ một phần dữ liệu lạ.
func MaskPhoneList(phone string) string {
	if phone == "" {
		return ""
	}
	if !ValidPhone(phone) {
		return strings.Repeat("•", 4)
	}
	return phone[:2] + "•• ••• " + phone[7:]
}
