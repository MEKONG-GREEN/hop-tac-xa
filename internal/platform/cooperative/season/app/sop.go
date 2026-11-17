package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Vai soạn và phát hành SOP (Admin API): phát hành là một bước của vai soạn (AH-8, thay luật R-56 cũ của SOP).
var (
	sopAuthorRoles  = []string{port.RoleSOPAdmin, authz.RolePlatformAdmin}
	sopPublishRoles = sopAuthorRoles
	sopAdminRead    = []string{port.RoleSOPAdmin, authz.RoleParamApprover, authz.RolePlatformAdmin}
)

// MaxAnchorNameLen là độ dài tối đa của tên mốc gốc.
const MaxAnchorNameLen = 100

// SOPView là mẫu SOP kèm các phiên bản.
type SOPView struct {
	Template store.Template
	Versions []store.Version
	Names    map[uuid.UUID]string // tên người phát hành (chỉ Admin API; thiếu → hiện "—")
}

// CreateSOPInput tạo mẫu SOP mới với bản nháp phiên bản 1 (HTX US-CAT-01). Code để trống thì hệ thống sinh mã.
type CreateSOPInput struct {
	Code           string
	Name           string
	Crop           string
	VarietyCode    string
	AnchorName     string
	Scope          string
	CooperativeRef uuid.UUID
	Standard       string
	Description    string
	Stages         []domain.Stage
	Steps          []domain.Step
	ChangeNote     string
}

// autoSOPCode sinh mã SOP từ ref của mẫu.
func autoSOPCode(ref uuid.UUID) string { return "SOP-" + strings.ToUpper(ref.String()[28:36]) }

// checkCatalogScope kiểm loại cây (là mục không có parent_code) và giống (thuộc loại cây đó) trong danh mục.
// Gọi danh mục là I/O nên phải chạy ngoài transaction (AGENTS.md §9 #1).
func (s *Service) checkCatalogScope(ctx context.Context, crop, variety string) error {
	it, err := s.catalogItem(ctx, mdport.CatalogCrop, "crop", crop)
	if err != nil {
		return err
	}
	if it != nil && it.Attrs[mdport.AttrParentCode] != "" {
		return ErrCatalogInvalid.WithField("crop", "chọn loại cây, không chọn giống hoặc nhóm con")
	}
	if variety == "" {
		return nil
	}
	v, err := s.catalogItem(ctx, mdport.CatalogRiceVariety, "variety_code", variety)
	if err != nil {
		return err
	}
	if v != nil && v.Attrs[mdport.AttrCropCode] != crop {
		return problem.Validation.WithField("variety_code", "Giống không thuộc loại cây đã chọn")
	}
	return nil
}

// cleanTemplateInfo chuẩn hóa và kiểm dạng các trường thông tin chung (chưa tra danh mục).
// Gom mọi lỗi theo trường vào một problem.Validation.
func cleanTemplateInfo(name, crop, variety, anchor *string) error {
	*name, *crop, *variety = strings.TrimSpace(*name), strings.TrimSpace(*crop), strings.TrimSpace(*variety)
	p := problem.Validation
	a, err := domain.CleanText("anchor_name", *anchor, MaxAnchorNameLen)
	if err != nil {
		if fe, ok := domain.AsFieldError(err); ok {
			p = p.WithField(fe.Field, fe.Reason)
		}
	} else {
		*anchor = a
	}
	if *name == "" || len([]rune(*name)) > 200 {
		p = p.WithField("name", "bắt buộc, tối đa 200 ký tự")
	}
	if *crop == "" || !domain.ValidCatalogCode(*crop) {
		p = p.WithField("crop", "bắt buộc, chọn loại cây trong danh mục")
	}
	if *variety != "" && !domain.ValidCatalogCode(*variety) {
		p = p.WithField("variety_code", "mã giống không hợp lệ")
	}
	if err == nil && *anchor == "" {
		p = p.WithField("anchor_name", "bắt buộc, vd Ngày xuống giống")
	}
	if len(p.Errors) == 0 {
		return nil
	}
	return p
}

