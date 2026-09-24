package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestResultSignoffPreservesVersionsAndRequiresIndependentReviewer(t *testing.T) {
	db := newSignoffTestDB(t)
	seedSignoffAssay(t, db, "ASSAY-101", "validated", "high")
	svc := NewResultSignoffService(repository.NewResultSignoffRepository(db), repository.NewAssayRunRepository(db), nil)
	ctx := context.Background()

	created, err := svc.Create(ctx, signoffInput("SIGNOFF-TEST-01"), "operator", "signoff-create-1")
	if err != nil {
		t.Fatalf("create signoff: %v", err)
	}
	if created.Version != 1 || created.PreparedBy != "operator" || len(created.Revisions) != 1 {
		t.Fatalf("unexpected initial signoff: %#v", created)
	}

	updatedInput := updateSignoffInput(created)
	updatedInput.Evidence = "PCR run sheet and control chart revision 2"
	updated, err := svc.Update(ctx, created.ID, updatedInput, "operator", "signoff-update-2")
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if updated.Version != 2 || len(updated.Revisions) != 2 || updated.Revisions[0].Evidence == updated.Revisions[1].Evidence {
		t.Fatalf("draft versions were not preserved: %#v", updated.Revisions)
	}

	peerReview, err := svc.Transition(ctx, updated.ID, dto.SignoffTransitionRequest{
		Status: "peer_review", ExpectedVersion: updated.Version, Reason: "result evidence complete",
	}, "operator", model.RoleOperator, "signoff-submit-3")
	if err != nil {
		t.Fatalf("submit peer review: %v", err)
	}
	if peerReview.Status != "peer_review" || peerReview.Version != 3 || len(peerReview.Revisions) != 3 {
		t.Fatalf("unexpected peer review version: %#v", peerReview)
	}

	decision := dto.SignoffTransitionRequest{Status: "signed", ExpectedVersion: peerReview.Version,
		Reason: "independent laboratory review passed", ReviewBasis: "复核 ASSAY-101 验证报告与质控记录，结果一致可签发"}
	if _, err := svc.Transition(ctx, peerReview.ID, decision, "operator", model.RoleOperator, "signoff-operator-denied"); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("operator signing must require reviewer role, got %v", err)
	}
	if _, err := svc.Transition(ctx, peerReview.ID, decision, "operator", model.RoleReviewer, "signoff-same-user-denied"); !errors.Is(err, ErrSeparationOfDuty) {
		t.Fatalf("same preparer and reviewer must be rejected, got %v", err)
	}

	signed, err := svc.Transition(ctx, peerReview.ID, decision, "reviewer", model.RoleReviewer, "signoff-sign-4")
	if err != nil {
		t.Fatalf("sign result: %v", err)
	}
	if signed.Status != "signed" || signed.Version != 4 || signed.ReviewedBy != "reviewer" || len(signed.Revisions) != 4 {
		t.Fatalf("unexpected signed result: %#v", signed)
	}
	for index, revision := range signed.Revisions {
		if revision.Evidence == "" || revision.Actor == "" || revision.RequestID == "" {
			t.Fatalf("revision %d lost attribution or evidence: %#v", index, revision)
		}
	}
	if signed.Revisions[0].RequestID != "signoff-create-1" || signed.Revisions[1].RequestID != "signoff-update-2" ||
		signed.Revisions[2].RequestID != "signoff-submit-3" || signed.Revisions[3].RequestID != "signoff-sign-4" {
		t.Fatalf("request ID chain is incomplete: %#v", signed.Revisions)
	}

	lateUpdate := updateSignoffInput(signed)
	if _, err := svc.Update(ctx, signed.ID, lateUpdate, "operator", "signoff-late-update"); !errors.Is(err, ErrLocked) {
		t.Fatalf("signed result must be immutable, got %v", err)
	}

	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Where("entity_type = ? AND entity_id = ?", "ResultSignoff", signed.ID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if auditCount != 4 {
		t.Fatalf("expected 4 atomic audits, got %d", auditCount)
	}
}

