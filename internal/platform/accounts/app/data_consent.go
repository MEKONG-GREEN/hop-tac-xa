package app

// Đồng ý thu thập ảnh gắn GPS và dữ liệu sản xuất (AH-23, REQ-011, KH-24): GET/POST /v1/me/data-consent và bước chặn
// tập trung ở middleware cho các route nhận ảnh/GPS (handler/data_consent.go).
//
// Nội dung và mã phiên bản hiện hành là tham số có phiên bản của pf/masterdata; thiếu hoặc sai → 503 (fail-closed,
// vẫn chặn). Tra tham số xảy ra TRƯỚC khi mở giao dịch (không gọi port module khác trong transaction).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/params"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Mã lỗi đồng ý thu thập dữ liệu (docs/errors.md).
var (
	ErrDataConsentRequired = problem.New(http.StatusForbidden, "accounts.data_consent_required",
		"Tài khoản chưa đồng ý thu thập ảnh, tọa độ GPS và dữ liệu sản xuất. Mở lại màn Đồng ý thu thập dữ liệu để tiếp tục.")
	ErrDataConsentStale = problem.New(http.StatusConflict, "accounts.data_consent_version_stale",
		"Nội dung đã được cập nhật. Vui lòng đọc lại và xác nhận.")
	ErrDataConsentUnconfigured = problem.New(http.StatusServiceUnavailable, "accounts.data_consent_unconfigured",
		"Chưa cấu hình nội dung Đồng ý thu thập dữ liệu")
)

// DataConsentView là trạng thái đồng ý của người gọi. Version, Text có khi Required; ConsentedAt có khi đã đồng ý
// phiên bản hiện hành. Tài khoản không thuộc diện phải đồng ý (Admin) nhận {Required:false} và không có gì khác.
type DataConsentView struct {
	Required    bool
	Version     *string
	Text        *string
	ConsentedAt *time.Time
}

// dataConsentTerms đọc mã phiên bản và nội dung hiện hành tại thời điểm at. Thiếu tham số, mã sai dạng hoặc nội dung
// rỗng → ErrDataConsentUnconfigured.
func (s *Service) dataConsentTerms(ctx context.Context, at time.Time) (version, text string, err error) {
	if s.Params == nil {
		return "", "", ErrDataConsentUnconfigured
	}
	v, err := s.Params.At(ctx, mdport.ParamAccountsDataConsentVersion, at)
	if err != nil {
		return "", "", s.dataConsentParamErr(err)
	}
	version = strings.TrimSpace(v.Text)
	if !domain.ValidConsentVersion(version) {
		return "", "", ErrDataConsentUnconfigured
	}
	t, err := s.Params.At(ctx, mdport.ParamAccountsDataConsentTextPrefix+":"+version, at)
	if err != nil {
		return "", "", s.dataConsentParamErr(err)
	}
	if strings.TrimSpace(t.Text) == "" {
		return "", "", ErrDataConsentUnconfigured
	}
	return version, t.Text, nil
}

func (s *Service) dataConsentParamErr(err error) error {
	if errors.Is(err, params.ErrParamNotFound) {
		return ErrDataConsentUnconfigured
	}
	return ErrDataConsentUnconfigured.WithCause(err)
}

// DataConsentStatus trả trạng thái đồng ý của người gọi với phiên bản hiện hành.
func (s *Service) DataConsentStatus(ctx context.Context) (DataConsentView, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return DataConsentView{}, problem.Unauthenticated
	}
	if !domain.RequiresDataConsent(p.Roles) {
		return DataConsentView{}, nil
	}
	version, text, err := s.dataConsentTerms(ctx, s.now())
	if err != nil {
		return DataConsentView{}, err
	}
	var prev *domain.DataConsent
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.DataConsentFor(ctx, tx, p.ActorRef, version)
		return e
	}); err != nil {
		return DataConsentView{}, err
	}
	if prev != nil {
		at := prev.EventTime.UTC()
		return DataConsentView{Version: &version, ConsentedAt: &at}, nil
	}
	return DataConsentView{Required: true, Version: &version, Text: &text}, nil
}

// RecordDataConsent ghi bằng chứng người gọi đồng ý phiên bản version (phải là bản hiện hành). Thời điểm là giờ máy
// chủ. Đã đồng ý phiên bản này rồi (hoặc phát lại cùng khóa) → trả bản đã ghi, created=false, không ghi dòng mới.
func (s *Service) RecordDataConsent(ctx context.Context, version, idemKey string) (c domain.DataConsent, created bool, err error) {
	p, err := authz.Require(ctx, "coop_manager")
	if err != nil {
		return c, false, err
	}
	if idemKey == "" {
		return c, false, problem.IdempotencyRequired
	}
	if !domain.ValidConsentVersion(version) {
		return c, false, problem.Validation.WithField("version", "Mã phiên bản nội dung không hợp lệ")
	}
	current, _, err := s.dataConsentTerms(ctx, s.now())
	if err != nil {
		return c, false, err
	}
	if version != current {
		return c, false, ErrDataConsentStale
	}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		created = false
		if err := store.LockDataConsent(ctx, tx, p.ActorRef); err != nil {
			return err
		}
		if prev, err := store.DataConsentByIdemKey(ctx, tx, idemKey); err != nil {
			return err
		} else if prev != nil {
			if prev.UserRef != p.ActorRef {
				return problem.IdempotencyConflict
			}
			c = *prev
			return nil
		}
		if prev, err := store.DataConsentFor(ctx, tx, p.ActorRef, version); err != nil {
			return err
		} else if prev != nil {
			c = *prev
			return nil
		}
		c = domain.DataConsent{ID: ids.New(), TenantID: p.TenantID, UserRef: p.ActorRef, TextVersion: version,
			EventTime: s.now(), IdemKey: idemKey}
		if err := store.InsertDataConsent(ctx, tx, &c); err != nil {
			return ledger.Classify(err)
		}
		created = true
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		// Request song song cùng khóa đã ghi trước: đọc lại bản đã ghi.
		err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			prev, e := store.DataConsentByIdemKey(ctx, tx, idemKey)
			if e == nil && prev != nil && prev.UserRef == p.ActorRef {
				c, created = *prev, false
				return nil
			}
			if e == nil {
				e = problem.IdempotencyConflict
			}
			return e
		})
	}
	if err != nil {
		return domain.DataConsent{}, false, err
	}
	return c, created, nil
}

// RequireDataConsent là bước chặn của middleware cho route nhận ảnh/GPS: tài khoản thuộc diện phải đồng ý mà chưa đồng ý
// phiên bản hiện hành → ErrDataConsentRequired; thiếu cấu hình → ErrDataConsentUnconfigured (503, vẫn chặn).
// Không có principal (route công khai) hoặc không thuộc diện → nil.
func (s *Service) RequireDataConsent(ctx context.Context) error {
	p, ok := authz.FromContext(ctx)
	if !ok || !domain.RequiresDataConsent(p.Roles) {
		return nil
	}
	version, _, err := s.dataConsentTerms(ctx, s.now())
	if err != nil {
		return err
	}
	var prev *domain.DataConsent
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.DataConsentFor(ctx, tx, p.ActorRef, version)
		return e
	}); err != nil {
		return err
	}
	if prev == nil {
		return ErrDataConsentRequired
	}
	return nil
}