func (s *Service) checkTemplateFields(ctx context.Context, in *CreateSOPInput) error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Standard = strings.TrimSpace(in.Standard)
	if err := cleanTemplateInfo(&in.Name, &in.Crop, &in.VarietyCode, &in.AnchorName); err != nil {
		return err
	}
	if in.Scope == "" {
		in.Scope = domain.ScopeSystem
	}
	switch {
	case !domain.ValidSOPCode(in.Code):
		return problem.Validation.WithField("code", "2–40 ký tự A-Z, 0-9, _ . -")
	case in.Standard != "" && !domain.ValidCode(in.Standard):
		return problem.Validation.WithField("standard", "dạng [a-z][a-z0-9_] (vd vietgap, srp)")
	case in.Scope != domain.ScopeSystem && in.Scope != domain.ScopeHTX:
		return problem.Validation.WithField("scope", "phải là system hoặc htx")
	case in.Scope == domain.ScopeHTX && in.CooperativeRef == uuid.Nil:
		return problem.Validation.WithField("cooperative_ref", "quy trình riêng của HTX phải gắn với một HTX")
	case in.Scope == domain.ScopeSystem && in.CooperativeRef != uuid.Nil:
		return problem.Validation.WithField("cooperative_ref", "quy trình hệ thống không gắn HTX")
	}
	desc, err := domain.CleanText("description", in.Description, 2000)
	if err != nil {
		return fieldProblem(err)
	}
	in.Description = desc
	if err := s.checkCatalogScope(ctx, in.Crop, in.VarietyCode); err != nil {
		return err
	}
	if in.Scope == domain.ScopeHTX {
		return s.checkCoop(ctx, in.CooperativeRef)
	}
	return nil
}

// resolveMaterials tra vật tư chọn từ danh mục: điền tên theo danh mục, đơn vị trống thì lấy đơn vị của danh mục.
// Vật tư nhập tên tự do (không có material_code) giữ nguyên. Chạy ngoài transaction (§9 #1); trả bản sao.
func (s *Service) resolveMaterials(ctx context.Context, steps []domain.Step) ([]domain.Step, error) {
	out := slices.Clone(steps)
	for j := range out {
		if len(out[j].Materials) == 0 {
			continue
		}
		ms := slices.Clone(out[j].Materials)
		for k := range ms {
			code := strings.TrimSpace(ms[k].MaterialCode)
			if code == "" {
				continue
			}
			field := "steps[" + itoa(j) + "].materials[" + itoa(k) + "].material_code"
			it, err := s.catalogItem(ctx, mdport.CatalogMaterial, field, code)
			if err != nil {
				return nil, err
			}
			if it == nil {
				continue
			}
			ms[k].MaterialCode, ms[k].Name = code, it.Name
			if strings.TrimSpace(ms[k].Unit) == "" {
				ms[k].Unit = it.Attrs["unit"]
			}
		}
		out[j].Materials = ms
	}
	return out, nil
}

// prepareContent tra vật tư rồi chuẩn hóa và kiểm nội dung SOP (ngoài transaction).
func (s *Service) prepareContent(ctx context.Context, stages []domain.Stage, steps []domain.Step) (domain.Content, error) {
	steps, err := s.resolveMaterials(ctx, steps)
	if err != nil {
		return domain.Content{}, err
	}
	c, err := domain.NormalizeContent(domain.Content{Stages: stages, Steps: steps})
	if err != nil {
		return domain.Content{}, fieldProblem(err)
	}
	return c, nil
}

