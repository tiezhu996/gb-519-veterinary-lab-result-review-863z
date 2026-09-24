package service

import (
	"context"
	"errors"
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
	seedAssayRun(t, db, "ASSAY-101", "validated", "high")
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

	peerReview, err := svc.Transition(ctx, updated.ID, dto.TransitionRequest{
		Status: "peer_review", ExpectedVersion: updated.Version, Reason: "result evidence complete",
	}, "operator", model.RoleOperator, "signoff-submit-3")
	if err != nil {
		t.Fatalf("submit peer review: %v", err)
	}
	if peerReview.Status != "peer_review" || peerReview.Version != 3 || len(peerReview.Revisions) != 3 {
		t.Fatalf("unexpected peer review version: %#v", peerReview)
	}

	decision := dto.TransitionRequest{Status: "signed", ExpectedVersion: peerReview.Version, Reason: "independent laboratory review passed", ReviewBasis: "依据 ASSAY-101 验证通过的 PCR 运行复核"}
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
	if signed.ReviewBasis != decision.ReviewBasis || signed.RunCode != "ASSAY-101" || signed.RunStatus != "validated" ||
		signed.RunMetricValue != 99.8 || signed.RunMetricUnit != "percent" || signed.RunEvidence == "" {
		t.Fatalf("signed record lost the review basis snapshot: %#v", signed)
	}
	signingRevision := signed.Revisions[3]
	if signingRevision.ReviewBasis != decision.ReviewBasis || signingRevision.RunCode != "ASSAY-101" ||
		signingRevision.RunStatus != "validated" || signingRevision.RunMetricValue != 99.8 || signingRevision.RunEvidence == "" {
		t.Fatalf("signing revision lost the review basis snapshot: %#v", signingRevision)
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

func newSignoffTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}, &model.AssayRun{}, &model.ResultSignoff{}, &model.ResultSignoffRevision{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func seedAssayRun(t *testing.T, db *gorm.DB, code, status, riskLevel string) {
	t.Helper()
	run := model.AssayRun{
		BaseModel: model.BaseModel{Code: code, Name: "PCR assay run " + code, Status: status, Version: 1},
		Facility:  "Veterinary Lab 2", Owner: "Assay desk", Category: "PCR", RiskLevel: riskLevel,
		MetricValue: 99.8, MetricUnit: "percent", EffectiveAt: time.Now().UTC(),
		Evidence: "validated run sheet and control chart",
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("seed assay run %s: %v", code, err)
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

func TestHighRiskSignoffRequiresValidatedAssayRun(t *testing.T) {
	db := newSignoffTestDB(t)
	seedAssayRun(t, db, "ASSAY-PLANNED", "planned", "high")
	seedAssayRun(t, db, "ASSAY-LOWRISK", "validated", "medium")
	seedAssayRun(t, db, "ASSAY-OK", "validated", "critical")
	svc := NewResultSignoffService(repository.NewResultSignoffRepository(db), repository.NewAssayRunRepository(db), nil)
	ctx := context.Background()

	submitForReview := func(t *testing.T, code, relatedCode, riskLevel string) model.ResultSignoff {
		t.Helper()
		input := signoffInput(code)
		input.RelatedCode = relatedCode
		input.RiskLevel = riskLevel
		created, err := svc.Create(ctx, input, "operator", "gate-create-"+code)
		if err != nil {
			t.Fatalf("create %s: %v", code, err)
		}
		review, err := svc.Transition(ctx, created.ID, dto.TransitionRequest{
			Status: "peer_review", ExpectedVersion: created.Version, Reason: "submit for review",
		}, "operator", model.RoleOperator, "gate-submit-"+code)
		if err != nil {
			t.Fatalf("submit %s: %v", code, err)
		}
		return review
	}
	sign := func(item model.ResultSignoff, basis string) (model.ResultSignoff, error) {
		return svc.Transition(ctx, item.ID, dto.TransitionRequest{
			Status: "signed", ExpectedVersion: item.Version, Reason: "high risk review decision", ReviewBasis: basis,
		}, "reviewer", model.RoleReviewer, "gate-sign-"+item.Code)
	}
	assertStaysPending := func(t *testing.T, item model.ResultSignoff) {
		t.Helper()
		current, err := svc.Get(ctx, item.ID)
		if err != nil {
			t.Fatalf("reload %s: %v", item.Code, err)
		}
		if current.Status != "peer_review" || current.Version != item.Version || current.RunCode != "" {
			t.Fatalf("blocked signoff must stay in peer_review without a run snapshot: %#v", current)
		}
	}

	t.Run("missing review basis", func(t *testing.T) {
		item := submitForReview(t, "GATE-BASIS", "ASSAY-OK", "high")
		if _, err := sign(item, ""); !errors.Is(err, ErrReviewBasisNeeded) {
			t.Fatalf("expected ErrReviewBasisNeeded, got %v", err)
		}
		assertStaysPending(t, item)
	})

	t.Run("missing assay run", func(t *testing.T) {
		item := submitForReview(t, "GATE-MISSING", "ASSAY-404", "high")
		if _, err := sign(item, "复核依据已填写"); !errors.Is(err, ErrAssayRunMissing) {
			t.Fatalf("expected ErrAssayRunMissing, got %v", err)
		}
		assertStaysPending(t, item)
	})

	t.Run("assay run not validated", func(t *testing.T) {
		item := submitForReview(t, "GATE-PLANNED", "ASSAY-PLANNED", "high")
		if _, err := sign(item, "复核依据已填写"); !errors.Is(err, ErrAssayRunInvalid) {
			t.Fatalf("expected ErrAssayRunInvalid, got %v", err)
		}
		assertStaysPending(t, item)
	})

	t.Run("assay run risk lower than signoff", func(t *testing.T) {
		item := submitForReview(t, "GATE-RISK", "ASSAY-LOWRISK", "critical")
		if _, err := sign(item, "复核依据已填写"); !errors.Is(err, ErrAssayRunRiskLow) {
			t.Fatalf("expected ErrAssayRunRiskLow, got %v", err)
		}
		assertStaysPending(t, item)
	})

	t.Run("validated run satisfies the gate", func(t *testing.T) {
		item := submitForReview(t, "GATE-OK", "ASSAY-OK", "high")
		signed, err := sign(item, "依据 ASSAY-OK 运行结果复核")
		if err != nil {
			t.Fatalf("sign with validated run: %v", err)
		}
		if signed.Status != "signed" || signed.RunCode != "ASSAY-OK" || signed.RunStatus != "validated" ||
			signed.RunMetricUnit != "percent" || signed.RunEvidence == "" || signed.ReviewBasis == "" {
			t.Fatalf("signed record missing run snapshot: %#v", signed)
		}
		latest := signed.Revisions[len(signed.Revisions)-1]
		if latest.RunCode != "ASSAY-OK" || latest.ReviewBasis == "" || latest.RunEvidence == "" {
			t.Fatalf("history revision missing review basis: %#v", latest)
		}
	})

	t.Run("low risk signoff stays ungated", func(t *testing.T) {
		item := submitForReview(t, "GATE-LOW", "ASSAY-404", "low")
		signed, err := sign(item, "")
		if err != nil {
			t.Fatalf("low risk signoff must not require the assay gate: %v", err)
		}
		if signed.Status != "signed" || signed.RunCode != "" {
			t.Fatalf("low risk signoff must sign without a run snapshot: %#v", signed)
		}
	})
}
