package domain

// Đồng ý thu thập ảnh gắn GPS và dữ liệu sản xuất (AH-23, REQ-011, KH-24; NĐ 13/2023/NĐ-CP).
// Bằng chứng là sổ chỉ ghi thêm pf_accounts.data_consent; nội dung và mã phiên bản hiện hành là tham số có phiên bản
// của pf/masterdata (xem masterdata/port: ParamAccountsDataConsent*).

import (
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
)

// DataConsentRuleVersion ghi vào cột rule_version của sổ.
const DataConsentRuleVersion = "pf.accounts.data_consent/v1"

// Vai tài khoản của HTX: Đại diện HTX (coop_manager) và Thành viên HTX (htx_member, AH-39).
const (
	roleCoopManager = "coop_manager"
	roleHTXMember   = "htx_member"
)

var consentVersionRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)

// RequiresDataConsent cho biết tài khoản mang các vai này phải đồng ý thu thập dữ liệu: Đại diện HTX hoặc Thành viên
// HTX. Tài khoản có cả vai Admin lẫn vai HTX vẫn phải đồng ý; tài khoản chỉ có vai Admin thì không.
func RequiresDataConsent(roles []string) bool {
	return slices.Contains(roles, roleCoopManager) || slices.Contains(roles, roleHTXMember)
}

// ValidConsentVersion kiểm dạng mã phiên bản nội dung (cùng quy tắc CHECK của bảng).
func ValidConsentVersion(v string) bool { return consentVersionRe.MatchString(v) }

// DataConsentStreamKey là stream_key của sổ cho một tài khoản.
func DataConsentStreamKey(user uuid.UUID) string { return "dataconsent:" + user.String() }

// DataConsent là một dòng sổ: tài khoản đã đồng ý với một phiên bản nội dung tại một thời điểm (giờ máy chủ).
type DataConsent struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	UserRef     uuid.UUID
	TextVersion string
	EventTime   time.Time
	IdemKey     string
	Seq         int64
	RecordedAt  time.Time
}

// dataCollectionRoutes là các route Application API nhận ảnh hoặc tọa độ GPS ("METHOD đường-dẫn-gin").
// Story thêm API nhận ảnh/GPS phải thêm route vào đây. Chặn cả route, không soi thân request.
var dataCollectionRoutes = map[string]bool{
	// pf/sync: ảnh mã hóa có GPS; bản ghi đồng bộ có GPS hoặc ảnh.
	"POST /v1/files":     true,
	"POST /v1/sync/push": true,
	// pf/cooperative season: DTO có gps.
	"POST /v1/cropcycle/seasons":                                                                true,
	"POST /v1/cooperatives/:ref/households/:household_ref/seasons":                              true,
	"POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/task-completions": true,
	"POST /v1/cropcycle/seasons/:ref/harvest":                                                   true,
	"POST /v1/cropcycle/farm-logs":                                                              true,
	"POST /v1/cropcycle/farm-logs/batch":                                                        true,
	"POST /v1/cropcycle/farm-logs/:id/corrections":                                              true,
	// pf/cooperative plot: gps_accuracy_m.
	"POST /v1/plots":                 true,
	"POST /v1/plots/:ref/boundaries": true,
}

// IsDataCollectionRoute cho biết route (method + fullPath của gin) nhận ảnh hoặc GPS.
func IsDataCollectionRoute(method, fullPath string) bool {
	return dataCollectionRoutes[method+" "+fullPath]
}