// CreateSOP tạo mẫu SOP (nháp) — sop_admin.
func (s *Service) CreateSOP(ctx context.Context, in CreateSOPInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	ref := ids.New()
	if strings.TrimSpace(in.Code) == "" {
		in.Code = autoSOPCode(ref)
	}
	if err := s.checkTemplateFields(ctx, &in); err != nil {
		return SOPView{}, err
	}
	content, err := s.prepareContent(ctx, in.Stages, in.Steps)
	if err != nil {
		return SOPView{}, err
	}
	note, err := domain.CleanText("change_note", in.ChangeNote, 1000)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	now := s.now()
	t := store.Template{
		Ref: ref, Code: in.Code, Name: in.Name, NameKey: domain.NameKey(in.Name), Crop: in.Crop, VarietyCode: in.VarietyCode,
		AnchorName: in.AnchorName, Scope: in.Scope,
		CooperativeRef: in.CooperativeRef, Standard: in.Standard, Description: in.Description, Status: domain.TemplateDraft,
		LatestVersion: 1, CreatedBy: p.ActorRef,
	}
	v := store.Version{ID: ids.New(), TemplateRef: t.Ref, VersionNo: 1, Status: domain.VersionDraft, Steps: content.Steps,
		Stages: content.Stages, ChangeNote: note, AuthoredBy: p.ActorRef, AuthoredAt: now}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := insertTemplate(ctx, tx, p.TenantID, t); err != nil {
			return err
		}
		if err := store.InsertVersion(ctx, tx, p.TenantID, v); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

func insertTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t store.Template) error {
	codeTaken, nameTaken, err := store.TemplateTaken(ctx, tx, t.Code, t.NameKey)
	if err != nil {
		return err
	}
	if nameTaken {
		return ErrSOPNameTaken
	}
	if codeTaken {
		return ErrSOPCodeTaken
	}
	err = store.InsertTemplate(ctx, tx, tid, t)
	switch {
	case isUnique(err, "sop_template_name_live"):
		return ErrSOPNameTaken
	case isUnique(err, "sop_template_code_live"):
		return ErrSOPCodeTaken
	}
	return err
}

func (s *Service) loadView(ctx context.Context, tx pgx.Tx, ref uuid.UUID, out *SOPView) error {
	t, err := store.TemplateByRef(ctx, tx, ref, false)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrSOPNotFound
	}
	vs, err := store.VersionsOf(ctx, tx, ref)
	if err != nil {
		return err
	}
	*out = SOPView{Template: *t, Versions: vs}
	return nil
}

// NewVersionInput tạo bản nháp phiên bản mới. CopySteps → chép giai đoạn và việc của phiên bản mới nhất.
type NewVersionInput struct {
	TemplateRef uuid.UUID
	Stages      []domain.Stage
	Steps       []domain.Step
	CopySteps   bool
	ChangeNote  string
}

// NewVersion tạo bản nháp phiên bản mới của mẫu (phiên bản đã phát hành bất biến — BR-07).
func (s *Service) NewVersion(ctx context.Context, in NewVersionInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	var content domain.Content
	if !in.CopySteps {
		if content, err = s.prepareContent(ctx, in.Stages, in.Steps); err != nil {
			return SOPView{}, err
		}
	}
	note, err := domain.CleanText("change_note", in.ChangeNote, 1000)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, in.TemplateRef, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Status == domain.TemplateRetired {
			return ErrSOPState.WithDetail("mẫu đã ngừng dùng")
		}
		vs, err := store.VersionsOf(ctx, tx, t.Ref)
		if err != nil {
			return err
		}
		latest := vs[len(vs)-1]
		if latest.Status == domain.VersionDraft {
			return ErrSOPState.WithDetail("mẫu đang có bản nháp phiên bản " + itoa(latest.VersionNo) + " — sửa bản nháp đó")
		}
		if in.CopySteps {
			content = domain.Content{Stages: latest.Stages, Steps: latest.Steps}
		}
		v := store.Version{ID: ids.New(), TemplateRef: t.Ref, VersionNo: latest.VersionNo + 1, Status: domain.VersionDraft,
			Steps: content.Steps, Stages: content.Stages, SourceVersionRef: latest.ID, ChangeNote: note, AuthoredBy: p.ActorRef, AuthoredAt: s.now()}
		if err := store.InsertVersion(ctx, tx, p.TenantID, v); err != nil {
			return err
		}
		t.LatestVersion = v.VersionNo
		if err := store.SetTemplateState(ctx, tx, *t); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

// UpdateDraft thay giai đoạn và việc của bản nháp (HTX US-CAT-01 AC-1, AC-6).
func (s *Service) UpdateDraft(ctx context.Context, versionID uuid.UUID, in domain.Content, changeNote string) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	content, err := s.prepareContent(ctx, in.Stages, in.Steps)
	if err != nil {
		return SOPView{}, err
	}
	note, err := domain.CleanText("change_note", changeNote, 1000)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := store.VersionByID(ctx, tx, versionID, true)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrSOPNotFound
		}
		// Mẫu đã đánh dấu xóa thì bản nháp của nó coi như không còn.
		if t, err := store.TemplateByRef(ctx, tx, v.TemplateRef, false); err != nil {
			return err
		} else if t == nil {
			return ErrSOPNotFound
		}
		if v.Status != domain.VersionDraft {
			return ErrSOPState.WithDetail("phiên bản đã phát hành không sửa được — tạo phiên bản mới")
		}
		v.Steps, v.Stages, v.ChangeNote = content.Steps, content.Stages, note
		if err := store.UpdateDraft(ctx, tx, *v); err != nil {
			return err
		}
		return s.loadView(ctx, tx, v.TemplateRef, &view)
	})
	return view, err
}

