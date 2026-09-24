package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/constants"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
)

type ResultSignoffService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.ResultSignoff], error)
	Get(context.Context, uint) (model.ResultSignoff, error)
	Create(context.Context, dto.CreateResultSignoff, string, string) (model.ResultSignoff, error)
	Update(context.Context, uint, dto.UpdateResultSignoff, string, string) (model.ResultSignoff, error)
	Transition(context.Context, uint, dto.SignoffTransitionRequest, string, string, string) (model.ResultSignoff, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type resultSignoffService struct {
	repository repository.ResultSignoffRepository
	assays     repository.AssayRunRepository
	security   SecurityService
}

func NewResultSignoffService(repo repository.ResultSignoffRepository, assays repository.AssayRunRepository, security SecurityService) ResultSignoffService {
	return &resultSignoffService{repository: repo, assays: assays, security: security}
}

func (s *resultSignoffService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.ResultSignoff], error) {
	return s.repository.List(ctx, query)
}

func (s *resultSignoffService) Get(ctx context.Context, id uint) (model.ResultSignoff, error) {
	return s.repository.Get(ctx, id)
}

func (s *resultSignoffService) Create(ctx context.Context, input dto.CreateResultSignoff, actor, requestID string) (model.ResultSignoff, error) {
	if err := validateResultSignoffBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ResultSignoff{}, err
	}
	item := model.ResultSignoff{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.ResultSignoffInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)), PreparedBy: actor,
	}
	if err := s.repository.CreateVersion(ctx, &item, actor, requestID); err != nil {
		return model.ResultSignoff{}, fmt.Errorf("create 结果签发: %w", err)
	}
	return s.repository.Get(ctx, item.ID)
}

func (s *resultSignoffService) Update(ctx context.Context, id uint, input dto.UpdateResultSignoff, actor, requestID string) (model.ResultSignoff, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ResultSignoff{}, err
	}
	if current.Status != model.ResultSignoffInitialStatus {
		return model.ResultSignoff{}, ErrLocked
	}
	if actor != current.PreparedBy {
		return model.ResultSignoff{}, ErrPreparationOwner
	}
	if err := validateResultSignoffBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ResultSignoff{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateVersion(ctx, id, input.ExpectedVersion, &current, repository.RevisionInput{
		Actor: actor, RequestID: requestID, Action: "update", Before: current.Status,
		Reason: "draft signoff fields updated",
	}); err != nil {
		return model.ResultSignoff{}, fmt.Errorf("update 结果签发: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *resultSignoffService) Transition(ctx context.Context, id uint, input dto.SignoffTransitionRequest, actor, role, requestID string) (model.ResultSignoff, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ResultSignoff{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.ResultSignoffTransitions, current.Status, target) {
		return model.ResultSignoff{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	if !isSignoffOperatorRole(role) {
		return model.ResultSignoff{}, ErrForbidden
	}
	var assaySnapshot *model.SignoffAssayEvidence
	if target == "peer_review" {
		if actor != current.PreparedBy {
			return model.ResultSignoff{}, ErrPreparationOwner
		}
		current.ReviewedBy = ""
		current.ReviewReason = ""
		current.ReviewBasis = ""
		current.PendingBlockReason = ""
	}
	if target == "signed" || target == "rejected" {
		if !isSignoffReviewerRole(role) {
			return model.ResultSignoff{}, ErrReviewRequired
		}
		if actor == current.PreparedBy {
			return model.ResultSignoff{}, ErrSeparationOfDuty
		}
		current.ReviewedBy = actor
		current.ReviewReason = strings.TrimSpace(input.Reason)
		current.PendingBlockReason = ""
		// 高风险签发必须带书面复核依据并绑定已验证通过的检测运行；
		// 低风险签发与退回补正照旧，不增加前置条件。
		if target == "signed" && constants.IsHighRiskSignoff(current.RiskLevel) {
			basis := strings.TrimSpace(input.ReviewBasis)
			if basis == "" {
				return s.blockSignoff(ctx, id, actor, requestID, "缺少复核依据：复核员必须在签发表单填写书面复核依据")
			}
			if len([]rune(basis)) < 5 {
				return s.blockSignoff(ctx, id, actor, requestID, "复核依据不充分：请写明判定依据（至少 5 个字）")
			}
			relatedCode := strings.ToUpper(strings.TrimSpace(current.RelatedCode))
			if relatedCode == "" {
				return s.blockSignoff(ctx, id, actor, requestID, "缺少关联编码：签发单未填写对应检测运行编码")
			}
			assay, lookupErr := s.assays.GetByCode(ctx, relatedCode)
			if lookupErr != nil {
				return s.blockSignoff(ctx, id, actor, requestID,
					fmt.Sprintf("缺少检测运行：关联编码 %s 找不到对应检测运行", relatedCode))
			}
			if assay.Status != "validated" {
				return s.blockSignoff(ctx, id, actor, requestID,
					fmt.Sprintf("检测运行未验证通过：%s 当前状态为 %s，需为 validated", assay.Code, assay.Status))
			}
			if !constants.RiskAtLeast(assay.RiskLevel, current.RiskLevel) {
				return s.blockSignoff(ctx, id, actor, requestID,
					fmt.Sprintf("风险等级不足：检测运行 %s 风险为 %s，低于签发单风险 %s", assay.Code, assay.RiskLevel, current.RiskLevel))
			}
			current.ReviewBasis = basis
			assaySnapshot = &model.SignoffAssayEvidence{
				AssayCode: assay.Code, AssayStatus: assay.Status, AssayMetricName: assay.Category,
				AssayMetricVal: assay.MetricValue, AssayMetricUnit: assay.MetricUnit,
				AssayEvidence: assay.Evidence, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}
		}
		if target == "rejected" {
			current.ReviewBasis = ""
		}
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateVersion(ctx, id, input.ExpectedVersion, &current, repository.RevisionInput{
		Actor: actor, RequestID: requestID, Action: "transition", Before: before,
		Reason: strings.TrimSpace(input.Reason), ReviewBasis: current.ReviewBasis, Assay: assaySnapshot,
	}); err != nil {
		return model.ResultSignoff{}, fmt.Errorf("transition 结果签发: %w", err)
	}
	return s.repository.Get(ctx, id)
}

// blockSignoff 挡下高风险签发：记录留在 peer_review，记录缺失项并返回可展示的业务错误。
func (s *resultSignoffService) blockSignoff(ctx context.Context, id uint, actor, requestID, reason string) (model.ResultSignoff, error) {
	if err := s.repository.RecordBlockedSignoff(ctx, id, actor, requestID, reason); err != nil {
		return model.ResultSignoff{}, fmt.Errorf("record blocked signoff: %w", err)
	}
	return model.ResultSignoff{}, fmt.Errorf("%w: %s", ErrSignoffBlocked, reason)
}

func (s *resultSignoffService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Status != model.ResultSignoffInitialStatus {
		return ErrLocked
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "ResultSignoff", id, current.Status, "deleted", "soft deleted 结果签发")
}

func isSignoffOperatorRole(role string) bool {
	return role == model.RoleOperator || role == model.RoleReviewer || role == model.RoleAdmin
}

func isSignoffReviewerRole(role string) bool {
	return role == model.RoleReviewer || role == model.RoleAdmin
}

func (s *resultSignoffService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validateResultSignoffBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}
