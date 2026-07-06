package worker

import (
	"context"
	"log/slog"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// CaseProjection is the single writer for business_cases.current_step and work-item
// seeds driven by Zeebe task lifecycle (service jobs and native user tasks).
type CaseProjection struct {
	caseRepo *repository.CaseRepository
}

func NewCaseProjection(caseRepo *repository.CaseRepository) *CaseProjection {
	return &CaseProjection{caseRepo: caseRepo}
}

func (p *CaseProjection) AfterServiceTaskCompleted(ctx context.Context, processInstanceKey int64, stepID, candidateRole string) {
	if p == nil || p.caseRepo == nil || processInstanceKey == 0 || stepID == "" {
		return
	}
	if candidateRole != "" {
		if err := p.caseRepo.MarkCaseAtStep(ctx, processInstanceKey, stepID, candidateRole); err != nil {
			slog.Error("case projection: mark step after service task", "processInstanceKey", processInstanceKey, "stepId", stepID, "err", err)
		}
		return
	}
	if err := p.caseRepo.MarkCaseStepCompleted(ctx, processInstanceKey, stepID); err != nil {
		slog.Error("case projection: complete step after service task", "processInstanceKey", processInstanceKey, "stepId", stepID, "err", err)
	}
}

func (p *CaseProjection) UpsertUserTaskWorkItem(ctx context.Context, seed repository.WorkItemSeed) {
	if p == nil || p.caseRepo == nil || seed.CaseID == "" {
		return
	}
	if _, err := p.caseRepo.UpsertWorkItem(ctx, seed); err != nil {
		slog.Error("case projection: upsert user task work item", "caseId", seed.CaseID, "stepCode", seed.StepCode, "err", err)
		return
	}
	if seed.ProcessInstanceKey != nil && seed.StepCode != "" {
		if err := p.caseRepo.MarkCaseAtStep(ctx, *seed.ProcessInstanceKey, seed.StepCode, seed.CandidateRole); err != nil {
			slog.Error("case projection: mark case at user task", "caseId", seed.CaseID, "stepId", seed.StepCode, "err", err)
		}
	}
}

func (p *CaseProjection) FinishCase(ctx context.Context, processInstanceKey int64, status string) {
	if p == nil || p.caseRepo == nil || processInstanceKey == 0 {
		return
	}
	if err := p.caseRepo.FinishCase(ctx, processInstanceKey, status); err != nil {
		slog.Error("case projection: finish case", "processInstanceKey", processInstanceKey, "err", err)
	}
}