// checkLiveCatalog kiểm loại cây, giống và vật tư của nội dung vẫn còn hiện trong danh mục (kiểm lại khi phát hành).
func (s *Service) checkLiveCatalog(ctx context.Context, t store.Template, steps []domain.Step) error {
	if err := s.checkCatalogScope(ctx, t.Crop, t.VarietyCode); err != nil {
		return err
	}
	_, err := s.resolveMaterials(ctx, steps)
	return err
}

// PublishSOP phát hành bản nháp một bước (vai soạn, không cần người thứ hai): nội dung phải hợp lệ, có việc
// Thu hoạch cuối với cửa sổ ngày (EX-05, Q-06). Phát pf.cropcycle.sop.published trong cùng giao dịch.
// Tra danh mục (I/O) nằm ngoài transaction: đọc → kiểm danh mục → khóa và ghi, UpdatedAt đổi giữa chừng thì từ chối.
func (s *Service) PublishSOP(ctx context.Context, versionID uuid.UUID) (SOPView, error) {
	p, err := authz.Require(ctx, sopPublishRoles...)
	if err != nil {
		return SOPView{}, err
	}
	var seen *store.Version
	var tpl *store.Template
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := store.VersionByID(ctx, tx, versionID, false)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrSOPNotFound
		}
		if tpl, err = store.TemplateByRef(ctx, tx, v.TemplateRef, false); err != nil {
			return err
		}
		seen = v
		return nil
	})
	if err != nil {
		return SOPView{}, err
	}
	if tpl == nil {
		return SOPView{}, ErrSOPNotFound
	}
	if err := s.checkLiveCatalog(ctx, *tpl, seen.Steps); err != nil {
		return SOPView{}, err
	}
	now := s.now()
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := store.VersionByID(ctx, tx, versionID, true)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrSOPNotFound
		}
		t, err := store.TemplateByRef(ctx, tx, v.TemplateRef, true)
		if err != nil {
			return err
		}
		if t == nil { // mẫu bị đánh dấu xóa giữa hai transaction
			return ErrSOPNotFound
		}
		if v.Status != domain.VersionDraft || t.Status == domain.TemplateRetired {
			return ErrSOPState.WithDetail("chỉ phát hành được bản nháp của mẫu chưa ngừng dùng")
		}
		if !v.UpdatedAt.Equal(seen.UpdatedAt) || !t.UpdatedAt.Equal(tpl.UpdatedAt) {
			return ErrConcurrentEdit
		}
		content, err := domain.NormalizeContent(domain.Content{Stages: v.Stages, Steps: v.Steps})
		if err != nil {
			return fieldProblem(err)
		}
		if err := domain.CheckPublishable(content); err != nil {
			var pb domain.PublishBlock
			if errors.As(err, &pb) {
				return publishProblem(pb)
			}
			return err
		}
		h, _ := domain.HarvestStep(content.Steps)
		if err := store.PublishVersion(ctx, tx, v.ID, p.ActorRef, now); err != nil {
			return err
		}
		no := v.VersionNo
		t.Status, t.PublishedVersion = domain.TemplatePublished, &no
		if err := store.SetTemplateState(ctx, tx, *t); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, p.TenantID, port.EventSOPPublished, now, port.SOPPublishedV1{
			TemplateRef: t.Ref, Code: t.Code, Name: t.Name, Scope: t.Scope, CooperativeRef: uuidPtr(t.CooperativeRef),
			VersionID: v.ID, VersionNo: v.VersionNo, StepCount: len(v.Steps), HarvestDayOffset: h.DayOffset,
			HarvestWindowDays: 0, AuthoredBy: v.AuthoredBy, PublishedBy: p.ActorRef, PublishedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

// CloneInput nhân bản mẫu SOP (HTX US-CAT-03).
// Crop, VarietyCode, AnchorName nil → lấy của SOP nguồn; VarietyCode rỗng → SOP chung của loại cây (AH-21 AC1).
type CloneInput struct {
	SourceRef      uuid.UUID
	Code           string
	Name           string
	Crop           *string
	VarietyCode    *string
	AnchorName     *string
	Scope          string
	CooperativeRef uuid.UUID
}

// CloneSOP tạo mẫu mới giữ nguyên toàn bộ bước và thứ tự của phiên bản nguồn
// (bản đã phát hành mới nhất, không có thì bản mới nhất); bản sao là nháp, tên
// phải khác mọi mẫu (AC-3); sửa bản sao không đổi nguồn (AC-2 — dòng khác).
func (s *Service) CloneSOP(ctx context.Context, in CloneInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	var src *store.Template
	var ver *store.Version
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var err error
		if src, err = store.TemplateByRef(ctx, tx, in.SourceRef, false); err != nil || src == nil {
			return err
		}
		if ver, err = store.LatestPublished(ctx, tx, src.Ref); err != nil || ver != nil {
			return err
		}
		vs, err := store.VersionsOf(ctx, tx, src.Ref)
		if err != nil {
			return err
		}
		ver = &vs[len(vs)-1]
		return nil
	})
	if err != nil {
		return SOPView{}, err
	}
	if src == nil {
		return SOPView{}, ErrSOPNotFound
	}
	ref := ids.New()
	ci := CreateSOPInput{Code: in.Code, Name: in.Name, Crop: orDefault(in.Crop, src.Crop), VarietyCode: orDefault(in.VarietyCode, src.VarietyCode),
		AnchorName: orDefault(in.AnchorName, src.AnchorName),
		Scope:      in.Scope, CooperativeRef: in.CooperativeRef, Standard: src.Standard, Description: src.Description}
	if ci.Scope == "" {
		ci.Scope, ci.CooperativeRef = src.Scope, src.CooperativeRef
	}
	if strings.TrimSpace(ci.Code) == "" {
		ci.Code = autoSOPCode(ref)
	}
	// Tra danh mục và HTX (I/O) ngoài transaction (§9 #1).
	if err := s.checkTemplateFields(ctx, &ci); err != nil {
		return SOPView{}, err
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t := store.Template{Ref: ref, Code: ci.Code, Name: ci.Name, NameKey: domain.NameKey(ci.Name), Crop: ci.Crop, VarietyCode: ci.VarietyCode,
			AnchorName: ci.AnchorName, Scope: ci.Scope, CooperativeRef: ci.CooperativeRef, Standard: ci.Standard, Description: ci.Description,
			Status: domain.TemplateDraft, SourceTemplateRef: src.Ref, LatestVersion: 1, CreatedBy: p.ActorRef}
		if err := insertTemplate(ctx, tx, p.TenantID, t); err != nil {
			return err
		}
		v := store.Version{ID: ids.New(), TemplateRef: t.Ref, VersionNo: 1, Status: domain.VersionDraft, Steps: ver.Steps, Stages: ver.Stages,
			SourceVersionRef: ver.ID, ChangeNote: "Nhân bản từ " + src.Code + " v" + itoa(ver.VersionNo), AuthoredBy: p.ActorRef, AuthoredAt: s.now()}
		if err := store.InsertVersion(ctx, tx, p.TenantID, v); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

func orDefault(v *string, def string) string {
	if v == nil {
		return def
	}
	return *v
}

// SOPInfoInput sửa thông tin chung của mẫu SOP còn nháp.
type SOPInfoInput struct {
	Name        string
	Crop        string
	VarietyCode string
	AnchorName  string
}

// UpdateSOPInfo sửa thông tin chung (tên, loại cây, giống, tên mốc gốc) của mẫu chưa phát hành lần nào.
// Mẫu đã phát hành: thông tin chung không đổi, nội dung sửa bằng phiên bản mới.
func (s *Service) UpdateSOPInfo(ctx context.Context, ref uuid.UUID, in SOPInfoInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	if err := cleanTemplateInfo(&in.Name, &in.Crop, &in.VarietyCode, &in.AnchorName); err != nil {
		return SOPView{}, err
	}
	if err := s.checkCatalogScope(ctx, in.Crop, in.VarietyCode); err != nil {
		return SOPView{}, err
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, ref, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Status != domain.TemplateDraft || t.PublishedVersion != nil {
			return ErrSOPState.WithDetail("SOP đã phát hành — thông tin chung không đổi; nội dung sửa bằng phiên bản mới")
		}
		key := domain.NameKey(in.Name)
		taken, err := store.TemplateNameTaken(ctx, tx, key, t.Ref)
		if err != nil {
			return err
		}
		if taken {
			return ErrSOPNameTaken
		}
		t.Name, t.NameKey, t.Crop, t.VarietyCode, t.AnchorName = in.Name, key, in.Crop, in.VarietyCode, in.AnchorName
		err = store.UpdateTemplateInfo(ctx, tx, *t)
		if isUnique(err, "sop_template_name_live") {
			return ErrSOPNameTaken
		}
		if err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

// RetireSOP ngừng dùng mẫu (không xóa — HTX US-CAT-01 AC-2): vụ đã ghim vẫn giữ phiên bản.
func (s *Service) RetireSOP(ctx context.Context, ref uuid.UUID, reason string) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	if !domain.ValidReason(reason) {
		return SOPView{}, problem.Validation.WithField("reason", "bắt buộc, ít nhất 10 ký tự")
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, ref, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Status == domain.TemplateRetired {
			return ErrSOPState.WithDetail("mẫu đã ngừng dùng")
		}
		t.Status, t.StatusReason = domain.TemplateRetired, strings.TrimSpace(reason)
		if err := store.SetTemplateState(ctx, tx, *t); err != nil {
			return err
		}
		return s.loadView(ctx, tx, ref, &view)
	})
	return view, err
}

// DeleteSOP đánh dấu đã xóa SOP chưa phát hành lần nào (Q-46): đang Nháp, không có phiên bản đã phát hành, không có vụ
// tham chiếu; không xóa dòng (RB-1). SOP đã phát hành (đang áp dụng hay đã ngừng) bị từ chối — chỉ ngừng áp dụng được.
// Ràng buộc CHECK của cơ sở dữ liệu kiểm lại điều kiện. SOP nháp đã được nhân bản vẫn xóa được vì dòng nguồn còn nguyên.
func (s *Service) DeleteSOP(ctx context.Context, ref uuid.UUID) error {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return err
	}
	return s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		// Khóa phiên bản trước, mẫu sau — cùng thứ tự với PublishSOP.
		if _, err := store.LockVersionsOf(ctx, tx, ref); err != nil {
			return err
		}
		t, err := store.TemplateByRef(ctx, tx, ref, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		switch {
		case t.PublishedVersion != nil || t.Status == domain.TemplatePublished:
			return ErrSOPNotDeletable
		case t.Status == domain.TemplateRetired:
			return ErrSOPNotDeletable.WithDetail("SOP đã ngừng áp dụng — không xóa được")
		}
		seasons, err := store.TemplateHasSeasons(ctx, tx, ref)
		if err != nil {
			return err
		}
		if seasons {
			return ErrSOPNotDeletable.WithDetail("SOP đã có vụ tham chiếu — chỉ ngừng áp dụng được")
		}
		if err := store.MarkTemplateDeleted(ctx, tx, ref); err != nil {
			if errors.Is(ledger.Classify(err), ledger.ErrImmutable) || isCheckViolation(err) {
				return ErrSOPNotDeletable
			}
			return err
		}
		return nil
	})
}