func TestHighRiskSignoffRequiresBasisAndValidatedAssay(t *testing.T) {
	db := newSignoffTestDB(t)
	seedSignoffAssay(t, db, "ASSAY-101", "validated", "high")
	seedSignoffAssay(t, db, "ASSAY-202", "running", "high")
	seedSignoffAssay(t, db, "ASSAY-303", "validated", "low")
	svc := NewResultSignoffService(repository.NewResultSignoffRepository(db), repository.NewAssayRunRepository(db), nil)
	ctx := context.Background()

	submit := func(code, relatedCode, risk string) model.ResultSignoff {
		t.Helper()
		input := signoffInput(code)
		input.RelatedCode = relatedCode
		input.RiskLevel = risk
		created, err := svc.Create(ctx, input, "operator", "req-create-"+code)
		if err != nil {
			t.Fatalf("create %s: %v", code, err)
		}
		peerReview, err := svc.Transition(ctx, created.ID, dto.SignoffTransitionRequest{
			Status: "peer_review", ExpectedVersion: created.Version, Reason: "submit for review",
		}, "operator", model.RoleOperator, "req-submit-"+code)
		if err != nil {
			t.Fatalf("submit %s: %v", code, err)
		}
		return peerReview
	}
	sign := func(item model.ResultSignoff, basis string) (model.ResultSignoff, error) {
		return svc.Transition(ctx, item.ID, dto.SignoffTransitionRequest{
			Status: "signed", ExpectedVersion: item.Version, Reason: "final review decision", ReviewBasis: basis,
		}, "reviewer", model.RoleReviewer, "req-sign-"+item.Code)
	}
	assertBlocked := func(item model.ResultSignoff, basis, wantMissing string) model.ResultSignoff {
		t.Helper()
		_, err := sign(item, basis)
		if !errors.Is(err, ErrSignoffBlocked) {
			t.Fatalf("expected blocked signoff for %s, got %v", item.Code, err)
		}
		reloaded, getErr := svc.Get(ctx, item.ID)
		if getErr != nil {
			t.Fatalf("reload %s: %v", item.Code, getErr)
		}
		if reloaded.Status != "peer_review" || reloaded.Version != item.Version {
			t.Fatalf("blocked signoff must stay in peer_review without a new version: %#v", reloaded)
		}
		if reloaded.PendingBlockReason == "" || !strings.Contains(reloaded.PendingBlockReason, wantMissing) {
			t.Fatalf("blocked reason must explain the missing item %q, got %q", wantMissing, reloaded.PendingBlockReason)
		}
		return reloaded
	}

	t.Run("missing review basis", func(t *testing.T) {
		item := submit("SIGNOFF-HR-01", "ASSAY-101", "high")
		assertBlocked(item, "", "复核依据")
	})
	t.Run("missing assay run", func(t *testing.T) {
		item := submit("SIGNOFF-HR-02", "ASSAY-404", "high")
		assertBlocked(item, "复核报告与质控记录一致，同意签发", "找不到对应检测运行")
	})
	t.Run("assay not validated", func(t *testing.T) {
		item := submit("SIGNOFF-HR-03", "ASSAY-202", "high")
		assertBlocked(item, "复核报告与质控记录一致，同意签发", "未验证通过")
	})
	t.Run("assay risk below signoff", func(t *testing.T) {
		item := submit("SIGNOFF-HR-04", "ASSAY-303", "critical")
		assertBlocked(item, "复核报告与质控记录一致，同意签发", "风险等级不足")
	})
	t.Run("high risk signed with basis and assay snapshot", func(t *testing.T) {
		item := submit("SIGNOFF-HR-05", "ASSAY-101", "high")
		blocked := assertBlocked(item, "", "复核依据")
		signed, err := sign(blocked, "复核 ASSAY-101 验证报告与阴阳性质控，结果一致可签发")
		if err != nil {
			t.Fatalf("sign high risk with basis: %v", err)
		}
		if signed.ReviewBasis == "" || signed.PendingBlockReason != "" {
			t.Fatalf("signed record must keep basis and clear pending reason: %#v", signed)
		}
		if signed.AssayEvidence == nil || signed.AssayEvidence.AssayCode != "ASSAY-101" ||
			signed.AssayEvidence.AssayStatus != "validated" || signed.AssayEvidence.AssayMetricVal != 88.5 {
			t.Fatalf("assay evidence snapshot missing: %#v", signed.AssayEvidence)
		}
		latest := signed.Revisions[len(signed.Revisions)-1]
		if latest.ReviewBasis == "" || latest.AssayCode != "ASSAY-101" || latest.AssayStatus != "validated" ||
			latest.AssayMetricName == "" || latest.AssayEvidence == "" {
			t.Fatalf("signed revision must carry basis and assay snapshot: %#v", latest)
		}
	})
	t.Run("low risk signs without basis or assay", func(t *testing.T) {
		item := submit("SIGNOFF-LR-01", "", "low")
		signed, err := sign(item, "")
		if err != nil {
			t.Fatalf("low risk signoff must not require basis or assay: %v", err)
		}
		if signed.Status != "signed" || signed.AssayEvidence != nil {
			t.Fatalf("unexpected low risk result: %#v", signed)
		}
	})
	t.Run("rejection unchanged for high risk", func(t *testing.T) {
		item := submit("SIGNOFF-HR-06", "ASSAY-404", "critical")
		rejected, err := svc.Transition(ctx, item.ID, dto.SignoffTransitionRequest{
			Status: "rejected", ExpectedVersion: item.Version, Reason: "退回补正：证据不足",
		}, "reviewer", model.RoleReviewer, "req-reject-hr-06")
		if err != nil {
			t.Fatalf("high risk rejection must stay unchanged: %v", err)
		}
		if rejected.Status != "rejected" || rejected.ReviewedBy != "reviewer" {
			t.Fatalf("unexpected rejection: %#v", rejected)
		}
	})
}

func newSignoffTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}, &model.AssayRun{}, &model.ResultSignoff{},
		&model.ResultSignoffRevision{}, &model.SignoffAssayEvidence{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func seedSignoffAssay(t *testing.T, db *gorm.DB, code, status, risk string) {
	t.Helper()
	item := model.AssayRun{
		BaseModel: model.BaseModel{Code: code, Name: "assay " + code, Status: status, Version: 1},
		Facility:  "Veterinary Lab 2", Owner: "assay desk", Category: "PCR",
		RiskLevel: risk, MetricValue: 88.5, MetricUnit: "copies/mL",
		EffectiveAt: time.Now().UTC(), Evidence: "run sheet and control chart for " + code,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed assay %s: %v", code, err)
	}
}

func signoffInput(code string) dto.CreateResultSignoff {
	return dto.CreateResultSignoff{
		Code: code, Name: "PCR result signoff", Description: "controlled veterinary result",
		Facility: "Veterinary Lab 2", Owner: "Result desk", Category: "PCR", RiskLevel: "high",
		MetricValue: 99.8, MetricUnit: "percent", EffectiveAt: time.Now().UTC(),
		Evidence: "PCR run sheet and control chart revision 1", RelatedCode: "ASSAY-101",
	}
}

func updateSignoffInput(item model.ResultSignoff) dto.UpdateResultSignoff {
	return dto.UpdateResultSignoff{
		ExpectedVersion: item.Version, Name: item.Name, Description: item.Description,
		Facility: item.Facility, Owner: item.Owner, Category: item.Category, RiskLevel: item.RiskLevel,
		MetricValue: item.MetricValue, MetricUnit: item.MetricUnit, EffectiveAt: item.EffectiveAt,
		Evidence: item.Evidence, RelatedCode: item.RelatedCode,
	}
}