// BindVariety gắn giống lúa với mẫu SOP mặc định (HTX US-SEASON-01 AC-1).
func (s *Service) BindVariety(ctx context.Context, variety string, template uuid.UUID, note string) (store.Binding, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return store.Binding{}, err
	}
	variety = strings.TrimSpace(variety)
	if _, err := s.catalogItem(ctx, mdport.CatalogRiceVariety, "variety_code", variety); err != nil {
		return store.Binding{}, err
	}
	note, err = domain.CleanText("note", note, 500)
	if err != nil {
		return store.Binding{}, fieldProblem(err)
	}
	b := store.Binding{ID: ids.New(), VarietyCode: variety, TemplateRef: template, Note: note, BoundBy: p.ActorRef, BoundAt: s.now()}
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, template, false)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Scope != domain.ScopeSystem || t.Status != domain.TemplatePublished {
			return ErrBindingTemplateBad
		}
		old, err := store.CurrentBinding(ctx, tx, variety, true)
		if err != nil {
			return err
		}
		if old != nil && old.TemplateRef == template {
			b = *old
			return nil
		}
		return store.ReplaceBinding(ctx, tx, p.TenantID, old, b)
	})
	return b, err
}

// Bindings liệt kê gắn giống ↔ SOP hiện hành (KHMV-FN-01 R: cả Thành viên HTX).
func (s *Service) Bindings(ctx context.Context) ([]store.Binding, error) {
	p, err := authz.Require(ctx, append(append([]string{htxroles.Member}, readRoles...), sopAdminRead...)...)
	if err != nil {
		return nil, err
	}
	var out []store.Binding
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListBindings(ctx, tx)
		return e
	})
	return out, err
}

// SOPListInput lọc danh sách mẫu.
type SOPListInput struct {
	Status string
	Admin  bool // Admin API: mọi mẫu, kể cả nháp
	After  uuid.UUID
	Limit  int
}

// ListSOPs liệt kê mẫu. Application API: chỉ mẫu đã ban hành, mẫu hệ thống +
// mẫu riêng của HTX người gọi; Admin API: mọi mẫu.
func (s *Service) ListSOPs(ctx context.Context, in SOPListInput) (Page[store.Template], error) {
	f := store.TemplateFilter{Status: in.Status, After: in.After, Limit: clampLimit(in.Limit) + 1}
	var tid uuid.UUID
	if in.Admin {
		p, err := authz.Require(ctx, sopAdminRead...)
		if err != nil {
			return Page[store.Template]{}, err
		}
		tid = p.TenantID
	} else {
		a, err := s.matrixAccess(ctx, fnSOP, htxroles.Read, legacyReadRoles...)
		if err != nil {
			return Page[store.Template]{}, err
		}
		tid = a.p.TenantID
		all, coop := a.sopVisibility()
		f.OnlyVisibleTo, f.CooperativeRef, f.Status = !all, coop, domain.TemplatePublished
	}
	var items []store.Template
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListTemplates(ctx, tx, f)
		return e
	})
	if err != nil {
		return Page[store.Template]{}, err
	}
	return pageOf(items, clampLimit(in.Limit), func(t store.Template) uuid.UUID { return t.Ref }), nil
}

func pageOf[T any](items []T, limit int, key func(T) uuid.UUID) Page[T] {
	var pg Page[T]
	if len(items) > limit {
		items = items[:limit]
		k := key(items[limit-1])
		pg.Next = &k
	}
	pg.Items = items
	if pg.Items == nil {
		pg.Items = []T{}
	}
	return pg
}

// GetSOP đọc mẫu kèm phiên bản. Application API chỉ thấy phiên bản đã phát hành
// của mẫu nhìn thấy được.
func (s *Service) GetSOP(ctx context.Context, ref uuid.UUID, admin bool) (SOPView, error) {
	var tid uuid.UUID
	visible := func(store.Template) bool { return true }
	if admin {
		p, err := authz.Require(ctx, sopAdminRead...)
		if err != nil {
			return SOPView{}, err
		}
		tid = p.TenantID
	} else {
		a, err := s.matrixAccess(ctx, fnSOP, htxroles.Read, legacyReadRoles...)
		if err != nil {
			return SOPView{}, err
		}
		tid = a.p.TenantID
		all, coop := a.sopVisibility()
		visible = func(t store.Template) bool {
			return t.PublishedVersion != nil && (all || t.Scope == domain.ScopeSystem || t.CooperativeRef == coop)
		}
	}
	var view SOPView
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error { return s.loadView(ctx, tx, ref, &view) })
	if err != nil {
		return SOPView{}, err
	}
	if !visible(view.Template) {
		return SOPView{}, ErrSOPNotFound
	}
	if !admin {
		pub := view.Versions[:0:0]
		for _, v := range view.Versions {
			if v.Status == domain.VersionPublished {
				pub = append(pub, v)
			}
		}
		view.Versions = pub
	}
	if admin {
		view.Names = s.publisherNames(ctx, view.Versions)
	}
	return view, nil
}

// publisherNames tra tên người phát hành ngoài transaction; không tra được thì để trống (giao diện hiện "—").
func (s *Service) publisherNames(ctx context.Context, vs []store.Version) map[uuid.UUID]string {
	if s.Names == nil {
		return nil
	}
	var refs []uuid.UUID
	for _, v := range vs {
		if v.PublishedBy != uuid.Nil && !slices.Contains(refs, v.PublishedBy) {
			refs = append(refs, v.PublishedBy)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	m, err := s.Names.DisplayNames(ctx, refs)
	if err != nil {
		return nil
	}
	return m
}

// GetSOPVersion đọc một phiên bản (Application API: chỉ bản đã phát hành).
func (s *Service) GetSOPVersion(ctx context.Context, id uuid.UUID, admin bool) (store.Version, store.Template, error) {
	var tid uuid.UUID
	all, coop := true, uuid.Nil
	if admin {
		p, err := authz.Require(ctx, sopAdminRead...)
		if err != nil {
			return store.Version{}, store.Template{}, err
		}
		tid = p.TenantID
	} else {
		a, err := s.matrixAccess(ctx, fnSOP, htxroles.Read, legacyReadRoles...)
		if err != nil {
			return store.Version{}, store.Template{}, err
		}
		tid = a.p.TenantID
		all, coop = a.sopVisibility()
	}
	var v *store.Version
	var t *store.Template
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		if v, e = store.VersionByID(ctx, tx, id, false); e != nil || v == nil {
			return e
		}
		t, e = store.TemplateByRef(ctx, tx, v.TemplateRef, false)
		return e
	})
	if err != nil {
		return store.Version{}, store.Template{}, err
	}
	if v == nil || t == nil || (!admin && (v.Status != domain.VersionPublished || (!all && t.Scope != domain.ScopeSystem && t.CooperativeRef != coop))) {
		return store.Version{}, store.Template{}, ErrSOPNotFound
	}
	return *v, *t, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
